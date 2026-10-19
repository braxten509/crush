// User and MCP entries (desktop-design-5-6 2.2; CMD-057 to CMD-059): custom
// commands from G-7's GET …/commands (skills among them), MCP prompts from
// GET …/mcp/prompts, and the arguments that fill them. Custom command
// arguments are $NAME placeholders, replaced longest name first so $FOOBAR
// is never cut by $FOO (u:pal.args_longest_first; 9.4 D20).

const PLACEHOLDER = /\$([A-Z][A-Z0-9_]*)/g;

// The placeholders a content uses, in order of first appearance.
export function placeholders(content) {
  const seen = [];
  for (const m of String(content ?? "").matchAll(PLACEHOLDER)) if (!seen.includes(m[1])) seen.push(m[1]);
  return seen;
}

// content with each $NAME of values replaced, longest names first.
export function fillArguments(content, values) {
  let out = String(content ?? "");
  const names = Object.keys(values ?? {}).sort((a, b) => b.length - a.length || (a < b ? -1 : 1));
  for (const name of names) out = out.split(`$${name}`).join(String(values[name] ?? ""));
  return out;
}

// "PROJECT_NAME" → "Project Name" (the arguments sheet's labels).
export function titleCase(text) {
  return String(text ?? "")
    .toLowerCase()
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((w) => w[0].toUpperCase() + w.slice(1))
    .join(" ");
}

// The arguments sheet's fields for a custom command.
export function commandArguments(command) {
  if (Array.isArray(command?.arguments) && command.arguments.length > 0) {
    return command.arguments.map((a) => ({ id: a.name, title: a.title || titleCase(a.name), description: "", required: a.required !== false }));
  }
  return placeholders(command?.content).map((name) => ({ id: name, title: titleCase(name), description: "", required: true }));
}

// …and for an MCP prompt.
export function promptArguments(prompt) {
  return (prompt?.arguments ?? []).map((a) => ({ id: a.id, title: a.title || titleCase(a.id), description: a.description ?? "", required: a.required === true }));
}
