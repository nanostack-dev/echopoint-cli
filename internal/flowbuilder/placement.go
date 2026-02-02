package flowbuilder

import (
	"fmt"
	"math"
	"sort"

	"github.com/google/uuid"
)

// Position represents a 2D coordinate
type Position struct {
	X int
	Y int
}

// NodePlacement represents a node with its position
type NodePlacement struct {
	ID       uuid.UUID
	Position Position
	Width    int
	Height   int
}

// Grid represents the flow canvas
type Grid struct {
	Width      int
	Height     int
	NodeWidth  int
	NodeHeight int
	PaddingX   int
	PaddingY   int
}

// NewGrid creates a new grid with default settings
func NewGrid() *Grid {
	return &Grid{
		Width:      2000,
		Height:     1000,
		NodeWidth:  220,
		NodeHeight: 80,
		PaddingX:   60,
		PaddingY:   100,
	}
}

// AutoPlacementAlgorithm places nodes optimally using a layered graph layout algorithm
// Based on Sugiyama-style hierarchical layout with collision detection
func (g *Grid) AutoPlacementAlgorithm(nodes []NodePlacement, edges []Edge) []NodePlacement {
	if len(nodes) == 0 {
		return nodes
	}

	// Step 1: Build adjacency list and calculate levels (topological layers)
	levels := g.calculateLevels(nodes, edges)

	// Step 2: Group nodes by level
	levelGroups := g.groupByLevel(nodes, levels)

	// Step 3: Calculate initial positions based on levels
	positions := g.calculateInitialPositions(levelGroups)

	// Step 4: Detect and resolve collisions
	positions = g.resolveCollisions(positions, levelGroups)

	// Step 5: Optimize edge crossings
	positions = g.minimizeEdgeCrossings(positions, edges, levelGroups)

	// Step 6: Fine-tune positions for better visual balance
	positions = g.fineTunePositions(positions, levelGroups)

	// Convert positions map back to slice
	result := make([]NodePlacement, len(nodes))
	for i, node := range nodes {
		if pos, ok := positions[node.ID]; ok {
			node.Position = pos
			result[i] = node
		} else {
			result[i] = node
		}
	}

	return result
}

// Edge represents a connection between nodes
type Edge struct {
	From uuid.UUID
	To   uuid.UUID
}

// calculateLevels assigns each node to a hierarchical level using topological sort
func (g *Grid) calculateLevels(nodes []NodePlacement, edges []Edge) map[uuid.UUID]int {
	levels := make(map[uuid.UUID]int)

	// Build adjacency list (incoming edges)
	incoming := make(map[uuid.UUID][]uuid.UUID)
	for _, edge := range edges {
		incoming[edge.To] = append(incoming[edge.To], edge.From)
	}

	// Initialize nodes with no incoming edges to level 0
	queue := make([]uuid.UUID, 0)
	for _, node := range nodes {
		if len(incoming[node.ID]) == 0 {
			levels[node.ID] = 0
			queue = append(queue, node.ID)
		}
	}

	// BFS to assign levels
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentLevel := levels[current]

		// Find all outgoing edges from current node
		for _, edge := range edges {
			if edge.From == current {
				targetLevel := currentLevel + 1
				if existingLevel, exists := levels[edge.To]; !exists || targetLevel > existingLevel {
					levels[edge.To] = targetLevel
					queue = append(queue, edge.To)
				}
			}
		}
	}

	// Handle disconnected nodes (assign to level 0)
	for _, node := range nodes {
		if _, exists := levels[node.ID]; !exists {
			levels[node.ID] = 0
		}
	}

	return levels
}

// groupByLevel groups nodes by their assigned level
func (g *Grid) groupByLevel(nodes []NodePlacement, levels map[uuid.UUID]int) map[int][]uuid.UUID {
	groups := make(map[int][]uuid.UUID)

	for _, node := range nodes {
		level := levels[node.ID]
		groups[level] = append(groups[level], node.ID)
	}

	return groups
}

// calculateInitialPositions assigns initial X,Y coordinates based on levels
func (g *Grid) calculateInitialPositions(levelGroups map[int][]uuid.UUID) map[uuid.UUID]Position {
	positions := make(map[uuid.UUID]Position)

	// Get sorted levels
	var levels []int
	for level := range levelGroups {
		levels = append(levels, level)
	}
	sort.Ints(levels)

	// Place nodes in each level
	for _, level := range levels {
		nodes := levelGroups[level]
		numNodes := len(nodes)

		// Calculate Y position for this level
		y := 100 + (level * (g.NodeHeight + g.PaddingY))

		// Center nodes horizontally within the level
		totalWidth := (numNodes * g.NodeWidth) + ((numNodes - 1) * g.PaddingX)
		startX := (g.Width - totalWidth) / 2
		if startX < 100 {
			startX = 100
		}

		for i, nodeID := range nodes {
			x := startX + (i * (g.NodeWidth + g.PaddingX))
			positions[nodeID] = Position{X: x, Y: y}
		}
	}

	return positions
}

// resolveCollisions detects overlapping nodes and adjusts positions
func (g *Grid) resolveCollisions(
	positions map[uuid.UUID]Position,
	levelGroups map[int][]uuid.UUID,
) map[uuid.UUID]Position {
	maxIterations := 10

	for iteration := 0; iteration < maxIterations; iteration++ {
		hasCollision := false

		// Check for collisions within each level
		for _, nodes := range levelGroups {
			for i := 0; i < len(nodes); i++ {
				for j := i + 1; j < len(nodes); j++ {
					pos1 := positions[nodes[i]]
					pos2 := positions[nodes[j]]

					if g.checkCollision(pos1, pos2) {
						hasCollision = true
						// Push nodes apart
						midpoint := (pos1.X + pos2.X) / 2
						positions[nodes[i]] = Position{X: midpoint - (g.NodeWidth+g.PaddingX)/2, Y: pos1.Y}
						positions[nodes[j]] = Position{X: midpoint + (g.NodeWidth+g.PaddingX)/2, Y: pos2.Y}
					}
				}
			}
		}

		if !hasCollision {
			break
		}
	}

	return positions
}

// checkCollision checks if two nodes overlap
func (g *Grid) checkCollision(pos1, pos2 Position) bool {
	return math.Abs(float64(pos1.X-pos2.X)) < float64(g.NodeWidth+g.PaddingX/2) &&
		math.Abs(float64(pos1.Y-pos2.Y)) < float64(g.NodeHeight+g.PaddingY/2)
}

// minimizeEdgeCrossings uses a heuristic to reduce edge crossings between levels
func (g *Grid) minimizeEdgeCrossings(
	positions map[uuid.UUID]Position,
	edges []Edge,
	levelGroups map[int][]uuid.UUID,
) map[uuid.UUID]Position {
	// Simple heuristic: sort nodes within each level by average X position of their connected nodes
	for level, nodes := range levelGroups {
		if level == 0 {
			continue // Skip first level
		}

		// Calculate average X position of incoming connections for each node
		type nodeScore struct {
			id    uuid.UUID
			score float64
		}
		scores := make([]nodeScore, 0, len(nodes))

		for _, nodeID := range nodes {
			var totalX float64
			var count int

			for _, edge := range edges {
				if edge.To == nodeID {
					if parentPos, exists := positions[edge.From]; exists {
						totalX += float64(parentPos.X)
						count++
					}
				}
			}

			if count > 0 {
				scores = append(scores, nodeScore{id: nodeID, score: totalX / float64(count)})
			} else {
				scores = append(scores, nodeScore{id: nodeID, score: float64(positions[nodeID].X)})
			}
		}

		// Sort by score (average parent X position)
		sort.Slice(scores, func(i, j int) bool {
			return scores[i].score < scores[j].score
		})

		// Reassign X positions based on sorted order
		numNodes := len(scores)
		totalWidth := (numNodes * g.NodeWidth) + ((numNodes - 1) * g.PaddingX)
		startX := (g.Width - totalWidth) / 2
		if startX < 100 {
			startX = 100
		}

		for i, score := range scores {
			x := startX + (i * (g.NodeWidth + g.PaddingX))
			pos := positions[score.id]
			positions[score.id] = Position{X: x, Y: pos.Y}
		}
	}

	return positions
}

// fineTunePositions makes final adjustments for visual balance
func (g *Grid) fineTunePositions(
	positions map[uuid.UUID]Position,
	levelGroups map[int][]uuid.UUID,
) map[uuid.UUID]Position {
	// Center the entire graph
	var minX, maxX, minY, maxY int
	minX, minY = g.Width, g.Height

	for _, pos := range positions {
		if pos.X < minX {
			minX = pos.X
		}
		if pos.X > maxX {
			maxX = pos.X
		}
		if pos.Y < minY {
			minY = pos.Y
		}
		if pos.Y > maxY {
			maxY = pos.Y
		}
	}

	// Calculate offset to center the graph
	graphWidth := maxX - minX + g.NodeWidth
	graphHeight := maxY - minY + g.NodeHeight

	offsetX := (g.Width - graphWidth) / 2
	offsetY := (g.Height - graphHeight) / 2

	// Apply offset to all positions
	for id, pos := range positions {
		positions[id] = Position{
			X: pos.X - minX + offsetX,
			Y: pos.Y - minY + offsetY,
		}
	}

	return positions
}

// CalculateNewNodePosition determines the best position for a new node
func (g *Grid) CalculateNewNodePosition(
	existingNodes []NodePlacement,
	edges []Edge,
	connectedFrom []uuid.UUID,
) Position {
	if len(existingNodes) == 0 {
		// First node - place at center
		return Position{X: g.Width / 2, Y: 100}
	}

	// If connected to existing nodes, place to the right of them
	if len(connectedFrom) > 0 {
		var totalX, totalY int
		validConnections := 0

		for _, nodeID := range connectedFrom {
			for _, node := range existingNodes {
				if node.ID == nodeID {
					totalX += node.Position.X
					totalY += node.Position.Y
					validConnections++
					break
				}
			}
		}

		if validConnections > 0 {
			avgX := totalX / validConnections
			avgY := totalY / validConnections

			// Place to the right with some vertical offset based on existing nodes
			newX := avgX + g.NodeWidth + g.PaddingX
			newY := avgY

			// Check if position is occupied and adjust
			for g.isPositionOccupied(newX, newY, existingNodes) {
				newY += g.NodeHeight + g.PaddingY/2
			}

			return Position{X: newX, Y: newY}
		}
	}

	// Find the rightmost node and place to its right
	maxX := 0
	for _, node := range existingNodes {
		if node.Position.X > maxX {
			maxX = node.Position.X
		}
	}

	// Find a suitable Y position
	newX := maxX + g.NodeWidth + g.PaddingX
	newY := 100

	for g.isPositionOccupied(newX, newY, existingNodes) {
		newY += g.NodeHeight + g.PaddingY/2
	}

	return Position{X: newX, Y: newY}
}

// isPositionOccupied checks if a position overlaps with any existing node
func (g *Grid) isPositionOccupied(x, y int, nodes []NodePlacement) bool {
	checkPos := Position{X: x, Y: y}
	for _, node := range nodes {
		if g.checkCollision(checkPos, node.Position) {
			return true
		}
	}
	return false
}

// GenerateUUIDv7 generates a new UUIDv7 for node IDs
func GenerateUUIDv7() uuid.UUID {
	// For now, use UUIDv4 - the API will validate
	// In production, implement proper UUIDv7 generation
	return uuid.New()
}

// FormatPosition formats a position for display
func FormatPosition(pos Position) string {
	return fmt.Sprintf("(%d, %d)", pos.X, pos.Y)
}
