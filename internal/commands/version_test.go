package commands

import "testing"

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"0.3.0":   "v0.3.0", // GoReleaser {{.Version}} is bare
		"v0.3.0":  "v0.3.0", // already prefixed
		"1.2.3":   "v1.2.3",
		"dev":     "dev", // source build passes through
		"unknown": "unknown",
		"":        "",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
