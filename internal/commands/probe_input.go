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

// readProbeRequest validates the original object before decoding into API types,
// so omitted fields and unsupported fields cannot be silently lost in a save.
func readProbeRequest[T any](cmd *cobra.Command, path, schema, environment string) (T, error) {
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
	if cmd.Flags().Changed("environment") {
		config, ok := value["config"].(map[string]any)
		if !ok {
			return request, fmt.Errorf("probe input must include a config object")
		}
		config["environment_key"] = strings.TrimSpace(environment)
	}
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
	data, err = json.Marshal(value)
	if err != nil {
		return request, err
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return request, err
	}
	return request, nil
}
