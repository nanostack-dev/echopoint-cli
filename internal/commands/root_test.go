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
		{"config show", []string{"config", "show"}, false},
		{"version", []string{"version"}, false},
		{"update (top-level)", []string{"update"}, false},

		// Regression: a subcommand named "update" MUST still require a token.
		{"flows update", []string{"flows", "update"}, true},
		{"flows get", []string{"flows", "get"}, true},
		{"flows run", []string{"flows", "run"}, true},
		{"flows create", []string{"flows", "create"}, true},
		{"flows list", []string{"flows", "list"}, true},
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
