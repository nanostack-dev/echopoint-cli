package commands

import "testing"

func TestParseKeyVals(t *testing.T) {
	t.Run("parses pairs", func(t *testing.T) {
		got, err := parseKeyVals([]string{"email={{userEmail}}", "token=authToken"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["email"] != "{{userEmail}}" || got["token"] != "authToken" {
			t.Errorf("unexpected map: %#v", got)
		}
	})

	t.Run("keeps = in value", func(t *testing.T) {
		got, err := parseKeyVals([]string{"q=a=b=c"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["q"] != "a=b=c" {
			t.Errorf("got %q, want %q", got["q"], "a=b=c")
		}
	})

	t.Run("allows empty value", func(t *testing.T) {
		got, err := parseKeyVals([]string{"k="})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v, ok := got["k"]; !ok || v != "" {
			t.Errorf("got %#v, want empty value for k", got)
		}
	})

	t.Run("rejects missing separator", func(t *testing.T) {
		if _, err := parseKeyVals([]string{"noequals"}); err == nil {
			t.Error("expected error for missing =")
		}
	})

	t.Run("rejects empty key", func(t *testing.T) {
		if _, err := parseKeyVals([]string{"=value"}); err == nil {
			t.Error("expected error for empty key")
		}
	})
}
