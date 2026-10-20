<script>
  // The left column while Settings shows (owner, 2026-10-19: the chat list
  // swaps to the section names). The wordmark as on the chat list, "Back to
  // chats" where New chat sits, then one row per section; the chosen one has
  // the chat list's band and vermilion edge. It slides in from the left.
  import Icon from "../Icon.svelte";
  import Rule from "../Rule.svelte";
  import { slidePage } from "../../lib/motion.js";
  import { closeSettings, settingsPage, settingsSections } from "../../lib/settings/page.svelte.js";
</script>

<nav class="settings-nav" aria-label="Settings sections" in:slidePage={{ dir: -1 }}>
  <h1 class="wordmark"><span class="crush">Crush</span> <span class="ai">AI</span></h1>

  <button class="back" type="button" onclick={closeSettings}>
    <span class="arrow"><Icon name="disclosure" size={10} /></span>
    <span class="label">Back to chats</span>
  </button>

  <Rule marks="both" class="under-back" />

  <h2 class="heading">Settings</h2>
  <ul class="list">
    {#each settingsSections() as s (s.id)}
      <li>
        <button type="button" class="row" class:selected={settingsPage.section === s.id} aria-current={settingsPage.section === s.id ? "page" : undefined} onclick={() => (settingsPage.section = s.id)}>
          {s.label}
        </button>
      </li>
    {/each}
  </ul>
</nav>

<style>
  .settings-nav {
    position: relative;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  /* The wordmark and the box under it match the chat list's (ChatList.svelte). */
  .wordmark {
    flex: none;
    margin: calc(27 * var(--px)) 0 0 calc(20 * var(--px));
    font: 700 calc(32 * var(--px)) / calc(40 * var(--px)) var(--face-brand);
    color: var(--ink);
    white-space: nowrap;
  }
  .wordmark .crush {
    text-transform: uppercase;
  }
  .wordmark .ai {
    margin-left: calc(14 * var(--px));
    font-weight: 400;
    color: var(--slate);
  }
  .back {
    flex: none;
    display: flex;
    align-items: center;
    box-sizing: border-box;
    height: calc(52 * var(--px));
    margin: calc(22 * var(--px)) calc(20 * var(--px)) 0;
    padding-left: calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    color: var(--ink);
    cursor: pointer;
  }
  .arrow {
    display: flex;
    transform: rotate(180deg);
  }
  .back .label {
    position: relative;
    top: calc(1 * var(--px));
    margin-left: calc(13 * var(--px));
    font: var(--t-button);
  }
  .back:hover .label {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .settings-nav :global(.rule.under-back) {
    flex: none;
    margin-top: calc(66 * var(--px));
  }
  .heading {
    margin: calc(16 * var(--px)) 0 calc(6 * var(--px)) calc(20 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    overflow-y: auto;
  }
  .row {
    position: relative;
    isolation: isolate;
    display: flex;
    align-items: center;
    width: 100%;
    height: calc(48 * var(--px));
    padding: 0 calc(22 * var(--px));
    border: 0;
    background: none;
    font: var(--t-item);
    color: var(--ink);
    text-align: left;
    cursor: pointer;
  }
  /* The band keeps a small gap from both edges, as the chat list's rows do. */
  .row::before {
    content: "";
    position: absolute;
    inset: 0 calc(8 * var(--px));
    z-index: -1;
    background: var(--band);
    opacity: 0;
    transition: opacity 160ms ease-out;
  }
  .row:hover::before,
  .row.selected::before {
    opacity: 1;
  }
  .row::after {
    content: "";
    position: absolute;
    left: calc(8 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(3 * var(--px));
    background: var(--vermilion);
    transform: scaleY(0);
    transition: transform 220ms cubic-bezier(0.33, 1, 0.68, 1);
  }
  .row.selected::after {
    transform: scaleY(1);
  }
  .row:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(-3 * var(--px));
  }
</style>
