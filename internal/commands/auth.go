package commands

import (
	"fmt"
	"os"
	"time"

	"echopoint-cli/internal/auth"

	"github.com/spf13/cobra"
)

func newAuthCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}

	cmd.AddCommand(
		newAuthLoginCmd(state),
		newAuthStatusCmd(state),
		newAuthLogoutCmd(state),
		newAuthHelpCmd(state),
	)

	return cmd
}

func newAuthLoginCmd(state *AppState) *cobra.Command {
	var debug bool
	var local bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in via browser",
		Long: `Open your browser to sign in to Echopoint.

This uses the same authentication flow as the web frontend.
A browser window will open where you can sign in, and the CLI
will automatically receive your session token.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine frontend URL based on API URL or --local flag
			frontendURL := "https://dev.echopoint.dev"
			if local || state.Config.API.BaseURL == "http://localhost:8080" {
				frontendURL = "http://localhost:3001"
			}

			creds, err := auth.BrowserLogin(cmd.Context(), frontendURL, debug)
			if err != nil {
				return err
			}

			path, err := auth.SaveCredentials(creds)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "\n✓ Successfully authenticated!\n")
			fmt.Fprintf(os.Stdout, "Credentials saved to %s\n", path)
			return nil
		},
	}

	cmd.Flags().BoolVar(&debug, "debug", false, "Print debug information")
	cmd.Flags().BoolVar(&local, "local", false, "Use localhost:3001 for authentication")

	return cmd
}

func newAuthHelpCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "help",
		Short: "Show authentication instructions",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(os.Stdout, `
┌─────────────────────────────────────────────────────────────────┐
│ Echopoint CLI Authentication                                   │
└─────────────────────────────────────────────────────────────────┘

The CLI uses Clerk authentication, the same system as the frontend.

How it works:

1. Run: echopoint auth login

2. Enter your email address (the one registered with Echopoint)

3. The CLI creates a secure session for you

4. You're authenticated! The session is valid for ~1 hour

Commands:

  echopoint auth login         Sign in with your email
  echopoint auth login -e X    Sign in with email X
  echopoint auth status        Check authentication status
  echopoint auth logout        Sign out and clear credentials

Note: You must have an existing Echopoint account. Sign up at
https://dev.echopoint.dev if you don't have one.
`)
		},
	}
}

func newAuthStatusCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, path, err := auth.LoadCredentials()
			if err != nil {
				return err
			}

			if creds == nil {
				fmt.Fprintln(os.Stdout, "No credentials found.")
				fmt.Fprintf(os.Stdout, "Expected path: %s\n", path)
				return nil
			}

			fmt.Fprintf(os.Stdout, "Credentials: %s\n", path)
			if creds.ExpiresAt != nil {
				fmt.Fprintf(os.Stdout, "Expires: %s\n", creds.ExpiresAt.Format(time.RFC3339))
			} else {
				fmt.Fprintln(os.Stdout, "Expires: unknown")
			}
			return nil
		},
	}
}

func newAuthLogoutCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := auth.DeleteCredentials()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "✓ Removed credentials at %s\n", path)
			return nil
		},
	}
}
