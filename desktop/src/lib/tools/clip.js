// Copying a tool, a group or a shell (CHAT-044, CHAT-056, CHAT-090): the
// text goes through clipboard_write; success shows the row's notice, as the
// terminal's ReportInfo; a failed write warns "Failed to copy to clipboard"
// (common/common.go:251-268).
import { clipboard_write } from "../engine/commands.js";
import { notify } from "../stores/ui.svelte.js";

export const COPY_FAILED = "Failed to copy to clipboard";

export async function copyWithNotice(text, notice, write = clipboard_write, tell = notify) {
  try {
    await write(text);
  } catch {
    tell("warn", COPY_FAILED);
    return false;
  }
  tell("info", notice);
  return true;
}
