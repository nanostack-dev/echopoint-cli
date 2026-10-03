package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// fallbackTokenTTL is used only when the token's own expiry can't be read.
const fallbackTokenTTL = time.Hour

// tokenExpiry reads the `exp` claim from a JWT (without verifying the
// signature — used only to schedule re-auth, not to trust the token) and
// returns it as a time. Falls back to now+fallbackTokenTTL when the token is
// not a readable JWT, so a short-lived token is never treated as longer-lived
// than it is.
func tokenExpiry(token string, now time.Time) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			var claims struct {
				Exp int64 `json:"exp"`
			}
			if json.Unmarshal(payload, &claims) == nil && claims.Exp > 0 {
				return time.Unix(claims.Exp, 0)
			}
		}
	}
	return now.Add(fallbackTokenTTL)
}

const (
	callbackPath    = "/callback"
	defaultTimeout  = 5 * time.Minute
	localServerPort = "8765"
)

// BrowserLogin opens the browser for authentication and waits for the callback
func BrowserLogin(ctx context.Context, frontendURL string, debug bool) (Credentials, error) {
	// Start local server to receive the callback
	listener, err := net.Listen("tcp", "127.0.0.1:"+localServerPort)
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to start local server: %w", err)
	}
	defer listener.Close()

	callbackURL := fmt.Sprintf("http://127.0.0.1:%s%s", localServerPort, callbackPath)
	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)

	// HTTP server to handle the callback
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != callbackPath {
				http.NotFound(w, r)
				return
			}

			// Get token from query parameter
			token := r.URL.Query().Get("token")
			if token == "" {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, errorPage("Missing token in callback"))
				errCh <- fmt.Errorf("no token in callback")
				return
			}

			// Success page
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, successPage())
			tokenCh <- token
		}),
	}

	go func() {
		_ = server.Serve(listener)
	}()

	// Build the auth URL - redirect to frontend's standalone CLI auth page
	authURL := fmt.Sprintf("%s/cli/auth?callback=%s", frontendURL, url.QueryEscape(callbackURL))

	if debug {
		fmt.Fprintf(os.Stderr, "Debug: Auth URL: %s\n", authURL)
		fmt.Fprintf(os.Stderr, "Debug: Callback URL: %s\n", callbackURL)
	}

	// Open the browser
	fmt.Fprintln(os.Stderr, "Opening browser for authentication...")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "If the browser doesn't open automatically, visit:")
	fmt.Fprintf(os.Stderr, "  %s\n", authURL)
	fmt.Fprintln(os.Stderr, "")

	if err := openBrowser(authURL); err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "Debug: Failed to open browser: %v\n", err)
		}
	}

	// Wait for token or timeout
	loginCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	select {
	case token := <-tokenCh:
		_ = server.Shutdown(context.Background())

		// Derive expiry from the token's own `exp` claim so long-lived
		// sessions are not discarded after an arbitrary hour.
		expiresAt := tokenExpiry(token, time.Now())

		return Credentials{
			AccessToken: token,
			ExpiresAt:   &expiresAt,
		}, nil

	case err := <-errCh:
		_ = server.Shutdown(context.Background())
		return Credentials{}, err

	case <-loginCtx.Done():
		_ = server.Shutdown(context.Background())
		return Credentials{}, fmt.Errorf("authentication timed out")
	}
}

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
