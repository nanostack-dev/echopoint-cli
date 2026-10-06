package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlowRunPrintsTheRunnerLogsOnlyWhenAskedTo(t *testing.T) {
	cases := []struct {
		name     string
		flags    []string
		wantLogs bool
	}{
		{name: "by default", wantLogs: false},
		{name: "as JSON", flags: []string{"-o", "json"}, wantLogs: false},
		{name: "with --verbose", flags: []string{"--verbose"}, wantLogs: true},
		{name: "with --debug", flags: []string{"--debug"}, wantLogs: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newGoldenFlowServer(t, nil)
			isolateCLIEnvironment(t, fake.server.URL)

			args := append([]string{"flow", "run", flowUUID().String()}, tc.flags...)
			_, stderr, code := runCLIWithLogs(t, args...)

			assert.Equal(t, exitSuccess, code)
			assert.Equal(t, tc.wantLogs, strings.Contains(stderr, `{"level":"debug"`), stderr)
		})
	}
}
