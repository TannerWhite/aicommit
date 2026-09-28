# aicommit Go Rewrite - Design

- Date: 2026-09-27
- Status: Approved
- Module: `github.com/TannerWhite/aicommit`
- License: MIT

## Goal

Rewrite the personal bash `aicommit` script (chezmoi:
`dot_local/bin/executable_aicommit`) as a public, open source Go CLI that
coworkers and other users can install with mise or a direct binary download.
The tool generates Conventional Commits messages with an AI model. When
changes are staged it writes one message; when nothing is staged it splits
all changes into logically separate commits and walks the user through each.

The name `aicommit` is kept. The README must state that this project is not
the npm `aicommits` package or `aicommit2`.

## Problems in the current script this design fixes

1. `git status --porcelain` parsing breaks on renames (`old -> new`) and on
   quoted paths (spaces, unicode).
2. The model's split plan is never validated (missing, duplicated, or
   invented paths), so `git add` can fail after some commits already exist.
3. Only untracked files are size-capped; tracked diffs are unbounded and
   binary untracked files are sent verbatim.
4. JSON extraction from surrounding prose uses a fragile `sed` range.
5. Secrets (`.env`, keys) are sent to the model with no warning or exclusion.
6. The message cannot be edited by hand, only regenerated.
7. Hook rejections and backend failures abort via `set -e` without a clear
   message or cleanup.
8. Requires bash 4 (`declare -A`, `mapfile`), which fails on stock macOS.
9. Backend choice, rules, and install hints are hard-coded and
   Flagship-specific.

## Non-goals (v1)

- Native Anthropic (or other provider-specific) API backend.
- Fetching rules from a URL; named rule presets.
- Homebrew, a tap, or any other package manager publishing.
- Signed or notarized macOS binaries.
- Pure-Go git libraries (go-git, git2, gix); the tool shells out to `git`.

## Architecture

Go 1.23 or later. Dependencies: `github.com/spf13/cobra` (CLI) and
`github.com/BurntSushi/toml` (config). Everything else uses the standard
library; HTTP uses `net/http` with no SDKs.

```
cmd/aicommit/main.go        entry point; wires cobra commands
internal/config/            load and merge settings, track source of each value,
                            reject disallowed repo keys
internal/rules/             built-in default rules (go:embed) and override lookup
internal/git/               git subprocess wrapper: status -z, diffs, add,
                            restore --staged, commit (-F, -e)
internal/changes/           builds the change set: renames, untracked, binary
                            detection, size caps, secret exclusions
internal/backend/           Backend interface
  command.go                prompt on stdin to any CLI, read stdout, timeout
  openai.go                 POST /chat/completions to an OpenAI-compatible endpoint
  detect.go                 auto-detection when no backend is configured
internal/prompt/            prompt templates (go:embed) for single and split modes
internal/plan/              parse and strictly validate the split plan JSON
internal/ui/                interactive loop, TTY detection, run summary
```

Dependency direction: `ui` -> `plan` / `backend`; `plan` -> `changes`;
`changes` -> `git`. Lower packages never import `ui`.

```go
type Backend interface {
    Generate(ctx context.Context, prompt string) (string, error)
}
```

## Backends

### `command`

Runs a configured argv (no shell) with the prompt on stdin and reads the
response from stdout. Example:
`["copilot", "-s", "--no-ask-user", "--model", "claude-sonnet-5"]`.
Works with any CLI that reads a prompt on stdin (`claude -p`, `llm`,
`ollama run <model>`). On failure, reports the exit code and the tail of
stderr.

### `openai`

Calls `<base_url>/chat/completions` on any OpenAI-compatible endpoint
(OpenAI, GitHub Models, Ollama, LM Studio, OpenRouter, Azure). Settings:
`base_url`, `model`, `api_key_env` (name of the environment variable holding
the key). For GitHub Models, the key may come from `gh auth token`. On
failure, reports the HTTP status and the API error message. The key is never
printed or logged, including in `--verbose` output.

### Auto-detection (no backend configured)

1. `copilot` on `PATH` -> `command` backend with the copilot CLI.
2. Else `gh` is installed and authenticated -> `openai` backend pointed at
   GitHub Models with the token from `gh auth token`.
3. Else error listing the three setup options (install copilot, run
   `gh auth login`, configure an openai endpoint) and pointing to
   `aicommit init`.

Default model for each detected backend is defined in code and overridable
with `--model` or config.

## Rules

The binary embeds a generic Conventional Commits rule set. Lookup order,
first match wins:

1. `--rules <path>`
2. Repo rules file: `rules` key in `.aicommit/config.toml`, else
   `.aicommit/rules.md` at the repo root
3. User rules file: `rules` key in `~/.config/aicommit/config.toml`, else
   `~/.config/aicommit/rules.md`
4. Built-in default

A rules file explicitly named in config or by flag that does not exist is an
error. A missing implicit file (`rules.md`) falls through to the next level.
The rules file replaces the built-in rules entirely; it is not merged.

User config directory follows `os.UserConfigDir()` conventions, with
`$XDG_CONFIG_HOME/aicommit` honored and `~/.config/aicommit` used on Linux
and macOS.

## Configuration

Format: TOML. Locations:

- User: `~/.config/aicommit/config.toml`
- Repo: `<repo root>/.aicommit/config.toml`

Precedence, highest first: flags > `AICOMMIT_*` environment variables > repo
config > user config > auto-detection / built-in defaults.

Keys:

| Key | Default | Repo config allowed |
|---|---|---|
| `backend` | auto-detect | no |
| `command` (argv array) | - | no |
| `base_url` | - | no |
| `api_key_env` | - | no |
| `model` | per backend | yes |
| `rules` | see Rules | yes |
| `max_file_bytes` | 100000 | yes |
| `max_total_bytes` | 400000 | yes |
| `timeout` (duration string) | `120s` | yes |
| `exclude` (glob array) | see Secrets | yes (adds to user list) |

Security rule: a repo config can never set `backend`, `command`, `base_url`,
or `api_key_env`. These would let a cloned repo run arbitrary commands or
send diffs and credentials to an attacker's server. If a repo config sets
one, loading fails with an error naming the key and the reason (fail
closed, never silently ignored).

Unknown keys produce a warning, not an error.

`aicommit config` prints the merged configuration and the source of each
value (flag, env, repo file, user file, detected, default).

## CLI

```
aicommit [flags]            staged -> one message; nothing staged -> split mode
  -n, --dry-run             show proposed message or plan, commit nothing
  -y, --yes                 accept everything without prompting
  -a, --all                 ignore the index and split all changes
      --backend <name>      command | openai
      --model <name>        model to use
      --rules <path>        rules file for this run
      --exclude <glob>      never send matching file contents (repeatable)
  -v, --verbose             print the prompt and backend details (no secrets)
      --version

aicommit config             print merged config with sources
aicommit init               write starter config.toml and rules.md to the
                            user config dir (refuses to overwrite)
```

Interactive choices:

- Single mode: `[a]ccept`, `[e]dit`, `[r]egenerate`, `[q]uit`
- Split mode: `[a]ccept`, `[e]dit`, `[r]egenerate`, `[s]kip`, `[q]uit`

`edit` opens `$EDITOR` through `git commit -e`. `regenerate` asks for
feedback text and includes it in the next prompt.

If stdin is not a terminal and `--yes` is not set, the tool exits with code
2 instead of waiting for input.

## Secrets and exclusions

Default exclude patterns: `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`,
`id_ed25519*`, `*.p12`, `*.pfx`. Binary files (NUL byte in the first 8 KB,
or git reports them as binary) are also excluded. Users add patterns with
`--exclude` or the `exclude` config key.

Excluded files are still listed to the model by path and status, with the
note `(contents excluded)`, so they can be placed in a commit group. Their
contents are never sent.

## Data flow

### Mode selection

- Index has changes and `--all` not set: single mode.
- Index empty, or `--all` set: split mode.
- Working tree clean: print "nothing to commit" and exit 0.

With `--all` and a non-empty index, staged and unstaged changes are
analyzed together. Immediately before staging the first accepted group, the
tool unstages everything with `git restore --staged :/` so the per-group
index checks below start from an empty index. `--dry-run` never modifies
the index.

### Single mode

1. `git diff --cached -z --name-status` lists files; `changes` builds the
   change set with exclusions and size caps applied.
2. `prompt` renders rules + change set; `backend.Generate` returns text.
3. Clean the text: strip code fence lines and surrounding quotes; require a
   non-empty subject line (else treat as a backend error).
4. UI loop. Accept runs `git commit -F <tempfile>`; edit runs
   `git commit -e -F <tempfile>`.

### Split mode

1. `git status --porcelain=v2 -z --untracked-files=all` lists entries.
   Unmerged entries (merge conflicts) cause an error before any model call.
2. `changes` builds entries: path, status, original path (renames), and
   either a diff (`git diff -- <path>`) or new-file content. Excluded and
   binary files are listed by name only.
3. The plan prompt asks for exactly one JSON object:
   `{"commits":[{"files":["..."],"message":"..."}]}`. Renames are
   identified by their new path.
4. `plan` parses and validates:
   - parses as JSON (if the raw text is not valid JSON, extract the first
     balanced top-level `{...}` object and try again)
   - every change-set path appears in exactly one group
   - no path outside the change set
   - every message has a non-empty subject line
   On failure, retry once with the validation errors appended to the
   prompt. On second failure, exit 1 and print the raw output. Nothing has
   been staged or committed at this point.
5. Print the full plan, then run the UI loop per group. Accept runs
   `git add -A -- <files>` (for renames, both old and new paths) then
   `git commit -F <tempfile>`. Skip leaves files unstaged.

### Size limits

- Per file: content or diff over `max_file_bytes` is truncated with a
  `[truncated: N bytes omitted]` marker.
- Total: if the combined change set exceeds `max_total_bytes`, the largest
  entries are replaced with an `[omitted: N bytes, over total size limit]`
  marker until the total fits.

### Flags

- `--dry-run` stops after displaying the message or plan; the index and
  history are unchanged.
- `--yes` accepts every proposal without prompting.

## Error handling

Before any commit (safe exit, nothing modified): backend failure, invalid
JSON after retry, plan validation failure, or git status failure -> exit 1
with a clear message; model problems include the raw output.

During split mode:

- `git commit` fails (hook rejection or other error): unstage the group's
  files with `git restore --staged -- <files>`, print git's output, offer
  `[r]etry`, `[e]dit`, `[s]kip`, `[q]uit`. Earlier commits remain.
- Ctrl-C (SIGINT): undo the current group's staging, then exit 2.
- Before staging each group, verify the index is empty; after staging,
  verify it contains only this group's paths. If either check fails, stop
  without committing.
- On every exit, print a summary: committed groups (with short SHAs),
  skipped groups, and files left unstaged.

Backends: configurable timeout (default 120s). Error messages as described
under Backends.

Config: unknown key -> warning; disallowed repo key -> error; missing
explicitly named rules file -> error.

Exit codes:

- 0: success, or nothing to commit
- 1: error
- 2: user quit or declined, interrupted, or no TTY without `--yes`

## Testing

Unit tests (standard `testing`, table-driven):

- `plan`: valid plans; missing, duplicate, and invented paths; empty
  messages; JSON embedded in prose; retry path.
- `changes`: renames, paths with spaces and unicode, deletions, binary
  detection, exclusions, both size caps.
- `config`: precedence, source tracking, disallowed repo keys fail closed,
  unknown-key warnings.
- `rules`: lookup order and explicit-missing-file error.
- `prompt`: golden files for rendered prompts.
- `backend`: `openai` against `httptest.Server` (success, HTTP errors,
  timeout, key never logged); `command` against stub scripts (non-zero
  exit, hang).

Integration tests: real git in `t.TempDir()` repos with a fake backend
returning scripted responses. Cover single and split flows with `--yes`,
then assert on `git log` and the index. Failure cases: rejecting
`commit-msg` hook (staging undone), index changed mid-run (abort), merge
conflicts (refuse to run).

UI tests: scripted answers through an `io.Reader` with TTY detection
overridable for tests.

CI (GitHub Actions): `gofmt` check, `go vet`, `staticcheck`,
`go test -race` on ubuntu, macos, and windows, and a goreleaser snapshot
build on pull requests. No live model calls in CI; an optional
`make smoke` target runs against a real backend locally.

## Release and distribution

- Semver, starting at `v0.1.0`. A `v*` tag triggers
  `.github/workflows/release.yml` running goreleaser.
- `CGO_ENABLED=0` builds for linux, darwin, windows on amd64 and arm64.
  `.tar.gz` archives (`.zip` on windows), `checksums.txt`, and a changelog
  from Conventional Commit history.
- Version, commit, and date injected via `-ldflags` and shown by
  `aicommit --version`.
- Default goreleaser archive names
  (`aicommit_<version>_<os>_<arch>.tar.gz`) so mise's `github` backend
  resolves assets without extra config.

Install paths documented in the README:

- `mise use -g github:TannerWhite/aicommit`
- Download from GitHub Releases and place the binary on `PATH`
  (macOS direct downloads may need `xattr -d com.apple.quarantine`)
- `go install github.com/TannerWhite/aicommit/cmd/aicommit@latest`

## Repository docs

- `README.md`: purpose (including split mode), install, quick start,
  backend setup, config reference, what data is sent to the model and how
  exclusions work, and the name disambiguation note.
- `LICENSE` (MIT), `CONTRIBUTING.md`.
- `docs/specs/` (this document) and `docs/plans/`.
- `aicommit init` writes a commented example `config.toml` and a starter
  `rules.md`.

## Migration of the author's setup

1. Add `"github:TannerWhite/aicommit" = "latest"` to the chezmoi-managed
   `~/.config/mise/config.toml`.
2. Create `~/.config/aicommit/config.toml` with `backend = "command"` and
   `command = ["copilot", "-s", "--no-ask-user", "--model", "claude-sonnet-5"]`
   to keep work commits on the Copilot Enterprise license.
3. Create `~/.config/aicommit/rules.md` containing the Flagship commit
   message standards plus the no-em-dash rule; optionally add
   `.aicommit/rules.md` to Flagship repos.
4. After the Go version is verified, remove
   `dot_local/bin/executable_aicommit` from dotfiles, and the gh-models
   install script if nothing else uses it.
