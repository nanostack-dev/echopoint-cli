package tui

import (
	"context"
	"fmt"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
	"echopoint-cli/internal/tui/floweditor"

	"os"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type view int

const (
	viewMenu view = iota
	viewFlows
	viewFlowCreate
	viewCollections
	viewFlowEditor
)

type item struct {
	title string
	desc  string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

type flowItem struct {
	flow api.Flow
}

func (f flowItem) Title() string       { return f.flow.Name }
func (f flowItem) Description() string { return f.flow.Id.String() }
func (f flowItem) FilterValue() string { return f.flow.Name }

type Model struct {
	client       *client.Client
	currentView  view
	list         list.Model
	nameInput    textinput.Model
	descInput    textinput.Model
	focusIndex   int
	width        int
	height       int
	err          error
	message      string
	flowEditor   *floweditor.Editor
	selectedFlow *api.Flow
}

func New(cli *client.Client) Model {
	items := []list.Item{
		item{title: "Flows", desc: "Create and manage flows"},
		item{title: "Collections", desc: "Manage collections"},
		item{title: "Quit", desc: "Exit Echopoint"},
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Echopoint CLI"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("57"))

	nameInput := textinput.New()
	nameInput.Placeholder = "Flow name"
	nameInput.Focus()
	nameInput.CharLimit = 100
	nameInput.Width = 50

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)"
	descInput.CharLimit = 200
	descInput.Width = 50

	return Model{
		client:      cli,
		currentView: viewMenu,
		list:        l,
		nameInput:   nameInput,
		descInput:   descInput,
	}
}

type flowsLoadedMsg struct {
	flows []api.Flow
	err   error
}

type flowCreatedMsg struct {
	flow *api.Flow
	err  error
}

func loadFlows(cli *client.Client) tea.Cmd {
	return func() tea.Msg {
		limit := int32(100)
		offset := int32(0)
		params := &api.ListFlowsParams{
			Limit:  api.LimitParameter(limit),
			Offset: api.OffsetParameter(offset),
		}
		resp, err := cli.API().ListFlowsWithResponse(context.Background(), params)
		if err != nil {
			return flowsLoadedMsg{err: fmt.Errorf("request failed: %w", err)}
		}

		// Check status code
		statusCode := resp.StatusCode()
		if statusCode == 403 {
			return flowsLoadedMsg{
				err: fmt.Errorf(
					"forbidden (403): your session may have expired. Try running 'echopoint auth login' again",
				),
			}
		}

		if resp.JSON200 == nil {
			// Check for specific error responses
			if resp.JSON400 != nil && len(resp.JSON400.Errors) > 0 {
				return flowsLoadedMsg{err: fmt.Errorf("bad request (400): %s", resp.JSON400.Errors[0].Code)}
			}
			if resp.JSON401 != nil && len(resp.JSON401.Errors) > 0 {
				return flowsLoadedMsg{
					err: fmt.Errorf("unauthorized (401): %s - try logging in again", resp.JSON401.Errors[0].Code),
				}
			}
			return flowsLoadedMsg{err: fmt.Errorf("unexpected response (status %d)", statusCode)}
		}
		return flowsLoadedMsg{flows: resp.JSON200.Items}
	}
}

func createFlow(cli *client.Client, name, description string) tea.Cmd {
	return func() tea.Msg {
		req := api.CreateFlowRequest{
			Name: name,
		}
		if description != "" {
			req.Description = &description
		}
		resp, err := cli.API().CreateFlowWithResponse(context.Background(), req)
		if err != nil {
			return flowCreatedMsg{err: err}
		}
		if resp.JSON201 == nil {
			return flowCreatedMsg{err: fmt.Errorf("failed to create flow")}
		}
		return flowCreatedMsg{flow: resp.JSON201}
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.currentView {
		case viewMenu:
			return m.updateMenu(msg)
		case viewFlows:
			return m.updateFlows(msg)
		case viewFlowCreate:
			return m.updateFlowCreate(msg)
		case viewFlowEditor:
			if m.flowEditor != nil {
				editor, cmd := m.flowEditor.Update(msg)
				m.flowEditor = editor
				return m, cmd
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height-2)

	case flowsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		items := make([]list.Item, len(msg.flows))
		for i, flow := range msg.flows {
			items[i] = flowItem{flow: flow}
		}
		m.list.SetItems(items)
		m.list.Title = "Flows (press n to create, enter to edit, esc to go back)"
		return m, nil

	case flowCreatedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.message = fmt.Sprintf("Flow created: %s", msg.flow.Name)
		m.currentView = viewFlows
		return m, loadFlows(m.client)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter":
		if choice, ok := m.list.SelectedItem().(item); ok {
			switch choice.title {
			case "Quit":
				return m, tea.Quit
			case "Flows":
				m.currentView = viewFlows
				return m, loadFlows(m.client)
			case "Collections":
				m.currentView = viewCollections
				m.message = "Collections view coming soon"
				return m, nil
			}
		}
	}
	// Allow the list to handle other keys (like arrow keys for navigation)
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) updateFlows(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.currentView = viewMenu
		m.message = ""
		m.err = nil
		// Reset menu items
		items := []list.Item{
			item{title: "Flows", desc: "Create and manage flows"},
			item{title: "Collections", desc: "Manage collections"},
			item{title: "Quit", desc: "Exit Echopoint"},
		}
		m.list.SetItems(items)
		m.list.Title = "Echopoint CLI"
		return m, nil
	case "n":
		m.currentView = viewFlowCreate
		m.focusIndex = 0
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		m.nameInput.Focus()
		m.descInput.Blur()
		return m, nil
	case "enter":
		// Open flow editor for selected flow
		if item, ok := m.list.SelectedItem().(flowItem); ok {
			m.selectedFlow = &item.flow

			// Check for debug environment variables
			debugLevel := floweditor.DebugLevelOff
			logPath := ""

			if level := os.Getenv("ECHOPOINT_DEBUG"); level != "" {
				debugLevel = floweditor.ParseDebugLevel(level)
				logPath = os.Getenv("ECHOPOINT_DEBUG_LOG")
				if logPath == "" {
					logPath = os.ExpandEnv("$HOME/.echopoint/debug.log")
				}
			}

			m.flowEditor = floweditor.NewEditor(floweditor.EditorConfig{
				Client:     m.client,
				FlowID:     item.flow.Id,
				Width:      m.width,
				Height:     m.height,
				DebugLevel: debugLevel,
				LogPath:    logPath,
			})
			m.currentView = viewFlowEditor
			return m, m.flowEditor.Init()
		}
		return m, nil
	}
	// Allow the list to handle other keys (like arrow keys for navigation)
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) updateFlowCreate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.currentView = viewFlows
		return m, nil
	case "tab", "shift+tab", "up", "down":
		if msg.String() == "up" || msg.String() == "shift+tab" {
			m.focusIndex--
		} else {
			m.focusIndex++
		}
		if m.focusIndex > 1 {
			m.focusIndex = 0
		} else if m.focusIndex < 0 {
			m.focusIndex = 1
		}
		if m.focusIndex == 0 {
			m.nameInput.Focus()
			m.descInput.Blur()
		} else {
			m.nameInput.Blur()
			m.descInput.Focus()
		}
		return m, nil
	case "enter":
		name := m.nameInput.Value()
		if name == "" {
			m.err = fmt.Errorf("name is required")
			return m, nil
		}
		m.err = nil
		return m, createFlow(m.client, name, m.descInput.Value())
	}

	var cmd tea.Cmd
	if m.focusIndex == 0 {
		m.nameInput, cmd = m.nameInput.Update(msg)
	} else {
		m.descInput, cmd = m.descInput.Update(msg)
	}
	return m, cmd
}

func (m Model) View() string {
	switch m.currentView {
	case viewMenu:
		return m.viewMenu()
	case viewFlows:
		return m.viewFlows()
	case viewFlowCreate:
		return m.viewFlowCreate()
	case viewCollections:
		return m.viewCollections()
	case viewFlowEditor:
		if m.flowEditor != nil {
			return m.flowEditor.View()
		}
		return "Loading flow editor..."
	}
	return ""
}

func (m Model) viewMenu() string {
	return "\n" + m.list.View()
}

func (m Model) viewFlows() string {
	s := "\n" + m.list.View()
	if m.err != nil {
		s += fmt.Sprintf("\n\nError: %s", m.err)
	}
	if m.message != "" {
		s += fmt.Sprintf("\n\n%s", m.message)
	}
	return s
}

func (m Model) viewFlowCreate() string {
	s := lipgloss.NewStyle().Bold(true).Render("Create New Flow") + "\n\n"
	s += m.nameInput.View() + "\n"
	s += m.descInput.View() + "\n\n"
	s += lipgloss.NewStyle().Faint(true).Render("Press Enter to create, Esc to cancel, Tab to switch fields")
	if m.err != nil {
		s += "\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("Error: %s", m.err))
	}
	return "\n" + s
}

func (m Model) viewCollections() string {
	s := lipgloss.NewStyle().Bold(true).Render("Collections") + "\n\n"
	s += "Coming soon...\n\n"
	s += lipgloss.NewStyle().Faint(true).Render("Press Esc to go back")
	if m.message != "" {
		s += "\n\n" + m.message
	}
	return "\n" + s
}
