// Package picker hosts the BubbleTea project picker `tm init` shows when
// stdout is a terminal. It is a separate package from internal/tui/board so
// the two models don't share key handling by accident.
package picker

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sn3d/tm/internal/client"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	tickerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

// Result is what the picker returns to `tm init`.
type Result struct {
	// Ticker is the chosen (or newly entered) project ticker, canonical.
	Ticker string
	// DisplayName preserves the casing the user typed for a new project.
	DisplayName string
	// Name is the human-readable name entered for a new project.
	Name string
	// Created reports whether the user chose "+ Create a new project".
	Created bool
	// Cancelled reports that the user aborted (q / esc / ctrl-c). The caller
	// must write nothing in that case.
	Cancelled bool
}

// mode is the picker's current screen.
type mode int

const (
	modeSelect mode = iota // choosing from the list
	modeTicker             // typing a new project's ticker
	modeName               // typing a new project's name
)

// Model is the picker's BubbleTea model.
type Model struct {
	instance    string
	dbPath      string
	folder      string
	projects    []client.Project
	counts      map[string]int
	cursor      int
	mode        mode
	tickerInput string
	nameInput   string
	errMsg      string
	result      Result
	done        bool
}

// NewModel builds the picker. projects should already exclude archived ones —
// archive is what hides a project from pickers.
func NewModel(instance, dbPath, folder string, projects []client.Project, counts map[string]int) Model {
	return Model{
		instance: instance,
		dbPath:   dbPath,
		folder:   folder,
		projects: projects,
		counts:   counts,
	}
}

// Result returns the user's choice. Valid once the program has exited.
func (m Model) Result() Result { return m.result }

func (m Model) Init() tea.Cmd { return nil }

// createIndex is the cursor position of the "+ Create a new project" row,
// which always sits after the existing projects.
func (m Model) createIndex() int { return len(m.projects) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.mode {
	case modeSelect:
		return m.updateSelect(key)
	case modeTicker:
		return m.updateTicker(key)
	case modeName:
		return m.updateName(key)
	}
	return m, nil
}

func (m Model) updateSelect(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q", "esc":
		m.result = Result{Cancelled: true}
		m.done = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.createIndex() {
			m.cursor++
		}
	case "enter":
		if m.cursor == m.createIndex() {
			m.mode = modeTicker
			m.errMsg = ""
			return m, nil
		}
		p := m.projects[m.cursor]
		m.result = Result{Ticker: p.Ticker, DisplayName: p.DisplayName, Name: p.Name}
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateTicker(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "esc":
		// esc backs out to the list rather than quitting, so a mistyped
		// "create" is recoverable without restarting.
		m.mode = modeSelect
		m.tickerInput = ""
		m.errMsg = ""
		return m, nil
	case "enter":
		canon, err := client.CanonicalTicker(m.tickerInput)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		for _, p := range m.projects {
			if p.Ticker == canon {
				m.errMsg = fmt.Sprintf("project %s already exists; pick it from the list instead", p.Label())
				return m, nil
			}
		}
		m.errMsg = ""
		m.mode = modeName
		return m, nil
	case "backspace":
		if n := len(m.tickerInput); n > 0 {
			m.tickerInput = m.tickerInput[:n-1]
		}
	default:
		if s := key.String(); len(s) == 1 {
			m.tickerInput += s
		}
	}
	return m, nil
}

func (m Model) updateName(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		m.result = Result{Cancelled: true}
		m.done = true
		return m, tea.Quit
	case "esc":
		m.mode = modeTicker
		return m, nil
	case "enter":
		canon, err := client.CanonicalTicker(m.tickerInput)
		if err != nil {
			// Unreachable: the ticker screen already validated. Returning to
			// it beats quitting with a bad value.
			m.errMsg = err.Error()
			m.mode = modeTicker
			return m, nil
		}
		m.result = Result{
			Ticker:      canon,
			DisplayName: strings.TrimSpace(m.tickerInput),
			Name:        strings.TrimSpace(m.nameInput),
			Created:     true,
		}
		m.done = true
		return m, tea.Quit
	case "backspace":
		if n := len(m.nameInput); n > 0 {
			m.nameInput = m.nameInput[:n-1]
		}
	default:
		if s := key.String(); len(s) == 1 {
			m.nameInput += s
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s\n\n",
		titleStyle.Render("Instance:"), m.instance, dimStyle.Render("("+m.dbPath+")"))

	switch m.mode {
	case modeSelect:
		m.viewSelect(&b)
	case modeTicker:
		fmt.Fprintf(&b, "  %s\n\n", titleStyle.Render("New project"))
		fmt.Fprintf(&b, "  Ticker: %s\n", tickerStyle.Render(m.tickerInput+"▌"))
		fmt.Fprintf(&b, "  %s\n", dimStyle.Render("letters, digits and dashes; max 15; starts with a letter"))
	case modeName:
		fmt.Fprintf(&b, "  %s\n\n", titleStyle.Render("New project"))
		fmt.Fprintf(&b, "  Ticker: %s\n", tickerStyle.Render(m.tickerInput))
		fmt.Fprintf(&b, "  Name:   %s\n", m.nameInput+"▌")
		fmt.Fprintf(&b, "  %s\n", dimStyle.Render("optional; press enter to finish"))
	}

	if m.errMsg != "" {
		fmt.Fprintf(&b, "\n  %s\n", errStyle.Render(m.errMsg))
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(m.footer()))
	b.WriteString("\n")
	return b.String()
}

func (m Model) viewSelect(b *strings.Builder) {
	fmt.Fprintf(b, "  Select a project for %s:\n\n", titleStyle.Render(m.folder))
	for i, p := range m.projects {
		prefix := "    "
		label := p.Label()
		name := p.Name
		if i == m.cursor {
			prefix = "  " + selectedStyle.Render(">") + " "
			label = selectedStyle.Render(label)
		}
		fmt.Fprintf(b, "%s%s %s %s\n",
			prefix,
			pad(label, 10),
			pad(name, 30),
			dimStyle.Render(fmt.Sprintf("%d tasks", m.counts[p.Ticker])))
	}
	if len(m.projects) > 0 {
		fmt.Fprintf(b, "    %s\n", dimStyle.Render(strings.Repeat("─", 50)))
	}
	createPrefix := "    "
	createLabel := "+ Create a new project…"
	if m.cursor == m.createIndex() {
		createPrefix = "  " + selectedStyle.Render(">") + " "
		createLabel = selectedStyle.Render(createLabel)
	}
	fmt.Fprintf(b, "%s%s\n", createPrefix, createLabel)
}

func (m Model) footer() string {
	switch m.mode {
	case modeSelect:
		return "  ↑/↓ move · enter select · q cancel"
	case modeTicker:
		return "  enter continue · esc back · ctrl-c cancel"
	default:
		return "  enter finish · esc back · ctrl-c cancel"
	}
}

// pad right-pads to n display cells, counting runes so multibyte names align.
func pad(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// Run shows the picker and returns the user's choice. The caller is
// responsible for having verified that stdout is a TTY.
func Run(m Model) (Result, error) {
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return Result{}, fmt.Errorf("run project picker: %w", err)
	}
	fm, ok := final.(Model)
	if !ok {
		return Result{}, fmt.Errorf("run project picker: unexpected final model %T", final)
	}
	return fm.Result(), nil
}
