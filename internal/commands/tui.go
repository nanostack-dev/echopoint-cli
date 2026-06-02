package commands

import (
	"fmt"
	"os"

	"echopoint-cli/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newTUICmd(state *AppState) *cobra.Command {
	var flagDebug bool

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch interactive TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Require authentication before launching TUI
			if err := requireToken(state); err != nil {
				return err
			}

			// Set debug environment variable if --debug flag is used
			if flagDebug {
				os.Setenv("ECHOPOINT_DEBUG", "DEBUG")
			}

			// Launch TUI with authenticated client
			model := tui.New(state.Client)
			program := tea.NewProgram(model, tea.WithAltScreen())
			if _, err := program.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&flagDebug, "debug", false, "Enable debug logging for flow editor")

	return cmd
}
