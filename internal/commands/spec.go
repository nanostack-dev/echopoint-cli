package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nanostack-dev/echopoint-kit/apispec"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/output"
)

// offlineAnnotation marks a command that works on local files and never
// needs credentials.
const offlineAnnotation = "echopoint/offline"

func offline() map[string]string {
	return map[string]string{offlineAnnotation: annotationEnabled}
}

func newSpecCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   specCommandName,
		Short: "Validate, format, and diff OpenAPI specs",
		Long: `Work with OpenAPI 3.0 and 3.1 documents.

validate, fmt, and diff work on local files and need no account. list, push,
pull, and check work with the specs EchoPoint keeps.`,
	}
	cmd.AddCommand(
		newSpecValidateCmd(state),
		newSpecFmtCmd(),
		newSpecDiffCmd(state),
		newSpecListCmd(state),
		newSpecPushCmd(state),
		newSpecPullCmd(state),
		newSpecCheckCmd(state),
	)
	return cmd
}

type specValidation struct {
	File     string   `json:"file"     yaml:"file"`
	Valid    bool     `json:"valid"    yaml:"valid"`
	OpenAPI  string   `json:"openapi"  yaml:"openapi"`
	Problems []string `json:"problems" yaml:"problems"`
}

func newSpecValidateCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file>",
		Short: "Check that a file is a valid, self-contained OpenAPI 3.0 or 3.1 document",
		Long: `Check a local OpenAPI document. Swagger 2.0, other OpenAPI versions, and
external $ref are refused. Exits non-zero when the document has a problem.`,
		Args:          cobra.ExactArgs(1),
		Annotations:   offline(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			result := validateSpecFile(args[0])
			if err := printSpecValidation(
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
				state.OutputFormat,
				result,
			); err != nil {
				return err
			}
			if !result.Valid {
				return &exitCodeError{code: 1}
			}
			return nil
		},
	}
}

func validateSpecFile(path string) specValidation {
	result := specValidation{File: path, Problems: []string{}}
	document, err := readSpec(path)
	if err == nil {
		result.OpenAPI = document.OpenAPIVersion()
		err = document.Validate()
	}
	if err != nil {
		result.Problems = specProblems(err)
		return result
	}
	result.Valid = true
	return result
}

func specProblems(err error) []string {
	if validationError, ok := errors.AsType[*apispec.ValidationError](err); ok {
		return validationError.Problems
	}
	if refError, ok := errors.AsType[*apispec.ExternalRefError](err); ok {
		problems := make([]string, 0, len(refError.Refs))
		for _, ref := range refError.Refs {
			problems = append(problems, fmt.Sprintf("external $ref %q at %s: bundle the document into one file first",
				ref.Ref, ref.Pointer))
		}
		return problems
	}
	return []string{err.Error()}
}

func printSpecValidation(stdout, stderr io.Writer, format output.Format, result specValidation) error {
	switch format {
	case output.FormatJSON:
		return output.PrintJSON(stdout, result)
	case output.FormatYAML:
		return output.PrintYAML(stdout, result)
	case output.FormatTable:
	}
	if result.Valid {
		_, err := fmt.Fprintf(stdout, "✓ %s is a valid OpenAPI %s document\n", result.File, result.OpenAPI)
		return err
	}
	fmt.Fprintf(stderr, "✗ %s has %d problem(s):\n", result.File, len(result.Problems))
	for i, problem := range result.Problems {
		fmt.Fprintf(stderr, "  %d. %s\n", i+1, problem)
	}
	return nil
}

func newSpecFmtCmd() *cobra.Command {
	var write, check bool
	cmd := &cobra.Command{
		Use:   "fmt <file>",
		Short: "Write an OpenAPI document in EchoPoint's canonical YAML layout",
		Long: `Print the document in the canonical YAML layout that EchoPoint stores and
'echopoint spec pull' writes: OpenAPI fields in a fixed order, two-space
indentation, no comments. JSON input is written as YAML.

--write rewrites the file in place. --check writes nothing and exits non-zero
when the file is not in the canonical layout.`,
		Args:          cobra.ExactArgs(1),
		Annotations:   offline(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if write && check {
				return errors.New("--write and --check cannot be used together")
			}
			path := args[0]
			current, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			document, err := apispec.Parse(current)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			canonical, err := document.Canonical()
			if err != nil {
				return err
			}

			switch {
			case check:
				if bytes.Equal(current, canonical) {
					return nil
				}
				fmt.Fprintf(
					cmd.ErrOrStderr(),
					"%s is not in the canonical layout; run 'echopoint spec fmt --write %s'\n",
					path,
					path,
				)
				return &exitCodeError{code: 1}
			case write:
				return os.WriteFile(path, canonical, 0o644)
			}
			_, err = cmd.OutOrStdout().Write(canonical)
			return err
		},
	}
	cmd.Flags().BoolVarP(&write, "write", "w", false, "Rewrite the file in place")
	cmd.Flags().BoolVar(&check, "check", false, "Exit non-zero if the file is not in the canonical layout")
	return cmd
}

type specDiff struct {
	Bump    apispec.Bump     `json:"bump"    yaml:"bump"`
	Changes []specDiffChange `json:"changes" yaml:"changes"`
}

type specDiffChange struct {
	ID       string           `json:"id"                yaml:"id"`
	Severity apispec.Severity `json:"severity"          yaml:"severity"`
	Method   string           `json:"method,omitempty"  yaml:"method,omitempty"`
	Path     string           `json:"path,omitempty"    yaml:"path,omitempty"`
	Pointer  string           `json:"pointer,omitempty" yaml:"pointer,omitempty"`
	Text     string           `json:"text"              yaml:"text"`
}

func newSpecDiffCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <base> <revision>",
		Short: "Show what changed between two OpenAPI documents and the version bump it calls for",
		Long: `Compare two local OpenAPI documents. Changes are grouped by what they mean
for clients: breaking, risky (needs a look), additive, and edits. The bump is
the one EchoPoint would apply to the Live version: major, minor, patch, or none.
info.version is ignored.`,
		Args:          cobra.ExactArgs(2), //nolint:mnd // base and revision
		Annotations:   offline(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := readSpec(args[0])
			if err != nil {
				return err
			}
			revision, err := readSpec(args[1])
			if err != nil {
				return err
			}
			comparison, err := apispec.Compare(base, revision)
			if err != nil {
				return err
			}
			return printSpecDiff(cmd.OutOrStdout(), state.OutputFormat, toSpecDiff(comparison))
		},
	}
}

func toSpecDiff(comparison apispec.Comparison) specDiff {
	changes := make([]specDiffChange, 0, len(comparison.Changes))
	for _, change := range comparison.Changes {
		changes = append(changes, specDiffChange{
			ID:       change.ID,
			Severity: change.Severity,
			Method:   change.Method,
			Path:     change.Path,
			Pointer:  change.Pointer,
			Text:     change.Text,
		})
	}
	return specDiff{Bump: comparison.Bump, Changes: changes}
}

func printSpecDiff(w io.Writer, format output.Format, diff specDiff) error {
	switch format {
	case output.FormatJSON:
		return output.PrintJSON(w, diff)
	case output.FormatYAML:
		return output.PrintYAML(w, diff)
	case output.FormatTable:
	}
	if diff.Bump == apispec.BumpNone {
		_, err := fmt.Fprintln(w, "No changes.")
		return err
	}
	groups := []struct {
		severity apispec.Severity
		title    string
	}{
		{apispec.SeverityBreaking, "Breaks clients"},
		{apispec.SeverityRisky, "Needs a look"},
		{apispec.SeverityAdditive, "Adds"},
		{apispec.SeverityEdit, "Edits"},
	}
	for _, group := range groups {
		var lines []string
		for _, change := range diff.Changes {
			if change.Severity == group.severity {
				lines = append(lines, "  - "+changeLine(change))
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(w, "%s (%d)\n%s\n\n", group.title, len(lines), strings.Join(lines, "\n"))
		}
	}
	_, err := fmt.Fprintf(w, "Version bump: %s\n", diff.Bump)
	return err
}

func changeLine(change specDiffChange) string {
	if change.Method == "" || change.Path == "" {
		return change.Text
	}
	return fmt.Sprintf("%s %s: %s", change.Method, change.Path, change.Text)
}

func readSpec(path string) (*apispec.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	document, err := apispec.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return document, nil
}
