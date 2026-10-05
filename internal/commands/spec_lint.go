package commands

import (
	"errors"
	"fmt"
	"io"

	"github.com/nanostack-dev/echopoint-kit/apispec"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/output"
)

type specLint struct {
	File     string            `json:"file"     yaml:"file"`
	Base     string            `json:"base"     yaml:"base"`
	Findings []apispec.Finding `json:"findings" yaml:"findings"`
}

func newSpecLintCmd(state *AppState) *cobra.Command {
	var (
		file, base, slug string
		failOnFindings   bool
	)
	cmd := &cobra.Command{
		Use:   "lint [file]",
		Short: "Report where an OpenAPI document departs from its own conventions",
		Long: `Report the places where a document departs from the conventions the rest of
it follows: the casing most properties and operation IDs use, the responses,
error shape, extensions, and security most operations declare, descriptions,
and declared path parameters. EchoPoint stores the same findings with every
Live version, and the editor shows them while you type.

--base reports only what the document adds compared with another local file,
judged by that file's conventions. --spec does the same against the spec's Live
version in EchoPoint, so a change can be checked before it is pushed.

Exits 0 even with findings, unless --fail-on-findings is set.`,
		Args:          cobra.MaximumNArgs(1),
		Annotations:   offlineUnless("spec"),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := fileArgument(file, args)
			if err != nil {
				return err
			}
			if base != "" && slug != "" {
				return errors.New("--base and --spec cannot be used together")
			}
			document, err := readSpec(path)
			if err != nil {
				return err
			}
			result := specLint{File: path, Base: base}
			switch {
			case slug != "":
				if err := requireToken(state); err != nil {
					return err
				}
				live, pullErr := pullSpec(state, slug, "")
				if pullErr != nil {
					return pullErr
				}
				liveDocument, parseErr := apispec.Parse([]byte(live.Document))
				if parseErr != nil {
					return fmt.Errorf("Live version of %s: %w", slug, parseErr)
				}
				result.Base = fmt.Sprintf("%s (Live %s)", slug, live.Version)
				result.Findings = apispec.LintChanges(liveDocument, document)
			case base != "":
				baseDocument, readErr := readSpec(base)
				if readErr != nil {
					return readErr
				}
				result.Findings = apispec.LintChanges(baseDocument, document)
			default:
				result.Findings = document.Lint()
			}
			if result.Findings == nil {
				result.Findings = []apispec.Finding{}
			}
			if err := printSpecLint(cmd.OutOrStdout(), state.OutputFormat, result); err != nil {
				return err
			}
			if failOnFindings && len(result.Findings) > 0 {
				return &exitCodeError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "OpenAPI document to lint (or pass it as the argument)")
	cmd.Flags().StringVar(&base, "base", "", "Report only findings the document adds compared with this file")
	cmd.Flags().
		StringVar(&slug, "spec", "", "Report only findings the document adds compared with this spec's Live version")
	cmd.Flags().BoolVar(&failOnFindings, "fail-on-findings", false, "Exit non-zero when there is a finding")
	return cmd
}

func fileArgument(file string, args []string) (string, error) {
	switch {
	case file != "" && len(args) > 0:
		return "", errors.New("pass the file as the argument or with --file, not both")
	case file != "":
		return file, nil
	case len(args) > 0:
		return args[0], nil
	}
	return "", errors.New("name the OpenAPI document to read, as the argument or with --file")
}

func printSpecLint(w io.Writer, format output.Format, result specLint) error {
	switch format {
	case output.FormatJSON:
		return output.PrintJSON(w, result)
	case output.FormatYAML:
		return output.PrintYAML(w, result)
	case output.FormatTable:
	}
	if len(result.Findings) == 0 {
		_, err := fmt.Fprintf(w, "✓ %s follows its conventions\n", result.File)
		return err
	}
	fmt.Fprintf(w, "%s: %d finding(s)\n", result.File, len(result.Findings))
	for _, finding := range result.Findings {
		fmt.Fprintf(w, "\n%s  %s\n  %s\n  Convention: %s\n", finding.Rule, finding.Pointer, finding.Message,
			finding.Convention)
		if finding.Evidence != "" {
			fmt.Fprintf(w, "  Evidence: %s\n", finding.Evidence)
		}
	}
	return nil
}
