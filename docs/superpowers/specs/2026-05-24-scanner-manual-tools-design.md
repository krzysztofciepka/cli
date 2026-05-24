# Scanner — Include Manually-Installed Tools (Task 69)

## Problem

The drawer does not list software that pacman doesn't manage. For example,
executables in `/usr/local/bin` (`cli`, `clipad`, `minimal-agent`,
`scriptpilot`, `usb-mount.sh`) never appear, even though that directory is on
`$PATH`.

### Root cause

`scanExecutables()` builds its list from pacman's explicitly-installed packages
(`pacman -Qqe` → `pacman -Ql`), keeping only executables that live on a `$PATH`
directory. A direct PATH scan exists, but only as a fallback that runs when the
pacman query returns **zero** results:

```go
result := getPackageExecutables(pkgs)
if len(result) == 0 {       // never true on a normal Arch system
    result = scanPath()
}
```

Because pacman always returns plenty of executables, the fallback never runs, so
any executable not owned by an explicitly-installed package is invisible. This
includes all manually-installed tools (`/usr/local/bin`, `~/.local/bin`,
`~/go/bin`, `~/.bun/bin`, …).

The original design (`2026-03-16-cli-app-drawer-design.md`) specified a plain
PATH scan. The pacman path was added later to *curate* the list — to avoid
flooding it with hundreds of low-level helper binaries from `/usr/bin`. That
curation is desirable; it just wrongly excludes manual installs.

## Goal

Keep the curated list, **and** include manually-installed tools.

Keep an executable if **either**:

1. its name is owned by an explicitly-installed package (curated set, as today), **or**
2. its path is **not owned by any package** (a manual install).

Executables owned only by *dependency* packages are dropped — this preserves the
clean, curated list. First occurrence of a name wins (PATH order), so a manual
`/usr/local/bin/foo` correctly shadows a system `foo`.

## Approach

Detect "not owned by pacman" with `pacman -Qlq` (no package argument), which
lists every owned file path (~350k lines on a typical system), filtered to
`$PATH` directories. This parses **only paths**, so it is immune to locale (the
target system runs pacman in Polish, which makes `-Qo` prose parsing fragile).
It is one extra subprocess call, and results are cached.

Rejected alternatives:

- **Per-file `pacman -Qo` exit code** — robust per file, but one subprocess call
  per PATH executable (thousands). Too slow.
- **Batched `pacman -Qo` parsing prose** — fewer calls, but parses localized
  messages ("…is owned by…" / "Żaden pakiet…"). Brittle; would need `LC_ALL=C`.

## Components (`scanner.go`)

```go
type pathExec struct {
    name string
    path string
}
```

- **`scanPathEntries() []pathExec`** — replaces `scanPath()`. Walks `$PATH` split
  on `:`, `os.ReadDir` each dir, returns `{name, path}` for entries that are a
  regular file or symlink. Skips names starting with `.`. First occurrence of a
  name wins (PATH order preserved). Testable by pointing `$PATH` at temp dirs.

- **`getOwnedPaths() map[string]bool`** — runs `pacman -Qlq`; for each output
  line whose directory is a `$PATH` directory, adds the full path to the set.
  Returns `nil`/empty if pacman is unavailable. Thin pacman wrapper.

- **`selectExecutables(execs []pathExec, explicitOwned, ownedPaths map[string]bool) []string`**
  — **pure**. Keeps `e` if `explicitOwned[e.name] || !ownedPaths[e.path]`.
  Deduplicates by name (first occurrence wins), returns sorted. No subprocess
  calls; fully unit-tested.

- **`getExplicitPackages()` / `getPackageExecutables()`** — unchanged. The latter
  already returns executable names owned by explicitly-installed packages; its
  result is converted to a name set (`explicitOwned`).

- **`scanExecutables()`** — wiring:
  1. If `$PATH` empty, return `nil`.
  2. Return cached result if valid (see Cache).
  3. `execs := scanPathEntries()`.
  4. `explicitOwned` := name set from `getPackageExecutables(getExplicitPackages())`.
  5. `ownedPaths := getOwnedPaths()`.
  6. `result := selectExecutables(execs, explicitOwned, ownedPaths)`.
  7. Save cache, return `result`.

## Portable fallback (no pacman)

When pacman is unavailable, `getExplicitPackages` and `getOwnedPaths` return
empty maps. `selectExecutables` then keeps every entry (`!ownedPaths[path]` is
always true) — i.e. a plain full-PATH scan. The old
`if len(result) == 0 { result = scanPath() }` special case is removed; the union
semantics subsume it. `scanPath()` is removed (replaced by `scanPathEntries`).

## Cache

Unchanged mechanism: keyed on the `$PATH` string with a 10-minute TTL. A small
`cacheVersion` constant is folded into the cache hash (`hash = cacheVersion + PATH`)
so this logic change invalidates any stale cache written by the old behavior;
otherwise a user could see the old (incomplete) list for up to 10 minutes after
upgrading.

## Testing (TDD)

`selectExecutables` (pure, table-driven):

- explicit-owned name is kept
- manual executable (path not in `ownedPaths`) is kept
- dependency-only executable (path in `ownedPaths`, name not in `explicitOwned`)
  is dropped
- first-occurrence shadowing: manual `/usr/local/bin/foo` shadows owned
  `/usr/bin/foo` and is kept
- empty `explicitOwned` and `ownedPaths` (no pacman) → all entries kept
- result is sorted alphabetically and deduplicated by name

`scanPathEntries` (temp-dir `$PATH`):

- executable regular files are returned; non-executable files are skipped
- dotfiles are skipped
- symlinks are returned
- duplicate names across dirs: first dir on PATH wins; returned path matches

The three pacman wrappers (`getExplicitPackages`, `getPackageExecutables`,
`getOwnedPaths`) stay thin and are verified manually: clear the cache and confirm
the `/usr/local/bin` tools now appear in the drawer.

## Out of scope

- Non-Arch package managers (dpkg, rpm, …). The portable fallback already shows
  everything on PATH on such systems.
- Visual distinction of manual vs packaged tools — manual tools sort in
  alphabetically like any other entry.
