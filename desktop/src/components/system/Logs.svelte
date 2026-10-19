<script>
  // The log viewer (desktop-design-5-6 2.28, MISC-016): the project's
  // crush.log, the last 1,000 lines, then new lines as they are written.
  // ↑/↓, PgUp/PgDn, Home/End scroll; F turns following on or off; Esc closes.
  // The text can be selected and copied.
  import { Channel } from "@tauri-apps/api/core";
  import { onMount, tick } from "svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { logsFollow, logsTail, logsUnfollow } from "../../lib/engine/commands.js";
  import { parseLogLine } from "../../lib/system/loglines.js";

  const ID = "logs";
  const TAIL = 1000;
  const KEEP = 5000; // lines kept while following

  let entries = $state([]);
  let missing = $state(false);
  let truncated = $state(false);
  let path = $state("");
  let error = $state("");
  let following = $state(true);
  let box = $state(null);
  let serial = 0;

  const help = $derived([
    { key: "↑/↓", text: "scroll" },
    { key: "f", text: following ? "stop following" : "follow" },
    { key: "esc", text: "close" },
  ]);

  function add(lines) {
    const fresh = [];
    for (const line of lines ?? []) {
      const entry = parseLogLine(line);
      if (entry) fresh.push({ id: ++serial, ...entry });
    }
    if (fresh.length === 0) return;
    const atEnd = !box || box.scrollTop + box.clientHeight >= box.scrollHeight - 4;
    const next = entries.concat(fresh);
    entries = next.length > KEEP ? next.slice(next.length - KEEP) : next;
    if (following && atEnd) tick().then(() => box && (box.scrollTop = box.scrollHeight));
  }

  function scroll(by) {
    if (!box) return;
    if (by === "home") box.scrollTop = 0;
    else if (by === "end") box.scrollTop = box.scrollHeight;
    else box.scrollTop += by;
  }

  function onKey(event, combo) {
    const line = 20;
    const page = Math.max(line, (box?.clientHeight ?? 400) - line);
    switch (combo) {
      case "ArrowDown":
        scroll(line);
        return true;
      case "ArrowUp":
        scroll(-line);
        return true;
      case "PageDown":
        scroll(page);
        return true;
      case "PageUp":
        scroll(-page);
        return true;
      case "Home":
        scroll("home");
        return true;
      case "End":
        scroll("end");
        return true;
      case "F":
      case "f":
        following = !following;
        if (following) scroll("end");
        return true;
      default:
        return false;
    }
  }

  onMount(() => {
    let followId = null;
    let gone = false;
    const channel = new Channel();
    channel.onmessage = (message) => {
      if (!gone && following) add(message?.lines);
    };
    (async () => {
      try {
        const tail = await logsTail(TAIL);
        if (gone) return;
        missing = tail.missing === true;
        truncated = tail.truncated === true || (tail.lines?.length ?? 0) >= TAIL;
        path = tail.path ?? "";
        add(tail.lines);
        await tick();
        scroll("end");
        if (missing) return;
        const followed = await logsFollow(channel);
        if (gone) logsUnfollow(followed.followId).catch(() => {});
        else followId = followed.followId;
      } catch (e) {
        if (!gone) error = e?.message ?? String(e);
      }
    })();
    return () => {
      gone = true;
      if (followId) logsUnfollow(followId).catch(() => {});
    };
  });
</script>

<Sheet id={ID} title="Logs" width="wide" maxHeight={760} {help} {onKey}>
  {#if error}
    <p class="note">{error}</p>
  {:else if missing}
    <p class="note">Looks like you are not in a crush project. No logs found.</p>
  {:else}
    {#if truncated}
      <p class="note">Showing last {TAIL} lines. Full logs available at: {path}</p>
    {/if}
    <div class="lines" bind:this={box} tabindex="-1">
      {#each entries as entry (entry.id)}
        <div class="line">
          <span class="time">{entry.time}</span>
          <span class="level {entry.level.toLowerCase()}">{entry.level.slice(0, 4)}</span>
          <span class="msg">{entry.msg}</span>
          {#each entry.parts as [key, value], i (i)}
            <span class="key">{key}=</span><span class="value">{value}</span>
          {/each}
        </div>
      {/each}
    </div>
    <p class="state">{following ? "Following new lines" : "Not following"}</p>
  {/if}
</Sheet>

<style>
  p {
    margin: 0;
  }
  .note,
  .state {
    font: var(--t-small);
    color: var(--slate);
  }
  .note {
    margin-bottom: calc(10 * var(--px));
    overflow-wrap: anywhere;
  }
  .state {
    margin-top: calc(10 * var(--px));
  }
  .lines {
    flex: 1 1 auto;
    min-height: calc(200 * var(--px));
    overflow: auto;
    border: 1px solid var(--rule-control);
    background: var(--code-bg, var(--band));
    padding: calc(8 * var(--px)) calc(10 * var(--px));
    font: var(--t-agent-mono);
    color: var(--ink);
    user-select: text;
    outline: none;
  }
  .line {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .line > span + span {
    margin-left: 0.6em;
  }
  .line > .value {
    margin-left: 0;
  }
  .time,
  .key {
    color: var(--slate);
  }
  .level.error {
    color: var(--error-ink);
  }
  .level.warn {
    color: var(--warn-ink, var(--ink));
  }
</style>
