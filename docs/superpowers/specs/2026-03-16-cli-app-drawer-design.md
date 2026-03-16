# CLI App Drawer — Design Spec

## Overview

A Go TUI application called `cli` that acts as an app drawer for command-line tools. It lists all executables found on the user's `$PATH`, supports starring favorites, adding short descriptions, fuzzy filtering, and outputs the selected command name to stdout for shell integration.

## Architecture

Single Go binary, three components:

1. **Scanner** — walks `$PATH` directories, collects unique executable names (first occurrence wins for deduplication)
2. **TUI** — Bubbletea model with filtered list, star/description management, keyboard handling
3. **Config** — reads/writes `~/.config/cli/config.json`

## Dependencies

- `github.com/charmbracelet/bubbletea` — TUI framework
- `github.com/charmbracelet/lipgloss` — styling
- `github.com/charmbracelet/bubbles` — text input component
- `github.com/sahilm/fuzzy` — fuzzy matching

## TUI Layout

```
 > filter...

 ★ git        — version control
 ★ nvim       — editor
 ★ docker     — containers
   awk
   bash
   cat
   curl
   ...

 ↑↓ navigate  / filter  s star  d describe  enter select  q quit
```

## Key Bindings

| Key | Action |
|-----|--------|
| ↑/↓ or j/k | Navigate list |
| / | Focus filter input (fuzzy search by name and description) |
| Escape | Clear filter / exit filter mode / quit if no filter |
| s | Toggle star on highlighted item |
| d | Edit description (inline text input, Enter to confirm, Esc to cancel) |
| Enter | Print selected command to stdout, exit 0 |
| q | Quit without selection, exit 1 |

## States

The TUI has three modes:
- **Browse** — normal navigation, star toggling, selecting
- **Filter** — typing into the filter input, list updates live
- **Describe** — editing the description of the highlighted item

## Sorting

1. Starred items first (alphabetical within starred)
2. Unstarred items second (alphabetical)
3. When filtering, results sorted by fuzzy match score, starred still on top within equal scores

## Config File

Location: `~/.config/cli/config.json`

```json
{
  "starred": ["git", "nvim", "docker"],
  "descriptions": {
    "git": "version control",
    "nvim": "editor",
    "docker": "containers"
  }
}
```

Created automatically on first write. Directory created if missing.

## Shell Integration

The binary prints the selected command name to stdout. A wrapper function places it on the command line:

**Zsh:**
```zsh
cli() {
  local cmd
  cmd=$(command cli "$@")
  if [ -n "$cmd" ]; then
    print -z "$cmd"
  fi
}
```

**Bash:**
```bash
cli() {
  local cmd
  cmd=$(command cli "$@")
  if [ -n "$cmd" ]; then
    bind '"\e[0n": "'"$cmd"'"'
    printf '\e[5n'
  fi
}
```

## Scanner

- Read `$PATH`, split on `:`
- For each directory, `os.ReadDir` and collect entries where `mode.IsRegular() || mode&os.ModeSymlink != 0` and executable bit is set
- Deduplicate: first occurrence wins (matches shell behavior)
- Skip entries starting with `.`

## Performance

- No caching needed — PATH scan of ~5-10 dirs with a few thousand total executables takes <100ms
- Config file is small JSON, read once at startup, written on star/describe changes

## Output Behavior

- Selection made: print command name to stdout, exit code 0
- No selection (q/Esc): print nothing, exit code 1
- TUI rendering goes to stderr (Bubbletea default) so it doesn't interfere with stdout capture
