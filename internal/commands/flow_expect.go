package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"echopoint-cli/internal/api"

	googleuuid "github.com/google/uuid"
	"github.com/spf13/cobra"
)

const (
	nodeTypeWebhookWait = "webhook_wait"
	addToNodeUse        = "add <flow-id> <node-id>"
)

var assertionOperators = []string{
	"equals", "notEquals", "contains", "notContains", "greaterThan", "lessThan",
	"greaterThanOrEqual", "lessThanOrEqual", "empty", "notEmpty", "startsWith", "endsWith", "regex",
}

var valuelessOperators = []string{"empty", "notEmpty"}

// parseMatch reads one check of an expected event, written as
// "<target> <operator> [value]". The target is a JSONPath ($.type), a header
// (header:webhook-signature) or the whole body (body). The value is the rest of
// the line, so it may hold spaces and {{node.output}} templates.
func parseMatch(match string) (api.CompositeAssertion, error) {
	parts := strings.SplitN(strings.TrimSpace(match), " ", 3)
	if len(parts) < 2 {
		return api.CompositeAssertion{}, fmt.Errorf(
			"--match %q: expected \"<target> <operator> [value]\", such as \"$.type equals order.created\"", match)
	}
	target, operator := parts[0], parts[1]
	if !slices.Contains(assertionOperators, operator) {
		return api.CompositeAssertion{}, fmt.Errorf("--match %q: unknown operator %q (one of %s)",
			match, operator, strings.Join(assertionOperators, ", "))
	}

	assertion := api.CompositeAssertion{
		OperatorType:  api.OperatorType(operator),
		OperatorData:  map[string]any{},
		ExtractorData: map[string]any{},
	}
	switch {
	case strings.HasPrefix(target, "$"):
		assertion.ExtractorType = api.ExtractorTypeJsonPath
		assertion.ExtractorData["path"] = target
	case strings.HasPrefix(target, "header:") && len(target) > len("header:"):
		assertion.ExtractorType = api.ExtractorTypeHeader
		assertion.ExtractorData["header_name"] = strings.TrimPrefix(target, "header:")
	case target == string(api.ExtractorTypeBody):
		assertion.ExtractorType = api.ExtractorTypeBody
	default:
		return api.CompositeAssertion{}, fmt.Errorf(
			"--match %q: target must be a JSONPath ($.type), header:<name> or body", match)
	}

	hasValue := len(parts) == 3
	valueless := slices.Contains(valuelessOperators, operator)
	switch {
	case valueless && hasValue:
		return api.CompositeAssertion{}, fmt.Errorf("--match %q: %s takes no value", match, operator)
	case !valueless && !hasValue:
		return api.CompositeAssertion{}, fmt.Errorf("--match %q: %s needs a value", match, operator)
	case hasValue:
		assertion.OperatorData["value"] = parts[2]
	}
	return assertion, nil
}

// expectationCount turns the count flags into min and max. No flag means at
// least once.
func expectationCount(once, never bool, minimum, maximum int) (*int, *int, error) {
	chosen := 0
	for _, set := range []bool{once, never, minimum >= 0 || maximum >= 0} {
		if set {
			chosen++
		}
	}
	if chosen > 1 {
		return nil, nil, errors.New("use one of --once, --never, or --min/--max")
	}
	switch {
	case once:
		return new(1), new(1), nil
	case never:
		return new(0), new(0), nil
	}
	var minPtr, maxPtr *int
	if minimum >= 0 {
		minPtr = new(minimum)
	}
	if maximum >= 0 {
		maxPtr = new(maximum)
	}
	if minPtr != nil && maxPtr != nil && *maxPtr < *minPtr {
		return nil, nil, errors.New("--max must not be below --min")
	}
	return minPtr, maxPtr, nil
}

// editWebhookWait loads a flow, lets edit change one webhook wait node, and saves the flow.
func editWebhookWait(
	state *AppState, flowArg, nodeID string, edit func(node *api.WebhookWaitFlowNode) error,
) error {
	if err := requireToken(state); err != nil {
		return err
	}
	flowID, err := googleuuid.Parse(flowArg)
	if err != nil {
		return fmt.Errorf("invalid flow ID: %w", err)
	}
	resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID, nil)
	if err != nil {
		return fmt.Errorf("failed to get flow: %w", err)
	}
	if resp.JSON200 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	definition := resp.JSON200.FlowDefinition

	index := -1
	var wait api.WebhookWaitFlowNode
	for i, node := range definition.Nodes {
		candidate, decodeErr := node.ValueByDiscriminator()
		if waitNode, ok := candidate.(api.WebhookWaitFlowNode); decodeErr == nil && ok && waitNode.Id == nodeID {
			index, wait = i, waitNode
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("webhook wait node not found: %s", nodeID)
	}
	if err := edit(&wait); err != nil {
		return err
	}
	if err := definition.Nodes[index].FromWebhookWaitFlowNode(wait); err != nil {
		return fmt.Errorf("failed to encode node: %w", err)
	}

	updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, nil,
		api.UpdateFlowRequest{FlowDefinition: &definition})
	if err != nil {
		return fmt.Errorf("failed to update flow: %w", err)
	}
	if updateResp.JSON200 == nil {
		return formatAPIError(updateResp.HTTPResponse, updateResp.Body)
	}
	return nil
}

func newFlowNodeExpectCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "expect",
		Short: "Manage the events a webhook wait expects",
		Long: `A webhook wait can expect a set of named events and judge them together, as the
final check of a flow. Each expected event is a group of checks: an event satisfies
it when it passes all of them, and one event counts for one expected event only.
Checks resolve {{node.output}} templates, which ties an event to the resource that
caused it. The node's own assertions ("flows node assertion add") then run on
every event an expected event claimed.`,
	}
	cmd.AddCommand(newFlowNodeExpectAddCmd(state), newFlowNodeExpectRemoveCmd(state))
	return cmd
}

func newFlowNodeExpectAddCmd(state *AppState) *cobra.Command {
	var name string
	var matches []string
	var once, never bool
	var minimum, maximum int

	cmd := &cobra.Command{
		Use:   addToNodeUse,
		Short: "Add an expected event to a webhook wait",
		Args:  cobra.ExactArgs(2),
		Long: `Add an expected event to a webhook wait node.

Each --match is one check, written "<target> <operator> [value]":
  $.data.invitation_id equals {{invite-member.id}}   JSONPath into the request body
  header:webhook-signature startsWith v1,            a request header
  body notContains anchor_inv_                       the whole body

Count (default: at least once):
  --once        exactly one event
  --never       no event may arrive
  --min/--max   a range

Examples:
  echopoint flows node expect add <flow-id> events --name "Resent with a new token" \
    --match '$.type equals organization.invitation.updated' \
    --match '$.data.invitation_id equals {{invite-resend.id}}'

  echopoint flows node expect add <flow-id> events --name "No accept after withdrawal" --never \
    --match '$.type equals organization.invitation.accepted' \
    --match '$.data.invitation_id equals {{invite-withdraw.id}}'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required: the run report lists expected events by name")
			}
			if len(matches) == 0 {
				return errors.New("at least one --match is required, or any event would satisfy it")
			}
			assertions := make([]api.CompositeAssertion, 0, len(matches))
			for _, match := range matches {
				assertion, err := parseMatch(match)
				if err != nil {
					return err
				}
				assertions = append(assertions, assertion)
			}
			minPtr, maxPtr, err := expectationCount(once, never, minimum, maximum)
			if err != nil {
				return err
			}

			err = editWebhookWait(state, args[0], args[1], func(node *api.WebhookWaitFlowNode) error {
				expectations := []api.WebhookExpectation{}
				if node.Data.Expect != nil {
					expectations = *node.Data.Expect
				}
				for _, existing := range expectations {
					if existing.Name == name {
						return fmt.Errorf("expected event %q already exists; remove it first", name)
					}
				}
				expectations = append(expectations, api.WebhookExpectation{
					Name: name, Min: minPtr, Max: maxPtr, Assertions: assertions,
				})
				node.Data.Expect = &expectations
				return nil
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Expected event added: %s (%d checks)\n", name, len(assertions))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Name of the expected event, as the run report shows it")
	cmd.Flags().StringArrayVar(&matches, "match", nil,
		`A check "<target> <operator> [value]" (repeatable); all must pass`)
	cmd.Flags().BoolVar(&once, "once", false, "Exactly one event")
	cmd.Flags().BoolVar(&never, "never", false, "No event may arrive")
	cmd.Flags().IntVar(&minimum, "min", -1, "Fewest events (default 1)")
	cmd.Flags().IntVar(&maximum, "max", -1, "Most events (default unbounded)")
	return cmd
}

func newFlowNodeExpectRemoveCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <flow-id> <node-id> <name>",
		Short: "Remove an expected event from a webhook wait",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[2]
			err := editWebhookWait(state, args[0], args[1], func(node *api.WebhookWaitFlowNode) error {
				if node.Data.Expect == nil {
					return fmt.Errorf("expected event not found: %s", name)
				}
				kept := slices.DeleteFunc(slices.Clone(*node.Data.Expect), func(e api.WebhookExpectation) bool {
					return e.Name == name
				})
				if len(kept) == len(*node.Data.Expect) {
					return fmt.Errorf("expected event not found: %s", name)
				}
				node.Data.Expect = &kept
				return nil
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Expected event removed: %s\n", name)
			return nil
		},
	}
}
