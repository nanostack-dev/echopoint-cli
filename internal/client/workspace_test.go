package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWorkspaceSelectionReachesGeneratedAndRawRequests(t *testing.T) {
	for _, apiKey := range []bool{false, true} {
		name := "bearer"
		if apiKey {
			name = "api-key"
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("X-Workspace-Id"); got != "ws_selected" {
					t.Errorf("workspace = %q", got)
				}
				if got := r.Header.Get("X-Organization-Id"); got != "org_selected" {
					t.Errorf("organization = %q", got)
				}
				if apiKey {
					if r.Header.Get("X-Api-Key") != "test-key" || r.Header.Get("Authorization") != "" {
						t.Error("API-key authentication changed")
					}
				} else if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("Bearer authentication changed")
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			var c *Client
			var err error
			if apiKey {
				c, err = NewWithAPIKey(
					server.URL,
					"test-key",
					"org_selected",
					time.Second,
					WithWorkspaceHeader("ws_selected"),
				)
			} else {
				c, err = New(server.URL, "test-token", "org_selected", time.Second, WithWorkspaceHeader("ws_selected"))
			}
			if err != nil {
				t.Fatal(err)
			}
			response, err := c.API().GetFlow(context.Background(), uuid.Nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if status, _, err := c.Do(
				context.Background(),
				http.MethodGet,
				"/flows/example",
				nil,
				nil,
			); err != nil ||
				status != http.StatusOK {
				t.Fatalf("raw request status=%d error=%v", status, err)
			}
		})
	}
}

func TestMissingWorkspaceDoesNotInventSelection(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	if err := WithWorkspaceHeader("")(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Values("X-Workspace-Id"); len(got) != 0 {
		t.Fatalf("unexpected selection: %v", got)
	}
}
