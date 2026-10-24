// The agent CLIs Settings → Accounts lists, in the sidebar's order (owner,
// 2026-10-24): the command that signs one in, and whether the tool can keep
// more than one login. Claude Code, Codex and Grok keep accounts in folders
// of their own (internal/config/accounts.go, AccountKinds); Antigravity and
// OpenCode sign in once, through their own CLI.
export const AGENT_CLIS = [
  { kind: "claude-code", name: "Claude Code", login: ["claude", "auth", "login"], accounts: true },
  { kind: "codex-cli", name: "Codex", login: ["codex", "login"], accounts: true },
  // Grok's detail also holds the separate xAI subscription sign-in.
  { kind: "grok-cli", name: "Grok", login: ["grok", "login"], accounts: true, subscription: true },
  // AGY has no login subcommand; `agy models` is what fetches a token when
  // none is stored (internal/agent/cliagent/limits.go, agyToken). OpenCode's
  // `auth login` asks its questions in a terminal, so both run in a tab.
  { kind: "agy-cli", name: "Antigravity", login: ["agy", "models"], accounts: false, terminal: true },
  { kind: "opencode-cli", name: "OpenCode Go", login: ["opencode", "auth", "login"], accounts: false, terminal: true },
  { kind: "opencode-zen-cli", name: "OpenCode Zen", login: ["opencode", "auth", "login"], accounts: false, terminal: true },
];

export function cliOf(kind) {
  return AGENT_CLIS.find((cli) => cli.kind === kind) ?? null;
}
