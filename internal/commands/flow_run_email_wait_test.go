package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"echopoint-cli/internal/api"
)

const emailWaitFlowJSON = `{
  "name": "email wait",
  "version": "1.0",
  "nodes": [{
    "id": "signup",
    "display_name": "Sign up",
    "type": "request",
    "data": {"method": "POST", "url": "{{signupUrl}}", "headers": {"Content-Type": "application/json"},
             "body": "{\"email\":\"{{inbox.address}}\"}"},
    "assertions": [{"extractor_type": "status_code", "extractor_data": {}, "operator_type": "equals",
                    "operator_data": {"value": "200"}}]
  }, {
    "id": "verify",
    "display_name": "Verification email",
    "type": "email_wait",
    "data": {"timeout_ms": 2000, "link_pattern": "/verify"},
    "assertions": [{
      "extractor_type": "json_path",
      "extractor_data": {"path": "$.subject"},
      "operator_type": "contains",
      "operator_data": {"value": "Verify"}
    }],
    "outputs": [{"name": "code", "secret": true, "extractor": {"type": "json_path", "path": "$.code"}}]
  }, {
    "id": "confirm",
    "display_name": "Follow the link",
    "type": "request",
    "data": {"method": "GET", "url": "{{verify.first_link}}"},
    "assertions": [{"extractor_type": "status_code", "extractor_data": {}, "operator_type": "equals",
                    "operator_data": {"value": "200"}}]
  }],
  "edges": [
    {"id": "e1", "source": "signup", "target": "verify", "type": "success"},
    {"id": "e2", "source": "verify", "target": "confirm", "type": "success"}
  ]
}`

// The local run declares the email_wait capability on its claim, reads the run
// inbox with the job token, and follows the link of the matched email.
func TestIntegration_EphemeralEmailWaitReadsTheRunInbox(t *testing.T) {
	var srv *httptest.Server
	var inboxReads, confirmed atomic.Int32
	var declared atomic.Bool
	const address = "aanuyjabcdefghijklmsjd6s2g@in.echopoint.dev"
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/launch") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(launchResponse(false))
		case strings.Contains(r.URL.Path, "/claim"):
			var body struct {
				Capabilities []string `json:"capabilities"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			declared.Store(slices.Contains(body.Capabilities, "email_wait"))
			claim := fakeClaimResponse()
			var definition api.FlowDefinition
			if err := json.Unmarshal([]byte(emailWaitFlowJSON), &definition); err != nil {
				t.Errorf("decode flow: %v", err)
			}
			claim.Job.FlowDefinition = definition
			claim.Job.Inputs = api.RunnerInputs{
				"signupUrl":          srv.URL + "/signup",
				"inbox.address":      address,
				"inbox.local":        strings.Split(address, "@")[0],
				"inbox.domain":       "in.echopoint.dev",
				"inbox.messages_url": srv.URL + "/runner/jobs/" + claim.Job.JobId.String() + "/inbox-messages",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(claim)
		case r.URL.Path == "/signup":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"sent"}`))
		case r.URL.Path == "/verify":
			confirmed.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"verified":true}`))
		case strings.HasSuffix(r.URL.Path, "/inbox-messages"):
			if r.Header.Get("X-Job-Token") != fakeClaimResponse().JobToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			inboxReads.Add(1)
			html := `<p>Your code is <b>482913</b></p><a href="` + srv.URL + `/verify?t=abc&amp;u=1">Verify</a>`
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"id": "msg-1", "envelope_from": "bounces@acme.example", "envelope_to": address,
					"from": "Acme <no-reply@acme.example>", "to": address, "subject": "Verify your email",
					"message_id": "<1@acme.example>", "text": nil, "html": html, "attachments": []any{},
					"raw_size": 512, "received_at": time.Now().UTC().Format(time.RFC3339),
				}},
				"count": 1,
				"total": 1,
			})
		default:
			if !serveFakeJobRequest(w, r) {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)
	state.OrganizationID = "org_test"
	results, exitCode := executeFlows(
		context.Background(), state, []string{flowUUID().String()}, "", "", "", 1, "",
	)

	if exitCode != exitSuccess || len(results) != 1 || results[0].ExitCode != exitSuccess {
		t.Fatalf("the email wait must match and its link must be followed: code=%d results=%+v", exitCode, results)
	}
	if !declared.Load() {
		t.Error("the claim did not declare the email_wait capability")
	}
	if inboxReads.Load() == 0 {
		t.Fatal("the wait never read the run inbox with the job token")
	}
	if confirmed.Load() == 0 {
		t.Fatal("the confirm node never requested {{verify.first_link}}")
	}
}
