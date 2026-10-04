package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"echopoint-cli/internal/output"
)

func TestStatusPageCommandsPreserveScopeAndVersions(t *testing.T) {
	fixture, err := os.ReadFile("testdata/status-page.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, method, path, body string
		args                     []string
	}{
		{"read", "GET", "/status-pages/current", "", []string{"get"}},
		{"save", "PUT", "/status-pages/current", string(fixture), []string{"save", "-"}},
		{
			"publish",
			"POST",
			"/status-pages/current/publish",
			`{"expected_draft_version":3,"expected_intent_version":2}`,
			[]string{"publish", "--expected-draft-version", "3", "--expected-intent-version", "2"},
		},
		{
			"unpublish",
			"POST",
			"/status-pages/current/unpublish",
			`{"expected_intent_version":4}`,
			[]string{"unpublish", "--expected-intent-version", "4"},
		},
		{
			"binding",
			"GET",
			"/status-pages/binding-options",
			"",
			[]string{
				"binding-options",
				"--schedule-id",
				"550e8400-e29b-41d4-a716-446655440001",
				"--flow-id",
				"550e8400-e29b-41d4-a716-446655440002",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var received []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("X-Api-Key") != "test-key" || r.Header.Get("X-Organization-Id") != "org_test" {
					t.Error("missing tenant authentication")
				}
				if tc.name == "binding" &&
					(r.URL.Query().Get("schedule_id") != tc.args[2] || r.URL.Query().Get("flow_id") != tc.args[4]) {
					t.Error("missing binding query")
				}
				received, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"slug":"example","draft_version":3,"intent_version":2,"published":false}`)
			}))
			defer server.Close()
			state := makeState(t, "test-key", "", server.URL)
			state.OutputFormat = output.FormatJSON
			cmd := newStatusPagesCmd(state)
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetIn(bytes.NewReader(fixture))
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if tc.body != "" {
				var want, got any
				if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(received, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(want, got) {
					t.Errorf("request body changed: %s", received)
				}
			}
			if !json.Valid(stdout.Bytes()) {
				t.Errorf("not structured JSON: %s", stdout.String())
			}
		})
	}
}

func TestStatusPagePublicNeverSendsStoredCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range []string{"Authorization", "X-Api-Key", "X-Organization-Id"} {
			if r.Header.Get(name) != "" {
				t.Errorf("public request leaked %s", name)
			}
		}
		if r.URL.Path != "/public/status-pages/example" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"brand_name":"Example","health":{"state":"unknown"}}`)
	}))
	defer server.Close()
	for _, key := range []string{"", "test-key"} {
		state := makeState(t, key, "test-token", server.URL)
		state.OutputFormat = output.FormatYAML
		cmd := newStatusPagesCmd(state)
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"public", "example"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "brand_name: Example") {
			t.Errorf("YAML = %s", stdout.String())
		}
	}
}

func TestStatusPageValidationRejectsIncompleteOrUnknownInput(t *testing.T) {
	fixture, err := os.ReadFile("testdata/status-page.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{}`, `{"unexpected":true}`, strings.Replace(string(fixture), `"orbit"`, `"invalid-theme"`, 1), string(fixture) + `{}`} {
		cmd := newStatusPagesCmd(&AppState{})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetIn(strings.NewReader(input))
		cmd.SetArgs([]string{"validate", "-"})
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted invalid request: %s", input)
		}
	}
	cmd := newStatusPagesCmd(&AppState{})
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"validate", "testdata/status-page.json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestStatusPageWritesRequireExplicitVersions(t *testing.T) {
	state := makeState(t, "test-key", "", "http://127.0.0.1:1")
	for _, args := range [][]string{{"publish"}, {"unpublish"}, {"publish", "--expected-draft-version", "1"}, {"publish", "--expected-draft-version", "-1", "--expected-intent-version", "0"}, {"unpublish", "--expected-intent-version", "-1"}} {
		cmd := newStatusPagesCmd(state)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("flags not rejected: %v: %v", args, err)
		}
	}
}

func TestStatusPageConflictRemainsAnError(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		io.WriteString(
			w,
			`{"errors":[{"code":"STATUS_PAGE_CONFLICT","message":"Reload the draft before publishing."}]}`,
		)
	}))
	defer server.Close()
	cmd := newStatusPagesCmd(makeState(t, "test-key", "", server.URL))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"publish", "--expected-draft-version", "1", "--expected-intent-version", "0"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "Reload the draft") || requests != 1 {
		t.Fatalf("conflict was not preserved: requests=%d, error=%v", requests, err)
	}
}
