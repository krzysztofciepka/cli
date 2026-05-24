package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type pathExec struct {
	name string
	path string
}

// selectExecutables returns the curated, sorted, deduplicated list of
// executable names to display. An executable is kept when its name is owned by
// an explicitly-installed package (explicitOwned) or its path is not owned by
// any package (ownedPaths) — i.e. a manual install. Executables owned only by
// dependency packages are dropped.
//
// execs must be in $PATH order: the first occurrence of a name is the one that
// would actually run, so the keep/drop decision is made on that occurrence and
// later duplicates of the same name are ignored — even when the first
// occurrence was dropped (a shadowed manual copy is unreachable by bare name,
// so surfacing it would be misleading).
func selectExecutables(execs []pathExec, explicitOwned, ownedPaths map[string]bool) []string {
	seen := make(map[string]bool)
	var result []string
	for _, e := range execs {
		if seen[e.name] {
			continue
		}
		seen[e.name] = true
		if explicitOwned[e.name] || !ownedPaths[e.path] {
			result = append(result, e.name)
		}
	}
	sort.Strings(result)
	return result
}

// cacheVersion is folded into the cache hash so that changes to the scanning
// logic invalidate any cache written by an older binary.
const cacheVersion = "v2"

type cache struct {
	Executables []string  `json:"executables"`
	Hash        string    `json:"hash"`
	Time        time.Time `json:"time"`
}

func cachePath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "cli", "executables.json")
}

func loadCache(hash string) []string {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return nil
	}
	var c cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	if c.Hash != hash {
		return nil
	}
	if time.Since(c.Time) > 10*time.Minute {
		return nil
	}
	return c.Executables
}

func saveCache(executables []string, hash string) {
	c := cache{
		Executables: executables,
		Hash:        hash,
		Time:        time.Now(),
	}
	data, _ := json.Marshal(c)
	p := cachePath()
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, data, 0644)
}

// getExplicitPackages returns the list of explicitly installed pacman/yay packages
func getExplicitPackages() map[string]bool {
	out, err := exec.Command("pacman", "-Qqe").Output()
	if err != nil {
		return nil
	}
	pkgs := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			pkgs[line] = true
		}
	}
	return pkgs
}

// getPackageExecutables returns executables owned by a set of packages
func getPackageExecutables(pkgs map[string]bool) []string {
	if len(pkgs) == 0 {
		return nil
	}

	// Get all files owned by explicitly installed packages
	args := []string{"-Ql"}
	for pkg := range pkgs {
		args = append(args, pkg)
	}
	out, err := exec.Command("pacman", args...).Output()
	if err != nil {
		return nil
	}

	// Build a set of PATH directories for fast lookup
	pathDirs := make(map[string]bool)
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		if dir != "" {
			pathDirs[dir] = true
		}
	}

	seen := make(map[string]bool)
	var result []string

	for _, line := range strings.Split(string(out), "\n") {
		// Format: "pkgname /usr/bin/executable"
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		fpath := parts[1]
		dir := filepath.Dir(fpath)
		name := filepath.Base(fpath)

		if !pathDirs[dir] {
			continue
		}
		if strings.HasPrefix(name, ".") || seen[name] {
			continue
		}

		// Check it's actually executable
		info, err := os.Stat(fpath)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode().Perm()&0111 != 0 {
			seen[name] = true
			result = append(result, name)
		}
	}

	sort.Strings(result)
	return result
}

// getOwnedPaths returns the set of file paths, owned by any installed package,
// that live on a $PATH directory. Returns nil if pacman is unavailable. Parses
// only paths (locale-independent).
func getOwnedPaths() map[string]bool {
	out, err := exec.Command("pacman", "-Qlq").Output()
	if err != nil {
		return nil
	}

	pathDirs := make(map[string]bool)
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		if dir != "" {
			pathDirs[dir] = true
		}
	}

	owned := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" || strings.HasSuffix(line, "/") {
			continue
		}
		if pathDirs[filepath.Dir(line)] {
			owned[line] = true
		}
	}
	return owned
}

func scanExecutables() []string {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return nil
	}

	hash := cacheVersion + "\x00" + pathEnv
	if cached := loadCache(hash); cached != nil {
		return cached
	}

	execs := scanPathEntries()

	explicitOwned := make(map[string]bool)
	for _, name := range getPackageExecutables(getExplicitPackages()) {
		explicitOwned[name] = true
	}
	ownedPaths := getOwnedPaths()

	result := selectExecutables(execs, explicitOwned, ownedPaths)

	saveCache(result, hash)
	return result
}

// scanPathEntries walks $PATH and returns each executable as a name+path pair.
// The first occurrence of a name wins (PATH order). Dotfiles and non-executable
// entries are skipped; symlinks are followed to check the executable bit.
func scanPathEntries() []pathExec {
	seen := make(map[string]bool)
	var result []pathExec

	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || seen[name] {
				continue
			}
			full := filepath.Join(dir, name)

			typ := e.Type()
			switch {
			case typ.IsRegular():
				info, err := e.Info()
				if err != nil || info.Mode().Perm()&0o111 == 0 {
					continue
				}
			case typ&os.ModeSymlink != 0:
				info, err := os.Stat(full) // follow the link
				if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
					continue
				}
			default:
				continue
			}

			seen[name] = true
			result = append(result, pathExec{name: name, path: full})
		}
	}

	return result
}
