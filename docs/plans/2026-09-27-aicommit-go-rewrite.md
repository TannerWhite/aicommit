# aicommit Go Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `aicommit`, a Go CLI that writes Conventional Commits messages with an AI model and splits unstaged changes into atomic commits.

**Architecture:** Small `internal/` packages with one job each (git, changes, rules, config, backend, prompt, plan, ui). The `app` package runs the single and split flows using a `Backend` interface, so every flow is testable with a fake model. The tool shells out to `git`.

**Tech Stack:** Go 1.23+, `github.com/spf13/cobra`, `github.com/BurntSushi/toml`, standard library `net/http`, goreleaser, GitHub Actions.

**Spec:** `docs/specs/2026-09-27-aicommit-go-rewrite-design.md`

## Global Constraints

- Module path: `github.com/TannerWhite/aicommit`; binary name: `aicommit`; `go 1.23` in `go.mod`.
- Only third-party dependencies: `github.com/spf13/cobra`, `github.com/BurntSushi/toml`. No LLM SDKs.
- Always shell out to `git`; never use go-git or libgit2.
- `command` backend runs an argv with no shell.
- Repo config may never set `backend`, `command`, `base_url`, `api_key_env`; doing so is an error (fail closed).
- API keys are never printed, logged, or included in `--verbose` output.
- Defaults: `max_file_bytes` 100000, `max_total_bytes` 400000, `timeout` 120s.
- Default excludes: `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`, `id_ed25519*`, `*.p12`, `*.pfx`.
- Exit codes: 0 success or nothing to commit; 1 error; 2 user quit/declined, interrupted, or no TTY without `--yes`.
- No em dashes in any file, prompt, or output; use `-`.
- Commit messages follow Conventional Commits (`type(scope): subject`, lowercase, imperative, no trailing period).
- Every package test must pass with `go test -race ./...` on linux, macos, windows.

## File Map

```
go.mod, go.sum
LICENSE                              MIT
Makefile                             build, test, lint, smoke
.gitignore
.goreleaser.yaml
.github/workflows/ci.yml
.github/workflows/release.yml
README.md, CONTRIBUTING.md
cmd/aicommit/main.go                 calls cli.Execute, exits with its code
internal/cli/root.go                 cobra root, flags, config and init subcommands
internal/cli/version.go              version vars set by ldflags
internal/git/git.go                  Runner: Run, RepoRoot, HasStaged, Add, RestoreStaged, Commit
internal/git/status.go               ParseStatusV2, ParseNameStatusZ
internal/changes/changes.go          Entry, Set, Build, Render
internal/changes/exclude.go          Excluded, IsBinary
internal/rules/rules.go              Resolve; default.md embedded
internal/rules/default.md
internal/config/config.go            Config, Load, sources, repo-key guard
internal/backend/backend.go          Backend interface, Clean
internal/backend/command.go          Command backend
internal/backend/openai.go           OpenAI backend
internal/backend/detect.go           New (selection + auto-detect)
internal/prompt/prompt.go            Single, Split; templates embedded
internal/prompt/single.tmpl, split.tmpl
internal/plan/plan.go                Plan, Parse, Validate
internal/ui/ui.go                    Prompter (choice + feedback input), IsTerminal
internal/app/app.go                  Run: mode selection, single flow, split flow, summary
internal/app/app_test.go             integration tests with real git + fake backend
```

Dependency direction: `cli` -> `app` -> (`ui`, `plan`, `prompt`, `backend`, `changes`, `rules`, `config`) ; `changes` -> `git`. Lower packages never import `app`, `cli`, or `ui`.

---

### Task 1: Scaffold, version, CI

**Files:**
- Create: `go.mod`, `LICENSE`, `.gitignore`, `Makefile`, `cmd/aicommit/main.go`, `internal/cli/version.go`, `internal/cli/root.go`, `internal/cli/root_test.go`, `.github/workflows/ci.yml`

**Interfaces:**
- Produces: `cli.Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int`; package vars `cli.Version`, `cli.Commit`, `cli.Date`.

- [ ] **Step 1: Init module and deps**

```bash
go mod init github.com/TannerWhite/aicommit
go mod edit -go=1.23
go get github.com/spf13/cobra@latest github.com/BurntSushi/toml@latest
```

- [ ] **Step 2: Write the failing test** `internal/cli/root_test.go`

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var out, errb bytes.Buffer
	code := Execute([]string{"--version"}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	if !strings.Contains(out.String(), "aicommit dev") {
		t.Fatalf("got %q", out.String())
	}
}
```

- [ ] **Step 3: Run it, expect FAIL** (`go test ./internal/cli/`: undefined Execute)

- [ ] **Step 4: Implement**

`internal/cli/version.go`:

```go
package cli

// Set by goreleaser via -ldflags "-X github.com/TannerWhite/aicommit/internal/cli.Version=...".
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
```

`internal/cli/root.go` (flags are filled in by Task 11):

```go
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newRoot(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "aicommit",
		Short:         "Write Conventional Commits messages with AI and split changes into atomic commits",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, args []string) error { return nil },
	}
	root.SetVersionTemplate("aicommit {{.Version}}\n")
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot(stdin, stdout, stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(stderr, "aicommit:", err)
		return 1
	}
	return 0
}
```

`cmd/aicommit/main.go`:

```go
package main

import (
	"os"

	"github.com/TannerWhite/aicommit/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
```

`Makefile`:

```make
.PHONY: build test lint smoke
build:
	go build -o bin/aicommit ./cmd/aicommit
test:
	go test -race ./...
lint:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
smoke: build
	cd "$$(mktemp -d)" && git init -q && echo hi > a.txt && $(CURDIR)/bin/aicommit --dry-run
```

`.gitignore`: `bin/` and `dist/`. `LICENSE`: standard MIT text, `Copyright (c) 2026 Tanner White`.

`.github/workflows/ci.yml`:

```yaml
name: ci
on:
  push: { branches: [main] }
  pull_request:
jobs:
  test:
    strategy:
      matrix: { os: [ubuntu-latest, macos-latest, windows-latest] }
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: git config --global user.email ci@example.com && git config --global user.name ci
      - run: go test -race ./...
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: make lint
```

- [ ] **Step 5: Run tests, expect PASS**; `go run ./cmd/aicommit --version` prints `aicommit dev (commit none, built unknown)`.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "chore: scaffold go module, cli entry point, and ci"
```

---

### Task 2: git wrapper and status parsing

**Files:**
- Create: `internal/git/git.go`, `internal/git/status.go`, `internal/git/status_test.go`, `internal/git/git_test.go`

**Interfaces:**
- Produces:
  - `type Runner struct{ Dir string }`
  - `func (r Runner) Run(stdin io.Reader, args ...string) (stdout string, err error)` - error wraps stderr and exit code
  - `func (r Runner) RepoRoot() (string, error)`
  - `func (r Runner) HasStaged() (bool, error)` - `git diff --cached --quiet` exit 1 means true
  - `func (r Runner) StagedPaths() ([]string, error)` - `git diff --cached --name-only -z`
  - `func (r Runner) Add(paths []string) error` - `git add -A -- paths...`
  - `func (r Runner) RestoreStaged(paths []string) error` - `git restore --staged -- paths...`; if repo has no commits, `git rm --cached -r -q --ignore-unmatch -- paths...`
  - `func (r Runner) RestoreAllStaged() error` - `git restore --staged :/` (or `git rm --cached -r -q :/` with no HEAD)
  - `func (r Runner) Commit(message string, edit bool) (sha string, output string, err error)` - writes message to a temp file, runs `git commit -F file` (plus `-e` when edit, with stdin/stdout/stderr attached to the terminal), returns `git rev-parse --short HEAD`
  - `type StatusEntry struct{ Code string; Path string; OrigPath string; Untracked bool; Unmerged bool }` (`Code` is the two-char XY, `??` for untracked)
  - `func ParseStatusV2(z string) ([]StatusEntry, error)`
  - `func ParseNameStatusZ(z string) ([]StatusEntry, error)` - parses `git diff --cached -z --name-status`

- [ ] **Step 1: Write failing tests** `internal/git/status_test.go`

```go
package git

import (
	"reflect"
	"testing"
)

func TestParseStatusV2(t *testing.T) {
	in := "1 .M N... 100644 100644 100644 aaa bbb dir/a b.go\x00" +
		"2 R. N... 100644 100644 100644 aaa bbb R100 new.go\x00old.go\x00" +
		"u UU N... 100644 100644 100644 100644 a b c conflict.go\x00" +
		"? héllo.txt\x00"
	got, err := ParseStatusV2(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []StatusEntry{
		{Code: ".M", Path: "dir/a b.go"},
		{Code: "R.", Path: "new.go", OrigPath: "old.go"},
		{Code: "UU", Path: "conflict.go", Unmerged: true},
		{Code: "??", Path: "héllo.txt", Untracked: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestParseStatusV2Ignored(t *testing.T) {
	got, err := ParseStatusV2("! build/\x00")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestParseNameStatusZ(t *testing.T) {
	in := "M\x00a.go\x00R087\x00old.go\x00new.go\x00D\x00gone.go\x00"
	got, err := ParseNameStatusZ(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []StatusEntry{
		{Code: "M", Path: "a.go"},
		{Code: "R", Path: "new.go", OrigPath: "old.go"},
		{Code: "D", Path: "gone.go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}
```

`internal/git/git_test.go` (real git):

```go
package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRepo(t *testing.T) Runner {
	t.Helper()
	r := Runner{Dir: t.TempDir()}
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		if _, err := r.Run(nil, a...); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestAddCommitRestore(t *testing.T) {
	r := newRepo(t)
	os.WriteFile(filepath.Join(r.Dir, "a.txt"), []byte("a\n"), 0o644)
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.HasStaged(); !ok {
		t.Fatal("expected staged")
	}
	if err := r.RestoreStaged([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.HasStaged(); ok {
		t.Fatal("expected nothing staged after restore on unborn branch")
	}
	r.Add([]string{"a.txt"})
	sha, _, err := r.Commit("feat: add a\n", false)
	if err != nil || sha == "" {
		t.Fatalf("commit: %v %q", err, sha)
	}
	log, _ := r.Run(nil, "log", "--format=%s")
	if strings.TrimSpace(log) != "feat: add a" {
		t.Fatalf("log %q", log)
	}
}
```

- [ ] **Step 2: Run, expect FAIL** (`go test ./internal/git/`)

- [ ] **Step 3: Implement** `internal/git/status.go`

```go
package git

import (
	"fmt"
	"strings"
)

// ParseStatusV2 parses `git status --porcelain=v2 -z --untracked-files=all`.
func ParseStatusV2(z string) ([]StatusEntry, error) {
	var out []StatusEntry
	recs := strings.Split(z, "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1':
			f := strings.SplitN(rec, " ", 9)
			if len(f) != 9 {
				return nil, fmt.Errorf("bad status record %q", rec)
			}
			out = append(out, StatusEntry{Code: f[1], Path: f[8]})
		case '2':
			f := strings.SplitN(rec, " ", 10)
			if len(f) != 10 || i+1 >= len(recs) {
				return nil, fmt.Errorf("bad rename record %q", rec)
			}
			i++
			out = append(out, StatusEntry{Code: f[1], Path: f[9], OrigPath: recs[i]})
		case 'u':
			f := strings.SplitN(rec, " ", 11)
			if len(f) != 11 {
				return nil, fmt.Errorf("bad unmerged record %q", rec)
			}
			out = append(out, StatusEntry{Code: f[1], Path: f[10], Unmerged: true})
		case '?':
			out = append(out, StatusEntry{Code: "??", Path: rec[2:], Untracked: true})
		case '!', '#':
		default:
			return nil, fmt.Errorf("unknown status record %q", rec)
		}
	}
	return out, nil
}

// ParseNameStatusZ parses `git diff --cached -z --name-status`.
func ParseNameStatusZ(z string) ([]StatusEntry, error) {
	var out []StatusEntry
	f := strings.Split(strings.TrimSuffix(z, "\x00"), "\x00")
	for i := 0; i < len(f); i++ {
		if f[i] == "" {
			continue
		}
		code := f[i][:1]
		if code == "R" || code == "C" {
			if i+2 >= len(f) {
				return nil, fmt.Errorf("truncated rename in name-status")
			}
			out = append(out, StatusEntry{Code: code, OrigPath: f[i+1], Path: f[i+2]})
			i += 2
			continue
		}
		if i+1 >= len(f) {
			return nil, fmt.Errorf("truncated name-status")
		}
		out = append(out, StatusEntry{Code: code, Path: f[i+1]})
		i++
	}
	return out, nil
}
```

`internal/git/git.go`

```go
package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type StatusEntry struct {
	Code      string
	Path      string
	OrigPath  string
	Untracked bool
	Unmerged  bool
}

type Runner struct{ Dir string }

func (r Runner) cmd(args ...string) *exec.Cmd {
	c := exec.Command("git", args...)
	c.Dir = r.Dir
	return c
}

func (r Runner) Run(stdin io.Reader, args ...string) (string, error) {
	c := r.cmd(args...)
	var out, errb bytes.Buffer
	c.Stdin, c.Stdout, c.Stderr = stdin, &out, &errb
	if err := c.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func (r Runner) RepoRoot() (string, error) {
	out, err := r.Run(nil, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

func (r Runner) hasHead() bool {
	_, err := r.Run(nil, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

func (r Runner) HasStaged() (bool, error) {
	err := r.cmd("diff", "--cached", "--quiet").Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

func (r Runner) StagedPaths() ([]string, error) {
	out, err := r.Run(nil, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func (r Runner) Add(paths []string) error {
	_, err := r.Run(nil, append([]string{"add", "-A", "--"}, paths...)...)
	return err
}

func (r Runner) RestoreStaged(paths []string) error {
	if !r.hasHead() {
		_, err := r.Run(nil, append([]string{"rm", "--cached", "-r", "-q", "--ignore-unmatch", "--"}, paths...)...)
		return err
	}
	_, err := r.Run(nil, append([]string{"restore", "--staged", "--"}, paths...)...)
	return err
}

func (r Runner) RestoreAllStaged() error {
	if !r.hasHead() {
		_, err := r.Run(nil, "rm", "--cached", "-r", "-q", "--ignore-unmatch", ":/")
		return err
	}
	_, err := r.Run(nil, "restore", "--staged", ":/")
	return err
}

// Commit commits the index with message. When edit is true git opens $EDITOR
// attached to the process terminal.
func (r Runner) Commit(message string, edit bool) (string, string, error) {
	f, err := os.CreateTemp("", "aicommit-msg-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(message); err != nil {
		f.Close()
		return "", "", err
	}
	f.Close()
	args := []string{"commit", "-F", f.Name()}
	if edit {
		args = append(args, "-e")
	}
	c := r.cmd(args...)
	var buf bytes.Buffer
	if edit {
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	} else {
		c.Stdout, c.Stderr = &buf, &buf
	}
	if err := c.Run(); err != nil {
		return "", buf.String(), fmt.Errorf("git commit failed: %w", err)
	}
	sha, err := r.Run(nil, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(sha), buf.String(), err
}
```

- [ ] **Step 4: Run, expect PASS** (`go test -race ./internal/git/`)

- [ ] **Step 5: Commit**

```bash
git add internal/git && git commit -m "feat(git): add git runner and porcelain v2 status parsing"
```

### Task 3: Change set (exclusions, binary detection, size caps)

**Files:**
- Create: `internal/changes/exclude.go`, `internal/changes/changes.go`, `internal/changes/changes_test.go`

**Interfaces:**
- Consumes: `git.Runner`, `git.StatusEntry` (Task 2)
- Produces:
  - `var DefaultExcludes = []string{".env", ".env.*", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "*.p12", "*.pfx"}`
  - `func Excluded(path string, patterns []string) bool` - matches each pattern against the full slash path and the base name with `path.Match`
  - `func IsBinary(b []byte) bool` - NUL byte in the first 8192 bytes
  - `type Options struct{ Staged bool; MaxFileBytes, MaxTotalBytes int; Exclude []string }`
  - `type Entry struct{ Path, OrigPath, Status string; Untracked, Excluded, Binary bool; Body string }`
  - `type Set struct{ Entries []Entry }`
  - `func Build(r git.Runner, st []git.StatusEntry, o Options) (Set, error)`
  - `func (s Set) Paths() []string` - every `Path` in order
  - `func (s Set) Find(path string) (Entry, bool)`
  - `func (s Set) Render() string`

Body rules: `Staged` true -> `git diff --cached -M -- <orig> <path>`. `Staged` false -> tracked files get `git diff --cached -M -- ...` followed by `git diff -- ...` (covers `--all` with a partially staged index, and works with no HEAD); untracked files get their file content. A diff containing `Binary files ` marks the entry binary. Excluded or binary entries get an empty `Body`.

- [ ] **Step 1: Write failing tests** `internal/changes/changes_test.go`

```go
package changes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TannerWhite/aicommit/internal/git"
)

func repo(t *testing.T) git.Runner {
	t.Helper()
	r := git.Runner{Dir: t.TempDir()}
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := r.Run(nil, a...); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func write(t *testing.T, r git.Runner, name, body string) {
	t.Helper()
	p := filepath.Join(r.Dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func status(t *testing.T, r git.Runner) []git.StatusEntry {
	out, err := r.Run(nil, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		t.Fatal(err)
	}
	st, err := git.ParseStatusV2(out)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func opts() Options {
	return Options{MaxFileBytes: 100000, MaxTotalBytes: 400000, Exclude: DefaultExcludes}
}

func TestExcluded(t *testing.T) {
	cases := map[string]bool{".env": true, "app/.env.local": true, "certs/a.pem": true, "id_rsa.pub": true, "main.go": false, "env.go": false}
	for p, want := range cases {
		if got := Excluded(p, DefaultExcludes); got != want {
			t.Errorf("%s: got %v", p, got)
		}
	}
}

func TestBuildUntrackedSecretsBinaryAndSpaces(t *testing.T) {
	r := repo(t)
	write(t, r, "dir/a b.txt", "hello\n")
	write(t, r, ".env", "TOKEN=secret\n")
	write(t, r, "img.bin", "\x00\x01\x02")
	set, err := Build(r, status(t, r), opts())
	if err != nil {
		t.Fatal(err)
	}
	out := set.Render()
	if strings.Contains(out, "secret") {
		t.Fatal("secret leaked into render")
	}
	for _, want := range []string{"=== FILE: dir/a b.txt", "hello", "=== FILE: .env", "(contents excluded)", "(binary file, contents omitted)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestBuildRenameAndTruncation(t *testing.T) {
	r := repo(t)
	write(t, r, "old.txt", strings.Repeat("line\n", 50))
	r.Add([]string{"."})
	r.Commit("chore: init\n", false)
	r.Run(nil, "mv", "old.txt", "new.txt")
	write(t, r, "big.txt", strings.Repeat("x", 500))
	o := opts()
	o.MaxFileBytes = 100
	set, err := Build(r, status(t, r), o)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := set.Find("new.txt")
	if !ok || e.OrigPath != "old.txt" {
		t.Fatalf("rename not tracked: %+v", set.Entries)
	}
	b, _ := set.Find("big.txt")
	if !strings.Contains(b.Body, "[truncated: 400 bytes omitted]") {
		t.Fatalf("not truncated: %q", b.Body)
	}
}

func TestBuildTotalCap(t *testing.T) {
	r := repo(t)
	write(t, r, "a.txt", strings.Repeat("a", 300))
	write(t, r, "b.txt", strings.Repeat("b", 50))
	o := opts()
	o.MaxTotalBytes = 200
	set, _ := Build(r, status(t, r), o)
	a, _ := set.Find("a.txt")
	if !strings.HasPrefix(a.Body, "[omitted: 300 bytes, over total size limit]") {
		t.Fatalf("largest entry not summarized: %q", a.Body)
	}
	b, _ := set.Find("b.txt")
	if !strings.Contains(b.Body, "bbbb") {
		t.Fatal("small entry should be kept")
	}
}
```

- [ ] **Step 2: Run, expect FAIL** (`go test ./internal/changes/`)

- [ ] **Step 3: Implement** `internal/changes/exclude.go`

```go
package changes

import (
	"bytes"
	"path"
)

var DefaultExcludes = []string{".env", ".env.*", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "*.p12", "*.pfx"}

func Excluded(p string, patterns []string) bool {
	base := path.Base(p)
	for _, pat := range patterns {
		if ok, _ := path.Match(pat, p); ok {
			return true
		}
		if ok, _ := path.Match(pat, base); ok {
			return true
		}
	}
	return false
}

func IsBinary(b []byte) bool {
	if len(b) > 8192 {
		b = b[:8192]
	}
	return bytes.IndexByte(b, 0) >= 0
}
```

`internal/changes/changes.go`

```go
package changes

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TannerWhite/aicommit/internal/git"
)

type Options struct {
	Staged        bool
	MaxFileBytes  int
	MaxTotalBytes int
	Exclude       []string
}

type Entry struct {
	Path, OrigPath, Status string
	Untracked, Excluded    bool
	Binary                 bool
	Body                   string
}

type Set struct{ Entries []Entry }

func Build(r git.Runner, st []git.StatusEntry, o Options) (Set, error) {
	var s Set
	for _, se := range st {
		e := Entry{Path: se.Path, OrigPath: se.OrigPath, Status: se.Code, Untracked: se.Untracked}
		e.Excluded = Excluded(se.Path, o.Exclude)
		if !e.Excluded {
			body, bin, err := body(r, se, o.Staged)
			if err != nil {
				return Set{}, err
			}
			e.Binary = bin
			if !bin {
				e.Body = truncate(body, o.MaxFileBytes)
			}
		}
		s.Entries = append(s.Entries, e)
	}
	capTotal(&s, o.MaxTotalBytes)
	return s, nil
}

func body(r git.Runner, se git.StatusEntry, staged bool) (string, bool, error) {
	if se.Untracked {
		b, err := os.ReadFile(filepath.Join(r.Dir, filepath.FromSlash(se.Path)))
		if err != nil {
			return "", false, err
		}
		return string(b), IsBinary(b), nil
	}
	paths := []string{se.Path}
	if se.OrigPath != "" {
		paths = []string{se.OrigPath, se.Path}
	}
	out, err := r.Run(nil, append([]string{"diff", "--cached", "-M", "--"}, paths...)...)
	if err != nil {
		return "", false, err
	}
	if !staged {
		wt, err := r.Run(nil, append([]string{"diff", "--"}, paths...)...)
		if err != nil {
			return "", false, err
		}
		out += wt
	}
	return out, strings.Contains(out, "Binary files "), nil
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n[truncated: %d bytes omitted]\n", len(s)-max)
}

func capTotal(s *Set, max int) {
	total := 0
	for _, e := range s.Entries {
		total += len(e.Body)
	}
	idx := make([]int, len(s.Entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return len(s.Entries[idx[a]].Body) > len(s.Entries[idx[b]].Body) })
	for _, i := range idx {
		if max <= 0 || total <= max {
			return
		}
		e := &s.Entries[i]
		n := len(e.Body)
		e.Body = fmt.Sprintf("[omitted: %d bytes, over total size limit]\n", n)
		total += len(e.Body) - n
	}
}

func (s Set) Paths() []string {
	p := make([]string, len(s.Entries))
	for i, e := range s.Entries {
		p[i] = e.Path
	}
	return p
}

func (s Set) Find(p string) (Entry, bool) {
	for _, e := range s.Entries {
		if e.Path == p {
			return e, true
		}
	}
	return Entry{}, false
}

func (s Set) Render() string {
	var b strings.Builder
	for _, e := range s.Entries {
		fmt.Fprintf(&b, "\n=== FILE: %s (%s)", e.Path, describe(e))
		if e.OrigPath != "" {
			fmt.Fprintf(&b, " renamed from %s", e.OrigPath)
		}
		b.WriteString(" ===\n")
		switch {
		case e.Excluded:
			b.WriteString("(contents excluded)\n")
		case e.Binary:
			b.WriteString("(binary file, contents omitted)\n")
		default:
			b.WriteString(e.Body)
		}
	}
	return b.String()
}

func describe(e Entry) string {
	if e.Untracked {
		return "new file"
	}
	switch {
	case strings.Contains(e.Status, "D"):
		return "deleted"
	case strings.Contains(e.Status, "R"):
		return "renamed"
	case strings.Contains(e.Status, "A"):
		return "added"
	}
	return "modified"
}
```

Note: the spec's total-cap summary uses a `--stat` line; this plan uses a byte-count marker instead because it works uniformly for untracked files. Record this in the spec's Size limits section during this task.

- [ ] **Step 4: Run, expect PASS**; update the spec Size limits bullet to say "replaced with an `[omitted: N bytes, over total size limit]` marker".

- [ ] **Step 5: Commit**

```bash
git add internal/changes docs/specs && git commit -m "feat(changes): build change set with exclusions and size caps"
```

---

### Task 4: Rules resolution

**Files:**
- Create: `internal/rules/default.md`, `internal/rules/rules.go`, `internal/rules/rules_test.go`

**Interfaces:**
- Produces:
  - `type Sources struct{ Flag, RepoKey, UserKey, RepoRoot, UserDir string }` - `*Key` values are absolute paths already resolved by config (empty if unset)
  - `func Resolve(s Sources) (text string, source string, err error)` - `source` is a file path or `"built-in"`
  - `func Default() string`

Lookup order: `Flag` (must exist) -> `RepoKey` (must exist) -> `<RepoRoot>/.aicommit/rules.md` (if present) -> `UserKey` (must exist) -> `<UserDir>/rules.md` (if present) -> built-in.

- [ ] **Step 1: Write failing tests** `internal/rules/rules_test.go`

```go
package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func file(t *testing.T, p, body string) string {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(body), 0o644)
	return p
}

func TestResolveOrder(t *testing.T) {
	repo, user := t.TempDir(), t.TempDir()
	s := Sources{RepoRoot: repo, UserDir: user}

	if text, src, _ := Resolve(s); src != "built-in" || !strings.Contains(text, "Conventional Commits") {
		t.Fatalf("expected built-in, got %s", src)
	}
	file(t, filepath.Join(user, "rules.md"), "user")
	if text, _, _ := Resolve(s); text != "user" {
		t.Fatal("user rules.md should win over built-in")
	}
	file(t, filepath.Join(repo, ".aicommit", "rules.md"), "repo")
	if text, _, _ := Resolve(s); text != "repo" {
		t.Fatal("repo rules.md should win over user")
	}
	s.Flag = file(t, filepath.Join(user, "flag.md"), "flag")
	if text, _, _ := Resolve(s); text != "flag" {
		t.Fatal("flag should win")
	}
}

func TestExplicitMissingIsError(t *testing.T) {
	_, _, err := Resolve(Sources{UserKey: "/nope/rules.md", RepoRoot: t.TempDir(), UserDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing explicit rules file")
	}
}

func TestDefaultHasNoEmDash(t *testing.T) {
	if strings.Contains(Default(), "\u2014") {
		t.Fatal("em dash in default rules")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement** `internal/rules/default.md` (generic, derived from the old script's RULES block without Flagship specifics):

```markdown
Follow Conventional Commits (https://conventionalcommits.org/en/v1.0.0).

FORMAT:
  type(scope): short imperative description

  optional body - explain what and why, not how (wrap at 72 chars)

  optional footer (BREAKING CHANGE, Refs, Co-authored-by)

TYPES:
  feat      new feature
  fix       bug fix
  refactor  restructure, no behavior change
  perf      performance improvement
  test      add or update tests
  docs      documentation only
  style     formatting, no logic change
  build     build system or dependencies
  chore     tooling and maintenance
  ci        CI/CD changes
  revert    reverts a previous commit

SCOPE: a short noun for the affected module or area. Omit it when the
change is truly global.

DO:
  - lowercase imperative mood: "add", "fix", "remove"
  - keep the subject line under 72 characters, no trailing period
  - separate subject and body with a blank line
  - one logical change per commit
  - mark breaking changes with "!" after type/scope or a BREAKING CHANGE footer

DON'T:
  - vague subjects like "update files" or "fix stuff"
  - past tense ("fixed", "added")
  - ALL CAPS types
```

`internal/rules/rules.go`

```go
package rules

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed default.md
var builtin string

func Default() string { return builtin }

type Sources struct{ Flag, RepoKey, UserKey, RepoRoot, UserDir string }

func Resolve(s Sources) (string, string, error) {
	type cand struct {
		path     string
		explicit bool
	}
	var cands []cand
	add := func(p string, explicit bool) {
		if p != "" {
			cands = append(cands, cand{p, explicit})
		}
	}
	add(s.Flag, true)
	add(s.RepoKey, true)
	if s.RepoRoot != "" {
		add(filepath.Join(s.RepoRoot, ".aicommit", "rules.md"), false)
	}
	add(s.UserKey, true)
	if s.UserDir != "" {
		add(filepath.Join(s.UserDir, "rules.md"), false)
	}
	for _, c := range cands {
		b, err := os.ReadFile(c.path)
		if err == nil {
			return string(b), c.path, nil
		}
		if c.explicit || !errors.Is(err, fs.ErrNotExist) {
			return "", "", fmt.Errorf("rules file %s: %w", c.path, err)
		}
	}
	return builtin, "built-in", nil
}
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add internal/rules && git commit -m "feat(rules): add built-in rules and override lookup"
```

---

### Task 5: Configuration loading

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `changes.DefaultExcludes` (Task 3)
- Produces:
  - `type Overrides struct{ Backend, Model, Rules string; Exclude []string }` - from CLI flags
  - `type Config struct { Backend string; Command []string; BaseURL, APIKeyEnv, Model string; RulesFlag, RepoRules, UserRules string; MaxFileBytes, MaxTotalBytes int; Timeout time.Duration; Exclude []string; Sources map[string]string; Warnings []string }`
  - `func UserDir(getenv func(string) string) (string, error)` - `$XDG_CONFIG_HOME/aicommit` if set; on linux/darwin `~/.config/aicommit`; else `os.UserConfigDir()/aicommit`
  - `func Load(userDir, repoRoot string, getenv func(string) string, ov Overrides) (Config, error)`
  - `func (c Config) Describe() string` - one `key = value  (source)` line per setting, never printing key values

Precedence: flags > env (`AICOMMIT_BACKEND`, `AICOMMIT_MODEL`, `AICOMMIT_BASE_URL`, `AICOMMIT_API_KEY_ENV`, `AICOMMIT_RULES`, `AICOMMIT_TIMEOUT`) > repo file > user file > defaults. `exclude` is additive: defaults + user + repo + flags. Relative `rules` paths resolve against the config file's directory's parent for repo (the repo root) and the user config dir for user. Source labels: `flag`, `env`, `repo`, `user`, `default`.

- [ ] **Step 1: Write failing tests** `internal/config/config_test.go`

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func write(t *testing.T, p, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(body), 0o644)
}

func TestDefaults(t *testing.T) {
	c, err := Load(t.TempDir(), t.TempDir(), env(nil), Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxFileBytes != 100000 || c.MaxTotalBytes != 400000 || c.Timeout != 120*time.Second {
		t.Fatalf("bad defaults %+v", c)
	}
	if c.Sources["timeout"] != "default" || len(c.Exclude) != 8 {
		t.Fatalf("sources %+v excl %v", c.Sources, c.Exclude)
	}
}

func TestPrecedence(t *testing.T) {
	user, repo := t.TempDir(), t.TempDir()
	write(t, filepath.Join(user, "config.toml"), "backend = \"openai\"\nmodel = \"u\"\ntimeout = \"30s\"\nrules = \"mine.md\"\n")
	write(t, filepath.Join(repo, ".aicommit", "config.toml"), "model = \"r\"\nexclude = [\"*.sql\"]\nrules = \"docs/rules.md\"\n")
	c, err := Load(user, repo, env(map[string]string{"AICOMMIT_TIMEOUT": "10s"}), Overrides{Exclude: []string{"*.lock"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Backend != "openai" || c.Sources["backend"] != "user" {
		t.Fatal("backend should come from user")
	}
	if c.Model != "r" || c.Sources["model"] != "repo" {
		t.Fatal("repo model should beat user")
	}
	if c.Timeout != 10*time.Second || c.Sources["timeout"] != "env" {
		t.Fatal("env should beat files")
	}
	if c.UserRules != filepath.Join(user, "mine.md") || c.RepoRules != filepath.Join(repo, "docs", "rules.md") {
		t.Fatalf("rules paths %q %q", c.UserRules, c.RepoRules)
	}
	joined := strings.Join(c.Exclude, ",")
	if !strings.Contains(joined, "*.sql") || !strings.Contains(joined, "*.lock") || !strings.Contains(joined, ".env") {
		t.Fatalf("exclude %v", c.Exclude)
	}
	c2, _ := Load(user, repo, env(nil), Overrides{Model: "f"})
	if c2.Model != "f" || c2.Sources["model"] != "flag" {
		t.Fatal("flag should win")
	}
}

func TestRepoCannotSetDangerousKeys(t *testing.T) {
	for _, key := range []string{`backend = "command"`, `command = ["sh", "-c", "evil"]`, `base_url = "http://evil"`, `api_key_env = "X"`} {
		repo := t.TempDir()
		write(t, filepath.Join(repo, ".aicommit", "config.toml"), key+"\n")
		_, err := Load(t.TempDir(), repo, env(nil), Overrides{})
		if err == nil || !strings.Contains(err.Error(), "not allowed in repo config") {
			t.Fatalf("%s: expected fail-closed error, got %v", key, err)
		}
	}
}

func TestUnknownKeyWarns(t *testing.T) {
	user := t.TempDir()
	write(t, filepath.Join(user, "config.toml"), "modle = \"x\"\n")
	c, err := Load(user, t.TempDir(), env(nil), Overrides{})
	if err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "modle") {
		t.Fatalf("warnings %v err %v", c.Warnings, err)
	}
}

func TestUserDirXDG(t *testing.T) {
	d, _ := UserDir(env(map[string]string{"XDG_CONFIG_HOME": "/x"}))
	if d != filepath.Join("/x", "aicommit") {
		t.Fatalf("got %s", d)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement** `internal/config/config.go`

```go
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/TannerWhite/aicommit/internal/changes"
)

type Overrides struct {
	Backend, Model, Rules string
	Exclude               []string
}

type Config struct {
	Backend                         string
	Command                         []string
	BaseURL, APIKeyEnv, Model       string
	RulesFlag, RepoRules, UserRules string
	MaxFileBytes, MaxTotalBytes     int
	Timeout                         time.Duration
	Exclude                         []string
	Sources                         map[string]string
	Warnings                        []string
}

type file struct {
	Backend       *string  `toml:"backend"`
	Command       []string `toml:"command"`
	BaseURL       *string  `toml:"base_url"`
	APIKeyEnv     *string  `toml:"api_key_env"`
	Model         *string  `toml:"model"`
	Rules         *string  `toml:"rules"`
	MaxFileBytes  *int     `toml:"max_file_bytes"`
	MaxTotalBytes *int     `toml:"max_total_bytes"`
	Timeout       *string  `toml:"timeout"`
	Exclude       []string `toml:"exclude"`
}

var repoForbidden = []string{"backend", "command", "base_url", "api_key_env"}

func UserDir(getenv func(string) string) (string, error) {
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "aicommit"), nil
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, ".config", "aicommit"), nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "aicommit"), nil
}

func readFile(path string) (*file, toml.MetaData, error) {
	var f file
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, md, nil
	}
	if err != nil {
		return nil, md, fmt.Errorf("%s: %w", path, err)
	}
	return &f, md, nil
}

func Load(userDir, repoRoot string, getenv func(string) string, ov Overrides) (Config, error) {
	c := Config{
		MaxFileBytes:  100000,
		MaxTotalBytes: 400000,
		Timeout:       120 * time.Second,
		Exclude:       append([]string{}, changes.DefaultExcludes...),
		Sources: map[string]string{
			"max_file_bytes": "default", "max_total_bytes": "default", "timeout": "default", "exclude": "default",
		},
	}
	layers := []struct {
		name, path, base string
	}{
		{"user", filepath.Join(userDir, "config.toml"), userDir},
		{"repo", filepath.Join(repoRoot, ".aicommit", "config.toml"), repoRoot},
	}
	for _, l := range layers {
		if l.base == "" {
			continue
		}
		f, md, err := readFile(l.path)
		if err != nil {
			return Config{}, err
		}
		if f == nil {
			continue
		}
		for _, k := range md.Undecoded() {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s: unknown key %q", l.path, k.String()))
		}
		if l.name == "repo" {
			for _, k := range repoForbidden {
				if md.IsDefined(k) {
					return Config{}, fmt.Errorf("%s: %q is not allowed in repo config (it could run commands or send data elsewhere); set it in your user config instead", l.path, k)
				}
			}
		}
		if err := c.apply(f, l.name, l.base); err != nil {
			return Config{}, fmt.Errorf("%s: %w", l.path, err)
		}
	}
	envs := map[string]*string{
		"backend": &c.Backend, "model": &c.Model, "base_url": &c.BaseURL, "api_key_env": &c.APIKeyEnv, "rules": &c.RulesFlag,
	}
	for k, dst := range envs {
		if v := getenv("AICOMMIT_" + strings.ToUpper(k)); v != "" {
			*dst = v
			c.Sources[k] = "env"
		}
	}
	if v := getenv("AICOMMIT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("AICOMMIT_TIMEOUT: %w", err)
		}
		c.Timeout, c.Sources["timeout"] = d, "env"
	}
	set := func(k string, dst *string, v string) {
		if v != "" {
			*dst, c.Sources[k] = v, "flag"
		}
	}
	set("backend", &c.Backend, ov.Backend)
	set("model", &c.Model, ov.Model)
	set("rules", &c.RulesFlag, ov.Rules)
	if len(ov.Exclude) > 0 {
		c.Exclude = append(c.Exclude, ov.Exclude...)
		c.Sources["exclude"] = "flag"
	}
	return c, nil
}

func (c *Config) apply(f *file, src, base string) error {
	str := func(k string, dst *string, v *string) {
		if v != nil {
			*dst, c.Sources[k] = *v, src
		}
	}
	str("backend", &c.Backend, f.Backend)
	str("base_url", &c.BaseURL, f.BaseURL)
	str("api_key_env", &c.APIKeyEnv, f.APIKeyEnv)
	str("model", &c.Model, f.Model)
	if f.Command != nil {
		c.Command, c.Sources["command"] = f.Command, src
	}
	if f.Rules != nil {
		p := *f.Rules
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		if src == "repo" {
			c.RepoRules = p
		} else {
			c.UserRules = p
		}
		c.Sources["rules"] = src
	}
	if f.MaxFileBytes != nil {
		c.MaxFileBytes, c.Sources["max_file_bytes"] = *f.MaxFileBytes, src
	}
	if f.MaxTotalBytes != nil {
		c.MaxTotalBytes, c.Sources["max_total_bytes"] = *f.MaxTotalBytes, src
	}
	if f.Timeout != nil {
		d, err := time.ParseDuration(*f.Timeout)
		if err != nil {
			return fmt.Errorf("timeout: %w", err)
		}
		c.Timeout, c.Sources["timeout"] = d, src
	}
	if len(f.Exclude) > 0 {
		c.Exclude = append(c.Exclude, f.Exclude...)
		c.Sources["exclude"] = src
	}
	return nil
}

func (c Config) Describe() string {
	vals := map[string]string{
		"backend": c.Backend, "command": strings.Join(c.Command, " "), "base_url": c.BaseURL,
		"api_key_env": c.APIKeyEnv, "model": c.Model,
		"rules": firstNonEmpty(c.RulesFlag, c.RepoRules, c.UserRules),
		"max_file_bytes": fmt.Sprint(c.MaxFileBytes), "max_total_bytes": fmt.Sprint(c.MaxTotalBytes),
		"timeout": c.Timeout.String(), "exclude": strings.Join(c.Exclude, ", "),
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		src := c.Sources[k]
		if src == "" {
			src = "unset"
		}
		fmt.Fprintf(&b, "%-16s = %-40s (%s)\n", k, vals[k], src)
	}
	return b.String()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
```

- [ ] **Step 4: Run, expect PASS** (`go test -race ./internal/config/`)

- [ ] **Step 5: Commit**

```bash
git add internal/config && git commit -m "feat(config): load layered toml config with repo key guard"
```

### Task 6: Backends (command, openai) and output cleanup

**Files:**
- Create: `internal/backend/backend.go`, `internal/backend/command.go`, `internal/backend/openai.go`, `internal/backend/backend_test.go`

**Interfaces:**
- Produces:
  - `type Backend interface { Generate(ctx context.Context, prompt string) (string, error); Name() string }`
  - `type Command struct{ Argv []string }` - `Name()` returns `"command: <argv[0]>"`
  - `type OpenAI struct{ BaseURL, Model, APIKey string; Client *http.Client }` - `Name()` returns `"openai: <BaseURL> (<Model>)"`
  - `func Clean(s string) string` - drops lines starting with three backticks, trims whitespace, strips one pair of wrapping `"` or `'`

Timeouts come from the caller's `ctx` (`context.WithTimeout`). Command runs with `exec.CommandContext`, argv only, no shell; on failure the error has the exit code and the last 20 lines of stderr. OpenAI sends `{"model", "messages":[{"role":"user","content":prompt}]}` to `<BaseURL>/chat/completions` with `Authorization: Bearer <key>`; on non-2xx the error has the status and the API `error.message` if present. Neither backend ever puts the key in an error.

- [ ] **Step 1: Write failing tests** `internal/backend/backend_test.go`

```go
package backend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"```\nfeat: x\n```":   "feat: x",
		"\"fix: y\"\n":         "fix: y",
		"  chore: z\n\nbody ": "chore: z\n\nbody",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q", in, got)
		}
	}
}

// TestHelperProcess is re-executed as a fake CLI by the command tests.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("AICOMMIT_HELPER") {
	case "echo":
		b, _ := io.ReadAll(os.Stdin)
		os.Stdout.WriteString("feat: " + strings.TrimSpace(string(b)))
		os.Exit(0)
	case "fail":
		os.Stderr.WriteString("boom\n")
		os.Exit(3)
	case "hang":
		time.Sleep(time.Minute)
	}
}

func helper(t *testing.T, mode string) Command {
	t.Setenv("AICOMMIT_HELPER", mode)
	return Command{Argv: []string{os.Args[0], "-test.run=^TestHelperProcess$"}}
}

func TestCommandBackend(t *testing.T) {
	out, err := helper(t, "echo").Generate(context.Background(), "hello")
	if err != nil || out != "feat: hello" {
		t.Fatalf("got %q %v", out, err)
	}
	_, err = helper(t, "fail").Generate(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err = helper(t, "hang").Generate(ctx, "x"); err == nil {
		t.Fatal("expected timeout")
	}
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("sh"); err == nil {
			// argv is not shell-interpreted: "$HOME" must arrive literally.
			out, _ := Command{Argv: []string{"sh", "-c", "cat; printf '%s' \"$1\"", "sh", "$HOME"}}.Generate(context.Background(), "")
			if out != "$HOME" {
				t.Fatalf("argv was shell-expanded: %q", out)
			}
		}
	}
}

func TestOpenAIBackend(t *testing.T) {
	const key = "sk-secret-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, `{"error":{"message":"bad auth"}}`, 401)
			return
		}
		var req struct {
			Model    string `json:"model"`
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Messages[0].Content == "slow" {
			time.Sleep(time.Second)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "feat: " + req.Model}}}})
	}))
	defer srv.Close()

	b := OpenAI{BaseURL: srv.URL, Model: "m1", APIKey: key}
	if out, err := b.Generate(context.Background(), "hi"); err != nil || out != "feat: m1" {
		t.Fatalf("got %q %v", out, err)
	}
	bad := OpenAI{BaseURL: srv.URL, Model: "m1", APIKey: "wrong-key-456"}
	_, err := bad.Generate(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad auth") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "wrong-key-456") || strings.Contains(b.Name(), key) {
		t.Fatal("key leaked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.Generate(ctx, "slow"); err == nil {
		t.Fatal("expected timeout")
	}
}
```

- [ ] **Step 2: Run, expect FAIL** (`go test ./internal/backend/`)

- [ ] **Step 3: Implement** `internal/backend/backend.go`

```go
package backend

import (
	"context"
	"strings"
)

type Backend interface {
	Generate(ctx context.Context, prompt string) (string, error)
	Name() string
}

func Clean(s string) string {
	var kept []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			continue
		}
		kept = append(kept, l)
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	for _, q := range []string{`"`, `'`} {
		if len(out) >= 2 && strings.HasPrefix(out, q) && strings.HasSuffix(out, q) {
			out = strings.TrimSpace(out[1 : len(out)-1])
			break
		}
	}
	return out
}
```

`internal/backend/command.go`

```go
package backend

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Command struct{ Argv []string }

func (c Command) Name() string { return "command: " + c.Argv[0] }

func (c Command) Generate(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	var out, errb bytes.Buffer
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: %w", c.Name(), ctx.Err())
		}
		return "", fmt.Errorf("%s: %w\n%s", c.Name(), err, tail(errb.String(), 20))
	}
	return out.String(), nil
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
```

`internal/backend/openai.go`

```go
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OpenAI struct {
	BaseURL, Model, APIKey string
	Client                 *http.Client
}

func (o OpenAI) Name() string { return fmt.Sprintf("openai: %s (%s)", o.BaseURL, o.Model) }

func (o OpenAI) Generate(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    o.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", o.Name(), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var parsed struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
		Error struct{ Message string } `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode/100 != 2 {
		msg := parsed.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return "", fmt.Errorf("%s: HTTP %d: %s", o.Name(), resp.StatusCode, msg)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("%s: response had no choices", o.Name())
	}
	return parsed.Choices[0].Message.Content, nil
}
```

- [ ] **Step 4: Run, expect PASS** (`go test -race ./internal/backend/`)

- [ ] **Step 5: Commit**

```bash
git add internal/backend && git commit -m "feat(backend): add command and openai-compatible backends"
```

---

### Task 7: Backend selection and auto-detection

**Files:**
- Create: `internal/backend/detect.go`, `internal/backend/detect_test.go`

**Interfaces:**
- Consumes: `config.Config` (Task 5), `Command`, `OpenAI` (Task 6)
- Produces:
  - `const GitHubModelsURL = "https://models.github.ai/inference"`, `const DefaultGitHubModel = "openai/gpt-4o-mini"`
  - `type Env struct{ LookPath func(string) (string, error); Getenv func(string) string; GHToken func() (string, error) }`
  - `func New(c config.Config, e Env) (Backend, error)`
  - `var ErrNoBackend` - message lists the three setup options and `aicommit init`

Rules:
- `backend = "command"`: `Command` required and non-empty, else error. `model` is ignored (the user controls argv).
- `backend = "openai"`: `BaseURL` defaults to `GitHubModelsURL`; `Model` defaults to `DefaultGitHubModel` for that URL, else required. Key: `Getenv(APIKeyEnv)` if `APIKeyEnv` set (error if empty); else `GHToken()` when BaseURL is GitHub Models; else no key (local servers).
- `backend = ""`: `copilot` on PATH -> `Command{copilot -s --no-ask-user [--model M]}`; else `GHToken()` succeeds -> GitHub Models; else `ErrNoBackend`.
- Any other value: error `unknown backend "x" (want command or openai)`.

- [ ] **Step 1: Write failing tests** `internal/backend/detect_test.go`

```go
package backend

import (
	"errors"
	"strings"
	"testing"

	"github.com/TannerWhite/aicommit/internal/config"
)

func envWith(copilot bool, token string, vars map[string]string) Env {
	return Env{
		LookPath: func(n string) (string, error) {
			if copilot && n == "copilot" {
				return "/bin/copilot", nil
			}
			return "", errors.New("not found")
		},
		Getenv: func(k string) string { return vars[k] },
		GHToken: func() (string, error) {
			if token == "" {
				return "", errors.New("not logged in")
			}
			return token, nil
		},
	}
}

func TestAutoDetect(t *testing.T) {
	b, err := New(config.Config{Model: "m"}, envWith(true, "tok", nil))
	if c, ok := b.(Command); err != nil || !ok || strings.Join(c.Argv, " ") != "copilot -s --no-ask-user --model m" {
		t.Fatalf("want copilot, got %#v %v", b, err)
	}
	b, _ = New(config.Config{}, envWith(false, "tok", nil))
	if o, ok := b.(OpenAI); !ok || o.BaseURL != GitHubModelsURL || o.APIKey != "tok" || o.Model != DefaultGitHubModel {
		t.Fatalf("want github models, got %#v", b)
	}
	if _, err := New(config.Config{}, envWith(false, "", nil)); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("want ErrNoBackend, got %v", err)
	}
}

func TestExplicit(t *testing.T) {
	if _, err := New(config.Config{Backend: "command"}, envWith(false, "", nil)); err == nil {
		t.Fatal("command without argv must fail")
	}
	b, _ := New(config.Config{Backend: "openai", BaseURL: "http://localhost:11434/v1", Model: "llama3"}, envWith(false, "", nil))
	if o := b.(OpenAI); o.APIKey != "" || o.Model != "llama3" {
		t.Fatalf("got %#v", o)
	}
	if _, err := New(config.Config{Backend: "openai", BaseURL: "http://x", Model: "m", APIKeyEnv: "K"}, envWith(false, "", nil)); err == nil {
		t.Fatal("empty api key env must fail")
	}
	b, _ = New(config.Config{Backend: "openai", BaseURL: "http://x", Model: "m", APIKeyEnv: "K"}, envWith(false, "", map[string]string{"K": "v"}))
	if b.(OpenAI).APIKey != "v" {
		t.Fatal("key not read from env")
	}
	if _, err := New(config.Config{Backend: "nope"}, envWith(false, "", nil)); err == nil {
		t.Fatal("unknown backend must fail")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement** `internal/backend/detect.go`

```go
package backend

import (
	"errors"
	"fmt"
	"strings"

	"github.com/TannerWhite/aicommit/internal/config"
)

const (
	GitHubModelsURL    = "https://models.github.ai/inference"
	DefaultGitHubModel = "openai/gpt-4o-mini"
)

var ErrNoBackend = errors.New(`no AI backend found. Set one up with any of:
  1. install the GitHub Copilot CLI ("copilot" on PATH)
  2. run "gh auth login" to use GitHub Models
  3. configure an OpenAI-compatible endpoint (backend = "openai", base_url, model, api_key_env)
Run "aicommit init" to create a starter config`)

type Env struct {
	LookPath func(string) (string, error)
	Getenv   func(string) string
	GHToken  func() (string, error)
}

func New(c config.Config, e Env) (Backend, error) {
	switch c.Backend {
	case "command":
		if len(c.Command) == 0 {
			return nil, errors.New(`backend "command" requires command = ["prog", "args..."] in your user config`)
		}
		return Command{Argv: c.Command}, nil
	case "openai":
		return newOpenAI(c, e)
	case "":
		if _, err := e.LookPath("copilot"); err == nil {
			argv := []string{"copilot", "-s", "--no-ask-user"}
			if c.Model != "" {
				argv = append(argv, "--model", c.Model)
			}
			return Command{Argv: argv}, nil
		}
		if tok, err := e.GHToken(); err == nil && tok != "" {
			return newOpenAI(c, e)
		}
		return nil, ErrNoBackend
	}
	return nil, fmt.Errorf("unknown backend %q (want command or openai)", c.Backend)
}

func newOpenAI(c config.Config, e Env) (Backend, error) {
	o := OpenAI{BaseURL: c.BaseURL, Model: c.Model}
	if o.BaseURL == "" {
		o.BaseURL = GitHubModelsURL
	}
	isGH := strings.TrimRight(o.BaseURL, "/") == GitHubModelsURL
	if o.Model == "" {
		if !isGH {
			return nil, errors.New(`backend "openai" requires model to be set`)
		}
		o.Model = DefaultGitHubModel
	}
	switch {
	case c.APIKeyEnv != "":
		o.APIKey = e.Getenv(c.APIKeyEnv)
		if o.APIKey == "" {
			return nil, fmt.Errorf("api_key_env %s is empty or unset", c.APIKeyEnv)
		}
	case isGH:
		tok, err := e.GHToken()
		if err != nil {
			return nil, fmt.Errorf("GitHub Models needs a gh login: %w", err)
		}
		o.APIKey = tok
	}
	return o, nil
}
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add internal/backend && git commit -m "feat(backend): add backend selection and auto-detection"
```

---

### Task 8: Prompt templates

**Files:**
- Create: `internal/prompt/single.tmpl`, `internal/prompt/split.tmpl`, `internal/prompt/prompt.go`, `internal/prompt/prompt_test.go`, `internal/prompt/testdata/single.golden`, `internal/prompt/testdata/split.golden`

**Interfaces:**
- Produces:
  - `func Single(rules, changes, feedback string) string` - also used to regenerate one split group's message
  - `func Split(rules, changes, validationErrors string) string`

- [ ] **Step 1: Write failing test** `internal/prompt/prompt_test.go`

```go
package prompt

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		os.WriteFile(p, []byte(got), 0o644)
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s mismatch; run go test ./internal/prompt -update\n%s", name, got)
	}
}

func TestSingle(t *testing.T) {
	golden(t, "single.golden", Single("RULES", "=== FILE: a.go ===\n+x\n", "shorter please"))
	if strings.Contains(Single("R", "C", ""), "rejected") {
		t.Fatal("feedback section should be omitted when empty")
	}
}

func TestSplit(t *testing.T) {
	golden(t, "split.golden", Split("RULES", "=== FILE: a.go ===\n", "file b.go missing"))
}

func TestNoEmDash(t *testing.T) {
	for _, s := range []string{Single("r", "c", "f"), Split("r", "c", "e")} {
		if strings.Contains(s, "\u2014") {
			t.Fatal("em dash in prompt")
		}
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement** `internal/prompt/single.tmpl`

```
{{.Rules}}

Write a git commit message for the changes below. Output ONLY the raw commit message text: no markdown code fences, no commentary, no surrounding quotes. Use a single-line subject in the form "type(scope): description" (omit scope if the change is global). Add a short body (2-5 bullet points) only if the changes touch multiple concerns or are large; otherwise output the subject line only.
{{- if .Feedback}}

The previous attempt was rejected with this feedback. Incorporate it:
{{.Feedback}}
{{- end}}

--- CHANGES ---
{{.Changes}}
```

`internal/prompt/split.tmpl`

```
{{.Rules}}

You are splitting uncommitted changes into logically separate git commits, following the "one logical change per commit" rule above. Group the files below into one or more commits. Each group must be a cohesive, atomic change with its own commit message that follows the rules.

Respond with ONLY one JSON object, no markdown code fences and no commentary, with this exact shape:
{"commits": [{"files": ["path/one", "path/two"], "message": "type(scope): subject\n\n- optional bullet body"}]}

Use the exact file paths shown after "=== FILE:". For renamed files use the new path. Every listed file must appear in exactly one commit. Files marked "(contents excluded)" or binary must still be placed in a commit based on their name. If everything is one logical change, return a single commit.
{{- if .Errors}}

Your previous response was invalid. Fix these problems:
{{.Errors}}
{{- end}}

--- FILES AND CHANGES ---
{{.Changes}}
```

`internal/prompt/prompt.go`

```go
package prompt

import (
	_ "embed"
	"strings"
	"text/template"
)

var (
	//go:embed single.tmpl
	singleSrc string
	//go:embed split.tmpl
	splitSrc string

	singleT = template.Must(template.New("single").Parse(singleSrc))
	splitT  = template.Must(template.New("split").Parse(splitSrc))
)

func render(t *template.Template, data any) string {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		panic(err) // templates are static; covered by tests
	}
	return b.String()
}

func Single(rules, changes, feedback string) string {
	return render(singleT, map[string]string{"Rules": rules, "Changes": changes, "Feedback": feedback})
}

func Split(rules, changes, validationErrors string) string {
	return render(splitT, map[string]string{"Rules": rules, "Changes": changes, "Errors": validationErrors})
}
```

- [ ] **Step 4: Generate goldens, review them by reading the files, then run normally**

```bash
mkdir -p internal/prompt/testdata
go test ./internal/prompt -update && go test ./internal/prompt
```

- [ ] **Step 5: Commit**

```bash
git add internal/prompt && git commit -m "feat(prompt): add single and split prompt templates"
```

---

### Task 9: Plan parsing and validation

**Files:**
- Create: `internal/plan/plan.go`, `internal/plan/plan_test.go`

**Interfaces:**
- Produces:
  - `type Commit struct{ Files []string `json:"files"`; Message string `json:"message"` }`
  - `type Plan struct{ Commits []Commit `json:"commits"` }`
  - `func Parse(raw string) (Plan, error)` - strict decode (unknown fields rejected); if the whole text fails, retry on the first balanced top-level `{...}` (string- and escape-aware)
  - `func Validate(p Plan, paths []string) []string` - human-readable problems; empty means valid

- [ ] **Step 1: Write failing tests** `internal/plan/plan_test.go`

```go
package plan

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	good := `{"commits":[{"files":["a.go"],"message":"feat: a"}]}`
	for _, in := range []string{good, "Sure! Here it is:\n" + good + "\nHope that helps {x}", "```json\n" + good + "\n```"} {
		p, err := Parse(in)
		if err != nil || len(p.Commits) != 1 || p.Commits[0].Files[0] != "a.go" {
			t.Fatalf("Parse(%q) = %+v, %v", in, p, err)
		}
	}
	if _, err := Parse(`{"commits":[{"files":["a"],"message":"m","extra":1}]}`); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
	if _, err := Parse("no json here"); err == nil {
		t.Fatal("expected error")
	}
	p, err := Parse(`note {"commits":[{"files":["a}b.go"],"message":"fix: brace \" }"}]}`)
	if err != nil || p.Commits[0].Files[0] != "a}b.go" {
		t.Fatalf("brace inside string broke extraction: %v", err)
	}
}

func TestValidate(t *testing.T) {
	paths := []string{"a.go", "b.go"}
	ok := Plan{Commits: []Commit{{Files: []string{"a.go"}, Message: "feat: a"}, {Files: []string{"b.go"}, Message: "fix: b"}}}
	if errs := Validate(ok, paths); len(errs) != 0 {
		t.Fatalf("unexpected %v", errs)
	}
	cases := map[string]Plan{
		`"b.go" is missing`:             {Commits: []Commit{{Files: []string{"a.go"}, Message: "feat: a"}}},
		`"a.go" appears in more than one`: {Commits: []Commit{{Files: []string{"a.go", "b.go"}, Message: "x: y"}, {Files: []string{"a.go"}, Message: "x: z"}}},
		`"c.go" is not in the change set`: {Commits: []Commit{{Files: []string{"a.go", "b.go", "c.go"}, Message: "x: y"}}},
		`commit 1 has an empty message`:  {Commits: []Commit{{Files: []string{"a.go", "b.go"}, Message: "  \n"}}},
		`commit 1 has no files`:          {Commits: []Commit{{Message: "x: y"}, {Files: paths, Message: "x: y"}}},
		`plan has no commits`:            {},
	}
	for want, p := range cases {
		errs := strings.Join(Validate(p, paths), "\n")
		if !strings.Contains(errs, want) {
			t.Errorf("want %q in %q", want, errs)
		}
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement** `internal/plan/plan.go`

```go
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Commit struct {
	Files   []string `json:"files"`
	Message string   `json:"message"`
}

type Plan struct {
	Commits []Commit `json:"commits"`
}

func decode(s string) (Plan, error) {
	var p Plan
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Plan{}, err
	}
	if dec.More() {
		return Plan{}, errors.New("trailing data after JSON object")
	}
	return p, nil
}

func Parse(raw string) (Plan, error) {
	p, err := decode(strings.TrimSpace(raw))
	if err == nil {
		return p, nil
	}
	obj, ok := firstObject(raw)
	if !ok {
		return Plan{}, fmt.Errorf("response is not JSON: %w", err)
	}
	return decode(obj)
}

// firstObject returns the first balanced top-level {...}, ignoring braces in strings.
func firstObject(s string) (string, bool) {
	start, depth, inStr, esc := -1, 0, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			if start >= 0 {
				inStr = true
			}
		case '{':
			if start < 0 {
				start = i
			}
			depth++
		case '}':
			if start >= 0 {
				depth--
				if depth == 0 {
					return s[start : i+1], true
				}
			}
		}
	}
	return "", false
}

func Validate(p Plan, paths []string) []string {
	var errs []string
	if len(p.Commits) == 0 {
		return []string{"plan has no commits"}
	}
	known := map[string]bool{}
	for _, x := range paths {
		known[x] = true
	}
	seen := map[string]int{}
	for i, c := range p.Commits {
		n := i + 1
		if strings.TrimSpace(c.Message) == "" {
			errs = append(errs, fmt.Sprintf("commit %d has an empty message", n))
		}
		if len(c.Files) == 0 {
			errs = append(errs, fmt.Sprintf("commit %d has no files", n))
		}
		for _, f := range c.Files {
			if !known[f] {
				errs = append(errs, fmt.Sprintf("%q is not in the change set", f))
				continue
			}
			seen[f]++
			if seen[f] == 2 {
				errs = append(errs, fmt.Sprintf("%q appears in more than one commit", f))
			}
		}
	}
	for _, x := range paths {
		if seen[x] == 0 {
			errs = append(errs, fmt.Sprintf("%q is missing from the plan", x))
		}
	}
	return errs
}
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add internal/plan && git commit -m "feat(plan): parse and strictly validate split plans"
```

### Task 10: Interactive prompter

**Files:**
- Create: `internal/ui/ui.go`, `internal/ui/ui_test.go`

**Interfaces:**
- Produces:
  - `type Prompter struct` (unexported fields), `func New(in io.Reader, out io.Writer) *Prompter`
  - `func (p *Prompter) Choose(prompt, valid string) (rune, error)` - re-asks on invalid input; case-insensitive; returns `io.EOF` (or read error) when input ends
  - `func (p *Prompter) Line(prompt string) (string, error)` - trimmed line
  - `func IsTerminal(f *os.File) bool` - `f.Stat()` mode has `os.ModeCharDevice` (no extra dependency)

- [ ] **Step 1: Write failing test** `internal/ui/ui_test.go`

```go
package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestChoose(t *testing.T) {
	var out bytes.Buffer
	p := New(strings.NewReader("x\nA\n"), &out)
	c, err := p.Choose("pick: ", "aq")
	if err != nil || c != 'a' {
		t.Fatalf("got %q %v", c, err)
	}
	if !strings.Contains(out.String(), "Please enter one of: a, q") {
		t.Fatalf("no reprompt: %q", out.String())
	}
	if _, err := p.Choose("pick: ", "aq"); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestLine(t *testing.T) {
	p := New(strings.NewReader("  make it shorter \n"), io.Discard)
	if s, _ := p.Line("fb: "); s != "make it shorter" {
		t.Fatalf("got %q", s)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement** `internal/ui/ui.go`

```go
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

type Prompter struct {
	in  *bufio.Reader
	out io.Writer
}

func New(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{in: bufio.NewReader(in), out: out}
}

func (p *Prompter) Choose(prompt, valid string) (rune, error) {
	for {
		fmt.Fprint(p.out, prompt)
		line, err := p.in.ReadString('\n')
		s := strings.ToLower(strings.TrimSpace(line))
		if len(s) == 1 && strings.ContainsRune(valid, rune(s[0])) {
			return rune(s[0]), nil
		}
		if err != nil {
			fmt.Fprintln(p.out)
			return 0, err
		}
		fmt.Fprintf(p.out, "Please enter one of: %s\n", strings.Join(strings.Split(valid, ""), ", "))
	}
}

func (p *Prompter) Line(prompt string) (string, error) {
	fmt.Fprint(p.out, prompt)
	line, err := p.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add internal/ui && git commit -m "feat(ui): add interactive choice and feedback prompter"
```

---

### Task 11: App flows (single, split, safety checks, summary)

**Files:**
- Create: `internal/app/app.go`, `internal/app/app_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2-10
- Produces:
  - `type Options struct{ DryRun, Yes, All, Verbose bool }`
  - `type App struct{ Git git.Runner; Backend backend.Backend; Rules string; Config config.Config; UI *ui.Prompter; Out io.Writer; Interactive bool; Opts Options }` (plus unexported state)
  - `func (a *App) Run(ctx context.Context) error`
  - `func (a *App) Interrupt()` - undo staging of the in-progress group and print the summary (called by the CLI on SIGINT)
  - `var ErrQuit = errors.New("quit")`, `var ErrNoTTY = errors.New("stdin is not a terminal; pass --yes to accept proposals without prompting")` - the CLI maps both to exit 2

Behavior (from spec Data flow and Error handling):
- No TTY and neither `--yes` nor `--dry-run` -> `ErrNoTTY` before any work.
- Index non-empty and not `--all` -> single mode; else split mode. Clean tree -> print `aicommit: nothing to commit (working tree clean)` and return nil.
- Every `Backend.Generate` call gets `context.WithTimeout(ctx, Config.Timeout)`. `--verbose` prints `Backend.Name()` and the prompt (no keys exist in either).
- Split: unmerged entries -> error before any model call. Plan retried once with validation errors; second failure -> error with problems and raw output.
- Per group: on first staging with `--all`, `RestoreAllStaged`. Before `Add`, the index must be empty; after `Add`, every staged path must be in the group's paths (group files plus rename `OrigPath`s). Violations -> undo and stop with an error.
- Commit failure: print git's output, undo the group's staging; with `--yes` return an error, otherwise re-show the group (accept = retry).
- Summary after every split run and on interrupt: committed `sha subject` lines and any files left uncommitted.

- [ ] **Step 1: Write failing integration tests** `internal/app/app_test.go`

```go
package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/TannerWhite/aicommit/internal/changes"
	"github.com/TannerWhite/aicommit/internal/config"
	"github.com/TannerWhite/aicommit/internal/git"
	"github.com/TannerWhite/aicommit/internal/ui"
)

type fake struct {
	responses []string
	prompts   []string
}

func (f *fake) Name() string { return "fake" }
func (f *fake) Generate(_ context.Context, p string) (string, error) {
	f.prompts = append(f.prompts, p)
	if len(f.responses) == 0 {
		return "", errors.New("fake: no more responses")
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return r, nil
}

func setup(t *testing.T) git.Runner {
	t.Helper()
	r := git.Runner{Dir: t.TempDir()}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := r.Run(nil, a...); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func write(t *testing.T, r git.Runner, name, body string) {
	t.Helper()
	os.WriteFile(filepath.Join(r.Dir, name), []byte(body), 0o644)
}

func hook(t *testing.T, r git.Runner, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not portable to windows")
	}
	p := filepath.Join(r.Dir, ".git", "hooks", name)
	os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755)
}

func newApp(r git.Runner, b *fake, input string, o Options) (*App, *bytes.Buffer) {
	var out bytes.Buffer
	return &App{
		Git: r, Backend: b, Rules: "RULES", Out: &out, Opts: o,
		Config:      config.Config{MaxFileBytes: 100000, MaxTotalBytes: 400000, Timeout: 5 * time.Second, Exclude: changes.DefaultExcludes},
		UI:          ui.New(strings.NewReader(input), &out),
		Interactive: input != "",
	}, &out
}

func log(t *testing.T, r git.Runner) []string {
	out, err := r.Run(nil, "log", "--format=%s")
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(out), "\n")
}

func staged(t *testing.T, r git.Runner) []string {
	s, err := r.StagedPaths()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSingleYes(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	r.Add([]string{"a.txt"})
	a, _ := newApp(r, &fake{responses: []string{"```\nfeat: add a\n```"}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log(t, r); got[0] != "feat: add a" {
		t.Fatalf("log %v", got)
	}
}

func TestSingleRegenerateThenAccept(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	r.Add([]string{"a.txt"})
	b := &fake{responses: []string{"feat: first", "feat: second"}}
	a, _ := newApp(r, b, "r\nshorter\na\n", Options{})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.prompts[1], "shorter") || log(t, r)[0] != "feat: second" {
		t.Fatalf("feedback not used: %v", log(t, r))
	}
}

func TestSingleEditUsesEditor(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	r.Add([]string{"a.txt"})
	t.Setenv("GIT_EDITOR", "true")
	a, _ := newApp(r, &fake{responses: []string{"feat: edited"}}, "e\n", Options{})
	if err := a.Run(context.Background()); err != nil || log(t, r)[0] != "feat: edited" {
		t.Fatalf("err %v log %v", err, log(t, r))
	}
}

func TestQuitAndNoTTY(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	r.Add([]string{"a.txt"})
	a, _ := newApp(r, &fake{responses: []string{"feat: x"}}, "q\n", Options{})
	if err := a.Run(context.Background()); !errors.Is(err, ErrQuit) {
		t.Fatalf("want ErrQuit, got %v", err)
	}
	a, _ = newApp(r, &fake{}, "", Options{})
	if err := a.Run(context.Background()); !errors.Is(err, ErrNoTTY) {
		t.Fatalf("want ErrNoTTY, got %v", err)
	}
}

const twoGroups = `{"commits":[{"files":["a.txt"],"message":"feat: add a"},{"files":["b.txt"],"message":"docs: add b"}]}`

func TestSplitYes(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	a, out := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log(t, r); len(got) != 2 || got[0] != "docs: add b" || got[1] != "feat: add a" {
		t.Fatalf("log %v", got)
	}
	if !strings.Contains(out.String(), "Committed:") {
		t.Fatalf("no summary: %s", out.String())
	}
}

func TestSplitRetryThenFail(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	bad := `{"commits":[{"files":["a.txt"],"message":"feat: a"}]}`
	b := &fake{responses: []string{bad, twoGroups}}
	a, _ := newApp(r, b, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.prompts[1], `"b.txt" is missing`) {
		t.Fatal("retry prompt lacks validation errors")
	}
	r2 := setup(t)
	write(t, r2, "a.txt", "a\n")
	write(t, r2, "b.txt", "b\n")
	a, _ = newApp(r2, &fake{responses: []string{bad, "not json"}}, "", Options{Yes: true})
	err := a.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "raw output") || log(t, r2) != nil {
		t.Fatalf("want failure with nothing committed, got %v %v", err, log(t, r2))
	}
}

func TestSplitSkip(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	a, out := newApp(r, &fake{responses: []string{twoGroups}}, "s\na\n", Options{})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log(t, r); len(got) != 1 || got[0] != "docs: add b" {
		t.Fatalf("log %v", got)
	}
	if !strings.Contains(out.String(), "Left uncommitted: a.txt") {
		t.Fatalf("summary: %s", out.String())
	}
}

func TestHookRejectionUndoesStaging(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	hook(t, r, "commit-msg", "echo rejected by hook; exit 1")
	a, out := newApp(r, &fake{responses: []string{`{"commits":[{"files":["a.txt"],"message":"feat: a"}]}`}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if len(staged(t, r)) != 0 || !strings.Contains(out.String(), "rejected by hook") {
		t.Fatalf("staged %v out %s", staged(t, r), out.String())
	}
}

func TestIndexChangedMidRunAborts(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	hook(t, r, "post-commit", "echo x > sneaky.txt && git add sneaky.txt")
	a, _ := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{Yes: true})
	err := a.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "index is not empty") || len(log(t, r)) != 1 {
		t.Fatalf("want abort after first commit, got %v %v", err, log(t, r))
	}
}

func TestMergeConflictRefused(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "base\n")
	r.Add([]string{"a.txt"})
	r.Commit("chore: base\n", false)
	r.Run(nil, "checkout", "-q", "-b", "other")
	write(t, r, "a.txt", "other\n")
	r.Run(nil, "commit", "-qam", "chore: other")
	r.Run(nil, "checkout", "-q", "main")
	write(t, r, "a.txt", "main\n")
	r.Run(nil, "commit", "-qam", "chore: main")
	r.Run(nil, "merge", "-q", "other")
	b := &fake{}
	a, _ := newApp(r, b, "", Options{Yes: true, All: true})
	err := a.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "merge conflict") || len(b.prompts) != 0 {
		t.Fatalf("got %v, prompts %d", err, len(b.prompts))
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	a, out := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{DryRun: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if log(t, r) != nil || len(staged(t, r)) != 0 || !strings.Contains(out.String(), "docs: add b") {
		t.Fatal("dry run modified repo or did not print plan")
	}
}

func TestAllWithPartialIndex(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	r.Add([]string{"a.txt"})
	a, _ := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{Yes: true, All: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log(t, r); len(got) != 2 {
		t.Fatalf("log %v", got)
	}
}

func TestSplitRenameStagesBothSides(t *testing.T) {
	r := setup(t)
	write(t, r, "old.txt", strings.Repeat("same\n", 20))
	r.Add([]string{"old.txt"})
	r.Commit("chore: init\n", false)
	os.Rename(filepath.Join(r.Dir, "old.txt"), filepath.Join(r.Dir, "new.txt"))
	// Working-tree renames appear as a deletion plus an untracked file.
	plan := `{"commits":[{"files":["old.txt","new.txt"],"message":"refactor: rename old to new"}]}`
	a, _ := newApp(r, &fake{responses: []string{plan}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := r.Run(nil, "status", "--porcelain")
	if strings.TrimSpace(st) != "" {
		t.Fatalf("tree not clean: %q", st)
	}
}
```

- [ ] **Step 2: Run, expect FAIL** (`go test ./internal/app/`)

- [ ] **Step 3: Implement** `internal/app/app.go`

```go
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/TannerWhite/aicommit/internal/backend"
	"github.com/TannerWhite/aicommit/internal/changes"
	"github.com/TannerWhite/aicommit/internal/config"
	"github.com/TannerWhite/aicommit/internal/git"
	"github.com/TannerWhite/aicommit/internal/plan"
	"github.com/TannerWhite/aicommit/internal/prompt"
	"github.com/TannerWhite/aicommit/internal/ui"
)

var (
	ErrQuit  = errors.New("quit")
	ErrNoTTY = errors.New("stdin is not a terminal; pass --yes to accept proposals without prompting")
)

type Options struct{ DryRun, Yes, All, Verbose bool }

type App struct {
	Git         git.Runner
	Backend     backend.Backend
	Rules       string
	Config      config.Config
	UI          *ui.Prompter
	Out         io.Writer
	Interactive bool
	Opts        Options

	mu        sync.Mutex
	pending   []string // paths staged by aicommit and not yet committed
	all       []string // every path in the split change set
	done      map[string]bool
	committed []string
	cleared   bool
}

func (a *App) Run(ctx context.Context) error {
	if !a.Interactive && !a.Opts.Yes && !a.Opts.DryRun {
		return ErrNoTTY
	}
	hasStaged, err := a.Git.HasStaged()
	if err != nil {
		return err
	}
	if hasStaged && !a.Opts.All {
		return a.single(ctx)
	}
	return a.split(ctx)
}

func (a *App) changeOpts(staged bool) changes.Options {
	return changes.Options{Staged: staged, MaxFileBytes: a.Config.MaxFileBytes, MaxTotalBytes: a.Config.MaxTotalBytes, Exclude: a.Config.Exclude}
}

func (a *App) generate(ctx context.Context, p string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.Config.Timeout)
	defer cancel()
	if a.Opts.Verbose {
		fmt.Fprintf(a.Out, "--- backend: %s ---\n%s\n--- end prompt ---\n", a.Backend.Name(), p)
	}
	return a.Backend.Generate(ctx, p)
}

func (a *App) message(ctx context.Context, body, feedback string) (string, error) {
	raw, err := a.generate(ctx, prompt.Single(a.Rules, body, feedback))
	if err != nil {
		return "", err
	}
	msg := backend.Clean(raw)
	if subject(msg) == "" {
		return "", errors.New("backend returned an empty commit message")
	}
	return msg + "\n", nil
}

func subject(msg string) string {
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(msg), "\n", 2)[0])
}

func (a *App) choose(prompt, valid string) (rune, error) {
	if a.Opts.Yes {
		return 'a', nil
	}
	c, err := a.UI.Choose(prompt, valid)
	if err != nil {
		return 0, ErrQuit
	}
	return c, nil
}

func (a *App) single(ctx context.Context) error {
	out, err := a.Git.Run(nil, "diff", "--cached", "-z", "--name-status", "-M")
	if err != nil {
		return err
	}
	st, err := git.ParseNameStatusZ(out)
	if err != nil {
		return err
	}
	set, err := changes.Build(a.Git, st, a.changeOpts(true))
	if err != nil {
		return err
	}
	body := set.Render()
	msg, err := a.message(ctx, body, "")
	if err != nil {
		return err
	}
	for {
		fmt.Fprintf(a.Out, "\n----- Proposed commit message -----\n%s------------------------------------\n", msg)
		if a.Opts.DryRun {
			return nil
		}
		c, err := a.choose("[a]ccept, [e]dit, [r]egenerate, [q]uit: ", "aerq")
		if err != nil {
			return err
		}
		switch c {
		case 'a', 'e':
			sha, output, err := a.Git.Commit(msg, c == 'e')
			if err != nil {
				fmt.Fprint(a.Out, output)
				return err
			}
			fmt.Fprintf(a.Out, "aicommit: committed %s %s\n", sha, subject(msg))
			return nil
		case 'r':
			fb, err := a.UI.Line("Feedback for regeneration: ")
			if err != nil {
				return ErrQuit
			}
			if msg, err = a.message(ctx, body, fb); err != nil {
				return err
			}
		case 'q':
			return ErrQuit
		}
	}
}

func (a *App) split(ctx context.Context) error {
	out, err := a.Git.Run(nil, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return err
	}
	st, err := git.ParseStatusV2(out)
	if err != nil {
		return err
	}
	if len(st) == 0 {
		fmt.Fprintln(a.Out, "aicommit: nothing to commit (working tree clean)")
		return nil
	}
	for _, e := range st {
		if e.Unmerged {
			return fmt.Errorf("unresolved merge conflict in %s; resolve it before running aicommit", e.Path)
		}
	}
	set, err := changes.Build(a.Git, st, a.changeOpts(false))
	if err != nil {
		return err
	}
	p, err := a.makePlan(ctx, set)
	if err != nil {
		return err
	}
	printPlan(a.Out, p)
	if a.Opts.DryRun {
		return nil
	}
	a.mu.Lock()
	a.all, a.done = set.Paths(), map[string]bool{}
	a.mu.Unlock()
	defer a.printSummary()
	for i, c := range p.Commits {
		if err := a.group(ctx, set, i, len(p.Commits), c); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) makePlan(ctx context.Context, set changes.Set) (plan.Plan, error) {
	var problems, last string
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := a.generate(ctx, prompt.Split(a.Rules, set.Render(), problems))
		if err != nil {
			return plan.Plan{}, err
		}
		last = raw
		p, perr := plan.Parse(raw)
		var errs []string
		if perr != nil {
			errs = []string{perr.Error()}
		} else {
			errs = plan.Validate(p, set.Paths())
		}
		if len(errs) == 0 {
			return p, nil
		}
		problems = "- " + strings.Join(errs, "\n- ")
	}
	return plan.Plan{}, fmt.Errorf("model returned an invalid commit plan:\n%s\n--- raw output ---\n%s", problems, last)
}

func printPlan(w io.Writer, p plan.Plan) {
	fmt.Fprintf(w, "\n===== Proposed commit plan (%d commit(s)) =====\n", len(p.Commits))
	for i, c := range p.Commits {
		fmt.Fprintf(w, "\n--- Commit %d/%d ---\nFiles:\n", i+1, len(p.Commits))
		for _, f := range c.Files {
			fmt.Fprintf(w, "  - %s\n", f)
		}
		fmt.Fprintf(w, "Message:\n  %s\n", strings.ReplaceAll(strings.TrimSpace(c.Message), "\n", "\n  "))
	}
	fmt.Fprintln(w, "================================================")
}

func stagePaths(set changes.Set, files []string) []string {
	var out []string
	for _, f := range files {
		out = append(out, f)
		if e, ok := set.Find(f); ok && e.OrigPath != "" {
			out = append(out, e.OrigPath)
		}
	}
	return out
}

func subsetOf(set changes.Set, files []string) changes.Set {
	var s changes.Set
	for _, f := range files {
		if e, ok := set.Find(f); ok {
			s.Entries = append(s.Entries, e)
		}
	}
	return s
}

func (a *App) group(ctx context.Context, set changes.Set, i, n int, c plan.Commit) error {
	msg := strings.TrimSpace(c.Message) + "\n"
	paths := stagePaths(set, c.Files)
	for {
		fmt.Fprintf(a.Out, "\n----- Commit %d/%d -----\nFiles: %s\n%s------------------------------------\n", i+1, n, strings.Join(c.Files, ", "), msg)
		ch, err := a.choose("[a]ccept, [e]dit, [r]egenerate, [s]kip, [q]uit: ", "aersq")
		if err != nil {
			return err
		}
		switch ch {
		case 's':
			fmt.Fprintf(a.Out, "aicommit: skipped commit %d, files left unstaged\n", i+1)
			return nil
		case 'q':
			return ErrQuit
		case 'r':
			fb, err := a.UI.Line("Feedback for regeneration: ")
			if err != nil {
				return ErrQuit
			}
			if msg, err = a.message(ctx, subsetOf(set, c.Files).Render(), fb); err != nil {
				return err
			}
		case 'a', 'e':
			ok, err := a.commitGroup(msg, paths, c.Files, ch == 'e')
			if err != nil || ok {
				return err
			}
			if a.Opts.Yes {
				return fmt.Errorf("commit %d failed; its staging was undone", i+1)
			}
			fmt.Fprintln(a.Out, "aicommit: commit failed and staging was undone; [a]ccept retries")
		}
	}
}

func (a *App) commitGroup(msg string, paths, files []string, edit bool) (bool, error) {
	if a.Opts.All && !a.cleared {
		if err := a.Git.RestoreAllStaged(); err != nil {
			return false, err
		}
	}
	a.cleared = true
	pre, err := a.Git.StagedPaths()
	if err != nil {
		return false, err
	}
	if len(pre) > 0 {
		return false, fmt.Errorf("index is not empty (%s); something was staged outside aicommit, stopping", strings.Join(pre, ", "))
	}
	a.setPending(paths)
	if err := a.Git.Add(paths); err != nil {
		a.undo()
		return false, err
	}
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[p] = true
	}
	post, err := a.Git.StagedPaths()
	if err != nil {
		a.undo()
		return false, err
	}
	for _, p := range post {
		if !allowed[p] {
			a.undo()
			return false, fmt.Errorf("unexpected staged path %q, stopping", p)
		}
	}
	sha, output, err := a.Git.Commit(msg, edit)
	if err != nil {
		fmt.Fprint(a.Out, output)
		fmt.Fprintln(a.Out, "aicommit:", err)
		a.undo()
		return false, nil
	}
	a.mu.Lock()
	a.pending = nil
	a.committed = append(a.committed, sha+" "+subject(msg))
	for _, f := range files {
		a.done[f] = true
	}
	a.mu.Unlock()
	return true, nil
}

func (a *App) setPending(p []string) {
	a.mu.Lock()
	a.pending = p
	a.mu.Unlock()
}

func (a *App) undo() {
	a.mu.Lock()
	p := a.pending
	a.pending = nil
	a.mu.Unlock()
	if len(p) > 0 {
		if err := a.Git.RestoreStaged(p); err != nil {
			fmt.Fprintln(a.Out, "aicommit: failed to unstage:", err)
		}
	}
}

// Interrupt undoes the in-progress group's staging and prints the summary.
func (a *App) Interrupt() {
	a.undo()
	a.printSummary()
}

func (a *App) printSummary() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done == nil {
		return
	}
	fmt.Fprintln(a.Out, "\n===== aicommit summary =====")
	fmt.Fprintln(a.Out, "Committed:")
	if len(a.committed) == 0 {
		fmt.Fprintln(a.Out, "  (none)")
	}
	for _, c := range a.committed {
		fmt.Fprintf(a.Out, "  %s\n", c)
	}
	var left []string
	for _, p := range a.all {
		if !a.done[p] {
			left = append(left, p)
		}
	}
	if len(left) > 0 {
		fmt.Fprintf(a.Out, "Left uncommitted: %s\n", strings.Join(left, ", "))
	}
}
```

Note on `TestSplitRenameStagesBothSides`: a rename done in the working tree shows up in porcelain v2 as a deleted tracked file plus an untracked file, so the plan lists both paths. `git add -A -- old.txt new.txt` stages the pair, and git records it as a rename. Staged renames (entry type `2`) are covered by `stagePaths` adding `OrigPath`.

- [ ] **Step 4: Run, expect PASS** (`go test -race -count=3 ./internal/app/`). `post-commit` runs after git releases the index lock, so the hook's `git add` is deterministic.

- [ ] **Step 5: Commit**

```bash
git add internal/app && git commit -m "feat(app): add single and split commit flows with safety checks"
```

---

### Task 12: CLI wiring (flags, config, init, signals, exit codes)

**Files:**
- Modify: `internal/cli/root.go` (replace the Task 1 stub `RunE`)
- Create: `internal/cli/init.go`, `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `config.Load`, `config.UserDir`, `rules.Resolve`, `rules.Default`, `backend.New`, `backend.Env`, `app.App`, `app.ErrQuit`, `app.ErrNoTTY`, `ui.New`, `ui.IsTerminal`, `git.Runner`
- Produces: `aicommit`, `aicommit config`, `aicommit init` as specified; exit codes 0/1/2.

- [ ] **Step 1: Write failing tests** `internal/cli/cli_test.go`

```go
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = d
		if err := c.Run(); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func run(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)
	var out, errb bytes.Buffer
	code := Execute(args, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func TestInitAndConfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir := repoDir(t)
	if code, _, e := run(t, dir, "init"); code != 0 {
		t.Fatalf("init failed: %s", e)
	}
	for _, f := range []string{"config.toml", "rules.md"} {
		if _, err := os.Stat(filepath.Join(xdg, "aicommit", f)); err != nil {
			t.Fatal(err)
		}
	}
	if code, _, _ := run(t, dir, "init"); code != 1 {
		t.Fatal("init must refuse to overwrite")
	}
	code, out, _ := run(t, dir, "config", "--model", "m9")
	if code != 0 || !strings.Contains(out, "m9") || !strings.Contains(out, "(flag)") {
		t.Fatalf("config output: %s", out)
	}
}

func TestRepoConfigForbiddenKeyExits1(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := repoDir(t)
	os.MkdirAll(filepath.Join(dir, ".aicommit"), 0o755)
	os.WriteFile(filepath.Join(dir, ".aicommit", "config.toml"), []byte(`command = ["evil"]`), 0o644)
	code, _, e := run(t, dir, "config")
	if code != 1 || !strings.Contains(e, "not allowed in repo config") {
		t.Fatalf("code %d stderr %s", code, e)
	}
}

func TestCleanTreeExits0(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AICOMMIT_BACKEND", "command")
	dir := repoDir(t)
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "aicommit")
	os.MkdirAll(cfg, 0o755)
	os.WriteFile(filepath.Join(cfg, "config.toml"), []byte(`command = ["cat"]`), 0o644)
	code, out, e := run(t, dir, "--yes")
	if code != 0 || !strings.Contains(out, "nothing to commit") {
		t.Fatalf("code %d out %s err %s", code, out, e)
	}
}

func TestNoTTYExits2(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AICOMMIT_BACKEND", "command")
	dir := repoDir(t)
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "aicommit")
	os.MkdirAll(cfg, 0o755)
	os.WriteFile(filepath.Join(cfg, "config.toml"), []byte(`command = ["cat"]`), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	if code, _, _ := run(t, dir); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement** `internal/cli/root.go` (full replacement)

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TannerWhite/aicommit/internal/app"
	"github.com/TannerWhite/aicommit/internal/backend"
	"github.com/TannerWhite/aicommit/internal/config"
	"github.com/TannerWhite/aicommit/internal/git"
	"github.com/TannerWhite/aicommit/internal/rules"
	"github.com/TannerWhite/aicommit/internal/ui"
)

type flags struct {
	dryRun, yes, all, verbose bool
	backend, model, rules     string
	exclude                   []string
}

type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func loadConfig(f *flags) (config.Config, git.Runner, string, error) {
	r := git.Runner{}
	root, err := r.RepoRoot()
	if err != nil {
		return config.Config{}, r, "", errors.New("not inside a git repository")
	}
	r.Dir = root
	userDir, err := config.UserDir(os.Getenv)
	if err != nil {
		return config.Config{}, r, "", err
	}
	c, err := config.Load(userDir, root, os.Getenv, config.Overrides{Backend: f.backend, Model: f.model, Rules: f.rules, Exclude: f.exclude})
	return c, r, userDir, err
}

func ghToken() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	return strings.TrimSpace(string(out)), err
}

func newRoot(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	f := &flags{}
	root := &cobra.Command{
		Use:           "aicommit",
		Short:         "Write Conventional Commits messages with AI and split changes into atomic commits",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, r, userDir, err := loadConfig(f)
			if err != nil {
				return err
			}
			for _, w := range c.Warnings {
				fmt.Fprintln(stderr, "aicommit: warning:", w)
			}
			text, _, err := rules.Resolve(rules.Sources{Flag: c.RulesFlag, RepoKey: c.RepoRules, UserKey: c.UserRules, RepoRoot: r.Dir, UserDir: userDir})
			if err != nil {
				return err
			}
			b, err := backend.New(c, backend.Env{LookPath: exec.LookPath, Getenv: os.Getenv, GHToken: ghToken})
			if err != nil {
				return err
			}
			interactive := false
			if file, ok := stdin.(*os.File); ok {
				interactive = ui.IsTerminal(file)
			}
			a := &app.App{
				Git: r, Backend: b, Rules: text, Config: c, Out: stdout,
				UI: ui.New(stdin, stdout), Interactive: interactive,
				Opts: app.Options{DryRun: f.dryRun, Yes: f.yes, All: f.all, Verbose: f.verbose},
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt)
			defer signal.Stop(sig)
			done := make(chan struct{})
			defer close(done)
			go func() {
				select {
				case <-sig:
					cancel() // kills an in-flight backend call
					a.Interrupt()
					fmt.Fprintln(stderr, "aicommit: interrupted")
					os.Exit(2)
				case <-done:
				}
			}()
			err = a.Run(ctx)
			if errors.Is(err, app.ErrQuit) {
				fmt.Fprintln(stderr, "aicommit: quit")
				return exitError{2}
			}
			if errors.Is(err, app.ErrNoTTY) {
				fmt.Fprintln(stderr, "aicommit:", err)
				return exitError{2}
			}
			return err
		},
	}
	root.SetVersionTemplate("aicommit {{.Version}}\n")
	pf := root.PersistentFlags()
	pf.StringVar(&f.backend, "backend", "", "backend: command | openai")
	pf.StringVar(&f.model, "model", "", "model to use")
	pf.StringVar(&f.rules, "rules", "", "rules file for this run")
	pf.StringArrayVar(&f.exclude, "exclude", nil, "never send matching file contents (repeatable)")
	fl := root.Flags()
	fl.BoolVarP(&f.dryRun, "dry-run", "n", false, "show the proposal, commit nothing")
	fl.BoolVarP(&f.yes, "yes", "y", false, "accept every proposal without prompting")
	fl.BoolVarP(&f.all, "all", "a", false, "ignore the index and split all changes")
	fl.BoolVarP(&f.verbose, "verbose", "v", false, "print prompts and backend details")

	root.AddCommand(&cobra.Command{
		Use:   "config",
		Short: "Print the merged configuration and where each value came from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, _, err := loadConfig(f)
			if err != nil {
				return err
			}
			for _, w := range c.Warnings {
				fmt.Fprintln(stderr, "aicommit: warning:", w)
			}
			fmt.Fprint(stdout, c.Describe())
			return nil
		},
	})
	root.AddCommand(newInit(stdout))
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root
}

func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot(stdin, stdout, stderr)
	root.SetArgs(args)
	err := root.Execute()
	var ee exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.code
	default:
		fmt.Fprintln(stderr, "aicommit:", err)
		return 1
	}
}
```

`internal/cli/init.go`

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/TannerWhite/aicommit/internal/config"
	"github.com/TannerWhite/aicommit/internal/rules"
)

const starterConfig = `# aicommit user config. Precedence: flags > AICOMMIT_* env > repo .aicommit/config.toml > this file.
# Repo configs may NOT set backend, command, base_url, or api_key_env.

# Leave backend unset to auto-detect: copilot CLI, then GitHub Models via "gh auth login".
# backend = "command"
# command = ["copilot", "-s", "--no-ask-user", "--model", "claude-sonnet-5"]

# backend = "openai"
# base_url = "http://localhost:11434/v1"   # Ollama; default is GitHub Models
# model = "llama3.1"
# api_key_env = "OPENAI_API_KEY"

# rules = "rules.md"            # relative to this directory
# max_file_bytes = 100000
# max_total_bytes = 400000
# timeout = "120s"
# exclude = ["*.sql"]           # added to the built-in secret patterns
`

func newInit(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Write a starter config.toml and rules.md to the user config directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.UserDir(os.Getenv)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			files := map[string]string{"config.toml": starterConfig, "rules.md": rules.Default()}
			for name := range files {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					return fmt.Errorf("%s already exists; not overwriting", filepath.Join(dir, name))
				}
			}
			for name, body := range files {
				p := filepath.Join(dir, name)
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					return err
				}
				fmt.Fprintln(stdout, "wrote", p)
			}
			return nil
		},
	}
}
```

- [ ] **Step 4: Run all tests, expect PASS** (`go test -race ./...`), then manual check in a scratch repo:

```bash
go build -o bin/aicommit ./cmd/aicommit
cd "$(mktemp -d)" && git init -q && echo hi > a.txt && "$OLDPWD/bin/aicommit" --dry-run; echo "exit $?"
```

Expected: a proposed plan from the auto-detected backend, `exit 0`, and `git status` unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/cli && git commit -m "feat(cli): wire flags, config and init commands, and exit codes"
```

### Task 13: Release pipeline and docs

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/release.yml`, `README.md`, `CONTRIBUTING.md`
- Modify: `.github/workflows/ci.yml` (add snapshot job)

- [ ] **Step 1: Write** `.goreleaser.yaml`

```yaml
version: 2
project_name: aicommit
builds:
  - main: ./cmd/aicommit
    env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X github.com/TannerWhite/aicommit/internal/cli.Version={{.Version}}
      - -X github.com/TannerWhite/aicommit/internal/cli.Commit={{.ShortCommit}}
      - -X github.com/TannerWhite/aicommit/internal/cli.Date={{.Date}}
archives:
  - formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    files: [LICENSE, README.md]
checksum:
  name_template: checksums.txt
changelog:
  use: git
  groups:
    - { title: Features, regexp: '^feat(\(.+\))?!?:', order: 0 }
    - { title: Fixes, regexp: '^fix(\(.+\))?!?:', order: 1 }
    - { title: Other, order: 999 }
  filters:
    exclude: ['^docs', '^test', '^chore', '^ci']
```

Default archive names are `aicommit_<version>_<os>_<arch>.tar.gz`, which mise's `github` backend resolves without extra config.

- [ ] **Step 2: Write** `.github/workflows/release.yml`

```yaml
name: release
on:
  push:
    tags: ['v*']
permissions:
  contents: write
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: git config --global user.email ci@example.com && git config --global user.name ci
      - run: go test -race ./...
      - uses: goreleaser/goreleaser-action@v6
        with: { version: '~> v2', args: release --clean }
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

Add to `ci.yml` under `jobs:`:

```yaml
  snapshot:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: goreleaser/goreleaser-action@v6
        with: { version: '~> v2', args: release --snapshot --clean }
```

- [ ] **Step 3: Verify locally**

```bash
go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
ls dist/*.tar.gz dist/*.zip dist/checksums.txt
./dist/aicommit_linux_amd64_v1/aicommit --version
```

Expected: `check` passes; six archives plus `checksums.txt`; version prints a snapshot version.

- [ ] **Step 4: Write** `README.md` with these sections (use the technical-writing skill; no em dashes):
  1. One-paragraph summary: writes Conventional Commits messages; with nothing staged, splits changes into atomic commits. Note: "Not related to the npm `aicommits` package or `aicommit2`."
  2. Install: `mise use -g github:TannerWhite/aicommit`; Releases download (with `xattr -d com.apple.quarantine ./aicommit` for macOS direct downloads); `go install github.com/TannerWhite/aicommit/cmd/aicommit@latest`.
  3. Quick start: `aicommit` with staged changes; `aicommit` with nothing staged; `--dry-run`; the `[a]ccept/[e]dit/[r]egenerate/[s]kip/[q]uit` choices.
  4. Backends: auto-detection order; `command` example for copilot, `claude -p`, `ollama run`; `openai` examples for OpenAI, GitHub Models, Ollama.
  5. Configuration: file locations, precedence, the key table from the spec, and the repo-config restriction with the reason.
  6. Rules: lookup order; `aicommit init`; per-repo `.aicommit/rules.md` for team standards.
  7. What is sent to the model: diffs and new-file contents, size caps, default exclude patterns, binary handling, `--exclude`, `--verbose` to inspect prompts.
  8. Exit codes table.
  9. License: MIT.

`CONTRIBUTING.md`: prerequisites (Go 1.23+, git), `make test`, `make lint`, `go test ./internal/prompt -update` for golden files, Conventional Commits required, releases are cut by pushing a `vX.Y.Z` tag.

- [ ] **Step 5: Final verification**

```bash
make lint && make test && grep -rn $'\u2014' . --include='*.go' --include='*.md' --include='*.tmpl' --include='*.yaml' --include='*.toml' ; echo "em-dash grep exit $? (1 = none found)"
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "build: add goreleaser release pipeline and project docs"
```

---

### Task 14: Publish and migrate the author's setup (manual, needs user approval)

Not code in this repo. Each step that touches GitHub or the dotfiles needs explicit user confirmation first.

- [ ] **Step 1:** Create the public repo and push (confirm first):

```bash
gh repo create TannerWhite/aicommit --public --source . --remote origin --push
```

- [ ] **Step 2:** Confirm CI is green: `gh run list --limit 3`.
- [ ] **Step 3:** Tag and release (confirm first): `git tag v0.1.0 && git push origin v0.1.0`, then `gh release view v0.1.0` shows six archives and `checksums.txt`.
- [ ] **Step 4:** In the chezmoi source for `~/.config/mise/config.toml`, add `"github:TannerWhite/aicommit" = "latest"` under `[tools]`; `chezmoi apply`; `mise install`; `aicommit --version`.
- [ ] **Step 5:** Create `~/.config/aicommit/config.toml` (via chezmoi) with:

```toml
backend = "command"
command = ["copilot", "-s", "--no-ask-user", "--model", "claude-sonnet-5"]
```

- [ ] **Step 6:** Create `~/.config/aicommit/rules.md` (via chezmoi) from the current Flagship commit message standards (fetch the latest with `gh api repos/FlagshipEngineering/devops-foundations/contents/docs/commit-message-standards.md --jq .content | base64 -d`), condensed like the old script's RULES block, plus: `Never use em dashes in the commit message; use a regular hyphen (-) instead.`
- [ ] **Step 7:** Run `aicommit --dry-run` in a Flagship repo and a personal repo; check `aicommit config` shows the command backend from `user`.
- [ ] **Step 8:** After a few days of use, remove `dot_local/bin/executable_aicommit` from dotfiles, and the gh-models install script if nothing else uses it (confirm first).

---

## Spec Coverage

| Spec section | Task |
|---|---|
| Porcelain v2 -z parsing, renames, quoted paths | 2, 3 |
| Plan validation and one retry | 9, 11 |
| Size caps, binary, secrets | 3 |
| Backends: command, openai, auto-detect | 6, 7 |
| Rules lookup | 4 |
| Config precedence, repo key guard, unknown-key warning, `aicommit config` | 5, 12 |
| CLI flags, `init`, TTY check, exit codes | 10, 11, 12 |
| Split safety: index checks, hook rejection undo, Ctrl-C, summary, `--all` | 11, 12 |
| Testing and CI matrix | 1, every task, 13 |
| Release and distribution | 13, 14 |
| Docs | 13 |
| Migration | 14 |

