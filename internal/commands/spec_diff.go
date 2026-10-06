package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nanostack-dev/echopoint-kit/apispec"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

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
	var file, from, to string
	cmd := &cobra.Command{
		Use:   "diff <slug> [-f <file>]",
		Short: "Show what changed between two versions of a spec, or what a file would change in Live",
		Long: `Show what changed in a spec, grouped by what it means for clients: breaking,
risky (needs a look), additive, and edits. The bump is the one EchoPoint applies
to a Live version: major, minor, patch, or none. info.version is ignored.

Without -f, compare two versions of the spec. --to defaults to Live and --from
to the version right before --to.

With -f, compare the file with Live: what 'echopoint spec push' would change,
and the bump it would make.`,
		Example: `  echopoint spec diff pets-api
  echopoint spec diff pets-api --from 1.2.0 --to 1.4.0
  echopoint spec diff pets-api -f openapi.yaml`,
		Args: specArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fromFile := cmd.Flags().Changed("file")
			if fromFile && (from != "" || to != "") {
				return errors.New("--from and --to cannot be used with -f: a file is compared with Live")
			}
			if err := requireToken(state); err != nil {
				return err
			}
			var diff specDiff
			var heading string
			var err error
			if fromFile {
				diff, heading, err = diffFileWithLive(cmd.Context(), state, args[0], file)
			} else {
				diff, heading, err = diffVersions(cmd.Context(), state, args[0], from, to)
			}
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if state.OutputFormat == output.FormatTable {
				fmt.Fprintf(w, "%s\n\n", heading)
			}
			return printSpecDiff(w, state.OutputFormat, diff)
		},
	}
	addSpecFileFlag(cmd, &file, "OpenAPI file to compare with Live")
	cmd.Flags().StringVar(&from, "from", "", "Version to compare from (default: the version before --to)")
	cmd.Flags().StringVar(&to, "to", "", "Version to compare to (default: Live)")
	registerSpecVersionCompletion(state, cmd, "from")
	registerSpecVersionCompletion(state, cmd, "to")
	return finishSpecCmd(state, cmd)
}

func diffVersions(ctx context.Context, state *AppState, name, from, to string) (specDiff, string, error) {
	toVersion, err := versionOrLive(ctx, state, name, to)
	if err != nil {
		return specDiff{}, "", err
	}
	if from == "" {
		version, fetchErr := fetchSpecVersion(ctx, state, name, toVersion)
		if fetchErr != nil {
			return specDiff{}, "", fetchErr
		}
		if version.Bump == api.Initial {
			return specDiff{}, "", fmt.Errorf(
				"%s %s is the first version of the spec: nothing is before it to compare with",
				name,
				toVersion,
			)
		}
		return specDiffOfVersion(version), fmt.Sprintf("Changes in %s %s against the version before it",
			name, toVersion), nil
	}
	base, err := pullDocument(ctx, state, name, from)
	if err != nil {
		return specDiff{}, "", err
	}
	revision, err := pullDocument(ctx, state, name, toVersion)
	if err != nil {
		return specDiff{}, "", err
	}
	comparison, err := apispec.Compare(base, revision)
	if err != nil {
		return specDiff{}, "", err
	}
	return toSpecDiff(comparison), fmt.Sprintf("Changes in %s from %s to %s", name, from, toVersion), nil
}

func diffFileWithLive(ctx context.Context, state *AppState, name, file string) (specDiff, string, error) {
	live, err := pullSpec(ctx, state, name, "")
	if err != nil {
		return specDiff{}, "", err
	}
	base, err := parsePulled(name, live)
	if err != nil {
		return specDiff{}, "", err
	}
	revision, err := readSpec(file)
	if err != nil {
		return specDiff{}, "", err
	}
	comparison, err := apispec.Compare(base, revision)
	if err != nil {
		return specDiff{}, "", err
	}
	return toSpecDiff(comparison), fmt.Sprintf("Pushing %s would change %s %s (Live)", file, name, live.Version), nil
}

func pullDocument(ctx context.Context, state *AppState, name, version string) (*apispec.Document, error) {
	pulled, err := pullSpec(ctx, state, name, version)
	if err != nil {
		return nil, err
	}
	return parsePulled(name, pulled)
}

func parsePulled(name string, pulled *api.SpecDocument) (*apispec.Document, error) {
	document, err := apispec.Parse([]byte(pulled.Document))
	if err != nil {
		return nil, fmt.Errorf("version %s of %s: %w", pulled.Version, name, err)
	}
	return document, nil
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

func specDiffOfVersion(version *api.SpecVersion) specDiff {
	changes := make([]specDiffChange, 0, len(version.Changes))
	for _, change := range version.Changes {
		changes = append(changes, specDiffChange{
			ID:       change.Id,
			Severity: apispec.Severity(change.Severity),
			Method:   valueOf(change.Method),
			Path:     valueOf(change.Path),
			Pointer:  valueOf(change.Pointer),
			Text:     change.Text,
		})
	}
	return specDiff{Bump: apispec.Bump(version.Bump), Changes: changes}
}

func valueOf(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

func printSpecDiff(w io.Writer, format output.Format, diff specDiff) error {
	if done, err := printStructured(w, format, diff); done {
		return err
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
