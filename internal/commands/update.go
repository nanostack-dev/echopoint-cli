package commands

import (
	"context"
	"fmt"
	"os"

	"echopoint-cli/internal/updater"

	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var (
		checkOnly  bool
		force      bool
		skipRunner bool
	)

	cmd := &cobra.Command{
		Use:   updateCommandName,
		Short: "Update the CLI and the echopoint-runner binary to the latest release",
		Long: `Download and install the latest echopoint CLI release from GitHub, and the
latest echopoint-runner binary that the CLI executes for local (ephemeral)
flow runs.

Both binaries are verified against their release checksums. The CLI replaces
itself in place; the runner is installed next to it (or over an existing
echopoint-runner on PATH). Use --check to see what is available without
installing, or --skip-runner to update only the CLI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := updateCLI(ctx, checkOnly, force); err != nil {
				return err
			}
			if skipRunner {
				return nil
			}
			return updateRunner(ctx, checkOnly)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates; do not install")
	cmd.Flags().BoolVar(&force, "force", false, "Reinstall even if already up to date")
	cmd.Flags().BoolVar(&skipRunner, "skip-runner", false, "Update only the CLI, not the echopoint-runner binary")

	return cmd
}

// updateCLI updates the running CLI binary in place.
func updateCLI(ctx context.Context, checkOnly, force bool) error {
	current := Version()

	release, err := updater.LatestRelease(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "CLI current version: %s\n", current)
	fmt.Fprintf(os.Stdout, "CLI latest version:  %s\n", release.TagName)

	newer := updater.IsNewer(current, release.TagName)

	if checkOnly {
		if newer {
			fmt.Fprintf(os.Stdout, "A CLI update is available: %s\n", release.HTMLURL)
		} else {
			fmt.Fprintln(os.Stdout, "✓ CLI already up to date.")
		}
		return nil
	}

	if !newer && !force {
		fmt.Fprintln(os.Stdout, "✓ CLI already up to date.")
		return nil
	}

	if !newer {
		fmt.Fprintln(os.Stdout, "Reinstalling latest CLI (--force)...")
	} else {
		fmt.Fprintf(os.Stdout, "Updating CLI to %s...\n", release.TagName)
	}

	if err := updater.Apply(ctx, release); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "✓ CLI updated to %s.\n", release.TagName)
	return nil
}

// updateRunner installs the latest echopoint-runner binary the CLI uses for
// local ephemeral flow execution.
func updateRunner(ctx context.Context, checkOnly bool) error {
	release, err := updater.LatestRunnerRelease(ctx)
	if err != nil {
		return fmt.Errorf("check runner release: %w", err)
	}

	path, err := updater.RunnerBinaryPath()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Runner latest version: %s (install path: %s)\n", release.TagName, path)

	if checkOnly {
		return nil
	}

	fmt.Fprintf(os.Stdout, "Installing echopoint-runner %s...\n", release.TagName)
	if err := updater.ApplyRunner(ctx, release, path); err != nil {
		return fmt.Errorf("update runner: %w", err)
	}

	fmt.Fprintf(os.Stdout, "✓ Runner updated to %s at %s.\n", release.TagName, path)
	return nil
}
