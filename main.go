package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/krzysztofciepka/selfupgrade"
)

// version is overridden at release build time via
// -ldflags "-X main.version=<tag>".
var version = "dev"

func selectionPath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "cli", "selection")
}

// runFlags handles -version/-upgrade. handled=true means the program should
// exit with code instead of launching the TUI. upgrade performs the upgrade.
func runFlags(args []string, stdout, stderr io.Writer, ver string, upgrade func() error) (handled bool, code int) {
	fs := flag.NewFlagSet("cli", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	doUpgrade := fs.Bool("upgrade", false, "fetch the latest release and replace this binary")
	if err := fs.Parse(args); err != nil {
		return true, 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "cli %s\n", ver)
		return true, 0
	}
	if *doUpgrade {
		if err := upgrade(); err != nil {
			fmt.Fprintln(stderr, err)
			return true, 1
		}
		return true, 0
	}
	return false, 0
}

func main() {
	handled, code := runFlags(os.Args[1:], os.Stdout, os.Stderr, version, func() error {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine cli binary path: %w", err)
		}
		return selfupgrade.Run(context.Background(), selfupgrade.Config{
			Owner:          "krzysztofciepka",
			Repo:           "cli",
			CurrentVersion: version,
			ExePath:        exe,
			Out:            os.Stderr,
		})
	})
	if handled {
		os.Exit(code)
	}

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
