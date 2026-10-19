<script>
  // The bottom of the open list (desktop-design-5-6 2.21, 2.22, look 5.17;
  // LIST-028 to LIST-032, LIST-047, LIST-048), the list section "servers",
  // kept at the bottom while the chats scroll: the line of a failed start,
  // then under a rule the server rows, as many as leave room for one chat
  // (internal/ui/sessionhost/servers.go:94-104), then, while one is picked,
  // the hint " ↵ open · x stop · esc back".
  //
  // A row is 40 high with a 20 wide slot kept at its right end for the stop
  // cross, so nothing moves when a row is picked (draw.go:336-375). A server:
  // the arrow, its name (a link to http://localhost:<port>, underlined on
  // hover) and ":<port>"; with no name, "localhost:<port>" alone. A program
  // with several ports: the disclosure triangle (turned when open), its name
  // and "<N> ports"; opened, its servers follow, indented, named after the
  // program holding each port. The picked row has the band, the vermilion
  // edge and the stop cross. Clicks and keys: lib/leftlist/servers.js.
  import { onMount } from "svelte";
  import { SERVER_HINT, clickRow, foldOf, isPicked, rowTexts, shownRows, urlOf } from "../../lib/leftlist/servers.js";
  import { leftList } from "../../lib/leftlist/store.svelte.js";
  import Icon from "../Icon.svelte";
  import Rule from "../Rule.svelte";
  import StartFailure from "./StartFailure.svelte";

  // Picture px the room leaves: one chat row, the gap and rule above the
  // rows, and the hint's line.
  const CHAT_ROW = 81;
  const ABOVE = 18;
  const HINT = 32;
  const ROW = 40;

  let box = $state(null);

  const rows = $derived(shownRows());
  const picked = $derived(rows.find((row) => isPicked(row)) ?? null);
  const pairs = SERVER_HINT.trim()
    .split(" · ")
    .map((pair) => {
      const at = pair.indexOf(" ");
      return { key: pair.slice(0, at), text: pair.slice(at + 1) };
    });

  // A picked row that has gone lets go by itself (servers.go:139-151).
  $effect(() => {
    if (leftList.picked && !picked) leftList.picked = null;
  });

  // How many rows fit in the list's scrolling part with room for one chat.
  onMount(() => {
    const scroll = box?.closest(".scroll");
    if (!scroll) return;
    const update = () => {
      const px = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1;
      leftList.serverRoom = Math.max(Math.floor((scroll.clientHeight / px - CHAT_ROW - ABOVE - HINT) / ROW), 0);
    };
    const observer = new ResizeObserver(update);
    observer.observe(scroll);
    update();
    return () => {
      observer.disconnect();
      leftList.serverRoom = Infinity;
    };
  });

  function keyOf(row) {
    if (row.ports > 0) return `fold:${foldOf(row.server)}`;
    return `${row.child ? "child" : "port"}:${row.server.port}`;
  }

  function open(row) {
    return Boolean(leftList.openFolds[foldOf(row.server)]);
  }
</script>

<div class="servers-part" bind:this={box}>
  {#if leftList.failure}
    <StartFailure text={leftList.failure} />
  {/if}
  {#if rows.length > 0}
    <Rule marks="both" class="servers-rule" />
    <ul class="servers" aria-label="Servers">
      {#each rows as row (keyOf(row))}
        {@const texts = rowTexts(row)}
        <li class="server" class:picked={isPicked(row)} class:child={row.child} class:fold={row.ports > 0} data-port={row.server.port}>
          <button
            class="server-row"
            type="button"
            title={row.ports > 0 ? texts.label : urlOf(row.server.port)}
            aria-expanded={row.ports > 0 ? open(row) : undefined}
            onclick={() => clickRow(row)}
          >
            {#if row.ports > 0}
              <Icon name="disclosure" class={open(row) ? "triangle open" : "triangle"} size={12} />
            {:else}
              <Icon name="arrow-up-right" size={12} />
            {/if}
            <span class="label">{texts.label}</span>
            {#if texts.right}<span class="right" class:count={row.ports > 0}>{texts.right}</span>{/if}
          </button>
          <button class="stop" type="button" tabindex="-1" aria-label="Stop {texts.label}" onclick={() => clickRow(row, true)}>
            {#if isPicked(row)}<Icon name="close" size={12} />{/if}
          </button>
        </li>
      {/each}
    </ul>
    {#if picked}
      <!-- Its text is the terminal's hint (servers.go:37), drawn as key pairs. -->
      <p class="hint">{#each pairs as pair, i (pair.key)}{#if i > 0}<span class="sep"> · </span>{/if}<span class="key">{pair.key}</span>{" "}<span class="text">{pair.text}</span>{/each}</p>
    {/if}
  {/if}
</div>

<style>
  /* Pinned to the bottom of the list's scrolling part, as the terminal draws
     them on the list's last rows (shownServers, servers.go:94-104): the chats
     scroll under them, and at the end of the list they sit in their place. */
  .servers-part {
    position: sticky;
    bottom: 0;
    z-index: 1;
    background: var(--paper);
  }
  .servers-part :global(.rule.servers-rule) {
    margin-top: calc(18 * var(--px));
  }
  .servers {
    margin: 0;
  }
  .server {
    position: relative;
  }

  /* 40 high, text from 23, the 20 wide stop slot at the right end. */
  .server-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    box-sizing: border-box;
    width: 100%;
    height: calc(40 * var(--px));
    padding: 0 calc(44 * var(--px)) 0 calc(23 * var(--px));
    text-align: left;
    color: var(--slate);
  }
  .child .server-row {
    padding-left: calc(55 * var(--px));
  }
  .label {
    flex: 0 1 auto;
    min-width: 0;
    font: var(--t-small);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .server:not(.fold) .server-row:hover .label {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .right {
    flex: none;
    margin-left: auto;
    font: var(--t-agent-mono);
    color: var(--slate);
    white-space: nowrap;
  }
  .right.count {
    font: var(--t-small);
  }
  .server-row :global(.triangle) {
    transition: none;
  }
  .server-row :global(.triangle.open) {
    transform: rotate(90deg);
  }

  /* The picked row: the band between faint lines, the vermilion edge. */
  .picked .server-row {
    background: var(--band);
  }
  .picked .server-row::before {
    content: "";
    position: absolute;
    inset: 0;
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
    pointer-events: none;
  }
  .picked .server-row::after {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
  }
  .picked .right {
    color: var(--ink);
  }

  .stop {
    position: absolute;
    top: 0;
    right: calc(20 * var(--px));
    display: grid;
    place-items: center;
    width: calc(20 * var(--px));
    height: calc(40 * var(--px));
    padding: 0;
    color: var(--slate);
  }
  .stop:hover {
    color: var(--ink);
  }
  .server-row:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(-2 * var(--px));
  }

  /* The hint in the help footer's style (look 5.1). */
  .hint {
    box-sizing: border-box;
    min-height: calc(32 * var(--px));
    margin: 0;
    padding: calc(5 * var(--px)) calc(20 * var(--px)) 0 calc(23 * var(--px));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .key {
    font: var(--t-agent-mono);
    color: var(--ink);
  }
  .text {
    font: var(--t-small);
    color: var(--mark);
  }
  .sep {
    white-space: pre;
    font: var(--t-small);
    color: var(--rule);
  }
</style>
