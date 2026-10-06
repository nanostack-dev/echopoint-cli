package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// cancelledError ends a command the user declined, with the cancelled exit code.
type cancelledError struct{}

func (cancelledError) Error() string { return "cancelled" }

func (cancelledError) ExitCode() int { return exitCancelled }

// confirmDestructive gates a destructive command: --yes skips the question, a
// terminal gets asked "<Verb> <thing>? [y/N]" on stderr, and anything else
// refuses, since nobody is there to answer.
func confirmDestructive(cmd *cobra.Command, state *AppState, verb, thing string) error {
	if state.AssumeYes {
		return nil
	}
	if !state.interactive() {
		return fmt.Errorf("pass --yes to %s %s without a prompt", verb, thing)
	}
	prompt := cmd.ErrOrStderr()
	fmt.Fprintf(prompt, "%s %s? [y/N] ", strings.ToUpper(verb[:1])+verb[1:], thing)
	answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return cancelledError{}
}

// quietOnError leaves printing a command's error to main, so a declined or
// refused command does not add cobra's own "Error:" line and usage.
func quietOnError(cmd *cobra.Command) *cobra.Command {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}

func (state *AppState) interactive() bool {
	if state.IsTerminal != nil {
		return state.IsTerminal()
	}
	return stdioIsTerminal()
}

func stdioIsTerminal() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stdout)
}

// isTerminal tells a terminal from a pipe, a file, or any other character device
// such as /dev/null or /dev/zero.
func isTerminal(file *os.File) bool {
	fd := file.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}
