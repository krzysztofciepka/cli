package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

func selectionPath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "cli", "selection")
}

func main() {
	executables := scanExecutables()
	cfg := loadConfig()
	m := initialModel(executables, cfg)

	// Remove any stale selection file
	selPath := selectionPath()
	os.Remove(selPath)

	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	final := result.(model)
	if final.selected != "" {
		os.MkdirAll(filepath.Dir(selPath), 0755)
		os.WriteFile(selPath, []byte(final.selected), 0644)
	} else {
		os.Exit(1)
	}
}
