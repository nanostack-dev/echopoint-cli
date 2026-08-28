package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// newFlowEnvCmd creates the env subcommand for flows.
func newFlowEnvCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage flow variables",
		Long: `Manage flow variables.

Flow variables are loaded last and override both organization layers. A
variable can be stored as a secret: encrypted at rest, never returned by a
read, and replaced by ` + "`***`" + ` in execution results, progress events and flow
exports.`,
	}

	cmd.AddCommand(
		newFlowEnvGetCmd(state),
		newFlowEnvSetCmd(state),
		newFlowEnvUnsetCmd(state),
		newFlowEnvDeleteCmd(state),
	)

	return cmd
}

// fetchFlowVariables reads one flow's variable set.
func fetchFlowVariables(state *AppState, flowID uuid.UUID) (*api.VariableSet, error) {
	resp, err := state.Client.API().GetFlowVariablesWithResponse(context.Background(), flowID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get flow variables: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200, nil
}

// setFlowVariable writes one variable into a flow's base layer.
func setFlowVariable(state *AppState, flowID uuid.UUID, key, value string, secret bool) error {
	body := api.SetVariableRequest{Value: value}
	if secret {
		body.Secret = &secret
	}

	resp, err := state.Client.API().SetFlowVariableWithResponse(
		context.Background(), flowID, key, nil, api.SetFlowVariableJSONRequestBody(body),
	)
	if err != nil {
		return fmt.Errorf("failed to set %s: %w", key, err)
	}
	if resp.JSON200 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

func newFlowEnvGetCmd(state *AppState) *cobra.Command {
	var showValues bool

	cmd := &cobra.Command{
		Use:   "get <flow-id>",
		Short: "Get flow variables",
		Long: `Get the variables of a flow.

Values are hidden by default and only names are shown. Pass --show-values to
reveal them. A secret has no value to reveal: a read never returns one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id %q: %w", args[0], err)
			}

			set, err := fetchFlowVariables(state, flowID)
			if err != nil {
				return err
			}

			vars := layerValues(set.Base)

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, layerPayload(vars, showValues))
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, layerPayload(vars, showValues))
			}

			printVars(os.Stdout, "Flow variables", vars, showValues)
			return nil
		},
	}

	cmd.Flags().BoolVar(&showValues, "show-values", false, "Reveal variable values instead of names only")
	return cmd
}

func newFlowEnvSetCmd(state *AppState) *cobra.Command {
	var (
		variables []string
		file      string
		secret    bool
	)

	cmd := &cobra.Command{
		Use:   "set <flow-id>",
		Short: "Set flow variables",
		Args:  cobra.ExactArgs(1),
		Long: `Set variables for a flow.

Each variable is written on its own; the others are left alone. Pass --secret to
encrypt the values at rest. A plain variable can become a secret; the reverse is
refused, so delete it and set it again.

Examples:
  echopoint flows env set <flow-id> --var KEY=value
  echopoint flows env set <flow-id> --var KEY1=value1 --var KEY2=value2
  echopoint flows env set <flow-id> --file env.json
  echopoint flows env set <flow-id> --secret --var API_KEY=sk-live-...`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, parseErr := uuid.Parse(args[0])
			if parseErr != nil {
				return fmt.Errorf("invalid flow id %q: %w", args[0], parseErr)
			}
			if len(variables) == 0 && file == "" {
				return fmt.Errorf("at least one --var KEY=value or --file is required")
			}

			updates, err := collectVarInputs(file, variables)
			if err != nil {
				return err
			}
			if len(updates) == 0 {
				return fmt.Errorf("no variables to set")
			}

			for _, key := range sortedKeys(updates) {
				if err := setFlowVariable(state, flowID, key, updates[key], secret); err != nil {
					return err
				}
			}

			kind := "variable(s)"
			if secret {
				kind = "secret(s)"
			}
			fmt.Printf("Set %d %s on flow %s\n", len(updates), kind, flowID)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&variables, "var", nil, "Variable in KEY=value format (repeatable)")
	cmd.Flags().StringVar(&file, "file", "", "Path to a JSON or dotenv file")
	cmd.Flags().BoolVar(&secret, "secret", false, "Store the values as secrets (encrypted, never read back)")
	return cmd
}

func newFlowEnvUnsetCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <flow-id> KEY [KEY...]",
		Short: "Remove flow variables",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id %q: %w", args[0], err)
			}

			keys := args[1:]
			for _, key := range keys {
				resp, delErr := state.Client.API().DeleteFlowVariableWithResponse(
					context.Background(), flowID, key, nil,
				)
				if delErr != nil {
					return fmt.Errorf("failed to delete %s: %w", key, delErr)
				}
				if resp.StatusCode() != http.StatusNoContent {
					return formatAPIError(resp.HTTPResponse, resp.Body)
				}
			}

			fmt.Printf("Removed %d variable(s) from flow %s\n", len(keys), flowID)
			return nil
		},
	}
}

func newFlowEnvDeleteCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <flow-id>",
		Short: "Delete all variables of a flow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id %q: %w", args[0], err)
			}

			resp, err := state.Client.API().DeleteFlowVariablesWithResponse(
				context.Background(), flowID, nil,
			)
			if err != nil {
				return fmt.Errorf("failed to delete flow variables: %w", err)
			}
			if resp.StatusCode() != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Printf("Deleted all variables of flow %s\n", flowID)
			return nil
		},
	}
}

// collectVarInputs merges a variable file with repeated --var flags. A flag
// wins over the file on a duplicate key, so a one-off override does not need
// the file edited.
func collectVarInputs(file string, flags []string) (map[string]string, error) {
	updates := make(map[string]string)

	if file != "" {
		fromFile, err := parseVarFile(file)
		if err != nil {
			return nil, err
		}
		for key, value := range fromFile {
			updates[key] = value
		}
	}

	if len(flags) > 0 {
		fromFlags, err := parseVarFlags(flags)
		if err != nil {
			return nil, err
		}
		for key, value := range fromFlags {
			updates[key] = value
		}
	}

	return updates, nil
}
