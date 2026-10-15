package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newCollectionCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     collectionCommandName,
		Aliases: []string{"collections"},
		Short:   "Manage collections of requests",
	}

	cmd.AddCommand(
		newCollectionListCmd(state),
		newCollectionViewCmd(state),
		newCollectionCreateCmd(state),
		newCollectionUpdateCmd(state),
		newCollectionDeleteCmd(state),
		newCollectionImportCmd(state),
	)

	return cmd
}

func newCollectionListCmd(state *AppState) *cobra.Command {
	var pages pageFlags

	cmd := &cobra.Command{
		Use:   listVerb,
		Short: "List collections",
		Long: `List collections, newest first.

A page ends with the cursor that continues the listing: -o json carries it as
next_cursor, and the table says how to pass it with --cursor. --all follows it to
the last collection.`,
		Example: `  echopoint collection list
  echopoint collection list --limit 100 -o json
  echopoint collection list --cursor <next_cursor> -o json
  echopoint collection list --all -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			page, err := fetchSearch(cmd.Context(), cmd, &pages, searchCollections(state))
			if err != nil {
				return err
			}
			return printSearchPage(cmd, state, page, func(w io.Writer) error {
				rows := make([][]string, 0, len(page.Items))
				for _, collection := range page.Items {
					rows = append(
						rows,
						[]string{collection.Id.String(), collection.Name, formatWhen(collection.UpdatedAt)},
					)
				}
				fmt.Fprintf(w, "Total: %d\n", page.Total)
				return output.PrintTableTo(w, []string{columnID, columnName, "Updated"}, rows)
			})
		},
	}

	pages.bind(cmd, defaultListLimit)

	return cmd
}

// searchCollections reads one page of the organization's collections.
func searchCollections(state *AppState) pageFetcher[api.Collection] {
	return func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.Collection], error) {
		resp, err := state.Client.API().SearchCollectionsWithResponse(
			ctx, nil, api.CollectionSearchRequest{Pagination: pagination},
		)
		if err != nil {
			return searchPage[api.Collection]{}, err
		}
		if resp.JSON200 == nil {
			return searchPage[api.Collection]{}, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
	}
}

func newCollectionViewCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "view <id>",
		Aliases: []string{getVerb},
		Short:   "Show a collection",
		Example: `  echopoint collection view <id>
  echopoint collection view <id> -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid collection id")
			}

			resp, err := state.Client.API().GetCollectionWithResponse(context.Background(), id, nil)
			if err != nil {
				return err
			}

			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON200)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON200)
			default:
				fmt.Fprintf(os.Stdout, "ID: %s\n", resp.JSON200.Id)
				fmt.Fprintf(os.Stdout, "Name: %s\n", resp.JSON200.Name)
				fmt.Fprintf(os.Stdout, "Updated: %s\n", resp.JSON200.UpdatedAt)
				fmt.Fprintf(os.Stdout, "Created: %s\n", resp.JSON200.CreatedAt)
				return nil
			}
		},
	}

	cmd.ValidArgsFunction = completeCollectionArgs(state)
	return cmd
}

func newCollectionCreateCmd(state *AppState) *cobra.Command {
	var name string
	var description string
	var source string

	cmd := &cobra.Command{
		Use:   createVerb,
		Short: "Create an empty collection",
		Example: `  echopoint collection create --name "Payments API"
  echopoint collection create --name "Payments API" --description "Staging requests"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			req := api.CreateCollectionRequest{
				Name: name,
			}
			if description != "" {
				req.Description = &description
			}
			if source != "" {
				value := api.CollectionSource(source)
				req.Source = &value
			}

			resp, err := state.Client.API().CreateCollectionWithResponse(context.Background(), nil, req)
			if err != nil {
				return err
			}
			if resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON201)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON201)
			default:
				fmt.Fprintf(os.Stdout, "ID: %s\n", resp.JSON201.Id)
				fmt.Fprintf(os.Stdout, "Name: %s\n", resp.JSON201.Name)
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Collection name")
	cmd.Flags().StringVar(&description, "description", "", "Collection description")
	cmd.Flags().StringVar(&source, "source", "", "Collection source (manual, openapi)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newCollectionUpdateCmd(state *AppState) *cobra.Command {
	var name string
	var description string

	cmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Update a collection",
		Example: `  echopoint collection update <id> --name "Payments API v2"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid collection id")
			}

			req := api.UpdateCollectionRequest{}
			if name != "" {
				req.Name = &name
			}
			if description != "" {
				req.Description = &description
			}

			resp, err := state.Client.API().UpdateCollectionWithResponse(context.Background(), id, nil, req)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON200)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON200)
			default:
				fmt.Fprintf(os.Stdout, "ID: %s\n", resp.JSON200.Id)
				fmt.Fprintf(os.Stdout, "Name: %s\n", resp.JSON200.Name)
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Collection name")
	cmd.Flags().StringVar(&description, "description", "", "Collection description")
	cmd.ValidArgsFunction = completeCollectionArgs(state)
	return cmd
}

func newCollectionDeleteCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a collection",
		Example: `  echopoint collection delete <id>
  echopoint collection delete <id> --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid collection id")
			}
			if err := confirmDestructive(cmd, state, "delete", "collection "+id.String()); err != nil {
				return err
			}

			resp, err := state.Client.API().DeleteCollectionWithResponse(context.Background(), id, nil)
			if err != nil {
				return err
			}
			if resp.HTTPResponse.StatusCode != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Fprintln(os.Stdout, "Collection deleted.")
			return nil
		},
	}

	cmd.ValidArgsFunction = completeCollectionArgs(state)
	return quietOnError(cmd)
}

func newCollectionImportCmd(state *AppState) *cobra.Command {
	var file string
	var name string
	var tagsAsFolders = true

	cmd := &cobra.Command{
		Use:   "import -f <file>",
		Short: "Import a collection from an OpenAPI file",
		Example: `  echopoint collection import -f openapi.json
  echopoint collection import -f openapi.json --name "Payments API" --tags-as-folders=false`,
		Args: fileFlagArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			var spec map[string]any
			if err := loadJSONFile(file, &spec); err != nil {
				return err
			}

			req := api.ImportOpenAPIRequest{
				Spec: &spec,
			}

			if name != "" || cmd.Flags().Changed("tags-as-folders") {
				opts := &api.OpenAPIImportOptions{}
				if name != "" {
					opts.CollectionName = &name
				}
				opts.TagsAsFolders = &tagsAsFolders
				req.Options = opts
			}

			resp, err := state.Client.API().ImportFromOpenAPIWithResponse(context.Background(), nil, req)
			if err != nil {
				return err
			}
			if resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON201)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON201)
			default:
				fmt.Fprintf(os.Stdout, "Collection imported: %s\n", resp.JSON201.Collection.Name)
				fmt.Fprintf(os.Stdout, "ID: %s\n", resp.JSON201.Collection.Id)
				fmt.Fprintf(os.Stdout, "Requests created: %d\n", resp.JSON201.RequestsCreated)
				fmt.Fprintf(os.Stdout, "Folders created: %d\n", resp.JSON201.FoldersCreated)
				return nil
			}
		},
	}

	addFileFlag(cmd, &file, "OpenAPI file to import (JSON)", "json")
	cmd.Flags().StringVar(&name, "name", "", "Collection name (defaults to API title)")
	cmd.Flags().BoolVar(&tagsAsFolders, "tags-as-folders", true, "Use OpenAPI tags as folder structure")
	_ = cmd.MarkFlagRequired("file")
	cmd.ValidArgsFunction = cobra.NoFileCompletions
	return quietOnError(cmd)
}
