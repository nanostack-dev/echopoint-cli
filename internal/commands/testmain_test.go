package commands

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	binaryOnce sync.Once
	binaryDir  string
	binaryPath string
	errBinary  error

	// startEnvironment is the environment before any test isolates HOME: the build
	// needs the real module and build caches.
	startEnvironment []string
)

func TestMain(m *testing.M) {
	startEnvironment = os.Environ()
	code := m.Run()
	if binaryDir != "" {
		_ = os.RemoveAll(binaryDir)
	}
	os.Exit(code)
}

// echopointBinaryEnv names a ready binary to run instead of a fresh build, such as
// the one built from origin/main, to check what the golden files say of it.
const echopointBinaryEnv = "ECHOPOINT_TEST_BINARY"

// echopointBinary builds cmd/echopoint once for the whole test run.
func echopointBinary(t *testing.T) string {
	t.Helper()
	if prebuilt := os.Getenv(echopointBinaryEnv); prebuilt != "" {
		return prebuilt
	}
	binaryOnce.Do(func() {
		binaryDir, errBinary = os.MkdirTemp("", "echopoint-cli-test-")
		if errBinary != nil {
			return
		}
		binaryPath = filepath.Join(binaryDir, "echopoint")
		if runtime.GOOS == "windows" {
			binaryPath += ".exe"
		}
		build := exec.Command("go", "build", "-o", binaryPath, "../../cmd/echopoint")
		build.Env = startEnvironment
		if output, err := build.CombinedOutput(); err != nil {
			errBinary = errors.New("go build ./cmd/echopoint: " + err.Error() + "\n" + string(output))
		}
	})
	if errBinary != nil {
		t.Fatal(errBinary)
	}
	return binaryPath
}

// runCLI runs the built binary with the environment of the test and no stdin, and
// reports what it wrote to stdout and stderr and its exit code.
func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), echopointBinary(t), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exitError, ok := errors.AsType[*exec.ExitError](err); ok {
		return stdout.String(), withoutLibraryLogs(stderr.String()), exitError.ExitCode()
	}
	if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return stdout.String(), withoutLibraryLogs(stderr.String()), 0
}

// withoutLibraryLogs drops the JSON log lines the embedded runner writes to stderr.
func withoutLibraryLogs(stderr string) string {
	var kept []string
	for line := range strings.SplitSeq(stderr, "\n") {
		if !strings.HasPrefix(line, `{"level":`) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func exitCodeFrom(err error) int {
	if err == nil {
		return exitSuccess
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}
