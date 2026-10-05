package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanostack-dev/echopoint-kit/apispec"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

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

func (e specEdit) summary(name string) string {
	switch e.action {
	case specEditAdd:
		return fmt.Sprintf("Added %s to %s", e.subject, name)
	case specEditRemove:
		return fmt.Sprintf("Removed %s from %s", e.subject, name)
	case specEditUpdate:
	}
	return fmt.Sprintf("Updated %s in %s", e.subject, name)
}

type specEditOutcome struct {
	document []byte
	changed  bool
}

// specEditTarget is where an edit lands. A local file is the only target
// today; the Live version of a spec in EchoPoint is the next one, and takes
// the same commands.
type specEditTarget interface {
	// Name is what the summary line and the JSON output call the target.
	Name() string
	// Document is the document the commands apply to, for the builders that
	// search it by what it holds.
	Document() ([]byte, error)
	// Apply makes the edit. With dryRun it leaves the target untouched.
	Apply(commands []apispec.Command, dryRun bool) (specEditOutcome, error)
}

type specFileTarget struct {
	path string
	data []byte
}

func (t *specFileTarget) Name() string { return t.path }

func (t *specFileTarget) Document() ([]byte, error) {
	if t.data == nil {
		data, err := os.ReadFile(t.path)
		if err != nil {
			return nil, err
		}
		t.data = data
	}
	return t.data, nil
}

func (t *specFileTarget) Apply(commands []apispec.Command, dryRun bool) (specEditOutcome, error) {
	data, err := t.Document()
	if err != nil {
		return specEditOutcome{}, err
	}
	edited, err := apispec.Edit(data, commands...)
	if err != nil {
		return specEditOutcome{}, err
	}
	outcome := specEditOutcome{document: edited, changed: string(edited) != string(data)}
	if outcome.changed && !dryRun {
		if err = writeFileAtomic(t.path, edited); err != nil {
			return specEditOutcome{}, err
		}
	}
	return outcome, nil
}

// writeFileAtomic replaces a file with a temporary file renamed over it, so a
// failed write never leaves a half-written document. The file keeps its mode,
// and a symlink keeps pointing at the file it names.
func writeFileAtomic(path string, data []byte) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(resolved), "."+filepath.Base(resolved)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Chmod(info.Mode().Perm()); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), resolved)
}

// specEditResult is the structured output of an edit command.
type specEditResult struct {
	File     string            `json:"file"     yaml:"file"`
	Commands []apispec.Command `json:"commands" yaml:"commands"`
	Changed  bool              `json:"changed"  yaml:"changed"`
}

// applySpecCommands applies the commands of an edit to the target and reports
// the outcome. A refused command prints its reason and exits 1.
func applySpecCommands(cmd *cobra.Command, state *AppState, target specEditTarget, edit specEdit, dryRun bool) error {
	structured := state.OutputFormat == output.FormatJSON || state.OutputFormat == output.FormatYAML
	if dryRun && structured {
		return errors.New("--dry-run prints the edited document: it cannot be combined with -o json or -o yaml")
	}
	outcome, err := target.Apply(edit.commands, dryRun)
	if errors.Is(err, apispec.ErrCommand) {
		return printSpecRefusal(cmd.ErrOrStderr(), err)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", target.Name(), err)
	}
	stdout := cmd.OutOrStdout()
	if dryRun {
		_, err = stdout.Write(outcome.document)
		return err
	}
	result := specEditResult{File: target.Name(), Commands: edit.commands, Changed: outcome.changed}
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(stdout, result)
	case output.FormatYAML:
		return printSpecEditYAML(stdout, result)
	case output.FormatTable:
	}
	if !outcome.changed {
		_, err = fmt.Fprintf(stdout, "✓ Nothing to change in %s\n", target.Name())
		return err
	}
	_, err = fmt.Fprintf(stdout, "✓ %s\n", edit.summary(target.Name()))
	return err
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

// printSpecEditYAML writes the result with the key names of its JSON form:
// apispec.Command has only JSON tags.
func printSpecEditYAML(w io.Writer, result specEditResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil {
		return err
	}
	clearYAMLStyle(&node)
	return output.PrintYAML(w, &node)
}

func clearYAMLStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		clearYAMLStyle(child)
	}
}

// specEditFlags are the flags every spec edit command takes.
type specEditFlags struct {
	file   string
	dryRun bool
	spec   string
	live   bool
}

func (f *specEditFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.file, "file", "", "OpenAPI document to edit in place (required)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Print the edited document instead of writing it")
	// --spec and --live edit the Live version in EchoPoint. The flags exist so a
	// command that sets --spec asks for credentials like the other spec
	// commands; they stay hidden until that mode ships.
	cmd.Flags().StringVar(&f.spec, "spec", "", "Spec to edit in EchoPoint")
	cmd.Flags().BoolVar(&f.live, "live", false, "Edit the Live version of the spec")
	_ = cmd.Flags().MarkHidden("spec")
	_ = cmd.Flags().MarkHidden("live")
}

func (f *specEditFlags) target() (specEditTarget, error) {
	if f.spec != "" || f.live {
		return nil, errors.New(
			"editing the Live version of a spec with --spec and --live is not available yet: edit a local file with --file",
		)
	}
	if f.file == "" {
		return nil, errors.New("name the OpenAPI document to edit with --file")
	}
	return &specFileTarget{path: f.file}, nil
}

// specEditBuilder turns the arguments, the flags and the document of an edit
// command into the edit.
type specEditBuilder func(flags flagReader, args []string, document []byte) (specEdit, error)

// newSpecEditCmd finishes a spec edit command: it adds the flags every edit
// takes and runs the builder against the target's document.
func newSpecEditCmd(state *AppState, cmd *cobra.Command, build specEditBuilder) *cobra.Command {
	var flags specEditFlags
	flags.register(cmd)
	cmd.Annotations = offlineUnless("spec")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		target, err := flags.target()
		if err != nil {
			return err
		}
		document, err := target.Document()
		if err != nil {
			return err
		}
		edit, err := build(flagReader{cmd}, args, document)
		if errors.Is(err, apispec.ErrCommand) {
			return printSpecRefusal(cmd.ErrOrStderr(), err)
		}
		if err != nil {
			return err
		}
		return applySpecCommands(cmd, state, target, edit, flags.dryRun)
	}
	return cmd
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
