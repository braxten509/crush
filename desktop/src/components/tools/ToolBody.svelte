<script>
  // What shows under an open tool's line (CHAT-049 to CHAT-079): the early
  // state, the error, plain output with its program colors, highlighted code
  // with a line-number gutter, a picture, a loaded skill, a diff, a note,
  // quiet Markdown, the to-do list, question answers, the Docker server
  // table. Each body starts at 10 lines and "… (N lines hidden) [click or
  // space to expand]" (CHAT-052, CHAT-113) until the tool is expanded.
  //   parts     the view's body (lib/tools/registry.js)
  //   expanded  every line shows
  //   onexpand  a click on a hidden-lines note
  //   xOffset   a diff's side-scroll (Shift+← →)
  import { ansiLines } from "../../lib/render/ansi.js";
  import { highlightLines, languageForPath } from "../../lib/render/code.js";
  import { codeLines, hiddenText, markdownLines, plainLines } from "../../lib/tools/body.js";
  import { show } from "../../lib/tools/nodes.js";
  import { openViewer } from "../../lib/stores/ui.svelte.js";
  import DiffView from "./DiffView.svelte";
  import ToolMarkdown from "./ToolMarkdown.svelte";

  let { parts = [], expanded = false, sessionId = "", messageId = "", onexpand = null, xOffset = 0 } = $props();

  // One fragment per line of program output, colors carried across lines.
  function outputLines(lines) {
    return ansiLines(lines.join("\n"));
  }

  function codeView(part) {
    const view = codeLines(part.text, part.offset, expanded);
    const lines = highlightLines(view.lines.join("\n"), languageForPath(part.path));
    return { ...view, nodes: lines };
  }

  function picture(part) {
    openViewer({ src: `data:${part.mime};base64,${part.data}`, name: part.mime, mime: part.mime });
  }
</script>

{#snippet hidden(count)}
  {#if count > 0}
    {#if onexpand}
      <button class="hidden-note" type="button" onclick={onexpand}>{hiddenText(count)}</button>
    {:else}
      <p class="hidden-note">{hiddenText(count)}</p>
    {/if}
  {/if}
{/snippet}

<div class="body">
  {#each parts as part, i (i)}
    {#if part.type === "early"}
      <p class="early {part.tone}">{part.text}</p>
    {:else if part.type === "error"}
      <p class="error-line"><span class="tag {part.tag === 'WARN' ? 'warn' : 'error'}">{part.tag}</span><span class="message {part.tag === 'WARN' ? 'warn' : 'error'}">{part.text}</span></p>
    {:else if part.type === "plain"}
      {@const view = plainLines(part.text, expanded)}
      <div class="block output">
        {#each outputLines(view.lines) as line, n (n)}
          <div class="line" {@attach show(() => line)}></div>
        {/each}
      </div>
      {@render hidden(view.hidden)}
    {:else if part.type === "code"}
      {@const view = codeView(part)}
      <div class="block code" style:--digits={view.digits}>
        {#each view.nodes as line, n (n)}
          <div class="line"><span class="gutter">{view.first + n}</span><span class="text" {@attach show(() => line)}></span></div>
        {/each}
      </div>
      {@render hidden(view.hidden)}
    {:else if part.type === "image"}
      <p class="loaded"><span class="label">Loaded Image</span><span class="arrow">→</span><span class="mime">{part.mime}</span><span class="size">{part.size}</span></p>
      <button class="thumb" type="button" onclick={() => picture(part)} aria-label="Open the picture">
        <img src={`data:${part.mime};base64,${part.data}`} alt="" draggable="false" />
      </button>
    {:else if part.type === "skill"}
      <p class="loaded"><span class="label">Loaded Skill</span><span class="arrow">→</span><span class="mime">{part.name}</span><span class="size">{part.description}</span></p>
    {:else if part.type === "diff"}
      <div class="diff-box">
        <DiffView files={part.files} mode="auto" maxLines={expanded ? 0 : 10} lineNumbers={true} wrap={false} {xOffset} cutAbove={part.cutAbove} {onexpand} />
      </div>
    {:else if part.type === "note"}
      <p class="note-line"><span class="tag">{part.tag}</span><span class="message">{part.text}</span></p>
    {:else if part.type === "markdown"}
      {@const view = markdownLines(part.text, expanded)}
      <ToolMarkdown text={view.text} {sessionId} {messageId} />
      {@render hidden(view.hidden)}
    {:else if part.type === "todos"}
      <ul class="todos">
        {#each part.items as todo, n (n)}
          <li class={todo.status}><span class="todo-icon">{todo.icon}</span>{todo.text}</li>
        {/each}
      </ul>
    {:else if part.type === "started"}
      <p class="todos"><span class="todo-icon">{part.icon}</span>{part.text}</p>
    {:else if part.type === "questions"}
      <ul class="questions">
        {#each part.blocks as block, n (n)}
          <li>
            <p class="question"><span class="todo-icon">✓</span>{block.question}{#each block.parts as answer, a (a)}<span class="answer {answer.tone ?? ''}">{a === 0 ? " " : ", "}{answer.text}</span>{/each}</p>
            {#each block.notes as note, k (k)}<p class="answer-note">╰ {note}</p>{/each}
          </li>
        {/each}
      </ul>
    {:else if part.type === "servers"}
      <table class="servers">
        <tbody>
          {#each part.rows as row, n (n)}
            <tr><td class="server">{row.name}</td><td>{row.description}</td></tr>
          {/each}
        </tbody>
      </table>
      {#if part.more}<p class="early">{part.more}</p>{/if}
    {:else if part.type === "empty"}
      <p class="early">{part.text}</p>
    {/if}
  {/each}
</div>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: calc(8 * var(--px));
    min-width: 0;
  }
  .body p {
    margin: 0;
  }
  .early {
    font: var(--t-body-small);
    color: var(--slate);
  }
  .error-line,
  .note-line,
  .loaded {
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    min-width: 0;
    font: var(--t-body-small);
  }
  .tag {
    flex: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
  }
  .tag.error,
  .message.error {
    color: var(--error-ink);
  }
  .tag.warn,
  .message.warn {
    color: var(--amber-ink);
  }
  .message {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .block {
    min-width: 0;
    padding: calc(8 * var(--px)) calc(12 * var(--px));
    background: var(--code-bg);
    font: var(--t-code);
  }
  .line {
    display: flex;
    min-width: 0;
    min-height: calc(24 * var(--px));
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .output .line {
    display: block;
  }
  .gutter {
    flex: none;
    width: calc(var(--digits) * 1ch);
    margin-right: calc(12 * var(--px));
    text-align: right;
    color: var(--slate);
    user-select: none;
  }
  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .label {
    font-weight: 500;
  }
  .arrow,
  .size {
    color: var(--slate);
  }
  .mime {
    font: 400 calc(17 * var(--px)) / calc(26 * var(--px)) var(--face-mono);
  }
  .thumb {
    align-self: flex-start;
    padding: 0;
    border: 1px solid var(--rule-faint);
    cursor: zoom-in;
  }
  .thumb img {
    display: block;
    max-height: calc(96 * var(--px));
    max-width: 100%;
  }
  .diff-box {
    min-width: 0;
  }
  .todos,
  .questions {
    margin: 0;
    padding: 0;
    list-style: none;
    font: var(--t-body-small);
  }
  .todo-icon {
    display: inline-block;
    width: calc(22 * var(--px));
    color: var(--slate);
  }
  .todos .completed {
    color: var(--slate);
  }
  .answer {
    color: var(--slate);
  }
  .answer.no,
  .answer.skipped {
    color: var(--slate);
  }
  .answer-note {
    padding-left: calc(22 * var(--px));
    color: var(--slate);
  }
  .servers {
    border-collapse: collapse;
    font: var(--t-code);
  }
  .servers td {
    padding: 0 calc(16 * var(--px)) 0 0;
    white-space: pre;
  }
  .hidden-note {
    display: block;
    margin: 0;
    padding: 0;
    font: var(--t-small);
    color: var(--slate);
    text-align: left;
  }
  button.hidden-note {
    cursor: pointer;
  }
  button.hidden-note:hover {
    color: var(--ink);
  }
</style>
