package commands

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"echopoint-cli/internal/auth"
)

const cloudFleetPath = "/administrations/cloud-fleet"

func cloudFleetSnapshot() map[string]any {
	return map[string]any{
		"daily_launch_limit": 200, "global_cap": 10, "revision": 0,
		"settings_source": "deployment_defaults", "settings_updated_at": nil,
		"launches_last_24h": 17, "launches_remaining": 183,
		"claimed_jobs": 2, "queued_jobs": 3, "paused": false,
		"window_start": "2026-10-07T12:00:00Z", "generated_at": "2026-10-08T12:00:00Z",
		"next_launch_available_at": nil,
	}
}

func isolateAdminCLI(t *testing.T, baseURL string) {
	t.Helper()
	isolateCLIEnvironment(t, baseURL)
	t.Setenv("ECHOPOINT_API_KEY", "")
	t.Setenv("ECHOPOINT_TOKEN", "admin-session")
}

func TestCloudFleetViewPreservesProductScopeAndNullableTimes(t *testing.T) {
	stub := newAPIStub(t).on(http.MethodGet, cloudFleetPath, http.StatusOK, cloudFleetSnapshot())
	isolateAdminCLI(t, stub.server.URL)
	for _, format := range []string{"json", "yaml", "table"} {
		t.Run(format, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, "admin", "cloud-fleet", "view", "--profile", "default", "-o", format)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			switch format {
			case "json":
				var snapshot map[string]any
				if err := json.Unmarshal([]byte(stdout), &snapshot); err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"next_launch_available_at", "settings_updated_at"} {
					value, exists := snapshot[field]
					if !exists || value != nil {
						t.Errorf("nullable %s = %v (exists=%t)", field, value, exists)
					}
				}
				if snapshot["launches_last_24h"] != float64(17) || snapshot["daily_launch_limit"] != float64(200) {
					t.Errorf("lost fleet values: %s", stdout)
				}
			case "yaml":
				if !strings.Contains(stdout, "next_launch_available_at: null") ||
					!strings.Contains(stdout, "settings_updated_at: null") ||
					!strings.Contains(stdout, "daily_launch_limit: 200") {
					t.Errorf("lost JSON field names or nullable times: %s", stdout)
				}
			case "table":
				for _, text := range []string{"Profile", "default", stub.server.URL, "2 / 10", "deployment_defaults", "Snapshot generated"} {
					if !strings.Contains(stdout, text) {
						t.Errorf("table missing %q: %s", text, stdout)
					}
				}
			}
		})
	}
	requests := stub.requestsTo(http.MethodGet, cloudFleetPath)
	if len(requests) != 3 {
		t.Fatalf("got %d requests", len(requests))
	}
	for _, request := range requests {
		if request.header.Get("Authorization") != "Bearer admin-session" ||
			request.header.Get(
				"X-Organization-Id",
			) != "" || request.header.Get("X-Api-Key") != "" || request.query != "" {
			t.Errorf("request did not use unscoped administrator session")
		}
	}
}

func TestCloudFleetUpdateUsesSelectedProfileAndOptimisticRevision(t *testing.T) {
	snapshot := cloudFleetSnapshot()
	snapshot["daily_launch_limit"], snapshot["global_cap"], snapshot["revision"] = 0, 4, 8
	snapshot["settings_source"], snapshot["settings_updated_at"] = "admin_override", "2026-10-08T12:00:01Z"
	snapshot["paused"], snapshot["launches_remaining"] = true, 0
	stub := newAPIStub(t).on(http.MethodPut, cloudFleetPath, http.StatusOK, snapshot)
	isolateAdminCLI(t, deadServerURL(t))
	t.Setenv("ECHOPOINT_API_URL", "")
	writeCLIConfig(t, "profiles:\n  dev:\n    api_base_url: "+stub.server.URL+"\n")

	stdout, stderr, code := runCLI(t, "admin", "cloud-fleet", "update", "--profile", "dev",
		"--expected-revision", "7", "--daily-launch-limit", "0", "--global-cap", "4", "-o", "json")
	requests := stub.requestsTo(http.MethodPut, cloudFleetPath)
	if code != 0 || len(requests) != 1 {
		t.Fatalf("exit %d, requests %d, stderr %q", code, len(requests), stderr)
	}
	var input map[string]any
	if err := json.Unmarshal(requests[0].body, &input); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"daily_launch_limit": float64(0), "global_cap": float64(4), "expected_revision": float64(7)}
	if !reflect.DeepEqual(input, want) {
		t.Errorf("body = %v, want %v", input, want)
	}
	if requests[0].header.Get("Authorization") != "Bearer admin-session" ||
		requests[0].header.Get("X-Organization-Id") != "" || requests[0].query != "" {
		t.Error("fleet update gained tenant scope or lost session auth")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result["revision"] != float64(8) || result["paused"] != true ||
		result["settings_updated_at"] != "2026-10-08T12:00:01Z" {
		t.Errorf("lost persisted update response: %s", stdout)
	}
}

func TestCloudFleetUpdateRejectsMissingOrInvalidInputsBeforeSending(t *testing.T) {
	stub := newAPIStub(t)
	isolateAdminCLI(t, stub.server.URL)
	t.Setenv("ECHOPOINT_PROFILE", "default")
	for _, args := range [][]string{
		{},
		{"--profile", "default", "--daily-launch-limit", "200", "--global-cap", "10"},
		{"--profile", "default", "--expected-revision", "0", "--global-cap", "10"},
		{"--profile", "default", "--expected-revision", "0", "--daily-launch-limit", "200"},
		{"--expected-revision", "0", "--daily-launch-limit", "200", "--global-cap", "10"},
		{"--profile", "", "--expected-revision", "0", "--daily-launch-limit", "200", "--global-cap", "10"},
		{"--profile", "default", "--expected-revision", "-1", "--daily-launch-limit", "200", "--global-cap", "10"},
		{"--profile", "default", "--expected-revision", "0", "--daily-launch-limit", "-1", "--global-cap", "10"},
		{"--profile", "default", "--expected-revision", "0", "--daily-launch-limit", "200", "--global-cap", "0"},
		{"--profile", "default", "--expected-revision", "0", "--daily-launch-limit", "2147483648", "--global-cap", "10"},
	} {
		_, stderr, code := runCLI(t, append([]string{"admin", "cloud-fleet", "update"}, args...)...)
		if code == 0 || stub.requestCount() != 0 {
			t.Fatalf("accepted input %v: exit=%d requests=%d stderr=%q", args, code, stub.requestCount(), stderr)
		}
	}
}

func TestCloudFleetRejectsAPIKeysAndMissingSessionsLocally(t *testing.T) {
	for _, principal := range []string{"api-key", "no-session"} {
		t.Run(principal, func(t *testing.T) {
			stub := newAPIStub(t)
			isolateAdminCLI(t, stub.server.URL)
			if principal == "api-key" {
				t.Setenv("ECHOPOINT_API_KEY", "organization-key")
			} else {
				t.Setenv("ECHOPOINT_TOKEN", "")
			}
			for _, args := range [][]string{
				{"view"},
				{"update", "--profile", "default", "--expected-revision", "0", "--daily-launch-limit", "200", "--global-cap", "10"},
			} {
				_, stderr, code := runCLI(t, append([]string{"admin", "cloud-fleet"}, args...)...)
				if code == 0 || stub.requestCount() != 0 || !strings.Contains(stderr, "requires") {
					t.Fatalf(
						"principal %s accepted: exit=%d requests=%d stderr=%q",
						principal,
						code,
						stub.requestCount(),
						stderr,
					)
				}
			}
		})
	}
}

func TestAdministratorLoginCannotStoreAnOrganizationAPIKey(t *testing.T) {
	stub := newAPIStub(t)
	isolateAdminCLI(t, stub.server.URL)
	stdout, stderr, code := runCLI(
		t,
		"auth",
		"login",
		"--admin",
		"--api-key",
		"organization-key",
		"--profile",
		"default",
	)
	if code == 0 || stdout != "" || stub.requestCount() != 0 ||
		!strings.Contains(stderr, "--admin cannot be combined with --api-key") {
		t.Fatalf(
			"administrator/API-key login accepted: exit=%d requests=%d stdout=%q stderr=%q",
			code,
			stub.requestCount(),
			stdout,
			stderr,
		)
	}
}

func TestExpiredAdminCredentialsRecommendAdminLoginForSelectedProfile(t *testing.T) {
	stub := newAPIStub(t)
	isolateAdminCLI(t, stub.server.URL)
	t.Setenv("ECHOPOINT_TOKEN", "")
	writeCLIConfig(t, "profiles:\n  qa:\n    api_base_url: "+stub.server.URL+"\n")
	expired := time.Now().Add(-time.Minute)
	_, err := auth.SaveCredentials("qa", auth.Credentials{AccessToken: "expired-session", ExpiresAt: &expired})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"admin", "cloud-fleet", "view", "--profile", "qa"},
		{"admin", "cloud-fleet", "update", "--profile", "qa", "--expected-revision", "0", "--daily-launch-limit", "200", "--global-cap", "10"},
	} {
		stdout, stderr, code := runCLI(t, args...)
		if code == 0 || stdout != "" || stub.requestCount() != 0 ||
			!strings.Contains(stderr, `echopoint auth login --admin --profile "qa"`) {
			t.Fatalf(
				"expired admin session guidance incorrect: exit=%d requests=%d stdout=%q stderr=%q",
				code,
				stub.requestCount(),
				stdout,
				stderr,
			)
		}
	}
	_, stderr, code := runCLI(t, "flow", "list", "--profile", "qa")
	if code == 0 || stub.requestCount() != 0 || strings.Contains(stderr, "--admin") ||
		!strings.Contains(stderr, "stored credentials have expired; run 'echopoint auth login' again") {
		t.Fatalf("normal login guidance changed: exit=%d requests=%d stderr=%q", code, stub.requestCount(), stderr)
	}
}

func TestCloudFleetUpdateDoesNotRetryConflictsOrForbiddenResponses(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			stub := newAPIStub(t).on(http.MethodPut, cloudFleetPath, status,
				map[string]any{
					"errors": []any{
						map[string]any{
							"code":    "CLOUD_FLEET_SETTINGS_CONFLICT",
							"message": "Reload current fleet settings.",
						},
					},
				})
			isolateAdminCLI(t, stub.server.URL)
			stdout, stderr, code := runCLI(t, "admin", "cloud-fleet", "update", "--profile", "default",
				"--expected-revision", "0", "--daily-launch-limit", "200", "--global-cap", "10", "-o", "json")
			if code == 0 || stdout != "" || stub.requestCount() != 1 ||
				!strings.Contains(stderr, "Reload current fleet settings") {
				t.Fatalf(
					"failure not preserved: exit=%d requests=%d stdout=%q stderr=%q",
					code,
					stub.requestCount(),
					stdout,
					stderr,
				)
			}
		})
	}
}
