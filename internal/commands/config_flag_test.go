package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A command that writes the config writes the file the run selected, the one the
// reads of the same run use: --config, else ECHOPOINT_CONFIG, else the user's own.
func TestEveryConfigWriteGoesToTheFileConfigSelects(t *testing.T) {
	stub := newAPIStub(t)
	isolateCLIEnvironment(t, stub.server.URL)
	selected := filepath.Join(t.TempDir(), "elsewhere", "config.yaml")
	byEnvironment := os.Getenv("ECHOPOINT_CONFIG")
	userFile := filepath.Join(os.Getenv("HOME"), ".echopoint", "config.yaml")
	run := func(args ...string) string {
		t.Helper()
		stdout, stderr, code := runCLI(t, append([]string{"--config", selected}, args...)...)
		if code != 0 {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
		return stdout
	}

	run("profile", "add", "qa", "--api-url", "https://qa.example.com")
	if !strings.Contains(readFile(t, selected), "qa:") {
		t.Errorf("profile add: %s", readFile(t, selected))
	}

	run("profile", "use", "qa")
	if !strings.Contains(readFile(t, selected), "current_profile: qa") {
		t.Errorf("profile use: %s", readFile(t, selected))
	}
	if list := run("profile", "list"); !strings.Contains(list, "qa") || !strings.Contains(list, "*") {
		t.Errorf("profile list does not read the selected file: %s", list)
	}

	run("config", "set", "defaults.output_format", "json")
	if !strings.Contains(readFile(t, selected), "output_format: json") {
		t.Errorf("config set: %s", readFile(t, selected))
	}

	run("profile", "delete", "qa", "--yes")
	if strings.Contains(readFile(t, selected), "qa:") {
		t.Errorf("profile delete: %s", readFile(t, selected))
	}

	run("profile", "add", "again", "--api-url", "https://again.example.com")
	run("config", "reset", "--yes")
	if strings.Contains(readFile(t, selected), "again") {
		t.Errorf("config reset: %s", readFile(t, selected))
	}

	for name, path := range map[string]string{"the user's file": userFile, "ECHOPOINT_CONFIG": byEnvironment} {
		if fileExists(path) {
			t.Errorf("%s was written although --config selected another file:\n%s", name, readFile(t, path))
		}
	}
}

func TestConfigWritesFollowECHOPOINTCONFIGWithoutTheFlag(t *testing.T) {
	stub := newAPIStub(t)
	isolateCLIEnvironment(t, stub.server.URL)
	byEnvironment := os.Getenv("ECHOPOINT_CONFIG")
	userFile := filepath.Join(os.Getenv("HOME"), ".echopoint", "config.yaml")

	if _, stderr, code := runCLI(t, "profile", "add", "qa", "--api-url", "https://qa.example.com"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}

	if !strings.Contains(readFile(t, byEnvironment), "qa:") || fileExists(userFile) {
		t.Errorf("env file %q, user file exists %v", readFile(t, byEnvironment), fileExists(userFile))
	}
}

func TestConfigWritesGoToTheUsersFileWhenNothingSelectsAnother(t *testing.T) {
	stub := newAPIStub(t)
	isolateCLIEnvironment(t, stub.server.URL)
	t.Setenv("ECHOPOINT_CONFIG", "")
	userFile := filepath.Join(os.Getenv("HOME"), ".echopoint", "config.yaml")

	if _, stderr, code := runCLI(t, "profile", "add", "qa", "--api-url", "https://qa.example.com"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}

	if !strings.Contains(readFile(t, userFile), "qa:") {
		t.Errorf("user file: %s", readFile(t, userFile))
	}
}

func TestProfileNamesCompleteFromTheConfigTheFlagSelects(t *testing.T) {
	completionStub(t)
	elsewhere := writeTemp(t, "config.yaml", "profiles:\n  qa:\n    api_base_url: https://qa.example.com\n")

	got := completeCLI(t, "--config", elsewhere, "profile", "use", "")

	if len(got) != 2 || !strings.HasPrefix(got[1], "qa\t") {
		t.Errorf("got %q", got)
	}
}
