package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type recordedAPIRequest struct {
	method string
	path   string
	query  string
	header http.Header
	body   string
}

type apiFakeServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedAPIRequest
}

// newAPIFakeServer answers every request with status and body, and records it.
func newAPIFakeServer(t *testing.T, status int, body string) *apiFakeServer {
	t.Helper()
	fake := &apiFakeServer{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedAPIRequest{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: string(data),
		})
		fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-1")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(fake.server.Close)
	isolateCLIEnvironment(t, fake.server.URL)
	return fake
}

func (f *apiFakeServer) only(t *testing.T) recordedAPIRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(f.requests))
	}
	return f.requests[0]
}

func TestAPIGetsAPathWithTheProfileCredentials(t *testing.T) {
	fake := newAPIFakeServer(t, http.StatusOK, `{"invitation_emails_last_24_hours":{"limit":20}}`)

	stdout, stderr, code := runCLI(t, "api", "/license/usage")

	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	req := fake.only(t)
	if req.method != http.MethodGet || req.path != "/license/usage" {
		t.Errorf("request %s %s", req.method, req.path)
	}
	if req.header.Get("X-Api-Key") != "golden-key" || req.header.Get("X-Organization-Id") != "org_test" {
		t.Errorf("auth headers %v", req.header)
	}
	want := "{\n  \"invitation_emails_last_24_hours\": {\n    \"limit\": 20\n  }\n}\n"
	if stdout != want {
		t.Errorf("stdout %q, want %q", stdout, want)
	}
}

func TestAPIAddsAMissingLeadingSlash(t *testing.T) {
	fake := newAPIFakeServer(t, http.StatusOK, `{}`)

	if _, stderr, code := runCLI(t, "api", "me"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if req := fake.only(t); req.path != "/me" {
		t.Errorf("path %q", req.path)
	}
}

func TestAPIFieldsOfAGetGoToTheQuery(t *testing.T) {
	fake := newAPIFakeServer(t, http.StatusOK, `{}`)

	if _, stderr, code := runCLI(t, "api", "/flows", "-X", "GET", "-F", "limit=5", "--raw-field", "q=a b"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	req := fake.only(t)
	if req.query != "limit=5&q=a+b" || req.body != "" {
		t.Errorf("query %q body %q", req.query, req.body)
	}
}

func TestAPIFieldsMakeATypedJSONBodyAndAPost(t *testing.T) {
	fake := newAPIFakeServer(t, http.StatusOK, `{}`)

	_, stderr, code := runCLI(t, "api", "/flows/search",
		"-F", "pagination[limit]=5", "-F", "include_definition=true", "-F", "folder_id=null",
		"--raw-field", "full_text_search=42")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	req := fake.only(t)
	if req.method != http.MethodPost || req.header.Get("Content-Type") != "application/json" {
		t.Errorf("request %s, content type %q", req.method, req.header.Get("Content-Type"))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(req.body), &got); err != nil {
		t.Fatalf("body %q: %v", req.body, err)
	}
	want := map[string]any{
		"pagination":         map[string]any{"limit": float64(5)},
		"include_definition": true,
		"folder_id":          nil,
		"full_text_search":   "42",
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("body %s, want %s", gotJSON, wantJSON)
	}
}

func TestAPISendsAFileAsTheBodyWithTheMethodAndHeaders(t *testing.T) {
	fake := newAPIFakeServer(t, http.StatusOK, `{}`)
	file := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(file, []byte(`{"name":"Acme"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, "api", "-X", "patch", "/organization", "-f", file, "-H", "X-Trace: abc")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	req := fake.only(t)
	if req.method != http.MethodPatch || req.body != `{"name":"Acme"}` || req.header.Get("X-Trace") != "abc" {
		t.Errorf("request %s body %q trace %q", req.method, req.body, req.header.Get("X-Trace"))
	}
}

func TestAPIPrintsTheBodyAndExitsOneOnAnErrorStatus(t *testing.T) {
	newAPIFakeServer(t, http.StatusNotFound, `{"errors":[{"code":"FLOW_NOT_FOUND","message":"Flow does not exist."}]}`)

	stdout, stderr, code := runCLI(t, "api", "/flows/x")

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stdout, `"FLOW_NOT_FOUND"`) {
		t.Errorf("stdout %q lacks the body", stdout)
	}
	if !strings.Contains(stderr, "HTTP 404 (FLOW_NOT_FOUND)") {
		t.Errorf("stderr %q", stderr)
	}
}

func TestAPIIncludePrintsTheStatusAndHeaders(t *testing.T) {
	newAPIFakeServer(t, http.StatusOK, `{"ok":true}`)

	stdout, stderr, code := runCLI(t, "api", "/me", "-i")

	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "HTTP/1.1 200 OK\n") || !strings.Contains(stdout, "X-Request-Id: req-1\n") {
		t.Errorf("stdout %q", stdout)
	}
}

func TestAPIPrintsYAML(t *testing.T) {
	newAPIFakeServer(t, http.StatusOK, `{"name":"Acme","id":"org_1"}`)

	stdout, stderr, code := runCLI(t, "api", "/organization", "-o", "yaml")

	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "name: Acme\n") || !strings.Contains(stdout, "id: org_1\n") {
		t.Errorf("stdout %q", stdout)
	}
}

func TestAPIRefusesBeforeAnyRequest(t *testing.T) {
	cases := map[string][]string{
		"administration route": {"api", "/administrations/users"},
		"administration root":  {"api", "administrations?x=1"},
		"full URL":             {"api", "https://evil.example/steal"},
		"fields and a file":    {"api", "/flows/search", "-F", "a=1", "-f", "x.json"},
		"field without value":  {"api", "/flows/search", "-F", "a"},
		"bad header":           {"api", "/me", "-H", "nocolon"},
		"two paths":            {"api", "/me", "/flows"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			fake := newAPIFakeServer(t, http.StatusOK, `{}`)

			_, stderr, code := runCLI(t, args...)

			if code == 0 {
				t.Fatalf("exit 0, stderr %q", stderr)
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if len(fake.requests) != 0 {
				t.Errorf("sent %d requests", len(fake.requests))
			}
		})
	}
}
