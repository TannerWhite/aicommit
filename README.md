# aicommit

`aicommit` writes Conventional Commits messages for a git repository using an AI backend. If changes are staged, it proposes one message for the staged changes. If nothing is staged, it groups all working tree changes into atomic commits and proposes a message for each group.

Not related to the npm `aicommits` package or `aicommit2`.

## Install

With mise:

```sh
mise use -g github:TannerWhite/aicommit
```

From a release: download the archive for your platform from the [Releases page](https://github.com/TannerWhite/aicommit/releases), extract it, and put `aicommit` on your `PATH`. On macOS, clear the quarantine attribute on a directly downloaded binary:

```sh
xattr -d com.apple.quarantine ./aicommit
```

With Go:

```sh
go install github.com/TannerWhite/aicommit/cmd/aicommit@latest
```

## Quick start

Commit staged changes with one generated message:

```sh
git add internal/parser.go
aicommit
```

Split all changes into atomic commits when nothing is staged:

```sh
aicommit
```

Show the proposed message or plan without changing the index or history:

```sh
aicommit --dry-run
```

For each proposal, `aicommit` prompts for a choice:

| Key | Action |
|---|---|
| `a` | Accept and commit. |
| `e` | Edit the message in `$EDITOR` (through `git commit -e`), then commit. |
| `r` | Regenerate. `aicommit` asks for feedback and includes it in the next prompt. |
| `s` | Skip this group and leave its files uncommitted (split mode only). |
| `q` | Quit. |

In split mode, `aicommit` prints a summary on exit that lists committed groups with short SHAs, skipped groups, and files left uncommitted. Ctrl-C undoes the staging of the group in progress, prints the summary, and exits with code 2. Earlier commits remain.

### Flags

| Flag | Description |
|---|---|
| `-n`, `--dry-run` | Show the proposal and commit nothing. |
| `-y`, `--yes` | Accept every proposal without prompting. |
| `-a`, `--all` | Ignore the index and split all changes, staged and unstaged. Before committing the first accepted group, aicommit unstages everything already staged. |
| `--backend <name>` | Backend: `command` or `openai`. |
| `--model <name>` | Model to use. |
| `--rules <path>` | Rules file for this run. |
| `--exclude <glob>` | Never send contents of matching files. Repeatable. |
| `-v`, `--verbose` | Print prompts and backend details. API keys are never printed. |
| `--version` | Print the version. |

Subcommands:

- `aicommit config` prints the merged configuration and the source of each value.
- `aicommit init` writes a starter `config.toml` and `rules.md` to the user config directory. It refuses to overwrite existing files.

If stdin is not a terminal and `--yes` is not set, `aicommit` exits with code 2 instead of waiting for input.

## Backends

### Auto-detection

If `backend` is not set, `aicommit` checks in this order:

1. `copilot` is on `PATH`: runs `copilot -s --no-ask-user`, adding `--model <model>` if a model is set.
2. `gh auth token` returns a token: calls GitHub Models (`https://models.github.ai/inference`) with that token. The default model is `openai/gpt-4o-mini`.
3. Otherwise, `aicommit` exits with an error that lists the setup options and points to `aicommit init`.

### `command`

Runs a configured argv without a shell, writes the prompt to stdin, and reads the message from stdout. Set it in your user config:

```toml
backend = "command"

# GitHub Copilot CLI
command = ["copilot", "-s", "--no-ask-user", "--model", "claude-sonnet-5"]

# Claude Code
# command = ["claude", "-p"]

# Ollama
# command = ["ollama", "run", "llama3.1"]
```

### `openai`

Calls `<base_url>/chat/completions` on an OpenAI-compatible endpoint. `api_key_env` names the environment variable that holds the key. If `base_url` is unset, `aicommit` uses GitHub Models. `model` is required for any endpoint other than GitHub Models.

OpenAI:

```toml
backend = "openai"
base_url = "https://api.openai.com/v1"
model = "gpt-4o-mini"
api_key_env = "OPENAI_API_KEY"
```

GitHub Models, with the key from `gh auth token`:

```toml
backend = "openai"
model = "openai/gpt-4o-mini"
```

Ollama:

```toml
backend = "openai"
base_url = "http://localhost:11434/v1"
model = "llama3.1"
```

## Configuration

Configuration files use TOML:

- User: `$XDG_CONFIG_HOME/aicommit/config.toml` if `XDG_CONFIG_HOME` is set, else `~/.config/aicommit/config.toml` on Linux and macOS. On Windows, the directory is `aicommit` under `os.UserConfigDir()` (usually `%AppData%`).
- Repo: `<repo root>/.aicommit/config.toml`

Precedence, highest first:

1. Flags
2. Environment variables: `AICOMMIT_BACKEND`, `AICOMMIT_MODEL`, `AICOMMIT_BASE_URL`, `AICOMMIT_API_KEY_ENV`, `AICOMMIT_RULES`, `AICOMMIT_TIMEOUT`
3. Repo config
4. User config
5. Auto-detection and built-in defaults

| Key | Default | Allowed in repo config |
|---|---|---|
| `backend` | auto-detect | no |
| `command` (argv array) | none | no |
| `base_url` | GitHub Models for `openai` | no |
| `api_key_env` | none | no |
| `model` | per backend | yes |
| `rules` | see [Rules](#rules) | yes |
| `max_file_bytes` | `100000` | yes |
| `max_total_bytes` | `400000` | yes |
| `timeout` (duration string) | `"120s"` | yes |
| `exclude` (glob array) | see [What is sent to the model](#what-is-sent-to-the-model) | yes, adds to the list |

A repo config cannot set `backend`, `command`, `base_url`, or `api_key_env`, in any letter case. These keys would let a cloned repository run arbitrary commands or send diffs and credentials to another server. If a repo config sets one, `aicommit` exits with an error that names the key. Set these keys in your user config.

Unknown keys produce a warning. `exclude` values from every layer and from `--exclude` are added to the built-in patterns.

A relative `rules` path is resolved against the directory that holds the config file for the user config, and against the repo root for the repo config.

## Rules

Rules are the instructions that tell the model how to write messages. `aicommit` uses the first rules source that exists:

1. `--rules <path>` or `AICOMMIT_RULES`
2. The `rules` key in the repo config, else `.aicommit/rules.md` at the repo root
3. The `rules` key in the user config, else `rules.md` in the user config directory
4. The built-in Conventional Commits rules

A rules file named by a flag, environment variable, or `rules` key must exist. A missing `rules.md` falls through to the next source. A rules file replaces the built-in rules. It is not merged with them.

Run `aicommit init` to write the built-in rules to the user config directory as a starting point. To share team standards, commit a `.aicommit/rules.md` file to the repository.

## What is sent to the model

`aicommit` sends the rules and, for each changed file, the path, status, and either the diff or, for new files, the full contents.

Size limits:

- If a file's diff or contents exceed `max_file_bytes`, `aicommit` truncates it and adds a `[truncated: N bytes omitted]` marker.
- If the combined change set exceeds `max_total_bytes`, `aicommit` replaces the largest entries with an `[omitted: N bytes, over total size limit]` marker until the total fits.

Excluded files are listed by path and status only, marked `(contents excluded)`. Their contents are never sent. The default exclude patterns are:

```
.env  .env.*  *.pem  *.key  id_rsa*  id_ed25519*  *.p12  *.pfx
```

A pattern matches either the full path or the file name. Binary files are also listed by name only. `aicommit` treats a file as binary if it has a NUL byte in the first 8 KB or git reports it as binary.

Add patterns with `--exclude` or the `exclude` config key:

```sh
aicommit --exclude '*.sql' --exclude 'fixtures/*'
```

Run with `--verbose` to print the exact prompts sent to the backend.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success, or nothing to commit. |
| `1` | Error. |
| `2` | Quit by the user, interrupted with Ctrl-C, or stdin is not a terminal and `--yes` is not set. |

## License

MIT. See [LICENSE](LICENSE).
