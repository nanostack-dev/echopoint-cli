package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
