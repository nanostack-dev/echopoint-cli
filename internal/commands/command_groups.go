package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const (
	groupAnnotation           = "echopoint/group"
	suggestionsMinimumLetters = 2

	resourcesGroupID = "resources"
	accountGroupID   = "account"
	toolsGroupID     = "tools"
)

var rootCommandGroups = map[string]string{
	adminCommandName:      toolsGroupID,
	flowCommandName:       resourcesGroupID,
	specCommandName:       resourcesGroupID,
	collectionCommandName: resourcesGroupID,
	statusPageCommandName: resourcesGroupID,
	orgCommandName:        resourcesGroupID,
	authCommandName:       accountGroupID,
	profileCommandName:    accountGroupID,
	configCommandName:     accountGroupID,
	mcpCommandName:        toolsGroupID,
	updateCommandName:     toolsGroupID,
	versionCommandName:    toolsGroupID,
}

// groupRootCommands files the root's commands under the headings of its help,
// including the help and completion commands cobra adds itself.
func groupRootCommands(root *cobra.Command) {
	root.AddGroup(
		&cobra.Group{ID: resourcesGroupID, Title: "Resources:"},
		&cobra.Group{ID: accountGroupID, Title: "Account and setup:"},
		&cobra.Group{ID: toolsGroupID, Title: "Tools:"},
	)
	for _, child := range root.Commands() {
		child.GroupID = rootCommandGroups[child.Name()]
	}
	root.SetHelpCommandGroupID(toolsGroupID)
	root.SetCompletionCommandGroupID(toolsGroupID)
}

func rejectUnknownSubcommands(root *cobra.Command) {
	markCommandGroups(root)
	// a group is runnable now, but its usage still names only [command]
	runnable := `{{if and .Runnable (ne (index .Annotations "` + groupAnnotation + `") "` + annotationEnabled + `")}}`
	root.SetUsageTemplate(strings.Replace(root.UsageTemplate(), "{{if .Runnable}}", runnable, 1))
}

func markCommandGroups(root *cobra.Command) {
	for _, child := range root.Commands() {
		markCommandGroups(child)
		if !child.HasSubCommands() || child.Runnable() {
			continue
		}
		if child.Annotations == nil {
			child.Annotations = map[string]string{}
		}
		child.Annotations[groupAnnotation] = annotationEnabled
		child.Args = unknownSubcommandError
		child.SilenceUsage = true
		child.SilenceErrors = true
		child.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	}
}

func unknownSubcommandError(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	message := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = suggestionsMinimumLetters
	}
	if !cmd.DisableSuggestions {
		if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
			message += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t")
		}
	}
	return errors.New(message)
}

func skipsConfiguration(cmd *cobra.Command) bool {
	return cmd.Annotations[groupAnnotation] == annotationEnabled ||
		// completion sets up its own state, quietly, from the command it completes
		cmd.Name() == cobra.ShellCompRequestCmd ||
		cmd.Name() == cobra.ShellCompNoDescRequestCmd
}
