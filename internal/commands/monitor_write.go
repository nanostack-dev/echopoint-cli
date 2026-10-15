package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const monitorInputLimit = 1 << 20

// readMonitorRequest reads a JSON or YAML request from a file (- reads stdin),
// checks it against its schema in the API contract, then decodes it into the
// generated type. Checking the document before decoding is what stops a field
// the API does not know, or one with the wrong type, from being dropped quietly.
func readMonitorRequest[T any](cmd *cobra.Command, path, schema string) (T, error) {
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
	data, err := io.ReadAll(io.LimitReader(reader, monitorInputLimit+1))
	if err != nil {
		return request, err
	}
	if len(data) > monitorInputLimit {
		return request, errors.New("monitor input exceeds 1 MiB")
	}
	value, err := decodeJSONOrYAML(data)
	if err != nil {
		return request, fmt.Errorf("invalid monitor input: %w", err)
	}
	if _, isObject := value.(map[string]any); !isObject {
		return request, errors.New("monitor input must be an object")
	}
	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPISpec)
	if err != nil {
		return request, fmt.Errorf("load monitor schema: %w", err)
	}
	ref, exists := spec.Components.Schemas[schema]
	if !exists || ref.Value == nil {
		return request, fmt.Errorf("monitor schema %s is unavailable", schema)
	}
	if err := ref.Value.VisitJSON(value); err != nil {
		return request, fmt.Errorf("invalid %s: %w", schema, err)
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return request, err
	}
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("invalid %s: %w", schema, err)
	}
	return request, nil
}

// decodeJSONOrYAML returns the document as JSON-shaped values. A YAML date
// is turned into text so the schema sees a string, as it does in JSON.
func decodeJSONOrYAML(data []byte) (any, error) {
	var value any
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return nil, err
		}
		return value, nil
	}
	if err := yaml.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result any
	if err := json.Unmarshal(normalized, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func newMonitorCreateCmd(state *AppState) *cobra.Command {
	var file string
	cmd := quietOnError(&cobra.Command{
		Use:   "create -f <file>",
		Short: "Create a monitor from a JSON or YAML request (-f -: stdin)",
		Long:  `Create a monitor from a CreateMonitorRequest in a JSON or YAML file. Choose either flow_ids or tags. Without a health policy the monitor runs on a cron_expression, at most every 15 minutes; with a health_policy it runs every interval_seconds (60 to 86400) and a status page can publish its health.`,
		Example: `  echopoint monitor create -f monitor.yaml
  cat monitor.json | echopoint --org <organization-id> monitor create -f - -o json`,
		Args:              fileFlagArgs(0),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			request, err := readMonitorRequest[api.CreateMonitorRequest](cmd, file, "CreateMonitorRequest")
			if err != nil {
				return err
			}
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().CreateFlowScheduleWithResponse(cmd.Context(), nil, request)
			if err != nil {
				return err
			}
			return printMonitor(cmd, state, resp.JSON201, resp.HTTPResponse, resp.Body)
		},
	})
	addFileFlag(cmd, &file, "CreateMonitorRequest as JSON or YAML; - reads stdin", "json", "yaml", "yml")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newMonitorUpdateCmd(state *AppState) *cobra.Command {
	var file string
	cmd := quietOnError(&cobra.Command{
		Use:   "update <monitor-id> -f <file>",
		Short: "Replace a monitor's configuration from a JSON or YAML request (-f -: stdin)",
		Long:  `Replace the mutable configuration of a monitor with an UpdateMonitorRequest in a JSON or YAML file. It has the rules of creation and replaces the whole configuration: a request without health_policy removes the policy. Read the current configuration with 'monitor view -o yaml' to edit it.`,
		Example: `  echopoint monitor update <monitor-id> -f monitor.yaml
  cat monitor.json | echopoint monitor update <monitor-id> -f -`,
		Args:              fileFlagArgs(1),
		ValidArgsFunction: completeMonitorArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseMonitorID(args[0])
			if err != nil {
				return err
			}
			request, err := readMonitorRequest[api.UpdateMonitorRequest](cmd, file, "UpdateMonitorRequest")
			if err != nil {
				return err
			}
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().UpdateFlowScheduleWithResponse(cmd.Context(), id, nil, request)
			if err != nil {
				return err
			}
			return printMonitor(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	})
	addFileFlag(cmd, &file, "UpdateMonitorRequest as JSON or YAML; - reads stdin", "json", "yaml", "yml")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newMonitorEstimateCmd(state *AppState) *cobra.Command {
	var file string
	cmd := quietOnError(&cobra.Command{
		Use:   "estimate -f <file>",
		Short: "Forecast a monitor's executions and requests before saving it (-f -: stdin)",
		Long:  `Resolve a tag selector or explicit flow set from a MonitorEstimateRequest in a JSON or YAML file and forecast the executions per occurrence, day and 30 days. For a Cloud monitor it also reports the daily Cloud execution limit and what the organization's other Cloud monitors already plan. Saving a monitor is never refused for the Cloud plan; runs past the limit are refused when they launch. HTTP request totals are omitted when dynamic flow behavior prevents a safe bound; read the estimate notes.`,
		Example: `  echopoint monitor estimate -f estimate.yaml
  echo '{"tags":["checkout"],"runner_type":"cloud","interval_seconds":300,"health_policy":true}' | echopoint monitor estimate -f - -o json`,
		Args:              fileFlagArgs(0),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			request, err := readMonitorRequest[api.MonitorEstimateRequest](cmd, file, "MonitorEstimateRequest")
			if err != nil {
				return err
			}
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().EstimateMonitorWithResponse(cmd.Context(), nil, request)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printMonitorResult(cmd, state, resp.JSON200, func(w io.Writer) error {
				return printMonitorEstimate(w, *resp.JSON200)
			})
		},
	})
	addFileFlag(cmd, &file, "MonitorEstimateRequest as JSON or YAML; - reads stdin", "json", "yaml", "yml")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func printMonitorEstimate(w io.Writer, estimate api.MonitorEstimate) error {
	lines := [][2]string{
		{"Matched flows", fmt.Sprint(estimate.MatchedFlowCount)},
		{"Executions per occurrence", fmt.Sprint(estimate.ExecutionsPerOccurrence)},
		{"Executions per day", fmt.Sprint(estimate.ExecutionsPerDay)},
		{"Executions per 30 days", fmt.Sprint(estimate.ExecutionsPer30Days)},
		{"Executions left this month", fmt.Sprint(estimate.ExecutionsRemainingMonth)},
	}
	if estimate.RequestsPerDay != nil {
		lines = append(lines, [2]string{"Requests per day", fmt.Sprint(*estimate.RequestsPerDay)})
	}
	if estimate.DailyCloudExecutionLimit != nil {
		lines = append(lines, [2]string{"Daily Cloud execution limit", fmt.Sprint(*estimate.DailyCloudExecutionLimit)})
	}
	if estimate.OtherCloudExecutionsPerDay != nil {
		lines = append(lines, [2]string{
			"Other Cloud monitors per day", fmt.Sprint(*estimate.OtherCloudExecutionsPerDay),
		})
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s: %s\n", line[0], line[1]); err != nil {
			return err
		}
	}
	for _, note := range estimate.EstimateNotes {
		if _, err := fmt.Fprintf(w, "Note: %s\n", note); err != nil {
			return err
		}
	}
	return nil
}

func newMonitorPreviewTagsCmd(state *AppState) *cobra.Command {
	var tags []string
	var matchMode string
	var ignoreModules bool
	cmd := quietOnError(&cobra.Command{
		Use:   "preview-tags --tag <tag>",
		Short: "List the flows a tag selector matches right now",
		Long:  `List the flows a tag selector matches right now, as a monitor with those tags would select them at its next run. Module flows are skipped unless --ignore-modules=false.`,
		Example: `  echopoint monitor preview-tags --tag checkout
  echopoint monitor preview-tags --tag checkout --tag smoke --match-mode all --ignore-modules=false -o json`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cleaned := make([]string, 0, len(tags))
			for _, tag := range tags {
				tag = strings.TrimSpace(tag)
				if tag == "" {
					return errors.New("--tag must not be empty")
				}
				cleaned = append(cleaned, tag)
			}
			request := api.ScheduleTagPreviewRequest{Tags: cleaned}
			if cmd.Flags().Changed("match-mode") {
				mode := api.TagMatchMode(matchMode)
				request.TagMatchMode = &mode
			}
			if cmd.Flags().Changed("ignore-modules") {
				request.IgnoreModules = &ignoreModules
			}
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().PreviewScheduleTagsWithResponse(cmd.Context(), nil, request)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printMonitorResult(cmd, state, resp.JSON200, func(w io.Writer) error {
				rows := make([][]string, 0, len(resp.JSON200.Flows))
				for _, flow := range resp.JSON200.Flows {
					rows = append(rows, []string{flow.Id.String(), oneLine(flow.Name)})
				}
				fmt.Fprintf(w, "Matched: %d\n", resp.JSON200.MatchedFlowCount)
				return output.PrintTableTo(w, []string{columnID, columnName}, rows)
			})
		},
	})
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Tag to select flows by (repeatable)")
	cmd.Flags().StringVar(&matchMode, "match-mode", "any", "Match any or all of the tags")
	cmd.Flags().
		BoolVar(&ignoreModules, "ignore-modules", true, "Skip module flows; --ignore-modules=false includes them")
	_ = cmd.MarkFlagRequired("tag")
	_ = cmd.RegisterFlagCompletionFunc("match-mode", staticCompletion("any", "all"))
	return cmd
}
