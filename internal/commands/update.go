package commands

import (
	"fmt"
	"os"

	"echopoint-cli/internal/updater"

	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var (
		checkOnly bool
		force     bool
	)

	cmd := &cobra.Command{
		Use:   updateCommandName,
		Short: "Update the CLI to the latest release",
		Long: `Download and install the latest echopoint CLI release from GitHub.

The running binary is replaced in place after its checksum is verified. Use
--check to see whether an update is available without installing it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			current := Version()

			release, err := updater.LatestRelease(cmd.Context())
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "Current version: %s\n", current)
			fmt.Fprintf(os.Stdout, "Latest version:  %s\n", release.TagName)

			newer := updater.IsNewer(current, release.TagName)
			if !newer && !force {
				fmt.Fprintln(os.Stdout, "✓ Already up to date.")
				return nil
			}

			if checkOnly {
				if newer {
					fmt.Fprintf(os.Stdout, "An update is available: %s\n", release.HTMLURL)
				}
				return nil
			}

			if !newer && force {
				fmt.Fprintln(os.Stdout, "Reinstalling latest version (--force)...")
			} else {
				fmt.Fprintf(os.Stdout, "Updating to %s...\n", release.TagName)
			}

			if err := updater.Apply(cmd.Context(), release); err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "✓ Updated to %s. Run 'echopoint version' to confirm.\n", release.TagName)
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for an update; do not install")
	cmd.Flags().BoolVar(&force, "force", false, "Reinstall even if already up to date")

	return cmd
}
