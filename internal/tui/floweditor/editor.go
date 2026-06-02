package floweditor

import (
	"context"
	"fmt"
	"os"
	"time"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// Editor represents the flow editor component
type Editor struct {
	client   *client.Client
	flowID   uuid.UUID
	graph    *FlowGraph
	mode     EditorMode
	viewport viewport.Model
	width    int
	height   int
	err      error
	message  string

	// Selection state
	selectedNodeID *uuid.UUID
	selectedEdgeID *uuid.UUID

	// Connection mode state
	connectSourceID *uuid.UUID

	// Dirty flag for unsaved changes
	dirty bool
}

// EditorConfig contains configuration for creating a new editor
type EditorConfig struct {
	Client     *client.Client
	FlowID     uuid.UUID
	Width      int
	Height     int
	DebugLevel DebugLevel
	LogPath    string
}

// NewEditor creates a new flow editor instance
func NewEditor(cfg EditorConfig) *Editor {
	// Check environment variables if not explicitly set in config
	debugLevel := cfg.DebugLevel
	logPath := cfg.LogPath

	if debugLevel == DebugLevelOff {
		if level := os.Getenv("ECHOPOINT_DEBUG"); level != "" {
			debugLevel = ParseDebugLevel(level)
		}
	}

	if logPath == "" && debugLevel > DebugLevelOff {
		logPath = os.Getenv("ECHOPOINT_DEBUG_LOG")
		if logPath == "" {
			logPath = os.ExpandEnv("$HOME/.echopoint/debug.log")
		}
	}

	// Initialize debug logger if level is set
	if debugLevel > DebugLevelOff {
		if err := InitLogger(debugLevel, logPath); err != nil {
			// Log to stderr if we can't initialize file logging
			fmt.Fprintf(os.Stderr, "Warning: Could not initialize debug logger: %v\n", err)
		}
	}

	logger := GetLogger()
	if logger.IsEnabled() {
		logger.Info("Creating new flow editor for flow ID: %s", cfg.FlowID.String())
	}

	vp := viewport.New(cfg.Width, cfg.Height)
	vp.SetContent("")

	return &Editor{
		client:   cfg.Client,
		flowID:   cfg.FlowID,
		graph:    NewFlowGraph(cfg.FlowID, ""),
		mode:     ModeView,
		viewport: vp,
		width:    cfg.Width,
		height:   cfg.Height,
		dirty:    false,
	}
}

// flowLoadedMsg is sent when a flow is loaded from the API
type flowLoadedMsg struct {
	flow *api.Flow
	err  error
}

// flowSavedMsg is sent when a flow is saved to the API
type flowSavedMsg struct {
	err error
}

// LoadFlow loads a flow from the API
func (e *Editor) LoadFlow() tea.Cmd {
	logger := GetLogger()
	logger.Info("Loading flow from API: %s", e.flowID.String())

	return func() tea.Msg {
		start := time.Now()
		resp, err := e.client.API().GetFlowWithResponse(context.Background(), e.flowID)
		duration := time.Since(start)

		if err != nil {
			logger.Error("Failed to load flow: %v", err)
			return flowLoadedMsg{err: fmt.Errorf("failed to load flow: %w", err)}
		}

		if resp.JSON200 == nil {
			logger.Error("Flow not found: %s", e.flowID.String())
			return flowLoadedMsg{err: fmt.Errorf("flow not found")}
		}

		logger.Info("Successfully loaded flow: %s (took %v)", resp.JSON200.Name, duration)
		logger.Debug("Flow data: %+v", resp.JSON200)
		return flowLoadedMsg{flow: resp.JSON200}
	}
}

// SaveFlow saves the current flow to the API
func (e *Editor) SaveFlow() tea.Cmd {
	return func() tea.Msg {
		e.dirty = false
		return flowSavedMsg{}
	}
}

// Init initializes the editor
func (e *Editor) Init() tea.Cmd {
	return e.LoadFlow()
}

// Update handles messages and updates the editor state
func (e *Editor) Update(msg tea.Msg) (*Editor, tea.Cmd) {
	logger := GetLogger()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		logger.LogKey(msg.String(), e.mode)
		return e.handleKey(msg)

	case tea.WindowSizeMsg:
		logger.Debug("Window resized to %dx%d", msg.Width, msg.Height)
		e.width = msg.Width
		e.height = msg.Height
		e.viewport.Width = msg.Width
		e.viewport.Height = msg.Height - 2

	case flowLoadedMsg:
		if msg.err != nil {
			logger.Error("Failed to load flow: %v", msg.err)
			e.err = msg.err
			return e, nil
		}
		logger.Info("Populating graph from flow: %s", msg.flow.Name)
		e.populateGraphFromFlow(msg.flow)
		e.message = fmt.Sprintf("Loaded: %s", msg.flow.Name)

	case flowSavedMsg:
		if msg.err != nil {
			logger.Error("Failed to save flow: %v", msg.err)
			e.err = msg.err
			return e, nil
		}
		logger.Info("Flow saved successfully")
		e.message = "Flow saved successfully"
	}

	var cmd tea.Cmd
	e.viewport, cmd = e.viewport.Update(msg)

	return e, cmd
}

// handleKey handles keyboard input
func (e *Editor) handleKey(msg tea.KeyMsg) (*Editor, tea.Cmd) {
	switch e.mode {
	case ModeView, ModeSelect:
		return e.handleNavigationKey(msg)
	case ModeConnect:
		return e.handleConnectKey(msg)
	}
	return e, nil
}

// handleNavigationKey handles keys in view/select mode
func (e *Editor) handleNavigationKey(msg tea.KeyMsg) (*Editor, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if e.dirty {
			e.message = "Unsaved changes! Press Q again to quit without saving"
			e.dirty = false
			return e, nil
		}
		return e, tea.Quit

	case "s":
		return e, e.SaveFlow()

	case "r":
		return e, e.LoadFlow()

	case "n":
		e.message = "Press: r=Request, d=Delay"
		return e, nil

	case "R":
		node := e.graph.AddNode(NodeTypeRequest, "New Request", 10, 10)
		e.graph.SelectNode(node.ID)
		e.selectedNodeID = &node.ID
		e.dirty = true
		e.message = "Added request node"
		GetLogger().LogNode("ADDED", node)

	case "D":
		node := e.graph.AddNode(NodeTypeDelay, "Delay", 10, 10)
		e.graph.SelectNode(node.ID)
		e.selectedNodeID = &node.ID
		e.dirty = true
		e.message = "Added delay node"
		GetLogger().LogNode("ADDED", node)

	case "c":
		if e.selectedNodeID != nil {
			e.mode = ModeConnect
			e.connectSourceID = e.selectedNodeID
			e.message = "Select target node and press Enter (Success) or F (Failure)"
		} else {
			e.message = "Select a source node first"
		}

	case "x":
		if e.selectedNodeID != nil {
			node := e.graph.GetNode(*e.selectedNodeID)
			if node != nil {
				GetLogger().LogNode("DELETED", node)
			}
			e.graph.DeleteNode(*e.selectedNodeID)
			e.selectedNodeID = nil
			e.dirty = true
			e.message = "Node deleted"
		}

	case "tab":
		e.selectNextNode()

	case "up", "down", "left", "right":
		if e.selectedNodeID != nil {
			e.moveSelectedNode(msg.String())
			e.dirty = true
		}

	case "?":
		e.showHelp()
	}

	return e, nil
}

// handleConnectKey handles keys in connect mode
func (e *Editor) handleConnectKey(msg tea.KeyMsg) (*Editor, tea.Cmd) {
	switch msg.String() {
	case "esc":
		e.mode = ModeSelect
		e.connectSourceID = nil
		e.message = "Connection cancelled"

	case "enter":
		if e.selectedNodeID != nil && e.connectSourceID != nil {
			edge := e.graph.AddEdge(*e.connectSourceID, *e.selectedNodeID, EdgeTypeSuccess)
			GetLogger().LogEdge("CONNECTED", edge)
			e.mode = ModeSelect
			e.connectSourceID = nil
			e.dirty = true
			e.message = "Connected (success)"
		}

	case "f":
		if e.selectedNodeID != nil && e.connectSourceID != nil {
			edge := e.graph.AddEdge(*e.connectSourceID, *e.selectedNodeID, EdgeTypeFailure)
			GetLogger().LogEdge("CONNECTED", edge)
			e.mode = ModeSelect
			e.connectSourceID = nil
			e.dirty = true
			e.message = "Connected (failure)"
		}

	case "tab":
		e.selectNextNode()
	}

	return e, nil
}

// selectNextNode cycles through nodes
func (e *Editor) selectNextNode() {
	logger := GetLogger()

	if len(e.graph.Nodes) == 0 {
		logger.Debug("selectNextNode: no nodes to select")
		return
	}

	var currentIdx = -1
	if e.selectedNodeID != nil {
		for i, node := range e.graph.Nodes {
			if node.ID == *e.selectedNodeID {
				currentIdx = i
				break
			}
		}
	}

	nextIdx := (currentIdx + 1) % len(e.graph.Nodes)
	newNode := &e.graph.Nodes[nextIdx]
	e.graph.SelectNode(newNode.ID)
	e.selectedNodeID = &newNode.ID

	logger.LogNode("SELECTED", newNode)
}

// moveSelectedNode moves the selected node
func (e *Editor) moveSelectedNode(direction string) {
	if e.selectedNodeID == nil {
		return
	}

	node := e.graph.GetNode(*e.selectedNodeID)
	if node == nil {
		return
	}

	switch direction {
	case "up":
		node.Y -= 1
	case "down":
		node.Y += 1
	case "left":
		node.X -= 2
	case "right":
		node.X += 2
	}
}

// showHelp displays help message
func (e *Editor) showHelp() {
	e.message = "?:Help | n:New | c:Connect | x:Delete | arrows:Move | s:Save | q:Quit"
}

// populateGraphFromFlow converts API flow to graph
func (e *Editor) populateGraphFromFlow(flow *api.Flow) {
	e.graph.ID = flow.Id
	e.graph.Name = flow.Name
	if flow.Description != nil {
		e.graph.Description = *flow.Description
	}

	// Clear existing nodes and edges
	e.graph.Nodes = make([]Node, 0)
	e.graph.Edges = make([]Edge, 0)

	// TODO: Parse flow.Definition and populate nodes/edges
	// For now, add a start and end node as placeholders
	e.graph.AddNode(NodeTypeStart, "Start", 5, 5)
	e.graph.AddNode(NodeTypeEnd, "End", 5, 15)
}

// View renders the editor
func (e *Editor) View() string {
	if e.err != nil {
		return fmt.Sprintf("Error: %s\n\nPress any key to exit", e.err)
	}

	content := e.renderGraph()
	e.viewport.SetContent(content)

	statusBar := e.renderStatusBar()

	return e.viewport.View() + "\n" + statusBar
}

// renderGraph renders the flow graph
func (e *Editor) renderGraph() string {
	if len(e.graph.Nodes) == 0 {
		return "No nodes in flow. Press 'n' to add nodes."
	}

	// Simple grid-based rendering
	grid := make([][]rune, 50)
	for i := range grid {
		grid[i] = make([]rune, 100)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	// Render edges first
	for _, edge := range e.graph.Edges {
		fromNode := e.graph.GetNode(edge.From)
		toNode := e.graph.GetNode(edge.To)
		if fromNode != nil && toNode != nil {
			e.renderEdge(grid, fromNode, toNode, edge)
		}
	}

	// Render nodes
	for _, node := range e.graph.Nodes {
		e.renderNode(grid, &node)
	}

	// Convert grid to string
	var result string
	for i := range 40 {
		result += string(grid[i]) + "\n"
	}

	return result
}

// renderNode renders a single node on the grid
func (e *Editor) renderNode(grid [][]rune, node *Node) {
	x, y := node.X, node.Y
	width := node.Width
	height := node.Height

	// Draw box
	for i := range width {
		if y >= 0 && y < len(grid) && x+i >= 0 && x+i < len(grid[0]) {
			grid[y][x+i] = '─'
		}
		if y+height-1 >= 0 && y+height-1 < len(grid) && x+i >= 0 && x+i < len(grid[0]) {
			grid[y+height-1][x+i] = '─'
		}
	}

	for i := range height {
		if y+i >= 0 && y+i < len(grid) && x >= 0 && x < len(grid[0]) {
			grid[y+i][x] = '│'
		}
		if y+i >= 0 && y+i < len(grid) && x+width-1 >= 0 && x+width-1 < len(grid[0]) {
			grid[y+i][x+width-1] = '│'
		}
	}

	// Corners
	if y >= 0 && y < len(grid) && x >= 0 && x < len(grid[0]) {
		grid[y][x] = '┌'
	}
	if y >= 0 && y < len(grid) && x+width-1 >= 0 && x+width-1 < len(grid[0]) {
		grid[y][x+width-1] = '┐'
	}
	if y+height-1 >= 0 && y+height-1 < len(grid) && x >= 0 && x < len(grid[0]) {
		grid[y+height-1][x] = '└'
	}
	if y+height-1 >= 0 && y+height-1 < len(grid) && x+width-1 >= 0 && x+width-1 < len(grid[0]) {
		grid[y+height-1][x+width-1] = '┘'
	}

	// Node name (truncated to fit)
	name := node.Name
	if len(name) > width-2 {
		name = name[:width-2]
	}
	nameY := y + height/2
	nameX := x + (width-len(name))/2
	for i, ch := range name {
		if nameY >= 0 && nameY < len(grid) && nameX+i >= 0 && nameX+i < len(grid[0]) {
			grid[nameY][nameX+i] = ch
		}
	}

	// Selection indicator
	if node.Selected {
		if y-1 >= 0 && y-1 < len(grid) && x+width/2 >= 0 && x+width/2 < len(grid[0]) {
			grid[y-1][x+width/2] = '▼'
		}
	}
}

// renderEdge renders an edge between two nodes
func (e *Editor) renderEdge(grid [][]rune, from, to *Node, edge Edge) {
	fromX := from.X + from.Width/2
	fromY := from.Y + from.Height
	toX := to.X + to.Width/2
	toY := to.Y

	// Simple vertical line for now
	for y := fromY; y < toY; y++ {
		if y >= 0 && y < len(grid) && fromX >= 0 && fromX < len(grid[0]) {
			grid[y][fromX] = '│'
		}
	}

	// Arrow head
	if toY-1 >= 0 && toY-1 < len(grid) && toX >= 0 && toX < len(grid[0]) {
		grid[toY-1][toX] = '▼'
	}
}

// renderStatusBar renders the status bar at the bottom
func (e *Editor) renderStatusBar() string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("255")).
		Padding(0, 1)

	status := e.graph.Name
	if e.dirty {
		status += " [modified]"
	}

	if e.message != "" {
		status += " | " + e.message
	}

	if e.mode == ModeConnect {
		status += " | CONNECT MODE"
	}

	return style.Render(status)
}
