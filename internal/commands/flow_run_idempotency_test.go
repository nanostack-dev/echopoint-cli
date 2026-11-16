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

// derivedLaunchKey runs one flow with no explicit key inside a GitHub Actions step and
// returns the idempotency key it was launched with.
func derivedLaunchKey(t *testing.T, step, flow string) string {
	t.Helper()
	fake := newGoldenFlowServer(t, nil)
	isolateCLIEnvironment(t, fake.server.URL)
	setGitHubActionsEnvironment(t, t.TempDir()+"/summary.md")
	t.Setenv("GITHUB_ACTION", step)

	if _, stderr, code := runCLI(t, "flow", "run", flow, "-o", "json"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	match := launchKeyPattern.FindStringSubmatch(fake.launchLines())
	if match == nil || match[2] == "" {
		t.Fatalf("no derived key in %q", fake.launchLines())
	}
	return match[2]
}

// Two single-flow runs of different flows in one job used to share the derived key,
// so the second launch got 409 IDEMPOTENCY_KEY_CONFLICT (infra run 43953894269).
func TestDerivedKeyDiffersPerFlowForSingleFlowRuns(t *testing.T) {
	first := derivedLaunchKey(t, "rerun", flowUUID().String())
	second := derivedLaunchKey(t, "rerun", goldenReplayFlow)

	if first == second {
		t.Fatalf("two flows launched with one key %q", first)
	}
}

// A flow run again by a later step is a new run, not a retry of the earlier launch:
// the same key would replay the earlier, possibly failed, execution.
func TestDerivedKeyDiffersPerStep(t *testing.T) {
	inSuite := derivedLaunchKey(t, "__nanostack-dev_echopoint-cli", flowUUID().String())
	inRerun := derivedLaunchKey(t, "rerun", flowUUID().String())

	if inSuite == inRerun {
		t.Fatalf("two steps launched the flow with one key %q", inSuite)
	}
}

// A single flow launched alone gets the key it would get inside a multi-flow run.
func TestDerivedKeyOfASingleFlowIsItsPerFlowKey(t *testing.T) {
	key := derivedLaunchKey(t, "suite", flowUUID().String())

	if want := derivePerFlowKey(deriveGitHubIdempotencyKey(), flowUUID().String()); key != want {
		t.Errorf("key %q, want %q", key, want)
	}
}
