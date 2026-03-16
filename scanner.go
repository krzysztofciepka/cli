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

func scanExecutables() []string {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return nil
	}

	if cached := loadCache(pathEnv); cached != nil {
		return cached
	}

	pkgs := getExplicitPackages()
	result := getPackageExecutables(pkgs)

	if len(result) == 0 {
		// Fallback: scan PATH directly
		result = scanPath()
	}

	saveCache(result, pathEnv)
	return result
}

func scanPath() []string {
	seen := make(map[string]bool)
	var result []string

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
			typ := e.Type()
			if typ.IsRegular() || typ&os.ModeSymlink != 0 {
				seen[name] = true
				result = append(result, name)
			}
		}
	}

	sort.Strings(result)
	return result
}
