package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

func newOrgViewCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:     viewVerb,
		Aliases: []string{getVerb, "show"},
		Short:   "Show the organization's name and id",
		Example: `  echopoint org view
  echopoint org view --org org_123 -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}
			resp, err := state.Client.API().GetOrganizationWithResponse(context.Background(), nil)
			if err != nil {
				return fmt.Errorf("failed to get organization: %w", err)
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printOrganization(os.Stdout, state.OutputFormat, *resp.JSON200)
		},
	}
}

func newOrgRenameCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <name>",
		Short: "Rename the organization",
		Long: `Rename the organization. The name has 2 to 100 characters; leading and
trailing spaces are removed. Renaming needs the organization:update permission,
which organization admins hold.`,
		Example: `  echopoint org rename "Acme Payments"
  echopoint org rename "Acme Payments" --org org_123 -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("the organization name is empty")
			}
			resp, err := state.Client.API().RenameOrganizationWithResponse(
				context.Background(), nil, api.RenameOrganizationJSONRequestBody{Name: name},
			)
			if err != nil {
				return fmt.Errorf("failed to rename organization: %w", err)
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printOrganization(os.Stdout, state.OutputFormat, *resp.JSON200)
		},
	}
}

func printOrganization(w io.Writer, format output.Format, organization api.Organization) error {
	switch format {
	case output.FormatJSON:
		return output.PrintJSON(w, organization)
	case output.FormatYAML:
		return output.PrintYAML(w, organization)
	}
	fmt.Fprintf(w, "Name: %s\nID:   %s\n", organization.Name, organization.Id)
	return nil
}
