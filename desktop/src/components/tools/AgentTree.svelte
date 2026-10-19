<script>
  // An Agent or Agentic Fetch call's task and its helper's tools (CHAT-078,
  // CHAT-079): the small-caps "TASK" (or "PROMPT") tag and the prompt, on
  // one line until the tool is expanded; the helper's tool calls as compact
  // lines hanging from a hairline tree (--figure, as the chat list's branch
  // tree); the activity mark until the call has a result. The nested tools
  // are the call's children, else the helper chat the messages store keeps
  // ("<message id>$$<tool call id>", loaded with its parent and updated by
  // live events). Port of chat/agent.go:139-206,212-374.
  //   call        the agent's ToolCallView
  //   agent       the view's {tag, prompt, running}
  //   expanded    the prompt shows whole
  //   workingDir  the project folder (a Bash "cd <it> && " is hidden)
  import { agentChatId, messages } from "../../lib/stores/messages.svelte.js";
  import { prompts } from "../../lib/stores/prompts.svelte.js";
  import { callsFromMessages, chatList } from "../../lib/tools/agent.js";
  import { toolView } from "../../lib/tools/registry.js";
  import { toolStatus } from "../../lib/tools/status.js";
  import ActivityMark from "../timeline/ActivityMark.svelte";
  import ToolHeader from "./ToolHeader.svelte";

  let { call, agent, expanded = false, workingDir = "" } = $props();

  const nested = $derived.by(() => {
    if (call.children?.length) return call.children;
    const chat = messages.bySession[agentChatId(call.messageId, call.id)];
    return callsFromMessages(chatList(chat));
  });
  const prompt = $derived(expanded ? agent.prompt : agent.prompt.replaceAll("\n", " "));

  export function probe() {
    return {
      tag: agent.tag.toUpperCase(),
      prompt: agent.prompt,
      running: agent.running,
      nested: nested.map((c) => {
        const view = toolView(c, { status: toolStatus(c, prompts.callStatus), workingDir, compact: true });
        return { id: c.id, name: c.name, main: view.main, status: toolStatus(c, prompts.callStatus) };
      }),
    };
  }
</script>

<div class="agent">
  <p class="task" class:expanded>
    <span class="tag">{agent.tag}</span><span class="prompt">{prompt}</span>
  </p>
  {#if nested.length > 0}
    <ul class="tree">
      {#each nested as child (child.id)}
        {@const status = toolStatus(child, prompts.callStatus)}
        <li class="branch">
          <ToolHeader view={toolView(child, { status, workingDir, compact: true })} {status} />
        </li>
      {/each}
    </ul>
  {/if}
  {#if agent.running}
    <div class="working"><ActivityMark size="line" /></div>
  {/if}
</div>

<style>
  .agent {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .task {
    display: flex;
    align-items: baseline;
    gap: calc(12 * var(--px));
    margin: 0;
    min-width: 0;
    font: var(--t-body-small);
  }
  .tag {
    flex: none;
    font: var(--t-label);
    font-variant-caps: all-small-caps;
    letter-spacing: var(--track-label);
    color: var(--slate);
  }
  .prompt {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .expanded .prompt {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  /* The helper's tools hang from a thin tree, as branches do in the chat
     list (ChatList.svelte .branch). */
  .tree {
    margin: calc(6 * var(--px)) 0 0;
    padding: 0;
    list-style: none;
  }
  .branch {
    position: relative;
    display: flex;
    align-items: center;
    min-height: calc(36 * var(--px));
    padding-left: calc(28 * var(--px));
  }
  .branch::before {
    content: "";
    position: absolute;
    left: calc(7 * var(--px));
    top: 0;
    bottom: 0;
    width: max(1px, calc(1.5 * var(--px)));
    background: var(--figure);
  }
  .branch:last-child::before {
    bottom: 50%;
  }
  .branch::after {
    content: "";
    position: absolute;
    left: calc(7 * var(--px));
    top: 50%;
    width: calc(16 * var(--px));
    height: max(1px, calc(1.5 * var(--px)));
    background: var(--figure);
  }
  .working {
    display: flex;
    align-items: center;
    height: calc(26 * var(--px));
    margin-top: calc(6 * var(--px));
  }
</style>
