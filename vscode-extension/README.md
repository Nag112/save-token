# save-token for VS Code

Installs the `save-token` terminal-output compressor and teaches your coding
agents to use it.

## What it does

- **Installs the binary** — bundles platform binaries for macOS/Linux/Windows
  (arm64/amd64) and installs to `~/.local/bin/save-token`
  (`%USERPROFILE%\.local\bin\save-token.exe` on Windows) on startup
  (or via the `save-token: Install/Update Binary` command).
- **Writes agent instructions** — creates
  `.github/instructions/save-token.instructions.md` in your workspace telling
  Copilot/agents to run terminal commands as `save-token <command>` so output
  comes back compressed (git, pytest, npm, eslint, pip, etc.) with exit codes
  preserved. Unsafe/interactive commands automatically run raw.

## Commands

| Command | Purpose |
| --- | --- |
| `save-token: Install/Update Binary` | Copy the bundled binary to the install dir |
| `save-token: Install Agent Instructions (this workspace)` | (Re)write the agent instruction file |
| `save-token: Check Installed Version` | Show the installed binary version |

## Settings

- `save-token.autoInstall` (default `true`) — install on startup if missing/outdated
- `save-token.installDir` (default `~/.local/bin`) — install target
- `save-token.agentInstructions` (default `true`) — auto-write agent instructions

## Building the VSIX

From the repo root:

```bash
./build.sh            # cross-compile into vscode-extension/binaries/
cd vscode-extension
npx @vscode/vsce package   # produces save-token-<version>.vsix
code --install-extension save-token-*.vsix
```

## Skeleton status

This extension deliberately does **not** hijack the integrated terminal.
Compression happens because agents (or you) invoke `save-token <command>`.
Terminal-profile integration is a possible future enhancement.
