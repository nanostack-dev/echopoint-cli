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
	if got := buf.String(); !strings.Contains(got, "No variables set") {
		t.Fatalf("got %q", got)
	}
}

// TestLayerPayload covers the JSON/YAML path of every "get": names only by
// default, the values map when --show-values is passed.
func TestLayerPayload(t *testing.T) {
	vars := map[string]string{"B": "value-b", "A": "value-a"}

	hidden := layerPayload(vars, false)
	names, ok := hidden.([]string)
	if !ok {
		t.Fatalf("expected []string payload when hidden, got %T", hidden)
	}
	if !reflect.DeepEqual(names, []string{"A", "B"}) {
		t.Fatalf("got %v, want sorted names", names)
	}

	revealed := layerPayload(vars, true)
	got, ok := revealed.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string payload when shown, got %T", revealed)
	}
	if !reflect.DeepEqual(got, vars) {
		t.Fatalf("got %v, want %v", got, vars)
	}
}

// TestLayerValuesMarksSecrets pins what --show-values can reveal. A read never
// returns a secret's value, so printing an empty string would read as "set to
// nothing" rather than "withheld".
func TestLayerValuesMarksSecrets(t *testing.T) {
	plain := "https://api.example.com"
	layer := api.VariableLayer{
		"BASE_URL": {Value: &plain},
		"API_KEY":  {Secret: true},
	}

	got := layerValues(layer)
	want := map[string]string{"BASE_URL": plain, "API_KEY": secretPlaceholder}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A plain variable whose value the server omitted is not a secret, so it must
// not borrow the secret marker.
func TestLayerValuesLeavesAMissingPlainValueEmpty(t *testing.T) {
	got := layerValues(api.VariableLayer{"ODD": {}})
	if got["ODD"] != "" {
		t.Fatalf("got %q, want an empty value", got["ODD"])
	}
}
