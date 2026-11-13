package auth

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// fallbackTokenTTL is used only when the token endpoint does not say how long
// the access token lasts.
const fallbackTokenTTL = time.Hour

const (
	callbackPath    = "/callback"
	defaultTimeout  = 5 * time.Minute
	localServerPort = "8765"
)

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}

func stderrWriter() io.Writer { return os.Stderr }
