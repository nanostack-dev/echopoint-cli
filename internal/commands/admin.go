package commands

import (
	"context"
	"errors"
	"net/http"

	"github.com/spf13/cobra"
)

func newAdminCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   adminCommandName,
		Short: "Manage product administration with a privileged session",
		Long: `Manage product-wide settings with an Echopoint administrator session.

Select the deployment using --profile or --api-url. These operations apply to
every organization in that deployment; --org does not narrow their scope.
Organization API keys are not accepted.`,
	}
	cmd.AddCommand(newAdminCloudFleetCmd(state))
	return cmd
}

func requireAdminSession(state *AppState) error {
	if state.APIKey != "" {
		return errors.New("product administration requires a session token; organization API keys are not accepted")
	}
	if state.Token == "" {
		return errors.New(
			"product administration requires an administrator session; run 'echopoint auth login --admin' with the selected profile",
		)
	}
	return nil
}

func isProductAdminCommand(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if current.Name() == adminCommandName && isRootChild(current) {
			return true
		}
	}
	return false
}

func productAdminRequest(_ context.Context, request *http.Request) error {
	request.Header.Del("X-Organization-Id")
	return nil
}
