package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

// find returns the command reached by walking the given path of names from root.
func find(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cur := root
	for _, name := range path {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Name() == name {
				next = c
				break
			}
		}
		if next == nil {
			t.Fatalf("command %q not found under %q", name, cur.Name())
		}
		cur = next
	}
	return cur
}

func TestRequiresToken(t *testing.T) {
	root := NewRootCmd()

	cases := []struct {
		name string
		path []string
		want bool
	}{
		// Self-management commands: no token needed.
		{"root", nil, false}, // root itself has no name match but also no token; treated as not requiring below
		{"auth", []string{"auth"}, false},
		{"auth login", []string{"auth", "login"}, false},
		{"profile", []string{"profile"}, false},
		{"config view", []string{"config", "view"}, false},
		{"version", []string{"version"}, false},
		{"update (top-level)", []string{"update"}, false},
		{"completion zsh", []string{"completion", "zsh"}, false},

		// Regression: a subcommand named "update" MUST still require a token.
		{"flow update", []string{"flow", "update"}, true},
		{"flow view", []string{"flow", "view"}, true},
		{"flow run", []string{"flow", "run"}, true},
		{"flow create", []string{"flow", "create"}, true},
		{"flow list", []string{"flow", "list"}, true},

		{"status-page view", []string{"status-page", "view"}, true},
		{"status-page save", []string{"status-page", "save"}, true},
		{"status-page publish", []string{"status-page", "publish"}, true},
		{"status-page unpublish", []string{"status-page", "unpublish"}, true},
		{"status-page binding-options", []string{"status-page", "binding-options"}, true},
		{"status-page public", []string{"status-page", "public"}, false},
		{"status-page validate", []string{"status-page", "validate"}, false},

		// Every spec command works on a spec in EchoPoint.
		{"spec list", []string{"spec", "list"}, true},
		{"spec view", []string{"spec", "view"}, true},
		{"spec versions", []string{"spec", "versions"}, true},
		{"spec create", []string{"spec", "create"}, true},
		{"spec push", []string{"spec", "push"}, true},
		{"spec pull", []string{"spec", "pull"}, true},
		{"spec check", []string{"spec", "check"}, true},
		{"spec lint", []string{"spec", "lint"}, true},
		{"spec diff", []string{"spec", "diff"}, true},
		{"spec route add", []string{"spec", "route", "add"}, true},
		{"spec param update", []string{"spec", "param", "update"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.path == nil {
				return // root has no RunE auth path; skip
			}
			cmd := find(t, root, tc.path...)
			if got := requiresToken(cmd); got != tc.want {
				t.Fatalf("requiresToken(%v) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
