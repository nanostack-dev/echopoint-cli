package commands

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

// Build information, injected via -ldflags at release time and wired in from
// main. Defaults are used for `go run` / source builds.
var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// SetBuildInfo wires the linker-injected build metadata from main.
func SetBuildInfo(version, commit, date string) {
	if version != "" {
		buildVersion = normalizeVersion(version)
	}
	if commit != "" {
		buildCommit = commit
	}
	if date != "" {
		buildDate = date
	}
}

// normalizeVersion prefixes a bare semantic version (e.g. "0.3.0", as injected
// by GoReleaser's {{.Version}}) with "v" so the CLI displays "v0.3.0",
// consistent with the git tags. Non-semver values (e.g. "dev") pass through.
func normalizeVersion(version string) string {
	if len(version) > 0 && version[0] >= '0' && version[0] <= '9' {
		return "v" + version
	}
	return version
}

// Version returns the current CLI version string (e.g. "v0.2.0" or "dev").
func Version() string {
	return buildVersion
}

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the CLI version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if short {
				fmt.Fprintln(os.Stdout, buildVersion)
				return nil
			}
			fmt.Fprintf(os.Stdout, "echopoint %s\n", buildVersion)
			fmt.Fprintf(os.Stdout, "commit:   %s\n", buildCommit)
			fmt.Fprintf(os.Stdout, "built:    %s\n", buildDate)
			fmt.Fprintf(os.Stdout, "go:       %s\n", runtime.Version())
			fmt.Fprintf(os.Stdout, "platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "Print only the version string")
	return cmd
}
