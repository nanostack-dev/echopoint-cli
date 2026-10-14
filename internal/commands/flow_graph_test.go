package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"echopoint-cli/internal/api"
)

func mixedNodeFlow(t *testing.T) api.Flow {
	t.Helper()
	flow := sampleFlow(idFor("1"), "Mixed nodes", nil)
	for _, kind := range []string{
		"assert", "branch", "delay", "loop", "module", "poll", "request", "set_variable", "sse", "webhook_wait",
	} {
		var node api.FlowNode
		if err := json.Unmarshal(
			fmt.Appendf(nil, `{"id":%q,"type":%q,"display_name":%q,"data":{}}`, kind, kind, kind),
			&node,
		); err != nil {
			t.Fatal(err)
		}
		flow.FlowDefinition.Nodes = append(flow.FlowDefinition.Nodes, node)
	}
	return flow
}

func TestNodeRemovalPreservesEveryOtherSchemaKind(t *testing.T) {
	flow := mixedNodeFlow(t)
	for _, removed := range flow.FlowDefinition.Nodes {
		id, err := flowNodeID(removed)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(id, func(t *testing.T) {
			stub := newAPIStub(t)
			path := "/flows/" + flow.Id.String()
			stub.on(http.MethodGet, path, http.StatusOK, flow)
			stub.on(http.MethodPut, path, http.StatusOK, flow)
			_, _, err := execute(t, newFlowCmd(stub.state(t)), "", "node", "remove", flow.Id.String(), id)
			if err != nil {
				t.Fatal(err)
			}
			requests := stub.requestsTo(http.MethodPut, path)
			if len(requests) != 1 {
				t.Fatalf("got %d updates", len(requests))
			}
			var update api.UpdateFlowRequest
			if err := json.Unmarshal(requests[0].body, &update); err != nil {
				t.Fatal(err)
			}
			if update.AutoLayout == nil || !*update.AutoLayout || len(update.FlowDefinition.Nodes) != 9 {
				t.Fatalf("update lost nodes or auto-layout: %+v", update)
			}
			for _, original := range flow.FlowDefinition.Nodes {
				otherID, err := flowNodeID(original)
				if err != nil {
					t.Fatal(err)
				}
				exists, err := nodeIDExists(update.FlowDefinition.Nodes, otherID)
				if err != nil || exists != (otherID != id) {
					t.Fatalf("node %s: present=%v err=%v", otherID, exists, err)
				}
			}
		})
	}
}

func TestEdgeEndpointsRecognizeEverySchemaKind(t *testing.T) {
	flow := mixedNodeFlow(t)
	for _, node := range flow.FlowDefinition.Nodes {
		id, err := flowNodeID(node)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(id, func(t *testing.T) {
			stub := newAPIStub(t)
			path := "/flows/" + flow.Id.String()
			stub.on(http.MethodGet, path, http.StatusOK, flow)
			stub.on(http.MethodPut, path, http.StatusOK, flow)
			target := "request"
			if id == target {
				target = "delay"
			}
			_, _, err := execute(
				t,
				newFlowCmd(stub.state(t)),
				"",
				"edge",
				"add",
				flow.Id.String(),
				"--from",
				id,
				"--to",
				target,
			)
			if err != nil || len(stub.requestsTo(http.MethodPut, path)) != 1 {
				t.Fatalf("edge endpoint %s: err=%v", id, err)
			}
		})
	}
}

func TestFlowEditingRequestsUseCommandContext(t *testing.T) {
	for _, args := range [][]string{
		{"node", "remove", flowUUID().String(), "request"},
		{"edge", "add", flowUUID().String(), "--from", "request", "--to", "delay"},
		{"env", "view", flowUUID().String()},
		{"env", "set", flowUUID().String(), "--var", "KEY=value"},
		{"env", "unset", flowUUID().String(), "KEY"},
	} {
		t.Run(strings.Join(args[:2], "/"), func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := newFlowCmd(makeState(t, "test-key", "", server.URL))
			cmd.SetContext(ctx)
			cmd.SetArgs(args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			finished := make(chan error, 1)
			go func() { finished <- cmd.Execute() }()
			select {
			case <-started:
				cancel()
			case <-ctx.Done():
				t.Fatal("command never reached the API")
			}
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v, want cancellation", err)
				}
			case <-time.After(time.Second):
				t.Fatal("command did not stop after cancellation")
			}
		})
	}
}

func TestUpdatingAnUnsupportedKindReportsItsEditingPolicy(t *testing.T) {
	stub := newAPIStub(t)
	flow := mixedNodeFlow(t)
	path := "/flows/" + flow.Id.String()
	stub.on(http.MethodGet, path, http.StatusOK, flow)
	_, _, err := execute(t, newFlowCmd(stub.state(t)), "", "node", "update", flow.Id.String(), "loop", "--name", "Loop")
	if err == nil || !strings.Contains(err.Error(), "does not support editing") ||
		strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
	if len(stub.requestsTo(http.MethodPut, path)) != 0 {
		t.Fatal("unsupported editing sent an update")
	}
}
