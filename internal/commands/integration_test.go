package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"echopoint-cli/internal/api"
)

func TestIntegrationConnectSendsTheKeyFromStandardInput(t *testing.T) {
	stub := newAPIStub(
		t,
	).on(http.MethodPut, "/integrations/stripe/connections/dev", http.StatusOK, api.IntegrationConnection{
		Id: uuid.New(), Integration: "stripe", EnvironmentKey: "dev", KeyHint: "rk_test_…a1B2", Signed: true,
		Checks: []api.IntegrationCheck{}, CheckedAt: time.Now(), CreatedAt: time.Now(),
	})
	state := makeState(t, "", "session-token", stub.server.URL)
	cmd := newIntegrationConnectCmd(state)
	cmd.SetArgs([]string{"stripe", "--env", "dev"})
	cmd.SetIn(strings.NewReader("rk_test_secret_from_stdin\n"))
	cmd.SetOut(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	sent := stub.requestsTo(http.MethodPut, "/integrations/stripe/connections/dev")
	if len(sent) != 1 {
		t.Fatalf("connect requests = %d, want 1", len(sent))
	}
	var body api.IntegrationConnectRequest
	if err := json.Unmarshal(sent[0].body, &body); err != nil || body.SecretKey == nil {
		t.Fatalf("body = %s, want a secret_key", sent[0].body)
	}
	if *body.SecretKey != "rk_test_secret_from_stdin" {
		t.Errorf("secret_key = %q, want the key read from stdin", *body.SecretKey)
	}
}

func TestIntegrationConnectRefusesAnEmptyKeyBeforeAnyRequest(t *testing.T) {
	stub := newAPIStub(t)
	state := makeState(t, "", "session-token", stub.server.URL)
	cmd := newIntegrationConnectCmd(state)
	cmd.SetArgs([]string{"stripe", "--env", "dev"})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "standard input") {
		t.Fatalf("err = %v, want it to ask for the key on standard input", err)
	}
	if len(stub.requestsTo(http.MethodPut, "/integrations/stripe/connections/dev")) != 0 {
		t.Error("a request was sent without a key")
	}
}

func TestIntegrationRefuseSendsEachKindAndKey(t *testing.T) {
	stub := newAPIStub(t).on(http.MethodPut, "/integrations/stripe/choices", http.StatusNoContent, nil)
	state := makeState(t, "", "session-token", stub.server.URL)
	cmd := newIntegrationChoiceCmd(state, "refuse", api.Refused)
	cmd.SetArgs([]string{"stripe", "operation:quote.list", "event:quote.created"})
	cmd.SetOut(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("refuse: %v", err)
	}
	var body api.IntegrationChoicesRequest
	if err := json.Unmarshal(
		stub.requestsTo(http.MethodPut, "/integrations/stripe/choices")[0].body,
		&body,
	); err != nil {
		t.Fatal(err)
	}
	want := []api.IntegrationChoiceInput{
		{Kind: api.Operation, Key: "quote.list", Choice: api.Refused},
		{Kind: api.Event, Key: "quote.created", Choice: api.Refused},
	}
	if len(body.Items) != 2 || body.Items[0] != want[0] || body.Items[1] != want[1] {
		t.Errorf("items = %+v, want %+v", body.Items, want)
	}
}

func TestIntegrationItemsRefusesAnUnknownKind(t *testing.T) {
	if _, err := itemFilter("webhook", nil, ""); err == nil {
		t.Fatal("want an error for --kind webhook")
	}
	filter, err := itemFilter("event", []string{"billing"}, "refused")
	if err != nil || *filter.Kind != api.Event || *filter.Choice != api.Refused || filter.Areas[0] != "billing" {
		t.Fatalf("filter = %+v, err = %v", filter, err)
	}
}
