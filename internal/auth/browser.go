package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"
)

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

	// Build the auth URL - redirect to frontend's CLI auth page
	authURL := fmt.Sprintf("%s/cli-auth?callback=%s", frontendURL, url.QueryEscape(callbackURL))

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

		// Token expires in ~1 hour
		expiresAt := time.Now().Add(1 * time.Hour)

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

func successPage() string {
	return `<!DOCTYPE html>
<html>
<head>
	<title>Echopoint CLI - Success</title>
	<style>
		* { margin: 0; padding: 0; box-sizing: border-box; }
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			display: flex;
			align-items: center;
			justify-content: center;
			min-height: 100vh;
			background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
		}
		.container {
			background: white;
			padding: 3rem;
			border-radius: 1rem;
			box-shadow: 0 20px 60px rgba(0,0,0,0.3);
			text-align: center;
			max-width: 400px;
		}
		.checkmark {
			width: 80px;
			height: 80px;
			border-radius: 50%;
			background: #10b981;
			display: flex;
			align-items: center;
			justify-content: center;
			margin: 0 auto 1.5rem;
		}
		.checkmark svg {
			width: 40px;
			height: 40px;
			stroke: white;
			stroke-width: 3;
			fill: none;
		}
		h1 {
			color: #1f2937;
			font-size: 1.5rem;
			margin-bottom: 0.5rem;
		}
		p {
			color: #6b7280;
			font-size: 1rem;
		}
	</style>
</head>
<body>
	<div class="container">
		<div class="checkmark">
			<svg viewBox="0 0 24 24">
				<polyline points="20 6 9 17 4 12"></polyline>
			</svg>
		</div>
		<h1>Authentication Successful!</h1>
		<p>You can close this window and return to the CLI.</p>
	</div>
</body>
</html>`
}

func errorPage(message string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Echopoint CLI - Error</title>
	<style>
		* { margin: 0; padding: 0; box-sizing: border-box; }
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			display: flex;
			align-items: center;
			justify-content: center;
			min-height: 100vh;
			background: linear-gradient(135deg, #ef4444 0%, #dc2626 100%%);
		}
		.container {
			background: white;
			padding: 3rem;
			border-radius: 1rem;
			box-shadow: 0 20px 60px rgba(0,0,0,0.3);
			text-align: center;
			max-width: 400px;
		}
		.icon {
			width: 80px;
			height: 80px;
			border-radius: 50%%;
			background: #ef4444;
			display: flex;
			align-items: center;
			justify-content: center;
			margin: 0 auto 1.5rem;
			font-size: 2.5rem;
			color: white;
		}
		h1 {
			color: #1f2937;
			font-size: 1.5rem;
			margin-bottom: 0.5rem;
		}
		p {
			color: #6b7280;
			font-size: 1rem;
		}
	</style>
</head>
<body>
	<div class="container">
		<div class="icon">✕</div>
		<h1>Authentication Failed</h1>
		<p>%s</p>
	</div>
</body>
</html>`, message)
}
