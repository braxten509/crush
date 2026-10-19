<script>
  // A tool that never folds into a group (Question, Agent, Agentic Fetch;
  // CHAT-072, CHAT-078, CHAT-079): the same line and body as a group's tools,
  // in a box of its own (4.6), always open; a click, or Space while it is
  // selected, shows the whole body. c/y copies it (CHAT-056).
  //   item  a ToolItem (lib/types.js): {kind: "tool", call}
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { prompts } from "../../lib/stores/prompts.svelte.js";
  import { copyWithNotice } from "../../lib/tools/clip.js";
  import { TOOL_COPIED, toolCopyText } from "../../lib/tools/copy.js";
  import { registerItem } from "../../lib/tools/open.svelte.js";
  import { SHELL_STEP } from "../../lib/tools/shell.js";
  import { toolStatus } from "../../lib/tools/status.js";
  import ToolEntry from "./ToolEntry.svelte";

  let { item } = $props();

  let entry = $state(null);
  let shift = $state(0);

  $effect(() => {
    if (!item.key) return;
    return registerItem(item.key, {
      toggle: () => entry?.toggle(),
      copy: () => copyWithNotice(toolCopyText(item.call, toolStatus(item.call, prompts.callStatus)), TOOL_COPIED),
      scroll: (dir) => {
        shift = Math.max(0, shift + dir * SHELL_STEP);
        return true;
      },
    });
  });
</script>

<div class="tool-box">
  <ToolEntry bind:this={entry} call={item.call} itemKey={item.key} nested={false} workingDir={conn.project?.cwd ?? ""} sessionId={item.sessionId} xOffset={shift} />
</div>

<style>
  .tool-box {
    border: 1px solid var(--rule-control);
  }
  .tool-box :global(.tool-line) {
    min-height: calc(42 * var(--px));
  }
</style>
