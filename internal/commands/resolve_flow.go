package commands

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// notAFlowIDError is an argument that is not a flow id; it keeps what the parser
// said, so that 'flow run' can report it in its own words.
type notAFlowIDError struct {
	arg   string
	cause error
}

func (err *notAFlowIDError) Error() string {
	return fmt.Sprintf("%q is not a flow id; list the flows with: echopoint flow list", err.arg)
}

func (err *notAFlowIDError) Unwrap() error { return err.cause }

// resolveFlowID turns a <flow-id> argument into the flow's id. It is the one
// place every flow argument goes through, and makes no request.
func resolveFlowID(_ context.Context, _ *AppState, arg string) (uuid.UUID, error) {
	id, err := uuid.Parse(arg)
	if err != nil {
		return uuid.Nil, &notAFlowIDError{arg: arg, cause: err}
	}
	return id, nil
}

// completeFlowFlag completes the id of a flow for --flow-id.
func completeFlowFlag(state *AppState) completionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeFromAPI(state, cmd, toComplete, fetchFlowCandidates)
	}
}
