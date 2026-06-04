package commands

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	googleuuid "github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

// templateRefPattern matches {{ ... }} references in a node's serialized form.
var templateRefPattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// newFlowValidateCmd statically validates a flow's graph before it is run.
func newFlowValidateCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:           "validate <flow-id>",
		Short:         "Statically validate a flow's graph (edges, references, cycles)",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Validate a flow without running it. Reports, in one pass, every:
  - edge that points at a node that does not exist
  - {{node.output}} reference that has no path from that node to the one using it
  - cycle in the graph

Exits non-zero if any problem is found.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			flowID, err := googleuuid.Parse(args[0])
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

			problems, err := validateFlowDefinition(resp.JSON200.FlowDefinition)
			if err != nil {
				return err
			}
			if len(problems) == 0 {
				fmt.Printf("✓ Flow %s is valid (%d nodes, %d edges)\n",
					args[0], len(resp.JSON200.FlowDefinition.Nodes), len(resp.JSON200.FlowDefinition.Edges))
				return nil
			}
			fmt.Fprintf(os.Stderr, "✗ Flow %s has %d problem(s):\n", args[0], len(problems))
			for i, p := range problems {
				fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, p)
			}
			return &exitCodeError{code: 1}
		},
	}
}

// validateFlowDefinition returns a sorted list of human-readable problems.
func validateFlowDefinition(def api.FlowDefinition) ([]string, error) {
	nodeIDs := make(map[string]bool, len(def.Nodes))
	for _, n := range def.Nodes {
		id, err := flowNodeID(n)
		if err != nil {
			return nil, err
		}
		nodeIDs[id] = true
	}

	// adjacency: source -> targets
	adj := make(map[string][]string, len(def.Edges))
	var problems []string
	for _, e := range def.Edges {
		if !nodeIDs[e.Source] {
			problems = append(problems, fmt.Sprintf("edge %s has unknown source node %q", e.Id, e.Source))
		}
		if !nodeIDs[e.Target] {
			problems = append(problems, fmt.Sprintf("edge %s has unknown target node %q", e.Id, e.Target))
		}
		adj[e.Source] = append(adj[e.Source], e.Target)
	}

	problems = append(problems, validateReferences(def.Nodes, nodeIDs, adj)...)
	if cycle := findCycle(nodeIDs, adj); cycle != "" {
		problems = append(problems, "graph contains a cycle: "+cycle)
	}

	sort.Strings(problems)
	return problems, nil
}

// validateReferences checks that every {{node.key}} used by a node is reachable
// from that node. References without a dot are treated as initial/env inputs.
func validateReferences(nodes []api.FlowNode, nodeIDs map[string]bool, adj map[string][]string) []string {
	var problems []string
	for _, n := range nodes {
		id, err := flowNodeID(n)
		if err != nil {
			continue
		}
		raw, err := n.MarshalJSON()
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, m := range templateRefPattern.FindAllStringSubmatch(string(raw), -1) {
			ref := strings.TrimSpace(m[1])
			dot := strings.IndexByte(ref, '.')
			if dot <= 0 {
				continue // initial input / env var, not a node output
			}
			source := ref[:dot]
			if source == id || seen[source] {
				continue
			}
			seen[source] = true
			if !nodeIDs[source] {
				problems = append(problems, fmt.Sprintf(
					"node %q references {{%s}} but no node %q exists", id, ref, source))
				continue
			}
			if !reachable(source, id, adj) {
				problems = append(problems, fmt.Sprintf(
					"node %q references {{%s}} but there is no edge path from %q to %q",
					id, ref, source, id))
			}
		}
	}
	return problems
}

// reachable reports whether target is reachable from source following edges.
func reachable(source, target string, adj map[string][]string) bool {
	visited := map[string]bool{}
	stack := []string{source}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, next := range adj[cur] {
			if next == target {
				return true
			}
			if !visited[next] {
				visited[next] = true
				stack = append(stack, next)
			}
		}
	}
	return false
}

// findCycle returns a short description of a cycle, or "" if the graph is acyclic.
func findCycle(nodeIDs map[string]bool, adj map[string][]string) string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(nodeIDs))
	var path []string
	var visit func(string) string
	visit = func(node string) string {
		color[node] = gray
		path = append(path, node)
		for _, next := range adj[node] {
			switch color[next] {
			case gray:
				return strings.Join(append(path, next), " -> ")
			case white:
				if c := visit(next); c != "" {
					return c
				}
			}
		}
		path = path[:len(path)-1]
		color[node] = black
		return ""
	}
	ids := make([]string, 0, len(nodeIDs))
	for id := range nodeIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if color[id] == white {
			if c := visit(id); c != "" {
				return c
			}
		}
	}
	return ""
}
