package commands

import (
	"regexp"
	"strings"
	"testing"
)

var launchKeyPattern = regexp.MustCompile(`POST /flows/(\S+)/launch idempotency-key="([^"]*)"`)

// launchKeys runs the flows with one base key against a fresh server and returns
// the idempotency key each flow was launched with.
func launchKeys(t *testing.T, key string, flows ...string) map[string]string {
	t.Helper()
	fake := newGoldenFlowServer(t, nil)
	isolateCLIEnvironment(t, fake.server.URL)

	args := append([]string{"flow", "run"}, flows...)
	if _, stderr, code := runCLI(t, append(args, "--idempotency-key", key, "-o", "json")...); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	keys := map[string]string{}
	for _, match := range launchKeyPattern.FindAllStringSubmatch(fake.launchLines(), -1) {
		keys[match[1]] = match[2]
	}
	return keys
}

// The key of a flow is derived from its id as it was typed, as it always was, so a
// retry of a CI job keeps the keys it sent.
func TestPerFlowKeysAreDerivedFromTheIDAsTyped(t *testing.T) {
	typed := []string{flowUUID().String(), strings.ToUpper(goldenReplayFlow)}

	keys := launchKeys(t, "retry-1", typed...)

	if len(keys) != 2 {
		t.Fatalf("keys %v", keys)
	}
	for _, flow := range typed {
		// the request path carries the id in its canonical lowercase form
		if want, got := derivePerFlowKey("retry-1", flow), keys[strings.ToLower(flow)]; got != want {
			t.Errorf("flow %s: key %q, want %q", flow, got, want)
		}
	}
	if keys[typed[0]] == keys[strings.ToLower(typed[1])] {
		t.Error("two flows share a key")
	}
}

func TestASingleFlowKeepsTheBaseKey(t *testing.T) {
	keys := launchKeys(t, "retry-1", flowUUID().String())

	if keys[flowUUID().String()] != "retry-1" {
		t.Errorf("keys %v, want the base key", keys)
	}
}
