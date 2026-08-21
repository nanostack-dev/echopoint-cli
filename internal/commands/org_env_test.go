package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
)

func TestParseVarFlags(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    map[string]string
		wantErr bool
	}{
		{name: "empty", in: nil, want: map[string]string{}},
		{name: "single", in: []string{"K=v"}, want: map[string]string{"K": "v"}},
		{
			name: "multiple",
			in:   []string{"A=1", "B=2"},
			want: map[string]string{"A": "1", "B": "2"},
		},
		{
			name: "equals in value",
			in:   []string{"URL=https://x?a=b&c=d"},
			want: map[string]string{"URL": "https://x?a=b&c=d"},
		},
		{name: "empty value", in: []string{"K="}, want: map[string]string{"K": ""}},
		{name: "missing equals", in: []string{"KEY"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVarFlags(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseVarFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "json object",
			content: `{"API_KEY":"secret","BASE_URL":"https://api.example.com"}`,
			want:    map[string]string{"API_KEY": "secret", "BASE_URL": "https://api.example.com"},
		},
		{
			name:    "json malformed",
			content: `{"API_KEY":`,
			wantErr: true,
		},
		{
			name:    "dotenv basic",
			content: "API_KEY=secret\nBASE_URL=https://api.example.com\n",
			want:    map[string]string{"API_KEY": "secret", "BASE_URL": "https://api.example.com"},
		},
		{
			name:    "dotenv comments and blanks",
			content: "# comment\n\nA=1\n  # indented comment\nB=2\n",
			want:    map[string]string{"A": "1", "B": "2"},
		},
		{
			name:    "dotenv quoted values stripped",
			content: "A=\"quoted\"\nB='single'\n",
			want:    map[string]string{"A": "quoted", "B": "single"},
		},
		{
			name:    "dotenv equals in value",
			content: "URL=https://x?a=b&c=d\n",
			want:    map[string]string{"URL": "https://x?a=b&c=d"},
		},
		{
			name:    "dotenv crlf",
			content: "A=1\r\nB=2\r\n",
			want:    map[string]string{"A": "1", "B": "2"},
		},
		{
			name:    "dotenv malformed line",
			content: "A=1\nNOEQUALS\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "env")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := parseVarFile(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseVarFileMissing(t *testing.T) {
	if _, err := parseVarFile(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]string{"b": "2", "a": "1", "c": "3"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestPrintVarsHidesValuesByDefault covers the human-readable "get" format:
// values are hidden by default, and the output makes that state obvious
// (a count plus the --show-values hint) rather than looking empty.
func TestPrintVarsHidesValuesByDefault(t *testing.T) {
	var buf bytes.Buffer
	printVars(&buf, "Organization base variables", map[string]string{
		"echopointApiKey": "echopoint_org_apikey_supersecret",
		"OTHER_KEY":       "otherSecret",
	}, false)

	out := buf.String()
	if strings.Contains(out, "supersecret") || strings.Contains(out, "otherSecret") {
		t.Fatalf("expected values to be hidden by default, got: %s", out)
	}
	if !strings.Contains(out, "echopointApiKey") || !strings.Contains(out, "OTHER_KEY") {
		t.Fatalf("expected variable names to still be listed, got: %s", out)
	}
	if !strings.Contains(out, "2 variable(s)") {
		t.Fatalf("expected a count of hidden variables, got: %s", out)
	}
	if !strings.Contains(out, "--show-values") {
		t.Fatalf("expected a hint to use --show-values, got: %s", out)
	}
}

// TestPrintVarsShowValuesReveals covers --show-values in the human format.
func TestPrintVarsShowValuesReveals(t *testing.T) {
	var buf bytes.Buffer
	printVars(&buf, "Organization base variables", map[string]string{
		"echopointApiKey": "echopoint_org_apikey_supersecret",
	}, true)

	out := buf.String()
	if !strings.Contains(out, "echopointApiKey=echopoint_org_apikey_supersecret") {
		t.Fatalf("expected the value to be revealed, got: %s", out)
	}
}

func TestPrintVarsEmpty(t *testing.T) {
	var buf bytes.Buffer
	printVars(&buf, "Organization base variables", map[string]string{}, false)
	if got := buf.String(); !strings.Contains(got, "No environment variables set") {
		t.Fatalf("got %q", got)
	}
}

// TestNamedEnvGetPayload covers the -e <overlay> JSON/YAML path: names only
// by default (a sorted array, never a map with emptied values), the raw map
// with --show-values.
func TestNamedEnvGetPayload(t *testing.T) {
	vars := map[string]string{"B": "secret-b", "A": "secret-a"}

	hidden := namedEnvGetPayload(vars, false)
	names, ok := hidden.([]string)
	if !ok {
		t.Fatalf("expected []string payload when hidden, got %T", hidden)
	}
	if !reflect.DeepEqual(names, []string{"A", "B"}) {
		t.Fatalf("got %v, want sorted names", names)
	}

	revealed := namedEnvGetPayload(vars, true)
	got, ok := revealed.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string payload when shown, got %T", revealed)
	}
	if !reflect.DeepEqual(got, vars) {
		t.Fatalf("got %v, want %v", got, vars)
	}
}

// TestOrgEnvGetPayload covers the base "org env get" (no -e) JSON/YAML path,
// including the named-overlay names nested under it.
func TestOrgEnvGetPayload(t *testing.T) {
	base := map[string]string{"BASE_KEY": "base-secret"}
	overlays := map[string]map[string]string{
		"dev": {"echopointApiKey": "echopoint_org_apikey_devsecret"},
	}
	env := &api.Environment{
		Variables: api.EnvironmentVariableSet{
			"BASE_KEY": {Value: "base-secret"},
		},
	}

	hidden := orgEnvGetPayload(env, base, overlays, false)
	names, ok := hidden.(orgEnvNames)
	if !ok {
		t.Fatalf("expected orgEnvNames payload when hidden, got %T", hidden)
	}
	if !reflect.DeepEqual(names.Variables, []string{"BASE_KEY"}) {
		t.Fatalf("got base names %v", names.Variables)
	}
	if !reflect.DeepEqual(names.Environments["dev"], []string{"echopointApiKey"}) {
		t.Fatalf("got dev overlay names %v", names.Environments["dev"])
	}

	revealed := orgEnvGetPayload(env, base, overlays, true)
	gotEnv, ok := revealed.(*api.Environment)
	if !ok || gotEnv != env {
		t.Fatalf("expected --show-values to return the raw environment unchanged, got %T", revealed)
	}
}

// TestFlowEnvGetPayload covers the "flow env get" JSON/YAML path.
func TestFlowEnvGetPayload(t *testing.T) {
	vars := map[string]string{"TOKEN": "flow-secret"}
	env := &api.Environment{Variables: api.EnvironmentVariableSet{"TOKEN": {Value: "flow-secret"}}}

	hidden := flowEnvGetPayload(env, vars, false)
	names, ok := hidden.([]string)
	if !ok {
		t.Fatalf("expected []string payload when hidden, got %T", hidden)
	}
	if !reflect.DeepEqual(names, []string{"TOKEN"}) {
		t.Fatalf("got %v", names)
	}

	revealed := flowEnvGetPayload(env, vars, true)
	if gotEnv, ok := revealed.(*api.Environment); !ok || gotEnv != env {
		t.Fatalf("expected --show-values to return the raw environment unchanged, got %T", revealed)
	}
}
