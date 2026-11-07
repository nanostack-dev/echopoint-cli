package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

// newIntegrationCmd creates the "integration" command group: the third-party
// APIs Echopoint offers as ready-made nodes, such as Stripe.
func newIntegrationCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     integrationCommandName,
		Aliases: []string{"integrations"},
		Short:   "Browse integrations and connect them to a workspace",
		Long: `Browse the integrations Echopoint offers, such as Stripe, and connect them.

An integration ships reviewed operations (request nodes) and events (expected
webhook events). A workspace accepts every item until it refuses one. A
connection holds one restricted test key per environment; Echopoint refuses
live keys, checks the key with the vendor, and creates a signed capture webhook
the vendor delivers events to.`,
	}
	cmd.AddCommand(
		newIntegrationListCmd(state),
		newIntegrationViewCmd(state),
		newIntegrationItemsCmd(state),
		newIntegrationChoiceCmd(state, "refuse", api.Refused),
		newIntegrationChoiceCmd(state, "accept", api.Accepted),
		newIntegrationConnectionsCmd(state),
		newIntegrationConnectCmd(state),
		newIntegrationDisconnectCmd(state),
	)
	return cmd
}

func newIntegrationListCmd(state *AppState) *cobra.Command {
	var pages pageFlags
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List the integrations Echopoint offers",
		Example: "  echopoint integration list\n  echopoint integration list -o json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			page, err := fetchSearch(cmd.Context(), cmd, &pages, searchIntegrations(state))
			if err != nil {
				return err
			}
			return printSearchPage(cmd, state, page, func(w io.Writer) error {
				rows := make([][]string, 0, len(page.Items))
				for _, item := range page.Items {
					rows = append(rows, []string{
						item.Id, item.Name, item.ApiVersion, licensedLabel(item.Licensed),
						fmt.Sprintf("%d / %d", item.OperationCount, item.EventCount),
					})
				}
				return output.PrintTableTo(
					w,
					[]string{columnID, columnName, "API version", "License", "Operations / events"},
					rows,
				)
			})
		},
	}
	pages.bind(cmd, defaultListLimit)
	return cmd
}

func searchIntegrations(state *AppState) pageFetcher[api.IntegrationSummary] {
	return func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.IntegrationSummary], error) {
		resp, err := state.Client.API().SearchIntegrationsWithResponse(
			ctx, nil, api.SearchIntegrationsJSONRequestBody{Pagination: pagination},
		)
		if err != nil {
			return searchPage[api.IntegrationSummary]{}, err
		}
		if resp.JSON200 == nil {
			return searchPage[api.IntegrationSummary]{}, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
	}
}

func licensedLabel(licensed bool) string {
	if licensed {
		return "included"
	}
	return "not in your plan"
}

func newIntegrationViewCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:     "view <integration-id>",
		Aliases: []string{"get", "show"},
		Short:   "Show an integration, its curated nodes and assertion presets",
		Example: "  echopoint integration view stripe\n  echopoint integration view stripe -o yaml",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().GetIntegrationWithResponse(cmd.Context(), args[0], nil)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			integration := resp.JSON200
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, integration)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, integration)
			}
			fmt.Fprintf(os.Stdout, "%s (%s)  API %s  %s\n", integration.Name, integration.Id,
				integration.ApiVersion, licensedLabel(integration.Licensed))
			fmt.Fprintf(os.Stdout, "%d operations, %d events. Docs: %s\n\nCurated nodes:\n",
				integration.OperationCount, integration.EventCount, integration.DocsUrl)
			rows := make([][]string, 0, len(integration.Curated))
			for _, node := range integration.Curated {
				rows = append(rows, []string{node.Key, node.Title, string(node.Kind), node.ItemKey})
			}
			return output.PrintTableTo(os.Stdout, []string{columnKey, "Title", "Kind", "Item"}, rows)
		},
	}
}

func newIntegrationItemsCmd(state *AppState) *cobra.Command {
	var (
		pages  pageFlags
		kind   string
		areas  []string
		choice string
		search string
	)
	cmd := &cobra.Command{
		Use:   "items <integration-id>",
		Short: "List an integration's reviewed operations and events",
		Example: `  echopoint integration items stripe --kind operation --area billing
  echopoint integration items stripe --search test_clock
  echopoint integration items stripe --choice refused --all -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			filter, err := itemFilter(kind, areas, choice)
			if err != nil {
				return err
			}
			fetch := func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.IntegrationItem], error) {
				body := api.SearchIntegrationItemsJSONRequestBody{Filter: filter, Pagination: pagination}
				if search != "" {
					body.FullTextSearch = &search
				}
				resp, callErr := state.Client.API().SearchIntegrationItemsWithResponse(ctx, args[0], nil, body)
				if callErr != nil {
					return searchPage[api.IntegrationItem]{}, callErr
				}
				if resp.JSON200 == nil {
					return searchPage[api.IntegrationItem]{}, formatAPIError(resp.HTTPResponse, resp.Body)
				}
				return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
			}
			page, err := fetchSearch(cmd.Context(), cmd, &pages, fetch)
			if err != nil {
				return err
			}
			return printSearchPage(cmd, state, page, func(w io.Writer) error {
				rows := make([][]string, 0, len(page.Items))
				for _, item := range page.Items {
					rows = append(
						rows,
						[]string{string(item.Kind), item.Key, item.Name, item.Area, string(item.Choice)},
					)
				}
				fmt.Fprintf(w, "Total: %d\n", page.Total)
				return output.PrintTableTo(w, []string{"Kind", columnKey, columnName, "Area", "Choice"}, rows)
			})
		},
	}
	pages.bind(cmd, defaultListLimit)
	cmd.Flags().StringVar(&kind, "kind", "", "Only operations or only events: operation or event")
	cmd.Flags().StringSliceVar(&areas, "area", nil, "Only items of this product area, such as billing (repeatable)")
	cmd.Flags().StringVar(&choice, "choice", "", "Only accepted or only refused items")
	cmd.Flags().StringVar(&search, "search", "", "Match the key, name or path")
	return cmd
}

func itemFilter(kind string, areas []string, choice string) (*api.IntegrationItemFilter, error) {
	filter := &api.IntegrationItemFilter{Areas: areas}
	switch kind {
	case "":
	case string(api.Operation), string(api.Event):
		value := api.IntegrationItemKind(kind)
		filter.Kind = &value
	default:
		return nil, fmt.Errorf("--kind must be operation or event, not %q", kind)
	}
	switch choice {
	case "":
	case string(api.Accepted), string(api.Refused):
		value := api.IntegrationChoice(choice)
		filter.Choice = &value
	default:
		return nil, fmt.Errorf("--choice must be accepted or refused, not %q", choice)
	}
	return filter, nil
}

var choiceDone = map[api.IntegrationChoice]string{api.Accepted: "Accepted", api.Refused: "Refused"}

func newIntegrationChoiceCmd(state *AppState, verb string, choice api.IntegrationChoice) *cobra.Command {
	return &cobra.Command{
		Use:     verb + " <integration-id> <kind:key>...",
		Short:   strings.ToUpper(verb[:1]) + verb[1:] + " operations or events for the workspace's palette",
		Example: fmt.Sprintf(`  echopoint integration %s stripe operation:quote.list event:quote.created`, verb),
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			items := make([]api.IntegrationChoiceInput, 0, len(args)-1)
			for _, ref := range args[1:] {
				kind, key, ok := strings.Cut(ref, ":")
				if !ok || (kind != string(api.Operation) && kind != string(api.Event)) || key == "" {
					return fmt.Errorf(
						"%q is not kind:key, such as operation:customer.create or event:invoice.paid",
						ref,
					)
				}
				items = append(
					items,
					api.IntegrationChoiceInput{Kind: api.IntegrationItemKind(kind), Key: key, Choice: choice},
				)
			}
			resp, err := state.Client.API().SetIntegrationChoicesWithResponse(
				cmd.Context(), args[0], nil, api.SetIntegrationChoicesJSONRequestBody{Items: items},
			)
			if err != nil {
				return err
			}
			if resp.StatusCode() != 204 {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			fmt.Fprintf(os.Stdout, "%s %d item(s) of %s\n", choiceDone[choice], len(items), args[0])
			return nil
		},
	}
}

func newIntegrationConnectionsCmd(state *AppState) *cobra.Command {
	var pages pageFlags
	cmd := &cobra.Command{
		Use:     "connections <integration-id>",
		Short:   "List the workspace's connections to an integration, one per environment",
		Example: "  echopoint integration connections stripe\n  echopoint integration connections stripe -o json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			fetch := func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.IntegrationConnection], error) {
				resp, err := state.Client.API().SearchIntegrationConnectionsWithResponse(
					ctx, args[0], nil, api.SearchIntegrationConnectionsJSONRequestBody{Pagination: pagination},
				)
				if err != nil {
					return searchPage[api.IntegrationConnection]{}, err
				}
				if resp.JSON200 == nil {
					return searchPage[api.IntegrationConnection]{}, formatAPIError(resp.HTTPResponse, resp.Body)
				}
				return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
			}
			page, err := fetchSearch(cmd.Context(), cmd, &pages, fetch)
			if err != nil {
				return err
			}
			return printSearchPage(cmd, state, page, func(w io.Writer) error {
				rows := make([][]string, 0, len(page.Items))
				for _, connection := range page.Items {
					rows = append(rows, []string{
						connection.EnvironmentKey, connection.KeyHint, valueOr(connection.AccountId, "-"),
						signedLabel(connection.Signed), connection.CheckedAt.Format("2006-01-02 15:04"),
					})
				}
				return output.PrintTableTo(w, []string{"Environment", columnKey, "Account", "Events", "Checked"}, rows)
			})
		},
	}
	pages.bind(cmd, defaultListLimit)
	return cmd
}

func valueOr(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func signedLabel(signed bool) string {
	if signed {
		return "signed"
	}
	return "not delivered"
}

func newIntegrationConnectCmd(state *AppState) *cobra.Command {
	var environment string
	cmd := &cobra.Command{
		Use:   "connect <integration-id> --env <environment>",
		Short: "Connect an integration for one environment with a restricted test key",
		Long: `Connect an integration for one environment of the workspace.

The key is read from standard input, so it never appears in your shell history
or the process list. Echopoint refuses live keys, checks the key with the
vendor, creates a capture webhook, registers it with the vendor and stores the
vendor's signing secret on it. Connecting the same environment again replaces
the connection.`,
		Example: `  printf '%s' "$STRIPE_TEST_KEY" | echopoint integration connect stripe --env dev
  echopoint integration connect stripe --env staging -o json < key.txt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			key, err := readSecretKey(cmd.InOrStdin())
			if err != nil {
				return err
			}
			resp, err := state.Client.API().ConnectIntegrationWithResponse(
				cmd.Context(), args[0], environment, nil, api.ConnectIntegrationJSONRequestBody{SecretKey: &key},
			)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			connection := resp.JSON200
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, connection)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, connection)
			}
			fmt.Fprintf(
				os.Stdout,
				"Connected %s for %s with %s\n",
				args[0],
				connection.EnvironmentKey,
				connection.KeyHint,
			)
			for _, check := range connection.Checks {
				fmt.Fprintf(os.Stdout, "  %-8s %s: %s\n", check.Status, check.Name, check.Message)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&environment, "env", "", "The environment the connection serves, such as dev")
	_ = cmd.MarkFlagRequired("env")
	return cmd
}

// readSecretKey reads the first line of input: a key, never a file name.
func readSecretKey(input io.Reader) (string, error) {
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read the key from standard input: %w", err)
	}
	key := strings.TrimSpace(line)
	if key == "" {
		return "", errors.New(
			"pass the key on standard input, such as: printf '%s' \"$KEY\" | echopoint integration connect stripe --env dev",
		)
	}
	return key, nil
}

func newIntegrationDisconnectCmd(state *AppState) *cobra.Command {
	var environment string
	cmd := &cobra.Command{
		Use:   "disconnect <integration-id> --env <environment>",
		Short: "Remove a connection, its capture webhook and its vendor endpoint",
		Example: `  echopoint integration disconnect stripe --env dev
  echopoint integration disconnect stripe --env dev --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := confirmDestructive(cmd, state, "disconnect", args[0]+" for "+environment); err != nil {
				return err
			}
			resp, err := state.Client.API().DisconnectIntegrationWithResponse(cmd.Context(), args[0], environment, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() != 204 {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			fmt.Fprintf(os.Stdout, "Disconnected %s for %s\n", args[0], environment)
			return nil
		},
	}
	cmd.Flags().StringVar(&environment, "env", "", "The environment whose connection to remove")
	_ = cmd.MarkFlagRequired("env")
	return quietOnError(cmd)
}
