<script>
  // The hooks that ran before a tool (CHAT-055), one line each, in aligned
  // columns: "Hook", the name (a long path cut on the left, "…/x.sh"), the
  // matcher, "→" and the outcome: OK, OK Rewrote Output, or Denied and the
  // reason; a denial colors the label and the arrow. Port of
  // chat/tool_hooks.go:19-179. Not drawn for nested (compact) tools.
  //   lines  lib/tools/hooks.js callHookLines(call)
  let { lines = [] } = $props();

  const matchers = $derived(lines.some((h) => h.matcher !== ""));
</script>

{#if lines.length > 0}
  <div class="hooks" class:matchers>
    {#each lines as hook, i (i)}
      <span class="label" class:denied={hook.denied}>Hook</span>
      <span class="name">{hook.name}</span>
      {#if matchers}<span class="matcher">{hook.matcher}</span>{/if}
      <span class="arrow" class:denied={hook.denied}>→</span>
      <span class="detail">
        {#if hook.detail.tone === "denied"}
          <span class="denied">{hook.detail.text}</span>{#if hook.detail.reason}<span class="reason"> {hook.detail.reason}</span>{/if}
        {:else}
          <span class="ok">{hook.detail.text}</span>{#if hook.detail.rewrote}<span class="rewrote"> {hook.detail.rewrote}</span>{/if}
        {/if}
      </span>
    {/each}
  </div>
{/if}

<style>
  .hooks {
    display: grid;
    grid-template-columns: auto auto auto 1fr;
    column-gap: 1ch;
    align-items: baseline;
    justify-content: start;
    font: 400 calc(17 * var(--px)) / calc(24 * var(--px)) var(--face-mono);
    color: var(--slate);
  }
  .hooks.matchers {
    grid-template-columns: auto auto auto auto 1fr;
  }
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    color: var(--ink);
  }
  .name {
    color: var(--ink);
    white-space: pre;
  }
  .matcher,
  .detail {
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .denied {
    color: var(--error-ink);
  }
  .ok {
    color: var(--ink);
  }
</style>
