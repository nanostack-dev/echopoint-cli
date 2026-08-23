package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCollectVarInputs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env.json")
	if err := os.WriteFile(path, []byte(`{"A":"from-file","B":"2"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		file    string
		flags   []string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "file only",
			file: path,
			want: map[string]string{"A": "from-file", "B": "2"},
		},
		{
			name:  "var only",
			flags: []string{"A=1"},
			want:  map[string]string{"A": "1"},
		},
		{
			name:  "var wins over file on duplicate key",
			file:  path,
			flags: []string{"A=from-flag"},
			want:  map[string]string{"A": "from-flag", "B": "2"},
		},
		{
			name: "neither returns empty map",
			want: map[string]string{},
		},
		{
			name:    "missing file",
			file:    filepath.Join(dir, "nope"),
			wantErr: true,
		},
		{
			name:    "malformed var flag",
			flags:   []string{"NOEQUALS"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := collectVarInputs(tt.file, tt.flags)
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
