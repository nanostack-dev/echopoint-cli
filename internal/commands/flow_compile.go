package commands

import (
	"errors"
	"fmt"
	"os"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/flowdsl"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

func newFlowCompileCmd(state *AppState) *cobra.Command {
	var file, root string
	var check bool
	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile flow files to CreateFlowRequest JSON",
		Long: `Compile a flow file, or a directory of them, into the CreateFlowRequest JSON
that flow create already accepts.

A file is YAML. Nodes are named, and next and on_failure name the nodes they
run. module: anchor/login resolves against the other flow files under --root
(the file's directory, when --root is omitted). The API stores the JSON, not
the YAML.

--check reports problems and prints no JSON. A warning still compiles.`,
		Example: `  echopoint flow compile -f flows
  echopoint flow compile -f flows/anchor/login.flow.yaml --root flows
  echopoint flow compile -f flows --check
  echopoint flow compile -f flows/anchor/login.flow.yaml > login.json
  echopoint flow create -f login.json`,
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
			doc[flow.Path] = flow.Request
		}
		return doc, nil
	}
	key, err := flowdsl.ModulePath(root, file)
	if err != nil {
		return nil, err
	}
	for _, flow := range out.Flows {
		if flow.Path == key {
			return flow.Request, nil
		}
	}
	return nil, &exitCodeError{code: 1}
}
