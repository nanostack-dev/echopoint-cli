package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"echopoint-cli/internal/api"
)

func TestOrgRenameSendsTheTrimmedNameToTheOrganization(t *testing.T) {
	var (
		mu     sync.Mutex
		body   api.OrganizationWriteRequest
		header string
		hit    bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/organization" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		hit = true
		header = r.Header.Get("X-Organization-ID")
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "org_123", "name": "Acme Payments"})
	}))
	defer srv.Close()

	state := makeState(t, "test-key", "", srv.URL)
	cmd := newOrgRenameCmd(state)
	cmd.SetArgs([]string{"  Acme Payments  "})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if !hit {
		t.Fatal("expected PATCH /organization")
	}
	if body.Name != "Acme Payments" {
		t.Errorf("name = %q, want %q", body.Name, "Acme Payments")
	}
	if header != "org_test" {
		t.Errorf("X-Organization-ID = %q, want org_test", header)
	}
}

func TestOrgRenameRefusesAnEmptyNameWithoutACall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no request expected")
	}))
	defer srv.Close()

	cmd := newOrgRenameCmd(makeState(t, "test-key", "", srv.URL))
	cmd.SetArgs([]string{"   "})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an empty name")
	}
}

func TestOrgRenameReportsAMissingPermission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(apiError("FORBIDDEN", "missing permission organization:update"))
	}))
	defer srv.Close()

	cmd := newOrgRenameCmd(makeState(t, "test-key", "", srv.URL))
	cmd.SetArgs([]string{"Acme"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected the 403 to fail the command")
	}
}
