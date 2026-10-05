package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
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

// summary is the edit as a line about a file: "Added POST /pets to openapi.yaml".
func (e specEdit) summary(name string) string {
	preposition := "in"
	switch e.action {
	case specEditAdd:
		preposition = "to"
	case specEditRemove:
		preposition = "from"
	case specEditUpdate:
	}
	return fmt.Sprintf("%s %s %s", e.what(), preposition, name)
}

type specEditOutcome struct {
	document []byte
	changed  bool
	// published is the Live version an edit of a spec in EchoPoint made; nil
	// for an edit of a file.
	published *specPublished
}

// specEditTarget is where an edit lands: a local file, or the Live version of
// a spec in EchoPoint. Both take the same commands.
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
	if refusal, ok := errors.AsType[*specRefusedError](err); ok {
		fmt.Fprintf(cmd.ErrOrStderr(), "✗ %s\n", refusal.message)
		return &exitCodeError{code: 1}
	}
	if err != nil {
		return fmt.Errorf("%s: %w", target.Name(), err)
	}
	stdout := cmd.OutOrStdout()
	if dryRun {
		_, err = stdout.Write(outcome.document)
		return err
	}
	if outcome.published != nil {
		return printSpecPublished(stdout, state.OutputFormat, target.Name(), edit, outcome.published)
	}
	result := specEditResult{File: target.Name(), Commands: edit.commands, Changed: outcome.changed}
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(stdout, result)
	case output.FormatYAML:
		return printJSONAsYAML(stdout, result)
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

// printJSONAsYAML writes a value as YAML with the key names of its JSON form:
// apispec.Command and the generated API types have only JSON tags.
func printJSONAsYAML(w io.Writer, result any) error {
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
	file      string
	dryRun    bool
	spec      string
	live      bool
	commandID string
}

func (f *specEditFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.file, "file", "", "OpenAPI document to edit in place")
	cmd.Flags().StringVar(&f.spec, "spec", "", "Spec in EchoPoint to edit, with --live (instead of --file)")
	cmd.Flags().BoolVar(&f.live, "live", false, "Edit the Live version of the spec: publishes the next Live version")
	cmd.Flags().StringVar(&f.commandID, "command-id", "",
		"With --spec: the UUID that makes a retry safe (default: a new one for each run)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Print the edited document instead of writing or publishing it")
}

func (f *specEditFlags) target(state *AppState) (specEditTarget, error) {
	switch {
	case f.file != "" && f.spec != "":
		return nil, errors.New("--file and --spec cannot be used together")
	case f.spec != "":
		return f.liveTarget(state)
	case f.live:
		return nil, errors.New("--live needs --spec <slug>")
	case f.commandID != "":
		return nil, errors.New("--command-id only applies to an edit of a spec in EchoPoint: use --spec with --live")
	case f.file == "":
		return nil, errors.New(
			"name the OpenAPI document to edit with --file, or a spec in EchoPoint with --spec <slug> --live")
	}
	return &specFileTarget{path: f.file}, nil
}

func (f *specEditFlags) liveTarget(state *AppState) (specEditTarget, error) {
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
	return &specLiveTarget{state: state, slug: f.spec, commandID: commandID}, nil
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
		target, err := flags.target(state)
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
