# Self-Upgrade for `cli` + Reusable `selfupgrade` Module

**Date:** 2026-05-17
**Task:** Task 69 — Extend the `cli` app with upgrade functionality; extract it into a standalone reusable Go package.

## Summary

Add self-upgrade capability to the `cli` Bubble Tea TUI (repo `krzysztofciepka/cli`,
module `github.com/kc/cli`). The upgrade logic is implemented as a new standalone,
reusable public Go module `github.com/krzysztofciepka/selfupgrade` so future projects
can depend on it instead of reimplementing. The logic is a generalization of the
already-battle-tested `~/repos/clipad/upgrade.go`.

## Decisions

| Question | Decision |
|----------|----------|
| Packaging | Standalone public module now: `github.com/krzysztofciepka/selfupgrade` |
| Module name | `selfupgrade` (repo `krzysztofciepka/selfupgrade`, local `~/repos/selfupgrade`) |
| Checksum verification | Support both; auto-detect: GitHub API per-asset `digest` first, fall back to a `checksums.txt` release asset |
| Platform support | linux/amd64 only (deliberate, matches clipad); other platforms get a clear manual-reinstall error |
| `cli` UX | CLI flags only: `cli -upgrade`, `cli -version` (handled before the TUI launches) |
| Library API | `Config` struct + single `Run(ctx, cfg)` function (Approach A) |

## Architecture

### New module: `github.com/krzysztofciepka/selfupgrade`

- Public GitHub repo `krzysztofciepka/selfupgrade`, local clone `~/repos/selfupgrade`.
- Single package `selfupgrade`. Standard library only — no external dependencies.
- Deliberately linux/amd64-only. Other GOOS/GOARCH combinations return a clear error
  instructing the user to reinstall manually.

### Changes to `cli` (`github.com/kc/cli`)

- Add `var version = "dev"` in `main.go`, overridable via
  `-ldflags "-X main.version=<tag>"` at release build time.
- Add flag handling at the top of `main()`, before scanning executables / launching
  the TUI. Existing selection-file and TUI behavior is left untouched.
- Add `require github.com/krzysztofciepka/selfupgrade <version>` to `go.mod`.

## Library API

```go
package selfupgrade

type Config struct {
    Owner          string    // GitHub repo owner (required)
    Repo           string    // GitHub repo name (required)
    CurrentVersion string    // e.g. "v0.1.0" or "dev" (required)
    ExePath        string    // from os.Executable() (required)
    Out            io.Writer // progress messages; nil → io.Discard
    AssetName      string    // optional; default "{repo}-{tag}-{goos}-{goarch}[.exe]"
    APIBaseURL     string    // optional; default "https://api.github.com"
}

func Run(ctx context.Context, cfg Config) error
```

`Config` validation: `Owner`, `Repo`, `CurrentVersion`, and `ExePath` are required;
`Run` returns an error if any are empty. `Out == nil` is treated as `io.Discard`.
Empty `AssetName`/`APIBaseURL` fall back to the documented defaults.

## Behavior (`Run`)

Ported and generalized from `clipad/upgrade.go`:

1. **Platform guard.** If `runtime.GOOS != "linux" || runtime.GOARCH != "amd64"`,
   return an error: self-upgrade unsupported on this platform — reinstall manually.
2. **Resolve target.** `filepath.EvalSymlinks(ExePath)`; on failure fall back to the
   raw `ExePath` (so synthetic test paths work). Install directory = `filepath.Dir(target)`.
3. **Fetch latest release.** `GET {APIBaseURL}/repos/{Owner}/{Repo}/releases/latest`
   with a 30s context timeout, `Accept: application/vnd.github+json`,
   `User-Agent: selfupgrade/<CurrentVersion>`, response body capped at 1 MiB.
   Non-200 → error including a truncated body snippet.
4. **Up-to-date short-circuit.** If `CurrentVersion != "dev"` and equals the release
   `tag_name`, print "up to date" and return `nil`.
5. **Pick asset.** Match by name. Default template:
   `{Repo}-{tag}-{goos}-{goarch}` (`+ ".exe"` only when GOOS=windows — not reachable
   under the linux/amd64 guard, but kept generic in the template logic). Not found →
   error listing the expected asset name and release tag.
6. **Checksum auto-detect.**
   - If the picked asset's JSON `digest` field is non-empty → use it
     (`sha256:` prefix stripped).
   - Else, locate a release asset named `checksums.txt`, download it (size-capped),
     and parse the `<sha256>  <asset-name>` line matching the picked asset.
   - If neither a `digest` nor a usable `checksums.txt` entry is found → error.
     Never install an unverified binary.
7. **Download + verify.** Stream the asset to a temp file in the install directory
   (`.{repo}-upgrade-{pid}`) through a `sha256` hasher (`io.MultiWriter`). On
   mismatch, remove the temp file and error. Permission errors are wrapped with a
   "re-run with sudo or move the binary to a user-owned path" hint. Temp file is
   removed on any failure path.
8. **Atomic install with rollback.** `rename(target → target+".old")`,
   `rename(temp → target)`. If the second rename fails, roll back
   `rename(target+".old" → target)`; if the rollback also fails, return an error
   telling the user where the backup is. On success, remove `target+".old"`.
9. **Report.** Print "Upgraded `<old>` → `<new>`. Restart to use the new version."

`renameImpl` is a package-level `var renameImpl = os.Rename`, exposed only so tests
can inject a failure into the rollback path. Production code never reassigns it.

## `cli` Integration (`main.go`)

```go
var version = "dev"

func main() {
    var showVersion, doUpgrade bool
    flag.BoolVar(&showVersion, "version", false, "print version and exit")
    flag.BoolVar(&doUpgrade, "upgrade", false, "fetch the latest release and replace this binary")
    flag.Parse()

    if showVersion {
        fmt.Printf("cli %s\n", version)
        return
    }
    if doUpgrade {
        exe, err := os.Executable()
        if err != nil { /* stderr + exit 1 */ }
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        if err := selfupgrade.Run(ctx, selfupgrade.Config{
            Owner: "krzysztofciepka", Repo: "cli",
            CurrentVersion: version, ExePath: exe, Out: os.Stderr,
        }); err != nil { /* stderr + exit 1 */ }
        return
    }

    // ...existing executable scan / config load / TUI launch, unchanged...
}
```

The existing stale-selection-file removal and Bubble Tea program logic are unchanged
and only run when no upgrade/version flag is given.

### Release process note

Published `cli` binaries must carry the injected version via
`-ldflags "-X main.version=<tag>"`. The existing `dist/` layout already uses
`cli-v<ver>-linux-amd64` asset naming plus a `checksums.txt`, which matches the
library defaults — no release-asset renaming required.

## Testing

### `selfupgrade` (table-driven, `httptest.Server` for `APIBaseURL`)

- Up-to-date short-circuit (non-dev version equals latest tag).
- Asset not found in release.
- Digest-path verification success.
- `checksums.txt` fallback path success.
- Checksum mismatch → error, temp file removed.
- Neither digest nor checksums.txt → error.
- Download permission error → wrapped hint message.
- Atomic install success (binary replaced).
- Rollback path via injected `renameImpl` failure.
- Platform guard error path.

### `cli`

- `-version` prints `cli <version>`.
- `-upgrade` dispatches into `selfupgrade.Run` without entering the TUI
  (using a test `APIBaseURL` seam where practical).

## Out of Scope (YAGNI)

- Check-only / "is there an update?" command.
- Multi-platform support beyond linux/amd64.
- GUI/TUI-triggered upgrade.
- Auto-update on startup.
- Downgrade or version pinning.
