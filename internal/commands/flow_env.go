package commands

import (
	"context"
	"fmt"
	"maps"
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
		Use:   envCommandName,
		Short: "Manage flow variables",
		Long: `Manage flow variables.

Flow variables are loaded last and override both organization layers. A
variable can be stored as a secret: encrypted at rest, never returned by a
read, and replaced by ` + "`***`" + ` in execution results, progress events and flow
exports.`,
	}

	cmd.AddCommand(
		newFlowEnvViewCmd(state),
		newFlowEnvSetCmd(state),
		newFlowEnvUnsetCmd(state),
		newFlowEnvDeleteCmd(state),
	)

	return cmd
}

// fetchFlowVariables reads one flow's variable set.
func fetchFlowVariables(ctx context.Context, state *AppState, flowID uuid.UUID) (*api.VariableSet, error) {
	resp, err := state.Client.API().GetFlowVariablesWithResponse(ctx, flowID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get flow variables: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200, nil
}

// setFlowVariable writes one variable into a flow's base layer.
func setFlowVariable(ctx context.Context, state *AppState, flowID uuid.UUID, key, value string, secret bool) error {
	body := api.SetVariableRequest{Value: value}
	if secret {
		body.Secret = &secret
	}

	resp, err := state.Client.API().SetFlowVariableWithResponse(
		ctx, flowID, key, nil, api.SetFlowVariableJSONRequestBody(body),
	)
	if err != nil {
		return fmt.Errorf("failed to set %s: %w", key, err)
	}
	if resp.JSON200 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

func newFlowEnvViewCmd(state *AppState) *cobra.Command {
	var showValues bool

	cmd := &cobra.Command{
		Use:     "view <flow-id>",
		Aliases: []string{getVerb},
		Short:   "Show flow variables",
		Example: `  echopoint flow env view <flow-id>
  echopoint flow env view <flow-id> --show-values`,
		Long: `Show the variables of a flow.

Values are hidden by default and only names are shown. Pass --show-values to
reveal them. A secret has no value to reveal: a read never returns one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}

			set, err := fetchFlowVariables(cmd.Context(), state, flowID)
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
		Args:  fileFlagArgs(1),
		Long: `Set variables for a flow.

Each variable is written on its own; the others are left alone. Variables come
from --var and from a JSON or dotenv file given with -f; --var wins on a
duplicate key. Pass --secret to encrypt the values at rest. A plain variable can
become a secret; the reverse is refused, so delete it and set it again.`,
		Example: `  echopoint flow env set <flow-id> --var KEY=value
  echopoint flow env set <flow-id> --var KEY1=value1 --var KEY2=value2
  echopoint flow env set <flow-id> -f env.json
  echopoint flow env set <flow-id> --secret --var API_KEY=sk-live-...`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, flowErr := resolveFlowID(cmd.Context(), state, args[0])
			if flowErr != nil {
				return flowErr
			}
			if len(variables) == 0 && !cmd.Flags().Changed("file") {
				return fmt.Errorf("at least one --var KEY=value or -f is required")
			}

			updates, err := collectVarInputs(file, variables)
			if err != nil {
				return err
			}
			if len(updates) == 0 {
				return fmt.Errorf("no variables to set")
			}

			for _, key := range sortedKeys(updates) {
				if err := setFlowVariable(cmd.Context(), state, flowID, key, updates[key], secret); err != nil {
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
	addFileFlag(cmd, &file, "JSON or dotenv file of variables", "json", "env")
	cmd.Flags().BoolVar(&secret, "secret", false, "Store the values as secrets (encrypted, never read back)")
	return quietOnError(cmd)
}

func newFlowEnvUnsetCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:     "unset <flow-id> KEY [KEY...]",
		Short:   "Remove flow variables",
		Example: `  echopoint flow env unset <flow-id> API_KEY BASE_URL`,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}

			keys := args[1:]
			for _, key := range keys {
				resp, delErr := state.Client.API().DeleteFlowVariableWithResponse(
					cmd.Context(), flowID, key, nil,
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
	cmd := &cobra.Command{
		Use:   deleteVerb + " <flow-id>",
		Short: "Delete all variables of a flow",
		Example: `  echopoint flow env delete <flow-id>
  echopoint flow env delete <flow-id> --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			if err := confirmDestructive(cmd, state, "delete", "the variables of flow "+flowID.String()); err != nil {
				return err
			}

			resp, err := state.Client.API().DeleteFlowVariablesWithResponse(
				cmd.Context(), flowID, nil,
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
	return quietOnError(cmd)
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
		maps.Copy(updates, fromFile)
	}

	if len(flags) > 0 {
		fromFlags, err := parseVarFlags(flags)
		if err != nil {
			return nil, err
		}
		maps.Copy(updates, fromFlags)
	}

	return updates, nil
}
