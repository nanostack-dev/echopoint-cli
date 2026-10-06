package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const (
	specPageSize            = 100
	specVersionsDefaultPage = 20
	specNotFoundCode        = "SPEC_NOT_FOUND"
)

func fetchSpec(ctx context.Context, state *AppState, name string) (*api.Spec, error) {
	resp, err := state.Client.API().GetSpecWithResponse(ctx, name, nil)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, specAPIError(resp.HTTPResponse, resp.Body, name, "")
	}
	return resp.JSON200, nil
}

func fetchSpecVersion(ctx context.Context, state *AppState, name, version string) (*api.SpecVersion, error) {
	resp, err := state.Client.API().GetSpecVersionWithResponse(ctx, name, version, nil)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, specAPIError(resp.HTTPResponse, resp.Body, name, version)
	}
	return resp.JSON200, nil
}

func fetchSpecVersions(
	ctx context.Context, state *AppState, name string, limit, offset int32,
) (*api.SpecVersionListResponse, error) {
	resp, err := state.Client.API().ListSpecVersionsWithResponse(ctx, name, &api.ListSpecVersionsParams{
		Limit:  api.LimitParameter(limit),
		Offset: api.OffsetParameter(offset),
	})
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, specAPIError(resp.HTTPResponse, resp.Body, name, "")
	}
	return resp.JSON200, nil
}

func pullSpec(ctx context.Context, state *AppState, name, version string) (*api.SpecDocument, error) {
	params := &api.PullSpecParams{}
	if version != "" {
		params.Version = &version
	}
	resp, err := state.Client.API().PullSpecWithResponse(ctx, name, params)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, specAPIError(resp.HTTPResponse, resp.Body, name, version)
	}
	return resp.JSON200, nil
}

func versionOrLive(ctx context.Context, state *AppState, name, version string) (string, error) {
	if version != "" {
		return version, nil
	}
	spec, err := fetchSpec(ctx, state, name)
	if err != nil {
		return "", err
	}
	return spec.Live.Version, nil
}

func specAPIError(resp *http.Response, body []byte, name, version string) error {
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		return formatAPIError(resp, body)
	}
	if version != "" && apiErrorCode(body) != specNotFoundCode {
		return fmt.Errorf("api error (%d): %s has no version %q; list its versions with: echopoint spec versions %s",
			resp.StatusCode, name, version, name)
	}
	return fmt.Errorf("api error (%d): no spec with slug %q; list the specs with: echopoint spec list",
		resp.StatusCode, name)
}

func newSpecListCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:               listVerb,
		Short:             "List the organization's specs and their Live versions",
		Example:           `  echopoint spec list`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		SilenceUsage:      true,
		SilenceErrors:     true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().ListSpecsWithResponse(cmd.Context(), &api.ListSpecsParams{
				Limit: api.LimitParameter(specPageSize),
			})
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			if done, err := printStructured(cmd.OutOrStdout(), state.OutputFormat, resp.JSON200); done {
				return err
			}
			rows := make([][]string, 0, len(resp.JSON200.Items))
			for _, item := range resp.JSON200.Items {
				rows = append(rows, []string{
					item.Slug, item.Title, item.Live.Version, string(item.Live.Bump), formatWhen(item.Live.CreatedAt),
				})
			}
			return output.PrintTableTo(cmd.OutOrStdout(), []string{"SLUG", "TITLE", "LIVE", "BUMP", "PUBLISHED"}, rows)
		},
	}
}

func newSpecViewCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <slug>",
		Short: "Show a spec: its Live version, findings, changes, and web page",
		Long: `Show a spec kept in EchoPoint: its title, the Live version and who published
it, the OpenAPI version, how many convention findings Live has, what Live
changed against the version before it, and the spec's page in EchoPoint.`,
		Example: `  echopoint spec view pets-api
  echopoint spec view pets-api -o json`,
		Args: specArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			spec, err := fetchSpec(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			if done, err := printStructured(cmd.OutOrStdout(), state.OutputFormat, spec); done {
				return err
			}
			return printSpecView(cmd.OutOrStdout(), state, spec)
		},
	}
	return finishSpecCmd(state, cmd)
}

func printSpecView(w io.Writer, state *AppState, spec *api.Spec) error {
	live := spec.Live
	rows := [][]string{
		{"Title:", spec.Title},
		{"Slug:", spec.Slug},
		{"Live:", fmt.Sprintf("%s (%s)", live.Version, live.Bump)},
		{"OpenAPI:", live.OpenapiVersion},
		{"Findings:", describeFindingCount(live.FindingCount)},
		{"Changes:", describeChangeCounts(live.ChangeCounts)},
		{"Published:", fmt.Sprintf("%s by %s", formatWhen(live.CreatedAt), actorLabel(live.CreatedBy))},
		{"Created:", fmt.Sprintf("%s by %s", formatWhen(spec.CreatedAt), actorLabel(spec.CreatedBy))},
		{"Updated:", formatWhen(spec.UpdatedAt)},
	}
	if frontend := strings.TrimRight(state.Config.FrontendURL, "/"); frontend != "" {
		rows = append(rows, []string{"URL:", frontend + "/api/specs/" + url.PathEscape(spec.Slug)})
	}
	return output.PrintTableTo(w, nil, rows)
}

func newSpecVersionsCmd(state *AppState) *cobra.Command {
	var limit, offset int32
	cmd := &cobra.Command{
		Use:   "versions <slug>",
		Short: "List a spec's Live versions, newest first",
		Long: `List the Live versions of a spec, newest first: the first one is Live now. Each
shows its bump, what it changed against the version before it, how many
convention findings it has, who published it, and when. The API returns at most
100 versions a page: page with --offset.`,
		Example: `  echopoint spec versions pets-api
  echopoint spec versions pets-api --limit 100 --offset 100`,
		Args: specArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			versions, err := fetchSpecVersions(cmd.Context(), state, args[0], limit, offset)
			if err != nil {
				return err
			}
			if done, err := printStructured(cmd.OutOrStdout(), state.OutputFormat, versions); done {
				return err
			}
			rows := make([][]string, 0, len(versions.Items))
			for i, item := range versions.Items {
				version := item.Version
				if offset == 0 && i == 0 {
					version += " (Live)"
				}
				rows = append(rows, []string{
					version, string(item.Bump), describeChangeCounts(item.ChangeCounts),
					describeFindingCount(item.FindingCount), actorLabel(item.CreatedBy), formatWhen(item.CreatedAt),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Total: %d\n", versions.Total)
			return output.PrintTableTo(cmd.OutOrStdout(),
				[]string{"VERSION", "BUMP", "CHANGES", "FINDINGS", "BY", "WHEN"}, rows)
		},
	}
	cmd.Flags().
		Int32Var(&limit, "limit", specVersionsDefaultPage, "Number of results to return (the API allows at most 100)")
	cmd.Flags().Int32Var(&offset, "offset", 0, "Offset for pagination")
	return finishSpecCmd(state, cmd)
}

func newSpecCreateCmd(state *AppState) *cobra.Command {
	var file string
	var bundle bool
	cmd := &cobra.Command{
		Use:   "create <slug> -f <file>",
		Short: "Create a spec in EchoPoint from an OpenAPI file",
		Long: `Create a spec in EchoPoint with the file as its first Live version. The slug is
the spec's unique identifier in the organization: lowercase letters, digits, and
single hyphens, at most 64 characters. A slug that is taken is refused.

--bundle inlines external $ref (other files, URLs) before sending; without it, a
document with an external $ref is refused.`,
		Example: `  echopoint spec create pets-api -f openapi.yaml
  echopoint spec create pets-api -f openapi.yaml --bundle`,
		Args: specArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			document, err := readDocumentForPush(file, bundle)
			if err != nil {
				return err
			}
			return createSpec(cmd, state, args[0], document)
		},
	}
	addSpecFileFlag(cmd, &file, "OpenAPI file to create the spec from")
	cmd.Flags().BoolVar(&bundle, "bundle", false, "Inline external $ref before sending")
	_ = cmd.MarkFlagRequired("file")
	return finishSpecCmd(state, cmd)
}

func addSpecFileFlag(cmd *cobra.Command, file *string, usage string) {
	addFileFlag(cmd, file, usage, "yaml", "yml", "json")
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

func createSpec(cmd *cobra.Command, state *AppState, name, document string) error {
	resp, err := state.Client.API().CreateSpecWithResponse(cmd.Context(), nil, api.CreateSpecRequest{
		Slug:     name,
		Document: document,
	})
	if err != nil {
		return err
	}
	if resp.JSON201 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	if done, err := printStructured(cmd.OutOrStdout(), state.OutputFormat, resp.JSON201); done {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "✓ Created spec %s: Live version %s\n",
		resp.JSON201.Slug, resp.JSON201.Live.Version)
	return err
}

func newSpecPushCmd(state *AppState) *cobra.Command {
	var file string
	var bundle bool
	cmd := &cobra.Command{
		Use:   "push <slug> -f <file>",
		Short: "Publish an OpenAPI file as the spec's next Live version",
		Long: `Publish an OpenAPI file to a spec in EchoPoint. EchoPoint compares it with the
Live version, computes the next version from the changes (breaking -> major,
additions -> minor, other edits -> patch), and keeps the history. A file identical
to Live, info.version aside, is refused. Create the spec first with 'echopoint
spec create'.

--bundle inlines external $ref (other files, URLs) before sending; without it, a
document with an external $ref is refused.`,
		Example: `  echopoint spec push pets-api -f openapi.yaml
  echopoint spec push pets-api -f openapi.yaml --bundle`,
		Args: specArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			document, err := readDocumentForPush(file, bundle)
			if err != nil {
				return err
			}
			return pushSpecVersion(cmd, state, args[0], document)
		},
	}
	addSpecFileFlag(cmd, &file, "OpenAPI file to publish")
	cmd.Flags().BoolVar(&bundle, "bundle", false, "Inline external $ref before sending")
	_ = cmd.MarkFlagRequired("file")
	return finishSpecCmd(state, cmd)
}

func pushSpecVersion(cmd *cobra.Command, state *AppState, name, document string) error {
	resp, err := state.Client.API().PushSpecVersionWithResponse(cmd.Context(), name, nil,
		api.PushSpecVersionRequest{Document: document})
	if err != nil {
		return err
	}
	if resp.JSON201 == nil {
		return specAPIError(resp.HTTPResponse, resp.Body, name, "")
	}
	pushed := resp.JSON201
	w := cmd.OutOrStdout()
	if done, err := printStructured(w, state.OutputFormat, pushed); done {
		return err
	}
	fmt.Fprintf(w, "✓ Published %s %s (%s)\n\n", name, pushed.Version, pushed.Bump)
	if err = printSpecDiff(w, output.FormatTable, specDiffOfVersion(pushed)); err != nil {
		return err
	}
	printNewFindings(w, pushed.Findings)
	return nil
}

func newSpecPullCmd(state *AppState) *cobra.Command {
	var file, version string
	cmd := &cobra.Command{
		Use:   "pull <slug> [-f <file>]",
		Short: "Write a spec's Live version, in the canonical YAML layout",
		Long: `Write a version of a spec, byte for byte as EchoPoint keeps it, to the file
named with -f, or to stdout without -f (and with -f -). The file is generated:
edit the spec in EchoPoint, or push a change, and pull again. --version pulls an
earlier version (default: Live).`,
		Example: `  echopoint spec pull pets-api -f openapi.yaml
  echopoint spec pull pets-api --version 1.2.0 -f old.yaml
  echopoint spec pull pets-api > openapi.yaml`,
		Args: specArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			pulled, err := pullSpec(cmd.Context(), state, args[0], version)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("file") || file == "-" {
				_, err = io.WriteString(cmd.OutOrStdout(), pulled.Document)
				return err
			}
			if err = os.WriteFile(file, []byte(pulled.Document), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Wrote %s %s to %s\n", args[0], pulled.Version, file)
			return nil
		},
	}
	addSpecFileFlag(cmd, &file, "File to write (default: stdout; - also means stdout)")
	cmd.Flags().StringVar(&version, "version", "", "Version to pull (default: Live)")
	registerSpecVersionCompletion(state, cmd, "version")
	return finishSpecCmd(state, cmd)
}

func newSpecCheckCmd(state *AppState) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "check <slug> -f <file>",
		Short: "Fail when a file differs from the spec's Live version",
		Long: `Compare a file byte for byte with the spec's Live version, in the canonical
layout EchoPoint stored it in. Exits non-zero on any difference, so CI fails when
the repository copy drifts from Live. Fix it with 'echopoint spec pull'.`,
		Example: `  echopoint spec check pets-api -f openapi.yaml`,
		Args:    specArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			name := args[0]
			pulled, err := pullSpec(cmd.Context(), state, name, "")
			if err != nil {
				return err
			}
			local, err := os.ReadFile(file)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if bytes.Equal(local, []byte(pulled.Document)) {
				fmt.Fprintf(cmd.OutOrStdout(), "✓ %s matches %s %s\n", file, name, pulled.Version)
				return nil
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"✗ %s differs from the Live version %s of %s (layout %s).\n  Run: echopoint spec pull %s -f %s\n",
				file, pulled.Version, name, strconv.Itoa(int(pulled.LayoutVersion)), name, file)
			return &exitCodeError{code: 1}
		},
	}
	addSpecFileFlag(cmd, &file, "File to compare with Live")
	_ = cmd.MarkFlagRequired("file")
	return finishSpecCmd(state, cmd)
}
