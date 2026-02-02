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

func newCollectionsCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collections",
		Short: "Manage collections",
	}

	cmd.AddCommand(
		newCollectionsListCmd(state),
		newCollectionsGetCmd(state),
		newCollectionsCreateCmd(state),
		newCollectionsUpdateCmd(state),
		newCollectionsDeleteCmd(state),
		newCollectionsImportCmd(state),
	)

	return cmd
}

func newCollectionsListCmd(state *AppState) *cobra.Command {
	var limit int32 = 20
	var offset int32

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List collections",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			params := &api.ListCollectionsParams{
				Limit:  api.LimitParameter(limit),
				Offset: api.OffsetParameter(offset),
			}

			resp, err := state.Client.API().ListCollectionsWithResponse(context.Background(), params)
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
				rows := make([][]string, 0, len(resp.JSON200.Items))
				for _, collection := range resp.JSON200.Items {
					rows = append(
						rows,
						[]string{collection.Id.String(), collection.Name, collection.UpdatedAt.String()},
					)
				}
				fmt.Fprintf(os.Stdout, "Total: %d\n", resp.JSON200.Total)
				return output.PrintTable([]string{"ID", "Name", "Updated"}, rows)
			}
		},
	}

	cmd.Flags().Int32Var(&limit, "limit", 20, "Number of results to return")
	cmd.Flags().Int32Var(&offset, "offset", 0, "Offset for pagination")

	return cmd
}

func newCollectionsGetCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get collection details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid collection id")
			}

			resp, err := state.Client.API().GetCollectionWithResponse(context.Background(), id)
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

	return cmd
}

func newCollectionsCreateCmd(state *AppState) *cobra.Command {
	var name string
	var description string
	var source string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a collection",
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

			resp, err := state.Client.API().CreateCollectionWithResponse(context.Background(), req)
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

func newCollectionsUpdateCmd(state *AppState) *cobra.Command {
	var name string
	var description string

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a collection",
		Args:  cobra.ExactArgs(1),
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

			resp, err := state.Client.API().UpdateCollectionWithResponse(context.Background(), id, req)
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
	return cmd
}

func newCollectionsDeleteCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid collection id")
			}

			resp, err := state.Client.API().DeleteCollectionWithResponse(context.Background(), id)
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

	return cmd
}

func newCollectionsImportCmd(state *AppState) *cobra.Command {
	var file string
	var name string
	var tagsAsFolders = true

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import collection from OpenAPI spec",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if file == "" {
				return fmt.Errorf("--file is required")
			}

			var spec map[string]interface{}
			if err := loadJSONFile(file, &spec); err != nil {
				return err
			}

			req := api.ImportOpenAPIRequest{
				Spec: spec,
			}

			if name != "" || cmd.Flags().Changed("tags-as-folders") {
				opts := &api.OpenAPIImportOptions{}
				if name != "" {
					opts.CollectionName = &name
				}
				opts.TagsAsFolders = &tagsAsFolders
				req.Options = opts
			}

			resp, err := state.Client.API().ImportFromOpenAPIWithResponse(context.Background(), req)
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

	cmd.Flags().StringVar(&file, "file", "", "Path to OpenAPI spec (JSON or YAML)")
	cmd.Flags().StringVar(&name, "name", "", "Collection name (defaults to API title)")
	cmd.Flags().BoolVar(&tagsAsFolders, "tags-as-folders", true, "Use OpenAPI tags as folder structure")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}
