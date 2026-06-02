package floweditor

import (
	"github.com/google/uuid"
)

// NodeType represents the type of a flow node
type NodeType string

const (
	NodeTypeRequest NodeType = "request"
	NodeTypeDelay   NodeType = "delay"
	NodeTypeStart   NodeType = "start"
	NodeTypeEnd     NodeType = "end"
)

// EdgeType represents the type of connection between nodes
type EdgeType string

const (
	EdgeTypeSuccess EdgeType = "success"
	EdgeTypeFailure EdgeType = "failure"
)

// Node represents a node in the flow graph
type Node struct {
	ID         uuid.UUID
	Type       NodeType
	Name       string
	X, Y       int // Position in the grid
	Width      int
	Height     int
	Data       NodeData
	Assertions int // Count of assertions (for display)
	Outputs    int // Count of outputs (for display)
	Selected   bool
}

// NodeData contains type-specific node configuration
type NodeData struct {
	// Request node data
	URL     string
	Method  string
	Headers map[string]string
	Body    string

	// Delay node data
	Duration int // in milliseconds
}

// Edge represents a connection between two nodes
type Edge struct {
	ID       uuid.UUID
	From     uuid.UUID
	To       uuid.UUID
	Type     EdgeType
	Selected bool
}

// FlowGraph represents the entire flow structure
type FlowGraph struct {
	ID          uuid.UUID
	Name        string
	Description string
	Nodes       []Node
	Edges       []Edge
}

// EditorMode represents the current editing mode
type EditorMode int

const (
	ModeView EditorMode = iota
	ModeSelect
	ModeConnect
	ModeEdit
)

// String returns the string representation of the editor mode
func (m EditorMode) String() string {
	switch m {
	case ModeView:
		return "VIEW"
	case ModeSelect:
		return "SELECT"
	case ModeConnect:
		return "CONNECT"
	case ModeEdit:
		return "EDIT"
	default:
		return "UNKNOWN"
	}
}

// NewFlowGraph creates a new empty flow graph
func NewFlowGraph(id uuid.UUID, name string) *FlowGraph {
	return &FlowGraph{
		ID:    id,
		Name:  name,
		Nodes: make([]Node, 0),
		Edges: make([]Edge, 0),
	}
}

// AddNode adds a new node to the graph
func (g *FlowGraph) AddNode(nodeType NodeType, name string, x, y int) *Node {
	node := Node{
		ID:     uuid.New(),
		Type:   nodeType,
		Name:   name,
		X:      x,
		Y:      y,
		Width:  20,
		Height: 3,
	}

	// Adjust dimensions based on node type
	switch nodeType {
	case NodeTypeStart, NodeTypeEnd:
		node.Width = 12
		node.Height = 3
	}

	g.Nodes = append(g.Nodes, node)
	return &g.Nodes[len(g.Nodes)-1]
}

// AddEdge adds a new edge between two nodes
func (g *FlowGraph) AddEdge(from, to uuid.UUID, edgeType EdgeType) *Edge {
	edge := Edge{
		ID:   uuid.New(),
		From: from,
		To:   to,
		Type: edgeType,
	}
	g.Edges = append(g.Edges, edge)
	return &g.Edges[len(g.Edges)-1]
}

// GetNode returns a node by ID
func (g *FlowGraph) GetNode(id uuid.UUID) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

// GetEdge returns an edge by ID
func (g *FlowGraph) GetEdge(id uuid.UUID) *Edge {
	for i := range g.Edges {
		if g.Edges[i].ID == id {
			return &g.Edges[i]
		}
	}
	return nil
}

// DeleteNode removes a node and its connected edges
func (g *FlowGraph) DeleteNode(id uuid.UUID) {
	// Remove edges connected to this node
	newEdges := make([]Edge, 0, len(g.Edges))
	for _, edge := range g.Edges {
		if edge.From != id && edge.To != id {
			newEdges = append(newEdges, edge)
		}
	}
	g.Edges = newEdges

	// Remove the node
	newNodes := make([]Node, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		if node.ID != id {
			newNodes = append(newNodes, node)
		}
	}
	g.Nodes = newNodes
}

// GetIncomingEdges returns all edges pointing to a node
func (g *FlowGraph) GetIncomingEdges(nodeID uuid.UUID) []Edge {
	var edges []Edge
	for _, edge := range g.Edges {
		if edge.To == nodeID {
			edges = append(edges, edge)
		}
	}
	return edges
}

// GetOutgoingEdges returns all edges originating from a node
func (g *FlowGraph) GetOutgoingEdges(nodeID uuid.UUID) []Edge {
	var edges []Edge
	for _, edge := range g.Edges {
		if edge.From == nodeID {
			edges = append(edges, edge)
		}
	}
	return edges
}

// ClearSelection clears all selections
func (g *FlowGraph) ClearSelection() {
	for i := range g.Nodes {
		g.Nodes[i].Selected = false
	}
	for i := range g.Edges {
		g.Edges[i].Selected = false
	}
}

// SelectNode selects a single node
func (g *FlowGraph) SelectNode(id uuid.UUID) {
	g.ClearSelection()
	node := g.GetNode(id)
	if node != nil {
		node.Selected = true
	}
}

// GetSelectedNode returns the currently selected node (if any)
func (g *FlowGraph) GetSelectedNode() *Node {
	for i := range g.Nodes {
		if g.Nodes[i].Selected {
			return &g.Nodes[i]
		}
	}
	return nil
}

// MoveNode moves a node to a new position
func (g *FlowGraph) MoveNode(id uuid.UUID, x, y int) {
	node := g.GetNode(id)
	if node != nil {
		node.X = x
		node.Y = y
	}
}

// NodeTypeDisplay returns a human-readable name for a node type
func NodeTypeDisplay(t NodeType) string {
	switch t {
	case NodeTypeRequest:
		return "Request"
	case NodeTypeDelay:
		return "Delay"
	case NodeTypeStart:
		return "Start"
	case NodeTypeEnd:
		return "End"
	default:
		return string(t)
	}
}

// EdgeTypeDisplay returns a human-readable name for an edge type
func EdgeTypeDisplay(t EdgeType) string {
	switch t {
	case EdgeTypeSuccess:
		return "Success"
	case EdgeTypeFailure:
		return "Failure"
	default:
		return string(t)
	}
}
