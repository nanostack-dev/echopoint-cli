package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nanostack-dev/echopoint-kit/apispec"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const (
	specFlagUsage = "Slug of the spec in EchoPoint"
	specPageSize  = 100
)

func newSpecListCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:           listVerb,
		Short:         "List the organization's specs and their Live versions",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().ListSpecsWithResponse(context.Background(), &api.ListSpecsParams{
				Limit: api.LimitParameter(specPageSize),
			})
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(cmd.OutOrStdout(), resp.JSON200)
			case output.FormatYAML:
				return output.PrintYAML(cmd.OutOrStdout(), resp.JSON200)
			case output.FormatTable:
			}
			rows := make([][]string, 0, len(resp.JSON200.Items))
			for _, item := range resp.JSON200.Items {
				rows = append(rows, []string{
					item.Slug, item.Title, item.Live.Version, string(item.Live.Bump),
					item.Live.CreatedAt.Format("2006-01-02 15:04"),
				})
			}
			return output.PrintTable([]string{"SLUG", "TITLE", "LIVE", "BUMP", "PUBLISHED"}, rows)
		},
	}
}

func newSpecPushCmd(state *AppState) *cobra.Command {
	var (
		slug      string
		createNew bool
		bundle    bool
	)
	cmd := &cobra.Command{
		Use:   "push <file>",
		Short: "Publish a local OpenAPI document as the spec's next Live version",
		Long: `Publish a local OpenAPI document to EchoPoint. EchoPoint compares it with the
Live version, computes the next version from the changes (breaking -> major,
additions -> minor, other edits -> patch), and keeps the history.

--new creates the spec with this document as its first Live version.
--bundle inlines external $ref (other files, URLs) before pushing; without it, a
document with an external $ref is refused.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			document, err := readDocumentForPush(args[0], bundle)
			if err != nil {
				return err
			}
			if createNew {
				return createSpec(cmd.OutOrStdout(), state, slug, document)
			}
			return pushSpecVersion(cmd.OutOrStdout(), state, slug, document)
		},
	}
	cmd.Flags().StringVar(&slug, "spec", "", specFlagUsage)
	cmd.Flags().BoolVar(&createNew, "new", false, "Create the spec")
	cmd.Flags().BoolVar(&bundle, "bundle", false, "Inline external $ref before pushing")
	_ = cmd.MarkFlagRequired("spec")
	return cmd
}

// readDocumentForPush reads the file as is, or, with bundle, loads it with its
// external references and inlines them into components.
func readDocumentForPush(path string, bundle bool) (string, error) {
	if !bundle {
		data, err := os.ReadFile(path)
		return string(data), err
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	document, err := loader.LoadFromFile(path)
	if err != nil {
		return "", fmt.Errorf("bundle %s: %w", path, err)
	}
	document.InternalizeRefs(context.Background(), nil)
	bundled, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("bundle %s: %w", path, err)
	}
	return string(bundled), nil
}

func createSpec(w io.Writer, state *AppState, slug, document string) error {
	resp, err := state.Client.API().CreateSpecWithResponse(context.Background(), nil, api.CreateSpecRequest{
		Slug:     slug,
		Document: document,
	})
	if err != nil {
		return err
	}
	if resp.JSON201 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(w, resp.JSON201)
	case output.FormatYAML:
		return output.PrintYAML(w, resp.JSON201)
	case output.FormatTable:
	}
	_, err = fmt.Fprintf(w, "✓ Created spec %s: Live version %s\n", resp.JSON201.Slug, resp.JSON201.Live.Version)
	return err
}

func pushSpecVersion(w io.Writer, state *AppState, slug, document string) error {
	resp, err := state.Client.API().PushSpecVersionWithResponse(context.Background(), slug, nil,
		api.PushSpecVersionRequest{Document: document})
	if err != nil {
		return err
	}
	if resp.JSON201 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	pushed := resp.JSON201
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(w, pushed)
	case output.FormatYAML:
		return output.PrintYAML(w, pushed)
	case output.FormatTable:
	}
	fmt.Fprintf(w, "✓ Published %s %s (%s)\n\n", slug, pushed.Version, pushed.Bump)
	return printSpecDiff(w, output.FormatTable, specDiffOfVersion(pushed))
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

func newSpecPullCmd(state *AppState) *cobra.Command {
	var slug, version string
	cmd := &cobra.Command{
		Use:   "pull <file>",
		Short: "Write the spec's Live version to a file, in the canonical YAML layout",
		Long: `Write a spec's Live version to a local file, byte for byte as EchoPoint keeps
it. The file is generated: edit the spec in EchoPoint, or push a change, and pull
again. --version pulls an earlier Live version.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			pulled, err := pullSpec(state, slug, version)
			if err != nil {
				return err
			}
			if err := os.WriteFile(args[0], []byte(pulled.Document), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Wrote %s %s to %s\n", slug, pulled.Version, args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "spec", "", specFlagUsage)
	cmd.Flags().StringVar(&version, "version", "", "Live version to pull (default: the current one)")
	_ = cmd.MarkFlagRequired("spec")
	return cmd
}

func pullSpec(state *AppState, slug, version string) (*api.SpecDocument, error) {
	params := &api.PullSpecParams{}
	if version != "" {
		params.Version = &version
	}
	resp, err := state.Client.API().PullSpecWithResponse(context.Background(), slug, params)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200, nil
}

func newSpecCheckCmd(state *AppState) *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "check <file>",
		Short: "Fail when a local file differs from the spec's Live version",
		Long: `Compare a local file byte for byte with the spec's Live version, in the
canonical layout EchoPoint stored it in. Exits non-zero on any difference, so CI
fails when the repository copy drifts from Live. Fix it with 'echopoint spec pull'.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			pulled, err := pullSpec(state, slug, "")
			if err != nil {
				return err
			}
			local, err := os.ReadFile(args[0])
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if bytes.Equal(local, []byte(pulled.Document)) {
				fmt.Fprintf(cmd.OutOrStdout(), "✓ %s matches %s %s\n", args[0], slug, pulled.Version)
				return nil
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"✗ %s differs from the Live version %s of %s (layout %s).\n  Run: echopoint spec pull --spec %s %s\n",
				args[0], pulled.Version, slug, strconv.Itoa(int(pulled.LayoutVersion)), slug, args[0])
			return &exitCodeError{code: 1}
		},
	}
	cmd.Flags().StringVar(&slug, "spec", "", specFlagUsage)
	_ = cmd.MarkFlagRequired("spec")
	return cmd
}
