package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/mcp"
	"echopoint-cli/internal/output"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl"
	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl/wire"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newFlowCompileCmd(state *AppState) *cobra.Command {
	var file, root string
	var check bool
	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile flow files to CreateFlowRequest JSON",
		Long: `Compile a flow file, or a directory of them, into the CreateFlowRequest JSON
that flow create -f sends. flow create and flow update compile a flow file
themselves. Use this command to inspect the JSON, or to lint with --check.

A file is YAML. Nodes are named, and next and on_failure name the nodes they
run. module: anchor/login resolves against the other flow files under --root
(the file's directory, when --root is omitted). The API stores the JSON, not
the YAML. Pass --json to flow create or flow update only when the file is
already that JSON.

--check reports problems and prints no JSON. A warning still compiles.`,
		Example: `  echopoint flow compile -f flows
  echopoint flow compile -f flows/anchor/login.flow.yaml --root flows
  echopoint flow compile -f flows --check
  echopoint flow create -f flows/anchor/login.flow.yaml --root flows`,
		Args:          fileFlagArgs(0),
		SilenceUsage:  true,
		SilenceErrors: true,
		Annotations:   offline(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFlowCompile(cmd, state, file, root, check)
		},
	}
	addFileFlag(cmd, &file, "Flow file or a directory of *.flow.yaml files", ".yaml", ".yml")
	cmd.Flags().StringVar(&root, "root", "", "Library root used to resolve module: references")
	cmd.Flags().BoolVar(&check, "check", false, "Report problems and print no JSON")
	return cmd
}

func runFlowCompile(cmd *cobra.Command, state *AppState, file, root string, check bool) error {
	if !cmd.Flags().Changed("file") {
		return errors.New("pass -f with a flow file or a directory")
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	out, err := compileFlowPath(file, root, info.IsDir())
	if err != nil {
		return err
	}
	for _, diag := range out.Diagnostics {
		fmt.Fprintln(cmd.ErrOrStderr(), diag.String())
	}
	if len(out.Errors()) > 0 {
		return &exitCodeError{code: 1}
	}
	if check {
		return nil
	}
	doc, err := compiledDocument(out, file, root, info.IsDir())
	if err != nil {
		return err
	}
	printed, err := printStructured(cmd.OutOrStdout(), state.OutputFormat, doc)
	if err != nil || printed {
		return err
	}
	return output.PrintJSON(cmd.OutOrStdout(), doc)
}

// fillIntegrations completes the integration nodes of a compiled flow from the
// workspace catalog before the CLI sends it.
type fillIntegrations func(*wire.CreateFlowRequest) error

func catalogFiller(cmd *cobra.Command, state *AppState) fillIntegrations {
	return func(req *wire.CreateFlowRequest) error {
		return mcp.FlowFileIntegrations(cmd.Context(), state.Client, req)
	}
}

func createRequestFromFile(
	cmd *cobra.Command,
	file, root string,
	asJSON bool,
	fill fillIntegrations,
) (api.CreateFlowRequest, error) {
	if asJSON && cmd.Flags().Changed("root") {
		return api.CreateFlowRequest{}, errors.New("pass --root with a flow file, not with --json")
	}
	if asJSON {
		var req api.CreateFlowRequest
		return req, loadJSONFile(file, &req)
	}
	return compileOneFlowFile(cmd, file, root, fill)
}

func updateRequestFromFile(
	cmd *cobra.Command,
	file, root string,
	asJSON bool,
	fill fillIntegrations,
) (api.UpdateFlowRequest, error) {
	if asJSON && cmd.Flags().Changed("root") {
		return api.UpdateFlowRequest{}, errors.New("pass --root with a flow file, not with --json")
	}
	if asJSON {
		var req api.UpdateFlowRequest
		return req, loadJSONFile(file, &req)
	}
	compiled, err := compileOneFlowFile(cmd, file, root, fill)
	if err != nil {
		return api.UpdateFlowRequest{}, err
	}
	if compiled.FolderId != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "folder is applied by flow create; flow update leaves the folder where it is")
	}
	name := compiled.Name
	return api.UpdateFlowRequest{
		Name:           &name,
		Description:    compiled.Description,
		FlowDefinition: &compiled.FlowDefinition,
		AutoLayout:     compiled.AutoLayout,
		Role:           compiled.Role,
		Tags:           compiled.Tags,
		Version:        compiled.Version,
	}, nil
}

func compileOneFlowFile(
	cmd *cobra.Command,
	file, root string,
	fill fillIntegrations,
) (api.CreateFlowRequest, error) {
	info, err := os.Stat(file)
	if err != nil {
		return api.CreateFlowRequest{}, err
	}
	if info.IsDir() {
		return api.CreateFlowRequest{}, errors.New("pass one flow file; flow compile accepts a directory")
	}
	out, err := compileFlowPath(file, root, false)
	if err != nil {
		if looksLikeAPIJSON(file) {
			return api.CreateFlowRequest{}, errors.New("this file is API JSON; pass --json to send it unchanged")
		}
		return api.CreateFlowRequest{}, err
	}
	for _, diag := range out.Diagnostics {
		fmt.Fprintln(cmd.ErrOrStderr(), diag.String())
	}
	if len(out.Errors()) > 0 {
		if looksLikeAPIJSON(file) {
			fmt.Fprintln(cmd.ErrOrStderr(), "this file is API JSON; pass --json to send it unchanged")
		}
		return api.CreateFlowRequest{}, &exitCodeError{code: 1}
	}
	if err := fillCompiledFlow(out, file, root, fill); err != nil {
		return api.CreateFlowRequest{}, err
	}
	doc, err := compiledDocument(out, file, root, false)
	if err != nil {
		return api.CreateFlowRequest{}, err
	}
	req, ok := doc.(api.CreateFlowRequest)
	if !ok {
		return api.CreateFlowRequest{}, errors.New("compiled flow is missing")
	}
	return req, nil
}

// fillCompiledFlow fills the integration nodes of the flow file itself; the
// modules it uses are stored apart.
func fillCompiledFlow(out flowdsl.Outcome, file, root string, fill fillIntegrations) error {
	key, err := flowdsl.ModulePath(root, file)
	if err != nil {
		return err
	}
	for i := range out.Flows {
		if out.Flows[i].Path != key {
			continue
		}
		if err := fill(&out.Flows[i].Request); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	return nil
}

func looksLikeAPIJSON(file string) bool {
	body, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	var doc map[string]any
	if yaml.Unmarshal(body, &doc) != nil {
		return false
	}
	_, ok := doc["flow_definition"]
	return ok
}

func compileFlowPath(file, root string, dir bool) (flowdsl.Outcome, error) {
	if dir {
		return flowdsl.CompileDir(file)
	}
	return flowdsl.CompileFile(file, root)
}

func compiledDocument(out flowdsl.Outcome, file, root string, dir bool) (any, error) {
	if dir {
		doc := make(map[string]api.CreateFlowRequest, len(out.Flows))
		for _, flow := range out.Flows {
			req, err := apiRequest(flow.Request)
			if err != nil {
				return nil, err
			}
			doc[flow.Path] = req
		}
		return doc, nil
	}
	key, err := flowdsl.ModulePath(root, file)
	if err != nil {
		return nil, err
	}
	for _, flow := range out.Flows {
		if flow.Path == key {
			return apiRequest(flow.Request)
		}
	}
	return nil, &exitCodeError{code: 1}
}

// The compiler's request is the API document with its own Go types. The CLI
// client prints and sends the generated type, so the JSON is the boundary.
func apiRequest(request wire.CreateFlowRequest) (api.CreateFlowRequest, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return api.CreateFlowRequest{}, err
	}
	var decoded api.CreateFlowRequest
	err = json.Unmarshal(raw, &decoded)
	return decoded, err
}
