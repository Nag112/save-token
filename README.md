# save-token

**Terminal output compression for AI coding agents.**

`save-token` is a small Go CLI that runs a shell command, compresses its
stdout/stderr with a rule-based engine, and prints condensed output — while
preserving the original exit code. Point your AI agent (Copilot, Claude, etc.)
at it and long command outputs cost far fewer tokens, without losing error
messages, test failures, or other critical lines.

```
save-token <command...>
```

## How it works

1. A **safety gate** inspects the command. Interactive, streaming, or unsafe
   commands (`sudo`, `vim`, `ssh`, `tail -f`, …) are refused and executed raw
   (fail-open) — output is never mangled when compression would be wrong.
2. A **processor router** picks the first matching formatter for the command:
   `git`, test runners (pytest / jest / `go test` / cargo test), linters
   (eslint / ruff), build output, package installs (pip / npm / cargo / go),
   with a **generic** fallback for everything else.
3. The **engine** applies compression gates (ratio checks, critical-line
   recovery) so that error lines, tracebacks, and failure summaries are never
   dropped.
4. `&&` / `;` command chains are split and compressed per segment.

The stdout you see is the compressed output; the process exit code is exactly
the original command's exit code. A timeout exits `124` with partial
compressed output.

## Install

### One-line install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/nagc/save-token/main/install.sh | bash
```

Downloads the latest release for your OS/arch into `~/.local/bin`
(override with `SAVE_TOKEN_INSTALL_DIR`). Make sure `~/.local/bin` is on
your `PATH`.

### From a local checkout

```bash
./install.sh            # builds with Go and installs to ~/.local/bin
./install.sh --from-release   # force download from GitHub Releases
```

### VS Code extension

The [`vscode-extension/`](vscode-extension/) folder is a VS Code extension that:

- Bundles the `save-token` binary and installs it to `~/.local/bin`
  on startup (configurable via `save-token.installDir` / `save-token.autoInstall`).
- Writes an **agent instruction file** so Copilot prefers
  `save-token <command>` over raw terminal calls, automatically reducing
  token usage in chat (toggle with `save-token.agentInstructions`).

Commands: `save-token: Install/Update Binary`,
`save-token: Install Agent Instructions (this workspace)`,
`save-token: Check Installed Version`.

## Usage

```bash
save-token pytest                 # compressed test output, real pytest exit code
save-token git status             # condensed git output
save-token "npm install && npm test"   # per-segment chain compression

save-token --dry-run --show-removed go build ./...   # compression diffstat, no replacement
save-token --no-compress make all       # run raw, unmodified output
save-token --timeout 600 make           # override timeout (default 300s)
save-token --version
```

### Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Print compression stats to stderr, compressed output to stdout |
| `--show-removed` | With `--dry-run`, show a removed-lines breakdown |
| `--no-compress` | Run the command with unmodified output |
| `--timeout N` | Override the command timeout in seconds (default 300) |
| `--version` | Print version and exit |

## Configuration

Config is loaded from defaults, then `~/.config/save-token/config.json`
(override the directory with `SAVE_TOKEN_CONFIG_DIR`), then `SAVE_TOKEN_*`
environment variables.

Example `~/.config/save-token/config.json`:

```json
{
  "enabled": true,
  "wrap_timeout": 300,
  "min_compression_ratio": 0.0,
  "disabled_processors": ["lint"]
}
```

Notable knobs: `generic_truncate_threshold`, `generic_keep_head` /
`generic_keep_tail`, `recover_critical_lines`, `max_diff_hunk_lines`,
`lint_example_count`, `max_traceback_lines`, `chars_per_token`.
See [`internal/config/config.go`](internal/config/config.go) for the full set.

## Development

```bash
go build ./...          # build
go test ./...           # unit + golden parity tests
./build.sh --local      # build current-platform binary into ./bin
./build.sh              # cross-compile release assets + extension binaries
```

Requires Go 1.27+.

## Repo layout

```
cmd/save-token/       CLI entrypoint (flags, exit-code passthrough)
internal/gate/        command safety policy, shell syntax, chain splitting
internal/exec/        command execution, capture, caps, timeout
internal/critical/    never-drop line detection (errors, tracebacks, …)
internal/engine/      router, ratio gates, generic clean pass, recovery
internal/processors/  generic, git, test, lint, build, package-install
internal/config/      defaults, config.json, env overrides
internal/diffstat/    --dry-run --show-removed reporting
vscode-extension/     VS Code extension (binary install + agent instructions)
install.sh            curl-able installer
build.sh              cross-compile & release packaging
```

`save-token` is a pure-Go port of the Python `token-saver` VS Code hook;
the Python tree is kept as the behavioral reference (see
[`PLAN.md`](PLAN.md)).

## License

MIT
