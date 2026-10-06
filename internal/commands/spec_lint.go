package commands

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/nanostack-dev/echopoint-kit/apispec"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

type specLint struct {
	Spec     string            `json:"spec"`
	Version  string            `json:"version"`
	File     string            `json:"file,omitempty"`
	Findings []api.SpecFinding `json:"findings"`
}

func newSpecLintCmd(state *AppState) *cobra.Command {
	var (
		file, version  string
		failOnFindings bool
	)
	cmd := &cobra.Command{
		Use:   "lint <slug> [-f <file>]",
		Short: "Report where a spec, or what a file adds to it, departs from its conventions",
		Long: `Report the places where a spec departs from the conventions the rest of it
follows: the casing most properties and operation IDs use, the responses, error
shape, extensions, and security most operations declare, descriptions, and
declared path parameters.

Without -f, list the findings EchoPoint stored with a version of the spec
(default: Live). The ones the version introduced are marked new.

With -f, list only the findings the file adds compared with Live, judged by
Live's conventions: the check to run before 'echopoint spec push'.

Exits 0 even with findings, unless --fail-on-findings is set.`,
		Example: `  echopoint spec lint pets-api
  echopoint spec lint pets-api --version 1.3.0
  echopoint spec lint pets-api -f openapi.yaml --fail-on-findings`,
		Args: specArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fromFile := cmd.Flags().Changed("file")
			if fromFile && version != "" {
				return errors.New("--version and -f cannot be used together: a file is compared with Live")
			}
			if err := requireToken(state); err != nil {
				return err
			}
			var result specLint
			var err error
			if fromFile {
				result, err = lintFileAgainstLive(cmd.Context(), state, args[0], file)
			} else {
				result, err = lintStoredFindings(cmd.Context(), state, args[0], version)
			}
			if err != nil {
				return err
			}
			if err = printSpecLint(cmd, state, result); err != nil {
				return err
			}
			if failOnFindings && len(result.Findings) > 0 {
				return &exitCodeError{code: 1}
			}
			return nil
		},
	}
	addSpecFileFlag(cmd, &file, "OpenAPI file: report only the findings it adds to Live")
	cmd.Flags().StringVar(&version, "version", "", "Version whose stored findings to list (default: Live)")
	cmd.Flags().BoolVar(&failOnFindings, "fail-on-findings", false, "Exit non-zero when there is a finding")
	registerSpecVersionCompletion(state, cmd, "version")
	return finishSpecCmd(state, cmd)
}

func lintStoredFindings(ctx context.Context, state *AppState, name, version string) (specLint, error) {
	version, err := versionOrLive(ctx, state, name, version)
	if err != nil {
		return specLint{}, err
	}
	stored, err := fetchSpecVersion(ctx, state, name, version)
	if err != nil {
		return specLint{}, err
	}
	findings := stored.Findings
	if findings == nil {
		findings = []api.SpecFinding{}
	}
	return specLint{Spec: name, Version: stored.Version, Findings: findings}, nil
}

func lintFileAgainstLive(ctx context.Context, state *AppState, name, file string) (specLint, error) {
	live, err := pullSpec(ctx, state, name, "")
	if err != nil {
		return specLint{}, err
	}
	base, err := parsePulled(name, live)
	if err != nil {
		return specLint{}, err
	}
	document, err := readSpec(file)
	if err != nil {
		return specLint{}, err
	}
	added := apispec.LintChanges(base, document)
	findings := make([]api.SpecFinding, 0, len(added))
	for _, finding := range added {
		findings = append(findings, api.SpecFinding{
			Rule:       api.SpecLintRule(finding.Rule),
			Pointer:    finding.Pointer,
			Message:    finding.Message,
			Convention: finding.Convention,
			Evidence:   finding.Evidence,
			Introduced: true,
		})
	}
	return specLint{Spec: name, Version: live.Version, File: file, Findings: findings}, nil
}

func printSpecLint(cmd *cobra.Command, state *AppState, result specLint) error {
	w := cmd.OutOrStdout()
	if done, err := printStructured(w, state.OutputFormat, result); done {
		return err
	}
	subject := fmt.Sprintf("%s %s", result.Spec, result.Version)
	if result.File != "" {
		return printFileFindings(w, result, subject)
	}
	if len(result.Findings) == 0 {
		_, err := fmt.Fprintf(w, "✓ %s follows its conventions\n", subject)
		return err
	}
	introduced := 0
	for _, finding := range result.Findings {
		if finding.Introduced {
			introduced++
		}
	}
	fmt.Fprintf(w, "%s: %d finding(s), %d new\n", subject, len(result.Findings), introduced)
	printFindings(w, result.Findings, true)
	return nil
}

func printFileFindings(w io.Writer, result specLint, subject string) error {
	if len(result.Findings) == 0 {
		_, err := fmt.Fprintf(w, "✓ %s adds no findings to %s (Live)\n", result.File, subject)
		return err
	}
	fmt.Fprintf(w, "%s adds %d finding(s) to %s (Live)\n", result.File, len(result.Findings), subject)
	printFindings(w, result.Findings, false)
	return nil
}

func printFindings(w io.Writer, findings []api.SpecFinding, markNew bool) {
	for _, finding := range findings {
		marker := ""
		if markNew && finding.Introduced {
			marker = "  (new)"
		}
		fmt.Fprintf(w, "\n%s  %s%s\n  %s\n  Convention: %s\n", finding.Rule, finding.Pointer, marker,
			finding.Message, finding.Convention)
		if finding.Evidence != "" {
			fmt.Fprintf(w, "  Evidence: %s\n", finding.Evidence)
		}
	}
}
