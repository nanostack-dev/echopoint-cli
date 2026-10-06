package commands

import (
	"encoding/json"
	"testing"

	"echopoint-cli/internal/api"
)

func TestLaunchFlowRequestBody(t *testing.T) {
	tests := []struct {
		name        string
		runner      string
		environment string
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, err := launchFlowRequestBody(tc.runner, tc.environment)
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
		})
	}
}
