package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"echopoint-cli/internal/auth"
	"echopoint-cli/internal/config"

	"github.com/spf13/cobra"
)

// recoverSessionOrganizationID finds the organization of a session stored without one,
// which a login saves when its /me lookup fails or finds no organization yet. It is the
// user's only organization, saved to the profile so later commands skip the lookup; with
// several it picks none, so a request never lands in an organization nobody chose.
func recoverSessionOrganizationID(ctx context.Context, cfg config.Config, token string) string {
	organizations, err := sessionOrganizations(ctx, cfg.API.BaseURL, token, cfg.API.Timeout)
	if err != nil || len(organizations) == 0 {
		return "" // the command's own request reports a session the API refuses
	}
	if len(organizations) > 1 {
		names := make([]string, 0, len(organizations))
		for _, organization := range organizations {
			names = append(names, fmt.Sprintf("%s (%s)", organization.Name, organization.ID))
		}
		fmt.Fprintf(os.Stderr, "Warning: you belong to %d organizations, pass --org with one of: %s\n",
			len(organizations), strings.Join(names, ", "))
		return ""
	}

	organizationID := organizations[0].ID
	creds := loadOrEmptyCredentials(cfg.Profile)
	if creds.AccessToken == token { // a --token or ECHOPOINT_TOKEN session is not the profile's to update
		creds.OrganizationID = organizationID
		if _, err := auth.SaveCredentials(cfg.Profile, creds); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save organization %s to profile %s: %v\n",
				organizationID, cfg.Profile, err)
		}
	}
	return organizationID
}

// contextOf is the command's context, which is nil when completion configures the state.
func contextOf(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
