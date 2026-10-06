package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var errPassTheFile = errors.New("pass the file with -f")

// fileFlagArgs is for a command that takes its file through -f: it accepts the
// given positionals and turns a leftover one, which is a file typed the old
// way, into a pointer at -f.
func fileFlagArgs(positionals int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > positionals {
			return errPassTheFile
		}
		if len(args) < positionals {
			return fmt.Errorf("accepts %d arg(s), received %d; usage: %s", positionals, len(args), cmd.UseLine())
		}
		return nil
	}
}

var errEmptyFile = errors.New("-f needs a file path")

// addFileFlag registers -f/--file, and refuses an -f given without a path: a
// shell variable that is unset must not turn "-f $FILE" into "no file", which on
// an optional -f would quietly mean another mode. A command reads whether a file
// was given with cmd.Flags().Changed("file"), never from the value.
func addFileFlag(cmd *cobra.Command, file *string, usage string, extensions ...string) {
	cmd.Flags().StringVarP(file, "file", "f", "", usage)
	_ = cmd.MarkFlagFilename("file", extensions...)
	other := cmd.Args
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("file") && strings.TrimSpace(*file) == "" {
			return errEmptyFile
		}
		if other == nil {
			return cobra.ArbitraryArgs(cmd, args)
		}
		return other(cmd, args)
	}
}
