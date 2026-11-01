package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

const columnRole = "Role"

// newWorkspaceCmd groups the commands that manage the organization's
// workspaces and who holds a role on them.
func newWorkspaceCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     workspaceCommandName,
		Aliases: []string{"workspaces"},
		Short:   "Manage workspaces and the members who hold a role on them",
		Long: `Manage the organization's workspaces.

A workspace holds flows, collections, webhooks, monitors and specs. A member
reaches it through their organization role, which covers every workspace, or
through a role held on that workspace alone. --workspace (or
ECHOPOINT_WORKSPACE_ID) selects the workspace other commands act in.`,
	}
	cmd.AddCommand(
		newWorkspaceListCmd(state),
		newWorkspaceCreateCmd(state),
		newWorkspaceRenameCmd(state),
		newWorkspaceMemberCmd(state),
		newWorkspaceRoleCmd(state),
	)
	return cmd
}

// workspaceRow is a workspace the signed-in member can open, with what they
// hold in it.
type workspaceRow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Role        *string  `json:"role"`
	Permissions []string `json:"permissions"`
}

func newWorkspaceListCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   listVerb,
		Short: "List the workspaces you can open in the organization",
		Long: `List the organization's workspaces you can open, oldest first, with the
role you hold on each. A workspace reached only through your organization role
shows no role.

It reads your session, so it needs 'echopoint auth login', not an API key.`,
		Example: `  echopoint workspace list
  echopoint workspace list -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			workspaces, err := fetchWorkspaces(cmd.Context(), state)
			if err != nil {
				return err
			}
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, workspaces)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, workspaces)
			}
			rows := make([][]string, 0, len(workspaces))
			for _, workspace := range workspaces {
				role := "(organization role)"
				if workspace.Role != nil {
					role = *workspace.Role
				}
				rows = append(rows, []string{workspace.ID, workspace.Name, role})
			}
			return output.PrintTable([]string{columnID, columnName, columnRole}, rows)
		},
	}
}

// fetchWorkspaces reads the current organization's workspaces from /me.
func fetchWorkspaces(ctx context.Context, state *AppState) ([]workspaceRow, error) {
	if state.Token == "" {
		return nil, fmt.Errorf("listing workspaces reads your session: sign in with 'echopoint auth login'")
	}
	include := api.CurrentUserIncludeParameter{api.CurrentUserIncludeWorkspace}
	resp, err := state.Client.API().GetMeWithResponse(ctx, &api.GetMeParams{Include: &include})
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	for _, organization := range resp.JSON200.User.Organizations {
		if organization.Id != state.OrganizationID || organization.Workspaces == nil {
			continue
		}
		rows := make([]workspaceRow, 0, len(*organization.Workspaces))
		for _, workspace := range *organization.Workspaces {
			row := workspaceRow{ID: workspace.Id, Name: workspace.Name, Permissions: workspace.Permissions}
			if workspace.Role != nil {
				row.Role = &workspace.Role.Name
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
	return []workspaceRow{}, nil
}

func newWorkspaceCreateCmd(state *AppState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   createVerb,
		Short: "Create a workspace; you hold its admin role",
		Long: `Create a workspace in the organization. Signed in, you become its first
member with the workspace_admin role. It needs the workspaces:create permission.`,
		Example: `  echopoint workspace create --name Payments
  echopoint workspace create --name Payments -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().CreateWorkspaceWithResponse(
				cmd.Context(), nil, api.CreateWorkspaceJSONRequestBody{Name: strings.TrimSpace(name)},
			)
			if err != nil {
				return err
			}
			if resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printWorkspace(state, *resp.JSON201, "created")
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Name of the workspace (1 to 100 characters)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newWorkspaceRenameCmd(state *AppState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "rename <workspace-id>",
		Short: "Rename a workspace",
		Long: `Rename a workspace. It needs workspace.settings:update on that workspace,
from a role on it or from your organization role.`,
		Example: `  echopoint workspace rename ws_2abc --name "Payments EU"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().RenameWorkspaceWithResponse(
				cmd.Context(), args[0], nil, api.RenameWorkspaceJSONRequestBody{Name: strings.TrimSpace(name)},
			)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printWorkspace(state, *resp.JSON200, "renamed")
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New name of the workspace (1 to 100 characters)")
	_ = cmd.MarkFlagRequired("name")
	cmd.ValidArgsFunction = completeWorkspaceArgs(state)
	return cmd
}

func printWorkspace(state *AppState, workspace api.Workspace, done string) error {
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(os.Stdout, workspace)
	case output.FormatYAML:
		return output.PrintYAML(os.Stdout, workspace)
	}
	fmt.Fprintf(os.Stdout, "%s %s  %s\n", done, workspace.Name, workspace.Id)
	return nil
}

func newWorkspaceMemberCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "member",
		Aliases: []string{"members"},
		Short:   "Manage who holds a role on a workspace",
		Long: `List, add, change and remove the members who hold a role on a workspace.

A member is an organization member, addressed by their user id ('member list'
shows it). Adding one takes effect at once: no invitation, no email. Nobody
changes or removes their own role. These commands need workspace.access:read
to list and workspace.access:manage to change.`,
	}
	cmd.AddCommand(
		newWorkspaceMemberListCmd(state),
		newWorkspaceMemberSetCmd(state),
		newWorkspaceMemberRemoveCmd(state),
	)
	return cmd
}

func newWorkspaceMemberListCmd(state *AppState) *cobra.Command {
	var pages pageFlags
	cmd := &cobra.Command{
		Use:   "list <workspace-id>",
		Short: "List the members who hold a role on a workspace",
		Long: `List the members who hold a role on a workspace, oldest first. Organization
admins reach every workspace through their organization role and are not listed.`,
		Example: `  echopoint workspace member list ws_2abc
  echopoint workspace member list ws_2abc --all -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			page, err := fetchSearch(cmd.Context(), cmd, &pages, searchWorkspaceMembers(state, args[0]))
			if err != nil {
				return err
			}
			return printSearchPage(cmd, state, page, func(w io.Writer) error {
				rows := make([][]string, 0, len(page.Items))
				for _, member := range page.Items {
					rows = append(rows, []string{member.UserId, string(member.Email), member.Role.Name})
				}
				fmt.Fprintf(w, "Total: %d\n", page.Total)
				return output.PrintTableTo(w, []string{"User ID", "Email", columnRole}, rows)
			})
		},
	}
	pages.bind(cmd, defaultListLimit)
	cmd.ValidArgsFunction = completeWorkspaceArgs(state)
	return cmd
}

// searchWorkspaceMembers reads one page of a workspace's members.
func searchWorkspaceMembers(state *AppState, workspaceID string) pageFetcher[api.WorkspaceMember] {
	return func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.WorkspaceMember], error) {
		resp, err := state.Client.API().SearchWorkspaceMembersWithResponse(
			ctx, workspaceID, nil, api.SearchWorkspaceMembersJSONRequestBody{Pagination: pagination},
		)
		if err != nil {
			return searchPage[api.WorkspaceMember]{}, err
		}
		if resp.JSON200 == nil {
			return searchPage[api.WorkspaceMember]{}, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
	}
}

func newWorkspaceMemberSetCmd(state *AppState) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "set <workspace-id> <user-id>",
		Short: "Give an organization member a role on a workspace, or change it",
		Long: `Give an organization member a role on a workspace, or change the one they hold.
--role takes a role id or name: workspace_admin, workspace_editor or
workspace_viewer ('echopoint workspace role list' shows them).`,
		Example: `  echopoint workspace member set ws_2abc pusr_2def --role workspace_editor
  echopoint workspace member set ws_2abc pusr_2def --role prole_2ghi -o json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			roleID, err := resolveWorkspaceRole(cmd.Context(), state, args[0], role)
			if err != nil {
				return err
			}
			resp, err := state.Client.API().SetWorkspaceMemberRoleWithResponse(
				cmd.Context(), args[0], args[1], nil, api.SetWorkspaceMemberRoleJSONRequestBody{RoleId: roleID},
			)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			member := *resp.JSON200
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, member)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, member)
			}
			fmt.Fprintf(os.Stdout, "%s holds %s on %s\n", member.Email, member.Role.Name, args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "Role id or name, such as workspace_editor")
	_ = cmd.MarkFlagRequired("role")
	cmd.ValidArgsFunction = completeWorkspaceArgs(state)
	return cmd
}

func newWorkspaceMemberRemoveCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <workspace-id> <user-id>",
		Short: "Remove the role a member holds on a workspace",
		Long: `Remove the role a member holds on a workspace. Their organization role does not
change, so an organization admin keeps reaching it. Removing a member who holds
no role succeeds.`,
		Example: `  echopoint workspace member remove ws_2abc pusr_2def`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().RemoveWorkspaceMemberWithResponse(cmd.Context(), args[0], args[1], nil)
			if err != nil {
				return err
			}
			if resp.HTTPResponse.StatusCode != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			fmt.Fprintf(os.Stdout, "removed %s from %s\n", args[1], args[0])
			return nil
		},
	}
	cmd.ValidArgsFunction = completeWorkspaceArgs(state)
	return cmd
}

func newWorkspaceRoleCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "role",
		Aliases: []string{"roles"},
		Short:   "Show the roles a member can hold on a workspace",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list <workspace-id>",
		Short: "List the roles a member can hold on a workspace, with their permissions",
		Example: `  echopoint workspace role list ws_2abc
  echopoint workspace role list ws_2abc -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			roles, err := fetchWorkspaceRoles(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, roles)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, roles)
			}
			rows := make([][]string, 0, len(roles))
			for _, role := range roles {
				rows = append(rows, []string{role.Id, role.Name, strings.Join(role.Permissions, ", ")})
			}
			return output.PrintTable([]string{columnID, columnName, "Permissions"}, rows)
		},
	})
	return cmd
}

func fetchWorkspaceRoles(ctx context.Context, state *AppState, workspaceID string) ([]api.WorkspaceRole, error) {
	resp, err := state.Client.API().ListWorkspaceRolesWithResponse(ctx, workspaceID, nil)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200.Items, nil
}

// resolveWorkspaceRole turns a role name into its id; an id is kept as given.
func resolveWorkspaceRole(ctx context.Context, state *AppState, workspaceID, role string) (string, error) {
	role = strings.TrimSpace(role)
	roles, err := fetchWorkspaceRoles(ctx, state, workspaceID)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(roles))
	for _, candidate := range roles {
		if candidate.Id == role || strings.EqualFold(candidate.Name, role) {
			return candidate.Id, nil
		}
		names = append(names, candidate.Name)
	}
	return "", fmt.Errorf("%q is not a workspace role; use one of: %s", role, strings.Join(names, ", "))
}

// completeWorkspaceArgs completes a command's first argument with the
// workspaces the signed-in member can open.
func completeWorkspaceArgs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return noCompletion()
		}
		return completeFromAPI(state, cmd, toComplete,
			func(ctx context.Context, _ *api.ClientWithResponses) []completionCandidate {
				workspaces, err := fetchWorkspaces(ctx, state)
				if err != nil {
					return nil
				}
				candidates := make([]completionCandidate, 0, len(workspaces))
				for _, workspace := range workspaces {
					candidates = append(candidates, completionCandidate{workspace.ID, oneLine(workspace.Name)})
				}
				return candidates
			})
	}
}
