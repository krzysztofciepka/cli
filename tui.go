package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

type mode int

const (
	modeBrowse mode = iota
	modeFilter
	modeDescribe
	modeAddCustom
)

type item struct {
	name    string
	starred bool
	desc    string
}

type model struct {
	allItems  []item
	filtered  []item
	cursor    int
	mode      mode
	config    Config
	filter    textinput.Model
	descEdit  textinput.Model
	addCustom textinput.Model
	width     int
	height    int
	selected  string
	quitting  bool
}

func initialModel(executables []string, cfg Config) model {
	fi := textinput.New()
	fi.Placeholder = "filter..."
	fi.CharLimit = 64

	di := textinput.New()
	di.Placeholder = "description..."
	di.CharLimit = 120

	ai := textinput.New()
	ai.Placeholder = "command name..."
	ai.CharLimit = 120

	m := model{
		config:    cfg,
		filter:    fi,
		descEdit:  di,
		addCustom: ai,
		width:     80,
		height:    24,
	}

	seen := make(map[string]bool)
	var items []item
	for _, name := range executables {
		seen[name] = true
		items = append(items, item{
			name:    name,
			starred: cfg.IsStarred(name),
			desc:    cfg.Descriptions[name],
		})
	}
	for _, name := range cfg.Custom {
		if !seen[name] {
			seen[name] = true
			items = append(items, item{
				name:    name,
				starred: cfg.IsStarred(name),
				desc:    cfg.Descriptions[name],
			})
		}
	}
	m.allItems = items
	m.sortAndFilter()
	return m
}

func (m *model) sortAndFilter() {
	if m.filter.Value() == "" {
		m.filtered = make([]item, len(m.allItems))
		copy(m.filtered, m.allItems)
	} else {
		query := m.filter.Value()
		// Build searchable strings (name + description)
		strs := make([]string, len(m.allItems))
		for i, it := range m.allItems {
			strs[i] = it.name + " " + it.desc
		}
		matches := fuzzy.Find(query, strs)
		m.filtered = make([]item, len(matches))
		for i, match := range matches {
			m.filtered[i] = m.allItems[match.Index]
		}
	}

	// Sort: starred first, then alphabetical within groups
	sort.SliceStable(m.filtered, func(i, j int) bool {
		if m.filtered[i].starred != m.filtered[j].starred {
			return m.filtered[i].starred
		}
		return m.filtered[i].name < m.filtered[j].name
	})

	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m model) Init() tea.Cmd {
	return tea.WindowSize()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modeDescribe:
			return m.updateDescribe(msg)
		case modeAddCustom:
			return m.updateAddCustom(msg)
		default:
			return m.updateBrowse(msg)
		}
	}
	return m, nil
}

func (m model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.sortAndFilter()
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "G":
		m.cursor = max(0, len(m.filtered)-1)
	case "g":
		m.cursor = 0
	case "/":
		m.mode = modeFilter
		m.filter.Focus()
		return m, textinput.Blink
	case "s":
		if len(m.filtered) > 0 {
			name := m.filtered[m.cursor].name
			m.config.ToggleStar(name)
			// Update all items
			for i := range m.allItems {
				if m.allItems[i].name == name {
					m.allItems[i].starred = m.config.IsStarred(name)
				}
			}
			saveConfig(m.config)
			m.sortAndFilter()
		}
	case "d":
		if len(m.filtered) > 0 {
			m.mode = modeDescribe
			m.descEdit.SetValue(m.filtered[m.cursor].desc)
			m.descEdit.Focus()
			m.descEdit.CursorEnd()
			return m, textinput.Blink
		}
	case "a":
		m.mode = modeAddCustom
		m.addCustom.SetValue("")
		m.addCustom.Focus()
		return m, textinput.Blink
	case "x":
		if len(m.filtered) > 0 {
			name := m.filtered[m.cursor].name
			// Only allow removing custom entries
			for i, c := range m.config.Custom {
				if c == name {
					m.config.Custom = append(m.config.Custom[:i], m.config.Custom[i+1:]...)
					// Remove from allItems
					for j := range m.allItems {
						if m.allItems[j].name == name {
							m.allItems = append(m.allItems[:j], m.allItems[j+1:]...)
							break
						}
					}
					delete(m.config.Descriptions, name)
					saveConfig(m.config)
					m.sortAndFilter()
					break
				}
			}
		}
	case "enter":
		if len(m.filtered) > 0 {
			m.selected = m.filtered[m.cursor].name
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeBrowse
		m.filter.Blur()
		return m, nil
	case "enter":
		m.mode = modeBrowse
		m.filter.Blur()
		if len(m.filtered) > 0 {
			m.selected = m.filtered[m.cursor].name
			return m, tea.Quit
		}
		return m, nil
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.sortAndFilter()
	return m, cmd
}

func (m model) updateAddCustom(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeBrowse
		m.addCustom.Blur()
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.addCustom.Value())
		if name != "" {
			// Check it doesn't already exist
			exists := false
			for _, it := range m.allItems {
				if it.name == name {
					exists = true
					break
				}
			}
			if !exists {
				m.config.Custom = append(m.config.Custom, name)
				saveConfig(m.config)
				m.allItems = append(m.allItems, item{
					name:    name,
					starred: m.config.IsStarred(name),
					desc:    m.config.Descriptions[name],
				})
				m.sortAndFilter()
			}
		}
		m.mode = modeBrowse
		m.addCustom.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.addCustom, cmd = m.addCustom.Update(msg)
	return m, cmd
}

func (m model) updateDescribe(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeBrowse
		m.descEdit.Blur()
		return m, nil
	case "enter":
		if len(m.filtered) > 0 {
			name := m.filtered[m.cursor].name
			desc := strings.TrimSpace(m.descEdit.Value())
			if desc == "" {
				delete(m.config.Descriptions, name)
			} else {
				m.config.Descriptions[name] = desc
			}
			for i := range m.allItems {
				if m.allItems[i].name == name {
					m.allItems[i].desc = desc
				}
			}
			saveConfig(m.config)
			m.sortAndFilter()
		}
		m.mode = modeBrowse
		m.descEdit.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.descEdit, cmd = m.descEdit.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	if m.selected != "" {
		return ""
	}

	var b strings.Builder

	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	starStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	countStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	// Title
	b.WriteString(titleStyle.Render("  cli"))
	b.WriteString(countStyle.Render(fmt.Sprintf("  %d commands", len(m.filtered))))
	b.WriteString("\n\n")

	// Filter
	if m.mode == modeFilter {
		b.WriteString("  " + m.filter.View() + "\n\n")
	} else if m.filter.Value() != "" {
		b.WriteString(fmt.Sprintf("  / %s\n\n", m.filter.Value()))
	}

	// Description edit
	if m.mode == modeDescribe && len(m.filtered) > 0 {
		b.WriteString(fmt.Sprintf("  describe %s: %s\n\n",
			m.filtered[m.cursor].name, m.descEdit.View()))
	}

	// Add custom
	if m.mode == modeAddCustom {
		b.WriteString("  add command: " + m.addCustom.View() + "\n\n")
	}

	// List
	listHeight := m.height - 6
	if m.mode == modeFilter || m.filter.Value() != "" {
		listHeight -= 2
	}
	if m.mode == modeDescribe || m.mode == modeAddCustom {
		listHeight -= 2
	}
	if listHeight < 1 {
		listHeight = 1
	}

	// Viewport scrolling
	start := 0
	if m.cursor >= listHeight {
		start = m.cursor - listHeight + 1
	}
	end := start + listHeight
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	for i := start; i < end; i++ {
		it := m.filtered[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		star := "  "
		if it.starred {
			star = starStyle.Render("★ ")
		}

		name := it.name
		desc := ""
		if it.desc != "" {
			desc = descStyle.Render(" — " + it.desc)
		}

		line := fmt.Sprintf("%s%s%s%s", cursor, star, name, desc)

		if i == m.cursor {
			// Re-render with selection style applied to the name
			nameStyled := selectedStyle.Render(name)
			line = fmt.Sprintf("%s%s%s%s", cursor, star, nameStyled, desc)
		}

		b.WriteString(line + "\n")
	}

	// Pad remaining space
	for i := end - start; i < listHeight; i++ {
		b.WriteString("\n")
	}

	// Help bar
	var help string
	switch m.mode {
	case modeFilter:
		help = "  ↑↓ navigate  enter select  esc clear"
	case modeDescribe, modeAddCustom:
		help = "  enter save  esc cancel"
	default:
		help = "  ↑↓/jk navigate  / filter  s star  d describe  a add  x remove  enter select  q quit"
	}
	b.WriteString("\n" + helpStyle.Render(help))

	return b.String()
}
