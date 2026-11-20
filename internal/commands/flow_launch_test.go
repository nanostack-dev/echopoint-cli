package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
)

func TestLaunchFlowRequestBody(t *testing.T) {
	tests := []struct {
		name        string
		runner      string
		environment string
		inputs      map[string]any
		wantErr     bool
		wantRunner  api.RunnerType
		wantEnv     *string
	}{
		{name: "defaults to cloud", runner: "", environment: "", wantRunner: api.Cloud},
		{name: "normalizes runner casing", runner: " Self_Hosted ", wantRunner: api.SelfHosted},
		{name: "invalid runner", runner: "banana", wantErr: true},
		{name: "environment overlay", runner: "cloud", environment: "dev", wantRunner: api.Cloud, wantEnv: new("dev")},
		{name: "blank environment omitted", runner: "cloud", environment: "  ", wantRunner: api.Cloud},
		{
			name:        "environment trimmed",
			runner:      "cloud",
			environment: " prod ",
			wantRunner:  api.Cloud,
			wantEnv:     new("prod"),
		},
		{
			name:       "carries launch inputs",
			runner:     "cloud",
			inputs:     map[string]any{"email": "alice@example.com", "count": float64(3)},
			wantRunner: api.Cloud,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, err := launchFlowRequestBody(tc.runner, tc.environment, tc.inputs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var payload api.LaunchFlowRequest
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if payload.RunnerType == nil || *payload.RunnerType != tc.wantRunner {
				t.Fatalf("runner = %v, want %v", payload.RunnerType, tc.wantRunner)
			}
			switch {
			case tc.wantEnv == nil && payload.EnvironmentKey != nil:
				t.Fatalf("environment_key = %q, want omitted", *payload.EnvironmentKey)
			case tc.wantEnv != nil && (payload.EnvironmentKey == nil || *payload.EnvironmentKey != *tc.wantEnv):
				t.Fatalf("environment_key = %v, want %q", payload.EnvironmentKey, *tc.wantEnv)
			}
			if len(tc.inputs) == 0 && payload.Inputs != nil {
				t.Fatalf("inputs = %v, want omitted", *payload.Inputs)
			}
			if len(tc.inputs) > 0 {
				if payload.Inputs == nil {
					t.Fatalf("inputs omitted, want %v", tc.inputs)
				}
				for k, want := range tc.inputs {
					if got := (*payload.Inputs)[k]; got != want {
						t.Fatalf("inputs[%q] = %#v, want %#v", k, got, want)
					}
				}
			}
		})
	}
}

func TestParseLaunchInputs(t *testing.T) {
	inputs, err := parseLaunchInputs(
		[]string{"email=alice@example.com", "token=abc=def"},
		[]string{"retries=3", `meta={"team":"core"}`},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["email"] != "alice@example.com" || inputs["token"] != "abc=def" {
		t.Fatalf("string inputs = %#v", inputs)
	}
	if inputs["retries"] != float64(3) {
		t.Fatalf("retries = %#v, want 3", inputs["retries"])
	}
	meta, ok := inputs["meta"].(map[string]any)
	if !ok || meta["team"] != "core" {
		t.Fatalf("meta = %#v", inputs["meta"])
	}

	for _, bad := range []struct {
		raw  []string
		json []string
	}{
		{raw: []string{"noequals"}},
		{raw: []string{"=missingkey"}},
		{json: []string{"bad={not-json}"}},
	} {
		if _, err := parseLaunchInputs(bad.raw, bad.json); err == nil {
			t.Fatalf("expected error for %+v", bad)
		}
	}
}

func TestFlowLaunch_SendsInputsAndPreferWaitAndPrintsOutputs(t *testing.T) {
	flowID := uuid.New()
	execID := uuid.New()
	var gotPrefer string
	var gotBody api.LaunchFlowRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrefer = r.Header.Get("Prefer")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode launch body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Preference-Applied", "wait=15")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"execution": map[string]any{
				"id":          execID.String(),
				"flow_id":     flowID.String(),
				"status":      "completed",
				"runner_type": "cloud",
			},
			"outputs": map[string]any{
				"summary":       "alice@example.com:pro",
				"masked_secret": "***",
			},
		})
	}))
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)
	state.OutputFormat = output.FormatTable

	cmd := newFlowLaunchCmd(state)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		flowID.String(),
		"--input", "email=alice@example.com",
		"--input-json", "retries=2",
		"--wait", "15",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("flow launch failed: %v", err)
	}
	if gotPrefer != "wait=15" {
		t.Fatalf("Prefer header = %q, want %q", gotPrefer, "wait=15")
	}
	if gotBody.Inputs == nil ||
		(*gotBody.Inputs)["email"] != "alice@example.com" ||
		(*gotBody.Inputs)["retries"] != float64(2) {
		t.Fatalf("sent inputs = %#v", gotBody.Inputs)
	}
	printed := out.String()
	for _, want := range []string{
		"Execution ID: " + execID.String(),
		"Status: completed",
		"Outputs:",
		"  masked_secret: ***",
		"  summary: alice@example.com:pro",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("output missing %q:\n%s", want, printed)
		}
	}
}

func TestFlowLaunch_RejectsOutOfRangeWait(t *testing.T) {
	state := makeState(t, "test-api-key", "", "http://127.0.0.1:1")
	cmd := newFlowLaunchCmd(state)
	cmd.SetArgs([]string{uuid.New().String(), "--wait", "60"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--wait") {
		t.Fatalf("err = %v, want --wait range error", err)
	}
}
