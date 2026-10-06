package output

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type sampleRecord struct {
	ID        string   `json:"id"`
	CreatedAt string   `json:"created_at"`
	Parent    *string  `json:"parent_id"`
	Tags      []string `json:"tags"`
	Nested    struct {
		BaseURL string `json:"base_url"`
	} `json:"nested"`
	Version string `json:"version"`
}

func TestPrintYAMLUsesTheKeyNamesOfTheJSONForm(t *testing.T) {
	record := sampleRecord{ID: "abc", CreatedAt: "2026-10-05T12:00:00Z", Tags: []string{"a", "b"}, Version: "1.0"}
	record.Nested.BaseURL = "https://api.example.com"
	var out bytes.Buffer

	if err := PrintYAML(&out, record); err != nil {
		t.Fatal(err)
	}

	want := "id: abc\ncreated_at: \"2026-10-05T12:00:00Z\"\nparent_id: null\ntags:\n    - a\n    - b\nnested:\n    base_url: https://api.example.com\nversion: \"1.0\"\n\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
	for _, lowercased := range []string{"createdat", "parentid", "baseurl"} {
		if strings.Contains(out.String(), lowercased) {
			t.Errorf("a Go field name leaked: %s", lowercased)
		}
	}
}

func TestPrintYAMLKeepsAStringAStringAndANumberANumber(t *testing.T) {
	var out bytes.Buffer
	value := map[string]any{
		"version": "1.0",
		"id":      "123",
		"flag":    "true",
		"empty":   "",
		"count":   5,
		"ratio":   1.5,
		"on":      true,
	}

	if err := PrintYAML(&out, value); err != nil {
		t.Fatal(err)
	}

	var back map[string]any
	if err := yaml.Unmarshal(out.Bytes(), &back); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for key, want := range map[string]any{
		"version": "1.0", "id": "123", "flag": "true", "empty": "", "count": 5, "ratio": 1.5, "on": true,
	} {
		if back[key] != want {
			t.Errorf("%s = %#v (%T), want %#v (%T)", key, back[key], back[key], want, want)
		}
	}
}

func TestPrintYAMLRefusesAValueJSONCannotEncode(t *testing.T) {
	var out bytes.Buffer

	if err := PrintYAML(&out, make(chan int)); err == nil {
		t.Error("a channel was printed")
	}
}
