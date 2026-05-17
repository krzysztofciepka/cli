# selfupgrade Module + `cli` Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a reusable public Go module `github.com/krzysztofciepka/selfupgrade` for GitHub-release self-upgrade, then wire the `cli` TUI to use it via `-upgrade`/`-version` flags.

**Architecture:** A standalone, stdlib-only package generalizing `clipad/upgrade.go`: a `Config` struct + single `Run(ctx, cfg)` entry point. Internals are split by responsibility (release fetch, checksum resolution, download, install). Checksum verification auto-detects the GitHub per-asset `digest`, falling back to a `checksums.txt` release asset. linux/amd64 only. `cli` gains a testable `runFlags` helper that dispatches `-upgrade` into the library.

**Tech Stack:** Go 1.26.x, standard library only (`net/http`, `crypto/sha256`, `httptest` for tests). GitHub via `gh` CLI for repo/release.

**Spec:** `docs/superpowers/specs/2026-05-17-selfupgrade-design.md`

**Reference (read-only):** `~/repos/clipad/upgrade.go` and `~/repos/clipad/upgrade_test.go` — proven prior art this generalizes.

---

## File Structure

### New module `~/repos/selfupgrade` (module path `github.com/krzysztofciepka/selfupgrade`)

| File | Responsibility |
|------|----------------|
| `go.mod` | Module declaration, `go 1.26` |
| `selfupgrade.go` | `Config`, defaults/validation, asset-name templating, `Run` orchestration, platform guard |
| `release.go` | `release`/`releaseAsset` types, `fetchLatestRelease`, `pickAsset`, `snippet` |
| `checksum.go` | `resolveChecksum` (digest → checksums.txt fallback), `parseChecksums` |
| `download.go` | `downloadAsset`, `humanSize` |
| `install.go` | `installBinary`, injectable `renameImpl` |
| `README.md` | Usage docs |
| `helpers_test.go` | Shared test helper `fakeRelease` |
| `release_test.go`, `checksum_test.go`, `download_test.go`, `install_test.go`, `selfupgrade_test.go` | Per-file tests |

### `~/repos/cli-app` (module `github.com/kc/cli`)

| File | Change |
|------|--------|
| `main.go` | Add `version` var, `runFlags` helper, wire `selfupgrade.Run`; `go.mod`/`go.sum` gain the dependency |
| `main_test.go` | Tests for `runFlags` |
| `README.md` | Document `-upgrade`/`-version` |

---

## Task 1: Create the `selfupgrade` repo

**Files:**
- Create: `~/repos/selfupgrade/go.mod`

- [ ] **Step 1: Confirm repo creation with the user**

Per the todo-task workflow, do not create a GitHub repo without explicit confirmation. Ask:
> "I'll create public repo `selfupgrade` at `~/repos/selfupgrade` (module `github.com/krzysztofciepka/selfupgrade`). Confirm or provide a different name/path."

Wait for confirmation before Step 2.

- [ ] **Step 2: Scaffold the module and repo**

```bash
mkdir -p ~/repos/selfupgrade
cd ~/repos/selfupgrade
git init
go mod init github.com/krzysztofciepka/selfupgrade
gh repo create selfupgrade --public --source=. --remote=origin
```

- [ ] **Step 3: Pin the Go version in go.mod**

Ensure `~/repos/selfupgrade/go.mod` reads exactly:

```
module github.com/krzysztofciepka/selfupgrade

go 1.26
```

- [ ] **Step 4: Verify the module builds**

Run: `cd ~/repos/selfupgrade && go build ./...`
Expected: no output, exit 0 (empty module compiles).

- [ ] **Step 5: Commit**

```bash
cd ~/repos/selfupgrade
git add go.mod
git commit -m "chore: initialize selfupgrade module"
```

---

## Task 2: `install.go` — atomic install with rollback

**Files:**
- Create: `~/repos/selfupgrade/install.go`
- Test: `~/repos/selfupgrade/install_test.go`

- [ ] **Step 1: Write the failing tests**

Create `~/repos/selfupgrade/install_test.go`:

```go
package selfupgrade

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallBinary_HappyPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cli")
	src := filepath.Join(dir, ".cli-upgrade-1234")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o755); err != nil {
		t.Fatalf("seed src: %v", err)
	}

	if err := installBinary(src, target); err != nil {
		t.Fatalf("installBinary: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("target content = %q, want \"new\"", got)
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf(".old should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("src should be moved away, stat err = %v", err)
	}
}

func TestInstallBinary_PermissionErrorOnFirstRename(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cli")
	src := filepath.Join(dir, ".cli-upgrade-1234")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o755); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	err := installBinary(src, target)
	if err == nil {
		t.Fatal("expected error on read-only dir")
	}
	if !strings.Contains(err.Error(), "move existing binary aside") {
		t.Fatalf("error should reference backup step, got: %v", err)
	}
	os.Chmod(dir, 0o755)
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("target content changed: %q", got)
	}
}

func TestInstallBinary_RollbackOnSecondRenameFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cli")
	src := filepath.Join(dir, ".cli-upgrade-1234")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o755); err != nil {
		t.Fatalf("seed src: %v", err)
	}

	original := renameImpl
	calls := 0
	renameImpl = func(oldpath, newpath string) error {
		calls++
		if calls == 2 {
			return errors.New("simulated rename failure")
		}
		return original(oldpath, newpath)
	}
	t.Cleanup(func() { renameImpl = original })

	err := installBinary(src, target)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "install new binary") {
		t.Fatalf("error should mention install failure, got: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("rollback failed: target content = %q, want \"old\"", got)
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf(".old should not be left behind after rollback, stat err = %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/repos/selfupgrade && go test ./... 2>&1 | head`
Expected: FAIL — build error, `undefined: installBinary` / `undefined: renameImpl`.

- [ ] **Step 3: Write the implementation**

Create `~/repos/selfupgrade/install.go`:

```go
package selfupgrade

import (
	"fmt"
	"os"
)

// renameImpl is os.Rename, exposed as a package-level variable so tests can
// inject a failure into the rollback path. Production code never reassigns it.
var renameImpl = os.Rename

// installBinary atomically replaces targetPath with srcPath. The existing
// binary is moved aside to targetPath+".old"; on failure it is restored.
func installBinary(srcPath, targetPath string) error {
	backup := targetPath + ".old"
	if err := renameImpl(targetPath, backup); err != nil {
		return fmt.Errorf("cannot move existing binary aside: %w", err)
	}
	if err := renameImpl(srcPath, targetPath); err != nil {
		if rerr := renameImpl(backup, targetPath); rerr != nil {
			return fmt.Errorf("failed to install new binary: %w; original saved at %s — restore manually", err, backup)
		}
		return fmt.Errorf("failed to install new binary: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/repos/selfupgrade && go test ./... -run TestInstallBinary -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
cd ~/repos/selfupgrade
git add install.go install_test.go
git commit -m "feat: atomic install with rollback"
```

---

## Task 3: `release.go` — fetch latest release and pick asset

**Files:**
- Create: `~/repos/selfupgrade/selfupgrade.go` (minimal stub: `Config`, `userAgentPrefix` — needed by `release.go`)
- Create: `~/repos/selfupgrade/release.go`
- Test: `~/repos/selfupgrade/release_test.go`

> Note: `release.go` references `Config` and `userAgentPrefix`. This task adds a minimal `selfupgrade.go` containing only those; Task 6 fills in `Run` and the rest.

- [ ] **Step 1: Write the failing tests**

Create `~/repos/selfupgrade/release_test.go`:

```go
package selfupgrade

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPickAsset_PicksMatchingFromMany(t *testing.T) {
	rel := release{
		TagName: "v0.0.22",
		Assets: []releaseAsset{
			{Name: "cli-v0.0.22-darwin-arm64"},
			{Name: "cli-v0.0.22-linux-amd64", BrowserDownloadURL: "https://example.test/want"},
			{Name: "checksums.txt"},
		},
	}
	a, err := pickAsset(rel, "cli-v0.0.22-linux-amd64")
	if err != nil {
		t.Fatalf("pickAsset: %v", err)
	}
	if a.BrowserDownloadURL != "https://example.test/want" {
		t.Fatalf("picked wrong asset: %+v", a)
	}
}

func TestPickAsset_NoMatchReturnsError(t *testing.T) {
	rel := release{TagName: "v0.0.22", Assets: []releaseAsset{{Name: "cli-v0.0.22-darwin-arm64"}}}
	_, err := pickAsset(rel, "cli-v0.0.22-linux-amd64")
	if err == nil {
		t.Fatal("expected error for no match")
	}
	if !strings.Contains(err.Error(), "cli-v0.0.22-linux-amd64") || !strings.Contains(err.Error(), "v0.0.22") {
		t.Fatalf("error should mention asset name and tag, got: %v", err)
	}
}

func TestFetchLatestRelease_Success(t *testing.T) {
	body := `{"tag_name":"v0.0.22","assets":[{"name":"cli-v0.0.22-linux-amd64","browser_download_url":"https://example.test/cli","size":16355490,"digest":"sha256:deadbeef"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/krzysztofciepka/cli/releases/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept header = %q", got)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "selfupgrade/") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := Config{Owner: "krzysztofciepka", Repo: "cli", CurrentVersion: "v0.0.20", APIBaseURL: srv.URL}
	rel, err := fetchLatestRelease(ctx, cfg)
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if rel.TagName != "v0.0.22" {
		t.Fatalf("TagName = %q", rel.TagName)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Digest != "sha256:deadbeef" {
		t.Fatalf("assets = %+v", rel.Assets)
	}
}

func TestFetchLatestRelease_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"rate limit"}`)
	}))
	defer srv.Close()

	cfg := Config{Owner: "o", Repo: "r", CurrentVersion: "v0", APIBaseURL: srv.URL}
	_, err := fetchLatestRelease(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("error should mention status and body, got: %v", err)
	}
}

func TestFetchLatestRelease_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json {`)
	}))
	defer srv.Close()

	cfg := Config{Owner: "o", Repo: "r", CurrentVersion: "v0", APIBaseURL: srv.URL}
	_, err := fetchLatestRelease(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "parse release metadata") {
		t.Fatalf("error should mention parse failure, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/repos/selfupgrade && go test ./... 2>&1 | head`
Expected: FAIL — `undefined: release`, `undefined: Config`, `undefined: fetchLatestRelease`.

- [ ] **Step 3: Write the minimal `selfupgrade.go` stub**

Create `~/repos/selfupgrade/selfupgrade.go`:

```go
package selfupgrade

import "io"

// Config controls a self-upgrade run. (Run and helpers are added in a later task.)
type Config struct {
	Owner          string    // GitHub repo owner (required)
	Repo           string    // GitHub repo name (required)
	CurrentVersion string    // e.g. "v0.1.0" or "dev" (required)
	ExePath        string    // path to the running binary (required)
	Out            io.Writer // progress messages; nil -> io.Discard
	AssetName      string    // optional name template; default "{repo}-{tag}-{goos}-{goarch}"
	APIBaseURL     string    // optional; default "https://api.github.com"
}

const userAgentPrefix = "selfupgrade/"
```

- [ ] **Step 4: Write `release.go`**

Create `~/repos/selfupgrade/release.go`:

```go
package selfupgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const maxBodyBytes = 1 << 20

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

func fetchLatestRelease(ctx context.Context, cfg Config) (release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", cfg.APIBaseURL, cfg.Owner, cfg.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return release{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgentPrefix+cfg.CurrentVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return release{}, fmt.Errorf("read release metadata: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("failed to fetch latest release: %d: %s", resp.StatusCode, snippet(body))
	}

	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return release{}, fmt.Errorf("failed to parse release metadata: %w", err)
	}
	return rel, nil
}

func snippet(b []byte) string {
	const max = 200
	s := string(b)
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func pickAsset(rel release, want string) (releaseAsset, error) {
	for _, a := range rel.Assets {
		if a.Name == want {
			return a, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("no asset matching %s in release %s", want, rel.TagName)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/repos/selfupgrade && go test ./... -run 'TestPickAsset|TestFetchLatestRelease' -v`
Expected: PASS (5 tests). `go vet ./...` clean.

- [ ] **Step 6: Commit**

```bash
cd ~/repos/selfupgrade
git add selfupgrade.go release.go release_test.go
git commit -m "feat: fetch latest GitHub release and pick asset"
```

---

## Task 4: `download.go` — verified download + humanSize

**Files:**
- Create: `~/repos/selfupgrade/download.go`
- Test: `~/repos/selfupgrade/download_test.go`

- [ ] **Step 1: Write the failing tests**

Create `~/repos/selfupgrade/download_test.go`:

```go
package selfupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadAsset_Success(t *testing.T) {
	payload := []byte("fake cli binary contents")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	cfg := Config{CurrentVersion: "vTest"}
	dst := filepath.Join(t.TempDir(), "cli.new")
	if err := downloadAsset(context.Background(), cfg, srv.URL, dst, digest); err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch")
	}
	info, _ := os.Stat(dst)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestDownloadAsset_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "cli.new")
	err := downloadAsset(context.Background(), Config{CurrentVersion: "vTest"}, srv.URL, dst, "0")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("error should mention status, got: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("temp file should not exist, stat err = %v", err)
	}
}

func TestDownloadAsset_DigestMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("payload"))
	}))
	defer srv.Close()

	wrong := strings.Repeat("0", 64)
	dst := filepath.Join(t.TempDir(), "cli.new")
	err := downloadAsset(context.Background(), Config{CurrentVersion: "vTest"}, srv.URL, dst, wrong)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error should mention checksum mismatch, got: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("temp file should be cleaned up on mismatch, stat err = %v", err)
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		512:                  "512 B",
		2048:                 "2.0 KB",
		5 * 1024 * 1024:      "5.0 MB",
	}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/repos/selfupgrade && go test ./... -run 'TestDownloadAsset|TestHumanSize' 2>&1 | head`
Expected: FAIL — `undefined: downloadAsset`, `undefined: humanSize`.

- [ ] **Step 3: Write the implementation**

Create `~/repos/selfupgrade/download.go`:

```go
package selfupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
)

// downloadAsset streams url into dstPath (mode 0755) while computing sha256,
// then verifies it equals expectedHexDigest (lowercase hex, no "sha256:"
// prefix). On any error the partial file is removed.
func downloadAsset(ctx context.Context, cfg Config, url, dstPath, expectedHexDigest string) (retErr error) {
	defer func() {
		if retErr != nil {
			os.Remove(dstPath)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgentPrefix+cfg.CurrentVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: %d", url, resp.StatusCode)
	}

	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dstPath, err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("download interrupted: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dstPath, err)
	}

	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedHexDigest {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHexDigest, got)
	}
	return nil
}

func humanSize(n int64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
	)
	switch {
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/repos/selfupgrade && go test ./... -run 'TestDownloadAsset|TestHumanSize' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
cd ~/repos/selfupgrade
git add download.go download_test.go
git commit -m "feat: verified asset download and humanSize"
```

---

## Task 5: `checksum.go` — digest with checksums.txt fallback

**Files:**
- Create: `~/repos/selfupgrade/checksum.go`
- Test: `~/repos/selfupgrade/checksum_test.go`

- [ ] **Step 1: Write the failing tests**

Create `~/repos/selfupgrade/checksum_test.go`:

```go
package selfupgrade

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveChecksum_UsesDigestField(t *testing.T) {
	rel := release{TagName: "v1"}
	asset := releaseAsset{Name: "cli-v1-linux-amd64", Digest: "sha256:ABCDEF"}
	got, err := resolveChecksum(context.Background(), Config{CurrentVersion: "v1"}, rel, asset)
	if err != nil {
		t.Fatalf("resolveChecksum: %v", err)
	}
	if got != "abcdef" {
		t.Fatalf("got %q, want lowercased hex without prefix", got)
	}
}

func TestResolveChecksum_FallsBackToChecksumsTxt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "deadbeef  cli-v1-linux-amd64\nfeed00  other-file\n")
	}))
	defer srv.Close()

	rel := release{
		TagName: "v1",
		Assets: []releaseAsset{
			{Name: "cli-v1-linux-amd64"},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL},
		},
	}
	asset := releaseAsset{Name: "cli-v1-linux-amd64"} // no Digest
	got, err := resolveChecksum(context.Background(), Config{CurrentVersion: "v1"}, rel, asset)
	if err != nil {
		t.Fatalf("resolveChecksum: %v", err)
	}
	if got != "deadbeef" {
		t.Fatalf("got %q, want deadbeef", got)
	}
}

func TestResolveChecksum_NoDigestNoChecksumsFile(t *testing.T) {
	rel := release{TagName: "v1", Assets: []releaseAsset{{Name: "cli-v1-linux-amd64"}}}
	asset := releaseAsset{Name: "cli-v1-linux-amd64"}
	_, err := resolveChecksum(context.Background(), Config{CurrentVersion: "v1"}, rel, asset)
	if err == nil || !strings.Contains(err.Error(), "no checksums.txt") {
		t.Fatalf("expected no-checksums error, got: %v", err)
	}
}

func TestResolveChecksum_ChecksumsFileMissingEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "deadbeef  some-other-asset\n")
	}))
	defer srv.Close()

	rel := release{
		TagName: "v1",
		Assets: []releaseAsset{
			{Name: "cli-v1-linux-amd64"},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL},
		},
	}
	asset := releaseAsset{Name: "cli-v1-linux-amd64"}
	_, err := resolveChecksum(context.Background(), Config{CurrentVersion: "v1"}, rel, asset)
	if err == nil || !strings.Contains(err.Error(), "no checksum entry for cli-v1-linux-amd64") {
		t.Fatalf("expected missing-entry error, got: %v", err)
	}
}

func TestParseChecksums(t *testing.T) {
	content := "  \nAAAA  fileA\nBBBB  fileB\n"
	got, err := parseChecksums(content, "fileB")
	if err != nil {
		t.Fatalf("parseChecksums: %v", err)
	}
	if got != "bbbb" {
		t.Fatalf("got %q, want bbbb", got)
	}
	if _, err := parseChecksums(content, "missing"); err == nil {
		t.Fatal("expected error for missing entry")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/repos/selfupgrade && go test ./... -run 'TestResolveChecksum|TestParseChecksums' 2>&1 | head`
Expected: FAIL — `undefined: resolveChecksum`, `undefined: parseChecksums`.

- [ ] **Step 3: Write the implementation**

Create `~/repos/selfupgrade/checksum.go`:

```go
package selfupgrade

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// resolveChecksum returns the expected lowercase hex sha256 for asset. It uses
// the asset's GitHub digest field when present, otherwise downloads and parses
// a "checksums.txt" release asset. It never returns "" without an error.
func resolveChecksum(ctx context.Context, cfg Config, rel release, asset releaseAsset) (string, error) {
	if d := strings.TrimSpace(asset.Digest); d != "" {
		return strings.ToLower(strings.TrimPrefix(d, "sha256:")), nil
	}

	var checksums releaseAsset
	found := false
	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			checksums = a
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("asset %s has no digest and release %s has no checksums.txt", asset.Name, rel.TagName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksums.BrowserDownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("build checksums request: %w", err)
	}
	req.Header.Set("User-Agent", userAgentPrefix+cfg.CurrentVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download checksums.txt: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download checksums.txt: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read checksums.txt: %w", err)
	}
	return parseChecksums(string(body), asset.Name)
}

// parseChecksums finds the lowercase sha256 hex for name in sha256sum-style
// content ("<hex>  <filename>" per line).
func parseChecksums(content, name string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[len(fields)-1] == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s in checksums.txt", name)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/repos/selfupgrade && go test ./... -run 'TestResolveChecksum|TestParseChecksums' -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
cd ~/repos/selfupgrade
git add checksum.go checksum_test.go
git commit -m "feat: checksum resolution with checksums.txt fallback"
```

---

## Task 6: `selfupgrade.go` — `Run` orchestration

**Files:**
- Modify: `~/repos/selfupgrade/selfupgrade.go` (replace the Task 3 stub with the full implementation)
- Create: `~/repos/selfupgrade/helpers_test.go`
- Create: `~/repos/selfupgrade/selfupgrade_test.go`

- [ ] **Step 1: Write the shared test helper**

Create `~/repos/selfupgrade/helpers_test.go`:

```go
package selfupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeRelease serves a release JSON whose linux/amd64 asset is named
// "{repo}-{tag}-linux-amd64" and serves payload. If withDigest is true the
// asset carries a sha256 digest; otherwise a checksums.txt asset is added.
func fakeRelease(t *testing.T, repo, tag string, payload []byte, withDigest bool) string {
	t.Helper()
	sum := sha256.Sum256(payload)
	hexsum := hex.EncodeToString(sum[:])
	assetName := fmt.Sprintf("%s-%s-linux-amd64", repo, tag)

	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	t.Cleanup(assetSrv.Close)

	checksumsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hexsum, assetName)
	}))
	t.Cleanup(checksumsSrv.Close)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if withDigest {
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":%q,"size":%d,"digest":%q}]}`,
				tag, assetName, assetSrv.URL+"/asset", len(payload), "sha256:"+hexsum)
		} else {
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":%q,"size":%d},{"name":"checksums.txt","browser_download_url":%q}]}`,
				tag, assetName, assetSrv.URL+"/asset", len(payload), checksumsSrv.URL)
		}
	}))
	t.Cleanup(apiSrv.Close)
	return apiSrv.URL
}
```

- [ ] **Step 2: Write the failing `Run` tests**

Create `~/repos/selfupgrade/selfupgrade_test.go`:

```go
package selfupgrade

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func baseCfg(repo, version, apiURL, exe string, out *bytes.Buffer) Config {
	return Config{
		Owner:          "krzysztofciepka",
		Repo:           repo,
		CurrentVersion: version,
		ExePath:        exe,
		Out:            out,
		APIBaseURL:     apiURL,
	}
}

func TestRun_MissingRequiredFields(t *testing.T) {
	err := Run(context.Background(), Config{Repo: "cli"})
	if err == nil || !strings.Contains(err.Error(), "missing required Config fields") {
		t.Fatalf("expected validation error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Owner") || !strings.Contains(err.Error(), "ExePath") {
		t.Fatalf("error should list missing fields, got: %v", err)
	}
}

func TestRun_UnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		t.Skip("on linux/amd64; unsupported-platform path not reachable here")
	}
	err := Run(context.Background(), Config{Owner: "o", Repo: "r", CurrentVersion: "v1", ExePath: "/x"})
	if err == nil || !strings.Contains(err.Error(), "self-upgrade is not supported") {
		t.Fatalf("expected unsupported-platform error, got: %v", err)
	}
}

func TestRun_AlreadyLatest(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("linux/amd64 only")
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, `{"tag_name":"v0.0.42","assets":[]}`)
	}))
	defer srv.Close()

	exe, _ := os.Executable()
	var out bytes.Buffer
	if err := Run(context.Background(), baseCfg("cli", "v0.0.42", srv.URL, exe, &out)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if requests != 1 {
		t.Fatalf("API requests = %d, want 1", requests)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatalf("output should mention up-to-date, got: %q", out.String())
	}
}

func TestRun_FullPath_DigestVerification(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("linux/amd64 only")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "cli")
	if err := os.WriteFile(target, []byte("v0.0.20-bytes"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	payload := []byte("v0.0.21-bytes")
	apiURL := fakeRelease(t, "cli", "v0.0.21", payload, true)

	var out bytes.Buffer
	if err := Run(context.Background(), baseCfg("cli", "v0.0.20", apiURL, target, &out)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(payload) {
		t.Fatalf("target = %q, want %q", got, payload)
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf(".old should be removed")
	}
	if !strings.Contains(out.String(), "v0.0.20") || !strings.Contains(out.String(), "v0.0.21") {
		t.Fatalf("output should mention both versions, got: %q", out.String())
	}
}

func TestRun_FullPath_ChecksumsTxtFallback(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("linux/amd64 only")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "cli")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	payload := []byte("new-via-checksums")
	apiURL := fakeRelease(t, "cli", "v0.0.99", payload, false)

	var out bytes.Buffer
	if err := Run(context.Background(), baseCfg("cli", "dev", apiURL, target, &out)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(payload) {
		t.Fatalf("target not replaced via checksums.txt: %q", got)
	}
}

func TestRun_PermissionHintOnReadOnlyDir(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("linux/amd64 only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "cli")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	apiURL := fakeRelease(t, "cli", "v0.0.99", []byte("new"), true)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	var out bytes.Buffer
	err := Run(context.Background(), baseCfg("cli", "v0.0.20", apiURL, target, &out))
	if err == nil || !strings.Contains(err.Error(), "re-run with sudo") {
		t.Fatalf("expected permission hint, got: %v", err)
	}
}

func TestConfig_AssetNameDefaultAndTemplate(t *testing.T) {
	def := Config{Repo: "cli"}.assetName("v1.2.3")
	want := fmt.Sprintf("cli-v1.2.3-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if def != want {
		t.Fatalf("default assetName = %q, want %q", def, want)
	}
	custom := Config{Repo: "cli", AssetName: "{repo}_{tag}.bin"}.assetName("v9")
	if custom != "cli_v9.bin" {
		t.Fatalf("templated assetName = %q, want cli_v9.bin", custom)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd ~/repos/selfupgrade && go test ./... -run TestRun 2>&1 | head`
Expected: FAIL — `Run` redeclared / undefined `assetName` / stub `selfupgrade.go` lacks `Run`.

- [ ] **Step 4: Replace `selfupgrade.go` with the full implementation**

Overwrite `~/repos/selfupgrade/selfupgrade.go`:

```go
// Package selfupgrade replaces the running binary with the latest GitHub
// release asset for a given owner/repo. linux/amd64 only.
package selfupgrade

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL = "https://api.github.com"
	userAgentPrefix   = "selfupgrade/"
)

// Config controls a self-upgrade run.
type Config struct {
	Owner          string    // GitHub repo owner (required)
	Repo           string    // GitHub repo name (required)
	CurrentVersion string    // e.g. "v0.1.0" or "dev" (required)
	ExePath        string    // path to the running binary (required)
	Out            io.Writer // progress messages; nil -> io.Discard
	AssetName      string    // optional name template; default "{repo}-{tag}-{goos}-{goarch}"
	APIBaseURL     string    // optional; default "https://api.github.com"
}

func (c Config) validate() error {
	var missing []string
	if c.Owner == "" {
		missing = append(missing, "Owner")
	}
	if c.Repo == "" {
		missing = append(missing, "Repo")
	}
	if c.CurrentVersion == "" {
		missing = append(missing, "CurrentVersion")
	}
	if c.ExePath == "" {
		missing = append(missing, "ExePath")
	}
	if len(missing) > 0 {
		return fmt.Errorf("selfupgrade: missing required Config fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.Out == nil {
		c.Out = io.Discard
	}
	if c.APIBaseURL == "" {
		c.APIBaseURL = defaultAPIBaseURL
	}
}

// assetName resolves the expected release asset filename for tag.
func (c Config) assetName(tag string) string {
	tmpl := c.AssetName
	if tmpl == "" {
		tmpl = "{repo}-{tag}-{goos}-{goarch}"
		if runtime.GOOS == "windows" {
			tmpl += ".exe"
		}
	}
	return strings.NewReplacer(
		"{repo}", c.Repo,
		"{tag}", tag,
		"{goos}", runtime.GOOS,
		"{goarch}", runtime.GOARCH,
	).Replace(tmpl)
}

// Run fetches the latest GitHub release for cfg.Owner/cfg.Repo and, if newer
// than cfg.CurrentVersion, downloads, verifies, and atomically replaces the
// running binary. Only linux/amd64 is supported.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	cfg.applyDefaults()

	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("self-upgrade is not supported on %s/%s — please reinstall manually", runtime.GOOS, runtime.GOARCH)
	}

	target, err := filepath.EvalSymlinks(cfg.ExePath)
	if err != nil {
		// EvalSymlinks fails for non-existent paths; fall back to the raw
		// path so synthetic test paths still work.
		target = cfg.ExePath
	}
	dir := filepath.Dir(target)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rel, err := fetchLatestRelease(ctx, cfg)
	if err != nil {
		return err
	}

	if cfg.CurrentVersion != "dev" && rel.TagName == cfg.CurrentVersion {
		fmt.Fprintf(cfg.Out, "%s is up to date (%s).\n", cfg.Repo, cfg.CurrentVersion)
		return nil
	}

	asset, err := pickAsset(rel, cfg.assetName(rel.TagName))
	if err != nil {
		return err
	}

	digest, err := resolveChecksum(ctx, cfg, rel, asset)
	if err != nil {
		return err
	}

	fmt.Fprintf(cfg.Out, "Current version: %s\n", cfg.CurrentVersion)
	fmt.Fprintf(cfg.Out, "Latest version:  %s\n", rel.TagName)
	fmt.Fprintf(cfg.Out, "Downloading %s (%s)...\n", asset.Name, humanSize(asset.Size))

	tmpPath := filepath.Join(dir, fmt.Sprintf(".%s-upgrade-%d", cfg.Repo, os.Getpid()))
	if err := downloadAsset(ctx, cfg, asset.BrowserDownloadURL, tmpPath, digest); err != nil {
		if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
			return fmt.Errorf("cannot write to %s: %w — re-run with sudo or move %s to a user-owned path", dir, err, cfg.Repo)
		}
		return err
	}
	fmt.Fprintln(cfg.Out, "Verifying checksum... ok")

	fmt.Fprintf(cfg.Out, "Installing to %s... ", target)
	if err := installBinary(tmpPath, target); err != nil {
		fmt.Fprintln(cfg.Out, "failed")
		os.Remove(tmpPath)
		return err
	}
	fmt.Fprintln(cfg.Out, "ok")

	fmt.Fprintf(cfg.Out, "Upgraded %s → %s. Restart %s to use the new version.\n", cfg.CurrentVersion, rel.TagName, cfg.Repo)
	return nil
}
```

- [ ] **Step 5: Run the full test suite to verify it passes**

Run: `cd ~/repos/selfupgrade && go test ./... -v && go vet ./...`
Expected: PASS (all tests across all files), vet clean.

- [ ] **Step 6: Commit**

```bash
cd ~/repos/selfupgrade
git add selfupgrade.go selfupgrade_test.go helpers_test.go
git commit -m "feat: Run orchestration with validation and platform guard"
```

---

## Task 7: README, push, and release `selfupgrade` v0.1.0

**Files:**
- Create: `~/repos/selfupgrade/README.md`

- [ ] **Step 1: Write the README**

Create `~/repos/selfupgrade/README.md`:

```markdown
# selfupgrade

Replace the running Go binary with the latest GitHub release asset.

linux/amd64 only. Standard library only — no dependencies.

## Usage

```go
import "github.com/krzysztofciepka/selfupgrade"

exe, _ := os.Executable()
err := selfupgrade.Run(context.Background(), selfupgrade.Config{
    Owner:          "krzysztofciepka",
    Repo:           "cli",
    CurrentVersion: version, // "dev" always upgrades
    ExePath:        exe,
    Out:            os.Stderr,
})
```

`Config` fields: `Owner`, `Repo`, `CurrentVersion`, `ExePath` are required.
`Out` defaults to discard, `APIBaseURL` to `https://api.github.com`,
`AssetName` to `{repo}-{tag}-{goos}-{goarch}` (`+.exe` on Windows; placeholders
`{repo} {tag} {goos} {goarch}` are substituted).

## Checksum verification

The downloaded asset's sha256 is verified against the GitHub API per-asset
`digest` field; if absent, against a `checksums.txt` release asset
(`<hex>  <filename>` lines). An unverifiable download is never installed.
```

- [ ] **Step 2: Final verification before release**

Run: `cd ~/repos/selfupgrade && go build ./... && go test ./... && go vet ./...`
Expected: all pass, no output from build/vet.

- [ ] **Step 3: Commit, push, and release**

```bash
cd ~/repos/selfupgrade
git add README.md
git commit -m "docs: add README"
git push -u origin "$(git branch --show-current)"
gh release create v0.1.0 --title "selfupgrade v0.1.0" --generate-notes
```

- [ ] **Step 4: Confirm the module is resolvable**

Run: `GOPROXY=direct GOSUMDB=off go list -m -versions github.com/krzysztofciepka/selfupgrade@v0.1.0`
Expected: prints the module path (proves the tagged version is fetchable). If it lags, retry once after a few seconds.

---

## Task 8: Wire `selfupgrade` into `cli`

**Files:**
- Modify: `~/repos/cli-app/main.go`
- Modify: `~/repos/cli-app/go.mod`, `~/repos/cli-app/go.sum`

- [ ] **Step 1: Add the dependency**

```bash
cd ~/repos/cli-app
GOPROXY=direct GOSUMDB=off go get github.com/krzysztofciepka/selfupgrade@v0.1.0
```
Expected: `go.mod` gains `require github.com/krzysztofciepka/selfupgrade v0.1.0`; `go.sum` updated.

- [ ] **Step 2: Replace `main.go` with the flag-aware version**

The current `~/repos/cli-app/main.go` ends with the TUI launch. Overwrite the
whole file with this (the `selectionPath`, selection-file, and TUI logic are
preserved exactly; only `version`, `runFlags`, imports, and the top of `main`
are added):

```go
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
```

- [ ] **Step 3: Build to verify wiring compiles**

Run: `cd ~/repos/cli-app && go build ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Manual smoke check of `-version`**

Run: `cd ~/repos/cli-app && go run . -version`
Expected: prints `cli dev` and exits 0 (no TUI).

- [ ] **Step 5: Commit**

```bash
cd ~/repos/cli-app
git add main.go go.mod go.sum
git commit -m "feat: add -upgrade/-version via selfupgrade"
```

---

## Task 9: `cli` flag tests

**Files:**
- Create: `~/repos/cli-app/main_test.go`

- [ ] **Step 1: Write the failing tests**

Create `~/repos/cli-app/main_test.go`:

```go
package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunFlags_Version(t *testing.T) {
	var out, errb bytes.Buffer
	handled, code := runFlags([]string{"-version"}, &out, &errb, "v1.2.3",
		func() error { return errors.New("upgrade should not be called") })
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
	if strings.TrimSpace(out.String()) != "cli v1.2.3" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestRunFlags_UpgradeInvokesCallback(t *testing.T) {
	called := false
	var out, errb bytes.Buffer
	handled, code := runFlags([]string{"-upgrade"}, &out, &errb, "dev",
		func() error { called = true; return nil })
	if !handled || code != 0 || !called {
		t.Fatalf("handled=%v code=%d called=%v", handled, code, called)
	}
}

func TestRunFlags_UpgradeErrorExitsNonZero(t *testing.T) {
	var out, errb bytes.Buffer
	handled, code := runFlags([]string{"-upgrade"}, &out, &errb, "dev",
		func() error { return errors.New("boom") })
	if !handled || code != 1 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
	if !strings.Contains(errb.String(), "boom") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRunFlags_NoFlagsFallsThrough(t *testing.T) {
	var out, errb bytes.Buffer
	handled, _ := runFlags(nil, &out, &errb, "dev",
		func() error { return errors.New("must not run") })
	if handled {
		t.Fatal("expected handled=false so the TUI launches")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail then pass**

Run: `cd ~/repos/cli-app && go test ./... -run TestRunFlags -v`
Expected: PASS (4 tests) — `runFlags` already exists from Task 8, so these pass immediately. If any fail, fix `runFlags` before continuing.

- [ ] **Step 3: Run the full `cli` test suite**

Run: `cd ~/repos/cli-app && go test ./... && go vet ./...`
Expected: all pass, vet clean.

- [ ] **Step 4: Commit**

```bash
cd ~/repos/cli-app
git add main_test.go
git commit -m "test: cover runFlags version/upgrade dispatch"
```

---

## Task 10: Document `cli` upgrade and finalize

**Files:**
- Modify: `~/repos/cli-app/README.md`

- [ ] **Step 1: Read the current README**

Run: `cat ~/repos/cli-app/README.md`
Identify where usage/flags are documented.

- [ ] **Step 2: Add an Upgrading section**

Append to `~/repos/cli-app/README.md`:

```markdown
## Upgrading

`cli` can replace itself with the latest GitHub release (linux/amd64):

```bash
cli -upgrade    # download + verify + replace the running binary
cli -version    # print the installed version
```

Upgrade is powered by [selfupgrade](https://github.com/krzysztofciepka/selfupgrade).
Release builds embed the version via `-ldflags "-X main.version=<tag>"`; the
release process must continue to publish `cli-<tag>-linux-amd64` and
`checksums.txt` assets.
```

- [ ] **Step 3: Final verification**

Run: `cd ~/repos/cli-app && go build ./... && go test ./... && go vet ./...`
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
cd ~/repos/cli-app
git add README.md
git commit -m "docs: document -upgrade/-version"
```

> The final `cli` push and GitHub release are handled by the todo-task workflow
> (Steps 8–9) after this plan completes. When that release is built, the binary
> MUST be compiled with `-ldflags "-X main.version=<tag>"` so `cli -version`
> and the up-to-date check report correctly.

---

## Self-Review Notes

- **Spec coverage:** module setup (T1); install/rollback + injectable rename (T2); fetch + pickAsset + 1MiB cap + UA + Accept (T3); verified download + permission path + humanSize (T4); digest→checksums.txt auto-detect + parse (T5); Run orchestration, validation, platform guard, EvalSymlinks, 30s timeout, up-to-date short-circuit, asset templating, permission hint (T6); README + push + v0.1.0 release (T7); cli dependency + flags + wiring (T8); runFlags tests (T9); cli docs (T10). All spec sections mapped.
- **Type consistency:** `Config`, `release`, `releaseAsset`, `fetchLatestRelease(ctx, cfg)`, `pickAsset(rel, want string)`, `resolveChecksum(ctx, cfg, rel, asset)`, `downloadAsset(ctx, cfg, url, dst, expectedHexDigest)`, `installBinary(src, target)`, `renameImpl`, `humanSize`, `runFlags` — names/signatures identical across all tasks.
- **Ordering constraint:** `selfupgrade` must be pushed and tagged (T7) before `cli` `go get` (T8); the plan sequences this. `release.go` (T3) depends on the `Config`/`userAgentPrefix` stub introduced in the same task and replaced wholesale in T6.
- **No placeholders:** every code/test step contains complete, runnable content.
