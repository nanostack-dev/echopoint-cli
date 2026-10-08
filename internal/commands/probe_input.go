package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

const probeNameField = "name"

type probeCreationFlags struct {
	name, runner      string
	interval, timeout int
	paused            bool
}

func readProbeCreation(
	cmd *cobra.Command,
	state *AppState,
	file, environment string,
	selectors probeSelectorFlags,
	creation probeCreationFlags,
) (api.CreateProbeRequest, error) {
	if cmd.Flags().Changed("file") {
		for _, flag := range []string{probeNameField, "runner", "interval", "timeout", "paused"} {
			if cmd.Flags().Changed(flag) {
				return api.CreateProbeRequest{}, fmt.Errorf(
					"--%s cannot be combined with --file; edit the complete configuration in the file",
					flag,
				)
			}
		}
		return readProbeRequest[api.CreateProbeRequest](cmd, state, file, "CreateProbeRequest", environment, selectors)
	}
	name := strings.TrimSpace(creation.name)
	if name == "" {
		return api.CreateProbeRequest{}, fmt.Errorf(
			"provide --name and --flow-id or --tag, or pass a complete request with -f",
		)
	}
	if len(selectors.flowIDs)+len(selectors.tags) == 0 {
		return api.CreateProbeRequest{}, fmt.Errorf("provide --flow-id or --tag")
	}
	config := map[string]any{
		probeNameField:      name,
		"environment_key":   strings.TrimSpace(environment),
		"runner_type":       creation.runner,
		"enabled":           !creation.paused,
		"interval_seconds":  creation.interval,
		"timeout_seconds":   creation.timeout,
		"confirmation_runs": 2,
		"recovery_runs":     2,
		"freshness_seconds": max(180, creation.interval+creation.timeout),
		"capabilities": []any{
			map[string]any{
				"id":           "suite",
				probeNameField: name,
				"enabled":      true,
				"checks":       []any{},
				"depends_on":   []string{},
			},
		},
	}
	return decodeProbeRequest[api.CreateProbeRequest](
		cmd,
		state,
		map[string]any{"config": config},
		"CreateProbeRequest",
		environment,
		selectors,
	)
}

type probeSelectorFlags struct {
	flowIDs   []string
	tags      []string
	matchMode string
}

func (flags *probeSelectorFlags) register(cmd *cobra.Command, state *AppState) {
	cmd.Flags().
		StringArrayVar(&flags.flowIDs, "flow-id", nil, "Select a flow by ID (repeatable); mutually exclusive with --tag")
	cmd.Flags().
		StringArrayVar(&flags.tags, "tag", nil, "Select flows by tag at each occurrence (repeatable); mutually exclusive with --flow-id")
	cmd.Flags().StringVar(&flags.matchMode, "match-mode", "any", "Match any or all selected tags")
	cmd.MarkFlagsMutuallyExclusive("flow-id", "tag")
	_ = cmd.RegisterFlagCompletionFunc("flow-id", completeFlowFlag(state))
	_ = cmd.RegisterFlagCompletionFunc("match-mode", staticCompletion("any", "all"))
}

func (flags probeSelectorFlags) apply(cmd *cobra.Command, state *AppState, config map[string]any) error {
	if cmd.Flags().Changed("flow-id") {
		ids := make([]string, 0, len(flags.flowIDs))
		for _, input := range flags.flowIDs {
			id, err := resolveFlowID(cmd.Context(), state, strings.TrimSpace(input))
			if err != nil {
				return fmt.Errorf("--flow-id: %w", err)
			}
			ids = append(ids, id.String())
		}
		config["flow_ids"] = ids
		delete(config, "tags")
		delete(config, "tag_match_mode")
	}
	if cmd.Flags().Changed("tag") {
		tags := make([]string, 0, len(flags.tags))
		for _, input := range flags.tags {
			tag := strings.TrimSpace(input)
			if tag == "" {
				return fmt.Errorf("--tag must not be empty")
			}
			tags = append(tags, tag)
		}
		config["tags"] = tags
		config["tag_match_mode"] = flags.matchMode
		delete(config, "flow_ids")
	}
	if cmd.Flags().Changed("match-mode") {
		if _, ok := config["tags"]; !ok {
			return fmt.Errorf("--match-mode requires a tag selector")
		}
		config["tag_match_mode"] = flags.matchMode
	}
	if mode, exists := config["tag_match_mode"]; exists && mode != "any" && mode != "all" {
		return fmt.Errorf("--match-mode must be any or all")
	}
	flows, tags := probeSelectorLength(config["flow_ids"]), probeSelectorLength(config["tags"])
	if (flows == 0) == (tags == 0) {
		return fmt.Errorf("select exactly one source: nonempty flow_ids or tags")
	}
	return nil
}

func probeSelectorLength(value any) int {
	switch items := value.(type) {
	case []string:
		return len(items)
	case []any:
		return len(items)
	default:
		return 0
	}
}

// readProbeRequest validates the original object before decoding into API types,
// so omitted fields and unsupported fields cannot be silently lost in a save.
func readProbeRequest[T any](
	cmd *cobra.Command,
	state *AppState,
	path, schema, environment string,
	selectors probeSelectorFlags,
) (T, error) {
	var request T
	reader := cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return request, err
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, 1_048_577))
	if err != nil {
		return request, err
	}
	if len(data) > 1_048_576 {
		return request, fmt.Errorf("probe input exceeds 1 MiB")
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return request, fmt.Errorf("invalid probe JSON: %w", err)
	}
	if value == nil {
		return request, fmt.Errorf("probe input must be a JSON object")
	}
	return decodeProbeRequest[T](cmd, state, value, schema, environment, selectors)
}

func decodeProbeRequest[T any](
	cmd *cobra.Command,
	state *AppState,
	value map[string]any,
	schema, environment string,
	selectors probeSelectorFlags,
) (T, error) {
	var request T
	if cmd.Flags().Changed("environment") {
		config, ok := value["config"].(map[string]any)
		if !ok {
			return request, fmt.Errorf("probe input must include a config object")
		}
		config["environment_key"] = strings.TrimSpace(environment)
	}
	config, ok := value["config"].(map[string]any)
	if !ok {
		return request, fmt.Errorf("probe input must include a config object")
	}
	if err := selectors.apply(cmd, state, config); err != nil {
		return request, err
	}
	return validateProbeValue[T](value, schema)
}

func validateProbeValue[T any](value map[string]any, schema string) (T, error) {
	var request T
	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPISpec)
	if err != nil {
		return request, fmt.Errorf("load probe schema: %w", err)
	}
	ref, exists := spec.Components.Schemas[schema]
	if !exists || ref.Value == nil {
		return request, fmt.Errorf("probe schema %s is unavailable", schema)
	}
	if err := ref.Value.VisitJSON(value); err != nil {
		return request, fmt.Errorf("invalid probe request: %w", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return request, err
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return request, err
	}
	return request, nil
}
