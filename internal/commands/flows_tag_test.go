package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
)

func TestMergeTags(t *testing.T) {
	cases := []struct {
		name    string
		current []string
		add     []string
		remove  []string
		want    []string
		changed bool
	}{
		{
			name:    "add new",
			current: []string{"prod"},
			add:     []string{"anchor"},
			want:    []string{"prod", "anchor"},
			changed: true,
		},
		{
			name:    "add existing is no-op",
			current: []string{"anchor"},
			add:     []string{"anchor"},
			want:    []string{"anchor"},
			changed: false,
		},
		{name: "add to empty", current: []string{}, add: []string{"anchor"}, want: []string{"anchor"}, changed: true},
		{
			name:    "remove present",
			current: []string{"anchor", "prod"},
			remove:  []string{"prod"},
			want:    []string{"anchor"},
			changed: true,
		},
		{
			name:    "remove absent is no-op",
			current: []string{"anchor"},
			remove:  []string{"prod"},
			want:    []string{"anchor"},
			changed: false,
		},
		{
			name:    "case-insensitive add is no-op",
			current: []string{"anchor"},
			add:     []string{"Anchor"},
			want:    []string{"anchor"},
			changed: false,
		},
		{
			name:    "add and remove together",
			current: []string{"old"},
			add:     []string{"anchor"},
			remove:  []string{"old"},
			want:    []string{"anchor"},
			changed: true,
		},
		{
			name:    "dedupe added",
			current: []string{},
			add:     []string{"anchor", "anchor"},
			want:    []string{"anchor"},
			changed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := mergeTags(tc.current, tc.add, tc.remove)
			if changed != tc.changed {
				t.Errorf("changed = %v, want %v", changed, tc.changed)
			}
			if !sameStringSet(got, tc.want) {
				t.Errorf("tags = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFlowsTag_AddTagUpdatesFlow(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440099")

	var (
		mu      sync.Mutex
		putBody api.UpdateFlowRequest
		putHit  bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id.String(), "tags": []string{"prod"}})
		case http.MethodPut:
			mu.Lock()
			putHit = true
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id.String(), "tags": []string{"anchor", "prod"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	state := makeState(t, "", "test-token", srv.URL)
	cmd := newFlowsTagCmd(state)
	cmd.SetArgs([]string{id.String(), "--add", "anchor"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if !putHit {
		t.Fatal("expected an update (PUT) request")
	}
	if putBody.Tags == nil {
		t.Fatal("expected tags in update body")
	}
	if !strings.Contains(strings.Join(*putBody.Tags, ","), "anchor") {
		t.Errorf("update tags = %v, want to include anchor", *putBody.Tags)
	}
}

func TestFlowsTag_RefusesTagAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	state := makeState(t, "", "test-token", srv.URL)
	cmd := newFlowsTagCmd(state)
	// No flow IDs and no search filter -> must refuse (never tag all flows).
	cmd.SetArgs([]string{"--add", "anchor"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error refusing to tag every flow with no filter")
	}
}

func TestFlowsTag_SearchSelectsAndTags(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-4466554400aa")
	var (
		mu       sync.Mutex
		searched bool
		putHit   bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/flows/search") && r.Method == http.MethodPost:
			mu.Lock()
			searched = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1, "total": 1,
				"items": []map[string]any{{"id": id.String(), "tags": []string{}}},
			})
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id.String(), "tags": []string{}})
		case r.Method == http.MethodPut:
			mu.Lock()
			putHit = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id.String(), "tags": []string{"anchor"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	state := makeState(t, "", "test-token", srv.URL)
	cmd := newFlowsTagCmd(state)
	cmd.SetArgs([]string{"--query", "anchor", "--add", "anchor"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if !searched {
		t.Error("expected a /flows/search request")
	}
	if !putHit {
		t.Error("expected the searched flow to be updated (PUT)")
	}
}

func TestFlowsTag_RequiresAddOrRemove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	state := makeState(t, "", "test-token", srv.URL)
	cmd := newFlowsTagCmd(state)
	cmd.SetArgs([]string{uuid.New().String()})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when neither --add nor --remove is given")
	}
}
