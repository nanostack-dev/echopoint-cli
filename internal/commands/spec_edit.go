package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-kit/apispec"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/output"
)

// specEditAction is the kind of change an edit makes, for the summary line.
type specEditAction int

const (
	specEditAdd specEditAction = iota
	specEditUpdate
	specEditRemove
)

// specEdit is one edit command as the builders describe it: the commands that
// make the edit and the words that name it in the summary line.
type specEdit struct {
	action   specEditAction
	subject  string
	commands []apispec.Command
}

// what is the edit in words: "Added POST /pets".
func (e specEdit) what() string {
	switch e.action {
	case specEditAdd:
		return "Added " + e.subject
	case specEditRemove:
		return "Removed " + e.subject
	case specEditUpdate:
	}
	return "Updated " + e.subject
}

// applySpecCommands applies the commands of an edit to the Live version of a
// spec and reports the outcome. A refused command prints its reason and exits 1.
func applySpecCommands(cmd *cobra.Command, state *AppState, target *specLiveTarget, edit specEdit, dryRun bool) error {
	structured := state.OutputFormat == output.FormatJSON || state.OutputFormat == output.FormatYAML
	if dryRun && structured {
		return errors.New("--dry-run prints the edited document: it cannot be combined with -o json or -o yaml")
	}
	outcome, err := target.Apply(cmd.Context(), edit.commands, dryRun)
	if errors.Is(err, apispec.ErrCommand) {
		return printSpecRefusal(cmd.ErrOrStderr(), err)
	}
	if refusal, ok := errors.AsType[*specRefusedError](err); ok {
		fmt.Fprintf(cmd.ErrOrStderr(), "✗ %s\n", refusal.message)
		return &exitCodeError{code: 1}
	}
	if err != nil {
		return fmt.Errorf("%s: %w", target.name, err)
	}
	stdout := cmd.OutOrStdout()
	if dryRun {
		_, err = stdout.Write(outcome.document)
		return err
	}
	return printSpecPublished(stdout, state.OutputFormat, target.name, edit, outcome.published)
}

// printSpecRefusal prints a refused edit as "✗ reason" and exits 1.
func printSpecRefusal(stderr io.Writer, err error) error {
	reason := strings.TrimPrefix(err.Error(), apispec.ErrCommand.Error()+": ")
	if commandError, ok := errors.AsType[*apispec.CommandError](err); ok {
		reason = commandError.Reason
	}
	fmt.Fprintf(stderr, "✗ %s\n", reason)
	return &exitCodeError{code: 1}
}

// specEditFlags are the flags every spec edit command takes.
type specEditFlags struct {
	dryRun    bool
	live      bool
	commandID string
}

func (f *specEditFlags) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.live, "live", false, "Edit the Live version of the spec: publishes the next Live version")
	cmd.Flags().StringVar(&f.commandID, "command-id", "",
		"The UUID that makes a retry safe (default: a new one for each run)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Print the edited document instead of publishing it")
}

func (f *specEditFlags) target(state *AppState, name string) (*specLiveTarget, error) {
	if !f.live {
		return nil, errors.New("drafts are not available yet: pass --live to write the Live version")
	}
	commandID := uuid.New()
	if f.commandID != "" {
		parsed, err := uuid.Parse(f.commandID)
		if err != nil {
			return nil, fmt.Errorf("--command-id %q is not a UUID", f.commandID)
		}
		commandID = parsed
	}
	if err := requireToken(state); err != nil {
		return nil, err
	}
	return &specLiveTarget{state: state, name: name, commandID: commandID}, nil
}

// specEditBuilder turns the arguments (after the spec name), the flags and the
// document of an edit command into the edit.
type specEditBuilder func(flags flagReader, args []string, document []byte) (specEdit, error)

// newSpecEditCmd finishes a spec edit command: it adds the flags every edit
// takes and runs the builder against the Live document.
func newSpecEditCmd(state *AppState, cmd *cobra.Command, build specEditBuilder) *cobra.Command {
	var flags specEditFlags
	flags.register(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		target, err := flags.target(state, args[0])
		if err != nil {
			return err
		}
		document, err := target.Document(cmd.Context())
		if err != nil {
			return err
		}
		edit, err := build(flagReader{cmd}, args[1:], document)
		if errors.Is(err, apispec.ErrCommand) {
			return printSpecRefusal(cmd.ErrOrStderr(), err)
		}
		if err != nil {
			return err
		}
		return applySpecCommands(cmd, state, target, edit, flags.dryRun)
	}
	return finishSpecCmd(state, cmd)
}

// flagReader reads the flags of a command the way the builders need them:
// a flag that was not passed is nil, so an update can tell "set to empty"
// from "leave alone".
type flagReader struct{ cmd *cobra.Command }

func (r flagReader) str(name string) string {
	value, _ := r.cmd.Flags().GetString(name)
	return value
}

func (r flagReader) boolean(name string) bool {
	value, _ := r.cmd.Flags().GetBool(name)
	return value
}

func (r flagReader) strs(name string) []string {
	value, _ := r.cmd.Flags().GetStringSlice(name)
	return value
}

func (r flagReader) changed(name string) bool {
	return r.cmd.Flags().Changed(name)
}

func (r flagReader) optStr(name string) *string {
	if !r.changed(name) {
		return nil
	}
	value := r.str(name)
	return &value
}

func (r flagReader) optBool(name string) *bool {
	if !r.changed(name) {
		return nil
	}
	value := r.boolean(name)
	return &value
}
