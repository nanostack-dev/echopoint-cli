package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const specSlugMaxLength = 64

var specSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func newSpecCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   specCommandName,
		Short: "Work with the OpenAPI specs kept in EchoPoint",
		Long: `Work with OpenAPI 3.0 and 3.1 specs kept in EchoPoint.

Every command works on a spec in EchoPoint, named by its unique slug: the first
argument. 'echopoint spec list' shows the slugs; a spec's title is free text.
Only the commands that read or write a real file take -f/--file: create and push
read one, pull writes one, and check, lint, and diff compare one with the spec's
Live version.

route, method, schema, property, param, and response edit the Live version in
EchoPoint, each with add, update, and remove (method has update only).`,
	}
	cmd.AddCommand(
		newSpecListCmd(state),
		newSpecViewCmd(state),
		newSpecVersionsCmd(state),
		newSpecCreateCmd(state),
		newSpecPushCmd(state),
		newSpecPullCmd(state),
		newSpecCheckCmd(state),
		newSpecLintCmd(state),
		newSpecDiffCmd(state),
		newSpecRouteCmd(state),
		newSpecMethodCmd(state),
		newSpecSchemaCmd(state),
		newSpecPropertyCmd(state),
		newSpecParamCmd(state),
		newSpecResponseCmd(state),
	)
	return cmd
}

func finishSpecCmd(state *AppState, cmd *cobra.Command) *cobra.Command {
	cmd.ValidArgsFunction = completeSpecSlugs(state)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}

func specArgs(count int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("name the spec by its slug: %s", cmd.UseLine())
		}
		if err := checkSpecSlug(cmd, args[0]); err != nil {
			return err
		}
		if err := cobra.ExactArgs(count)(cmd, args); err != nil {
			return fmt.Errorf("%w; usage: %s", err, cmd.UseLine())
		}
		return nil
	}
}

func checkSpecSlug(cmd *cobra.Command, slug string) error {
	if validSpecSlug(slug) || !looksLikePath(slug) {
		return nil
	}
	message := fmt.Sprintf("%q is not a spec slug. Spec slugs are listed by echopoint spec list", slug)
	if cmd.Flags().Lookup("file") != nil {
		message += "; pass a file with -f"
	}
	return errors.New(message + ".")
}

func validSpecSlug(slug string) bool {
	return len(slug) <= specSlugMaxLength && specSlugPattern.MatchString(slug)
}

func looksLikePath(arg string) bool {
	switch strings.ToLower(filepath.Ext(arg)) {
	case ".yaml", ".yml", ".json":
		return true
	}
	if strings.ContainsAny(arg, `/\`) {
		return true
	}
	info, err := os.Stat(arg)
	return err == nil && !info.IsDir()
}

func printStructured(w io.Writer, format output.Format, value any) (bool, error) {
	switch format {
	case output.FormatJSON:
		return true, output.PrintJSON(w, value)
	case output.FormatYAML:
		// the API types have only JSON tags
		return true, output.PrintYAML(w, value)
	case output.FormatTable:
	}
	return false, nil
}

func formatWhen(when time.Time) string {
	return when.Format("2006-01-02 15:04")
}

func actorLabel(actor api.SpecActor) string {
	if actor.Type == api.SpecActorTypeApiKey {
		return "API key " + actor.Id
	}
	return string(actor.Type) + " " + actor.Id
}

func describeChangeCounts(counts api.SpecChangeCounts) string {
	var parts []string
	for _, count := range []struct {
		number int32
		label  string
	}{
		{counts.Breaking, "breaking"}, {counts.Risky, "risky"}, {counts.Additive, "additive"}, {counts.Edit, "edit"},
	} {
		if count.number > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count.number, count.label))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func describeFindingCount(count *int32) string {
	if count == nil {
		return "not recorded"
	}
	return fmt.Sprint(*count)
}
