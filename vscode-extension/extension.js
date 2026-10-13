// save-token VS Code extension — skeleton.
//
// Responsibilities:
//   1. Bundle the save-token binary and install it to ~/.local/bin
//      (or the save-token.installDir setting) on startup.
//   2. Write an agent instruction file into the workspace so coding
//      agents prefix terminal commands with `save-token`.

const vscode = require("vscode");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { execFile } = require("child_process");

const EXT_ID = "save-token";

const AGENT_INSTRUCTION = `---
description: Run terminal commands through save-token to compress verbose output.
applyTo: "**"
---

# save-token

A terminal output compressor is installed as \`save-token\` (~/.local/bin).

- Run shell commands as: \`save-token <command> ...args\`
- It compresses verbose tool output (git, pytest, npm, eslint, etc.)
  and returns only the useful lines. Exit codes are preserved.
- Unsafe or interactive commands (sudo, vim, ssh, streaming servers)
  are detected automatically and run raw — no behavior change.
- Never wrap interactive commands, \`watch\`, \`tail -f\`, editors, pagers,
  or commands whose raw output you need byte-for-byte (e.g. \`cat\` of a
  file you will re-parse) — pass \`--no-compress\` or just run them directly.
`;

function arch() {
  switch (process.arch) {
    case "arm64": return "arm64";
    case "x64": return "amd64";
    default: return null;
  }
}

function platformAsset() {
  const map = { darwin: "darwin", linux: "linux", win32: "windows" };
  const p = map[process.platform] || null;
  const a = arch();
  if (!p || !a) return null;
  const ext = p === "windows" ? ".exe" : "";
  return `save-token-${p}-${a}${ext}`;
}

function installDir() {
  const cfg = vscode.workspace.getConfiguration(EXT_ID);
  let dir = cfg.get("installDir", "~/.local/bin");
  if (dir.startsWith("~")) dir = path.join(os.homedir(), dir.slice(1));
  return dir;
}

function bundledBinaryPath() {
  const asset = platformAsset();
  if (!asset) return null;
  const p = path.join(__dirname, "binaries", asset);
  return fs.existsSync(p) ? p : null;
}

function installedVersion(target) {
  return new Promise((resolve) => {
    execFile(target, ["--version"], { timeout: 5000 }, (err, stdout) => {
      resolve(err ? null : stdout.trim());
    });
  });
}

async function installBinary(showOutput) {
  const src = bundledBinaryPath();
  if (!src) {
    const msg = `save-token: no bundled binary for ${process.platform}/${process.arch}`;
    if (showOutput) vscode.window.showWarningMessage(msg);
    else console.warn(msg);
    return false;
  }
  const dir = installDir();
  const exe = process.platform === "win32" ? "save-token.exe" : "save-token";
  const target = path.join(dir, exe);
  try {
    fs.mkdirSync(dir, { recursive: true });
    const wantVersion = readBundledVersion();
    if (fs.existsSync(target)) {
      const have = await installedVersion(target);
      if (have && wantVersion && have.includes(wantVersion)) {
        if (showOutput) vscode.window.showInformationMessage(`save-token already installed (${have}).`);
        return true;
      }
    }
    const tmp = target + ".tmp";
    fs.copyFileSync(src, tmp);
    fs.chmodSync(tmp, 0o755);
    fs.renameSync(tmp, target);
    const ver = await installedVersion(target);
    const msg = `save-token installed to ${target}${ver ? ` (${ver})` : ""}`;
    if (showOutput) vscode.window.showInformationMessage(msg);
    else console.log(msg);
    return true;
  } catch (e) {
    vscode.window.showErrorMessage(`save-token install failed: ${e.message}`);
    return false;
  }
}

function readBundledVersion() {
  try {
    return JSON.parse(fs.readFileSync(path.join(__dirname, "package.json"), "utf8")).version;
  } catch {
    return null;
  }
}

function installAgentInstructions(showOutput) {
  const folder = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders[0];
  if (!folder) {
    if (showOutput) vscode.window.showWarningMessage("save-token: no workspace folder open.");
    return false;
  }
  const file = path.join(folder.uri.fsPath, ".github", "instructions", "save-token.instructions.md");
  try {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    const existing = fs.existsSync(file) ? fs.readFileSync(file, "utf8") : "";
    if (existing.includes("# save-token")) {
      if (showOutput) vscode.window.showInformationMessage(`Agent instructions already present: ${file}`);
      return true;
    }
    fs.writeFileSync(file, AGENT_INSTRUCTION);
    if (showOutput) vscode.window.showInformationMessage(`Wrote agent instructions to ${file}`);
    return true;
  } catch (e) {
    vscode.window.showErrorMessage(`save-token: failed to write agent instructions: ${e.message}`);
    return false;
  }
}

function activate(context) {
  const cfg = vscode.workspace.getConfiguration(EXT_ID);

  context.subscriptions.push(
    vscode.commands.registerCommand("save-token.install", () => installBinary(true)),
    vscode.commands.registerCommand("save-token.installAgentInstructions", () => installAgentInstructions(true)),
    vscode.commands.registerCommand("save-token.checkVersion", async () => {
      const exe = process.platform === "win32" ? "save-token.exe" : "save-token";
      const ver = await installedVersion(path.join(installDir(), exe));
      if (ver) vscode.window.showInformationMessage(ver);
      else vscode.window.showWarningMessage("save-token is not installed. Run: save-token: Install/Update Binary");
    })
  );

  if (cfg.get("autoInstall", true)) {
    installBinary(false).then((ok) => {
      if (ok && cfg.get("agentInstructions", true) && vscode.workspace.workspaceFolders) {
        installAgentInstructions(false);
      }
    });
  }
}

function deactivate() {}

module.exports = { activate, deactivate };
