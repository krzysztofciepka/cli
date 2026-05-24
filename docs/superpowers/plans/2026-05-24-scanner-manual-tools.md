# Scanner — Include Manually-Installed Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `cli` drawer list manually-installed tools (e.g. `/usr/local/bin`) while keeping the pacman-curated list clean.

**Architecture:** Replace the "pacman list, fall back to PATH scan only if empty" logic with a union: scan `$PATH` for all executables, then keep each one if it is owned by an explicitly-installed package OR not owned by any package. A small pure function (`selectExecutables`) holds the decision logic so it is unit-testable; thin pacman wrappers supply the inputs.

**Tech Stack:** Go 1.26, standard library only. Tests via `go test`. Target system: Arch Linux with pacman (Polish locale).

---

### Task 1: `selectExecutables` pure selection logic

**Files:**
- Modify: `scanner.go` (add `pathExec` type and `selectExecutables` near top, after imports)
- Test: `scanner_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `scanner_test.go`:

```go
package main

import (
	"reflect"
	"testing"
)

func TestSelectExecutables(t *testing.T) {
	tests := []struct {
		name          string
		execs         []pathExec
		explicitOwned map[string]bool
		ownedPaths    map[string]bool
		want          []string
	}{
		{
			name:          "explicit-owned kept",
			execs:         []pathExec{{name: "git", path: "/usr/bin/git"}},
			explicitOwned: map[string]bool{"git": true},
			ownedPaths:    map[string]bool{"/usr/bin/git": true},
			want:          []string{"git"},
		},
		{
			name:          "manual (unowned) kept",
			execs:         []pathExec{{name: "clipad", path: "/usr/local/bin/clipad"}},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{},
			want:          []string{"clipad"},
		},
		{
			name:          "dependency-only dropped",
			execs:         []pathExec{{name: "helperlib", path: "/usr/bin/helperlib"}},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{"/usr/bin/helperlib": true},
			want:          nil,
		},
		{
			name: "manual shadows owned (first wins)",
			execs: []pathExec{
				{name: "foo", path: "/usr/local/bin/foo"},
				{name: "foo", path: "/usr/bin/foo"},
			},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{"/usr/bin/foo": true},
			want:          []string{"foo"},
		},
		{
			name: "no pacman keeps everything, sorted",
			execs: []pathExec{
				{name: "zeta", path: "/usr/bin/zeta"},
				{name: "alpha", path: "/usr/bin/alpha"},
			},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{},
			want:          []string{"alpha", "zeta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectExecutables(tt.execs, tt.explicitOwned, tt.ownedPaths)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestSelectExecutables -v`
Expected: FAIL — `undefined: pathExec` / `undefined: selectExecutables`.

- [ ] **Step 3: Write minimal implementation**

In `scanner.go`, immediately after the `import (...)` block, add:

```go
type pathExec struct {
	name string
	path string
}

// selectExecutables returns the curated, sorted, deduplicated list of
// executable names to display. An executable is kept when its name is owned by
// an explicitly-installed package (explicitOwned) or its path is not owned by
// any package (ownedPaths) — i.e. a manual install. Executables owned only by
// dependency packages are dropped. The first occurrence of a name wins.
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
```

(`sort` is already imported in `scanner.go`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestSelectExecutables -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add scanner.go scanner_test.go
git commit -m "$(cat <<'EOF'
feat: add pure selectExecutables curation logic

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: `scanPathEntries` — scan PATH returning name + path

**Files:**
- Modify: `scanner.go` (add `scanPathEntries`)
- Test: `scanner_test.go` (add `TestScanPathEntries`)

- [ ] **Step 1: Write the failing test**

Append to `scanner_test.go`:

```go
func TestScanPathEntries(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	mustWrite := func(dir, name string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
		return p
	}

	mustWrite(dirA, "alpha", 0o755)   // executable -> included
	mustWrite(dirA, "notexec", 0o644) // not executable -> skipped
	mustWrite(dirA, ".hidden", 0o755) // dotfile -> skipped
	target := mustWrite(dirA, "linktarget", 0o755)
	mustWrite(dirA, "dup", 0o755) // duplicate name, dirA wins
	mustWrite(dirB, "dup", 0o755) // duplicate name, shadowed
	mustWrite(dirB, "beta", 0o755)

	link := filepath.Join(dirB, "blink") // symlink to an executable
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dirA+":"+dirB)

	byName := map[string]string{}
	for _, e := range scanPathEntries() {
		if _, ok := byName[e.name]; ok {
			t.Fatalf("duplicate name in result: %s", e.name)
		}
		byName[e.name] = e.path
	}

	if _, ok := byName["alpha"]; !ok {
		t.Errorf("alpha should be included")
	}
	if _, ok := byName["notexec"]; ok {
		t.Errorf("notexec should be skipped (not executable)")
	}
	if _, ok := byName[".hidden"]; ok {
		t.Errorf("dotfile should be skipped")
	}
	if _, ok := byName["blink"]; !ok {
		t.Errorf("symlink blink should be included")
	}
	if _, ok := byName["beta"]; !ok {
		t.Errorf("beta should be included")
	}
	if byName["dup"] != filepath.Join(dirA, "dup") {
		t.Errorf("dup should resolve to dirA (first on PATH), got %s", byName["dup"])
	}
}
```

Add `"os"` and `"path/filepath"` to `scanner_test.go` imports:

```go
import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestScanPathEntries -v`
Expected: FAIL — `undefined: scanPathEntries`.

- [ ] **Step 3: Write minimal implementation**

In `scanner.go`, add (you will delete the old `scanPath` in Task 4):

```go
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
```

(`os`, `path/filepath`, `strings` are already imported in `scanner.go`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestScanPathEntries -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add scanner.go scanner_test.go
git commit -m "$(cat <<'EOF'
feat: add scanPathEntries returning name and path

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: `getOwnedPaths` — set of pacman-owned PATH files

**Files:**
- Modify: `scanner.go` (add `getOwnedPaths`)

No unit test: this is a thin pacman wrapper, verified manually in Task 5.

- [ ] **Step 1: Write the implementation**

In `scanner.go`, add after `getPackageExecutables`:

```go
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
		if line == "" {
			continue
		}
		if pathDirs[filepath.Dir(line)] {
			owned[line] = true
		}
	}
	return owned
}
```

(`exec` is already imported in `scanner.go`.)

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: builds with no errors. (`getOwnedPaths` is unused until Task 4; Go allows unused package-level functions, so this compiles.)

- [ ] **Step 3: Commit**

```bash
git add scanner.go
git commit -m "$(cat <<'EOF'
feat: add getOwnedPaths pacman wrapper

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Rewire `scanExecutables`, bump cache version, remove `scanPath`

**Files:**
- Modify: `scanner.go` (`scanExecutables`; add `cacheVersion`; delete `scanPath`)

- [ ] **Step 1: Add the cache version constant**

In `scanner.go`, just above the `cache` struct definition, add:

```go
// cacheVersion is folded into the cache hash so that changes to the scanning
// logic invalidate any cache written by an older binary.
const cacheVersion = "v2"
```

- [ ] **Step 2: Replace `scanExecutables`**

Replace the entire existing `scanExecutables` function with:

```go
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
```

- [ ] **Step 3: Delete the old `scanPath` function**

Remove the entire `scanPath` function from `scanner.go` (the old `func scanPath() []string { ... }` that ranges over `$PATH` and returns names). It is fully replaced by `scanPathEntries`.

- [ ] **Step 4: Verify the whole suite builds and passes**

Run: `go build ./... && go test . -v`
Expected: build succeeds; all tests PASS (including the existing `TestRunFlags_*` tests and the new scanner tests). No references to `scanPath` remain.

Sanity check there are no stragglers:

Run: `grep -rn "scanPath\b" --include=*.go .`
Expected: no output (only `scanPathEntries` should exist).

- [ ] **Step 5: Commit**

```bash
git add scanner.go
git commit -m "$(cat <<'EOF'
fix: include manually-installed tools in the drawer (Task 69)

Build the list as a union of pacman-curated executables and PATH
executables not owned by any package, so manual installs in
/usr/local/bin, ~/.local/bin, ~/go/bin, etc. now appear. Bump the cache
version to invalidate stale caches from the old behavior.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Manual verification

**Files:** none (runtime check)

- [ ] **Step 1: Build the binary**

Run: `go build -o cli .`
Expected: produces `./cli`.

- [ ] **Step 2: Clear any cached executable list**

Run: `rm -f "${XDG_CACHE_HOME:-$HOME/.cache}/cli/executables.json"`
Expected: cache removed (no error if absent).

- [ ] **Step 3: Confirm discovery via a throwaway check**

The app's only runtime entry point launches the interactive TUI, so verify the
scanner directly with a temporary test that calls `scanExecutables()` and prints
what it finds. Create `verify_manual_test.go`:

```go
package main

import (
	"fmt"
	"testing"
)

func TestManualToolsDiscovered_throwaway(t *testing.T) {
	got := scanExecutables()
	set := map[string]bool{}
	for _, n := range got {
		set[n] = true
	}
	for _, name := range []string{"clipad", "minimal-agent", "scriptpilot"} {
		if !set[name] {
			t.Errorf("manual tool %q not found in scan result", name)
		}
	}
	fmt.Printf("scan returned %d executables\n", len(got))
}
```

Run: `go test . -run TestManualToolsDiscovered_throwaway -v`
Expected: PASS, and the printed count is in the curated range (hundreds, not
several thousand) — confirming manual tools now appear AND the list is still
curated (dependency helper binaries from `/usr/bin` are not flooding it).

- [ ] **Step 4: Remove the throwaway test**

Run: `rm verify_manual_test.go`
Expected: file removed. It is environment-specific and must not be committed.

Run: `go test . -v`
Expected: PASS (only the permanent tests remain).

---

## Self-Review

**Spec coverage:**
- Union keep rule (explicit-owned OR unowned) → Task 1 `selectExecutables`. ✓
- Scan PATH for name+path, first-wins, exec-bit, dotfile skip → Task 2 `scanPathEntries`. ✓
- Detect unowned via `pacman -Qlq` filtered to PATH dirs → Task 3 `getOwnedPaths`. ✓
- Cache version bump to invalidate stale caches → Task 4 `cacheVersion`. ✓
- Remove `len(result)==0 → scanPath()` fallback and `scanPath` → Task 4. ✓
- Portable no-pacman fallback (empty maps keep everything) → covered by Task 1 "no pacman" test + Task 4 wiring. ✓
- Testing: `selectExecutables` table tests + `scanPathEntries` temp-dir test → Tasks 1–2. ✓
- Manual verification of pacman wrappers → Task 5. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code.

**Type consistency:** `pathExec{name, path}` and `selectExecutables(execs []pathExec, explicitOwned, ownedPaths map[string]bool) []string` are used identically in Tasks 1, 2, and 4.
