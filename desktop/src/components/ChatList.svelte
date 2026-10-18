<script>
  // The left column: the wordmark, New chat, search, and the chats by day. The
  // open chat sits on a band with a vermilion edge; its branches hang below it
  // on a thin tree.
  import Icon from "./Icon.svelte";
  import Rule from "./Rule.svelte";

  let { groups } = $props();
</script>

<nav class="chats" aria-label="Chats">
  <h1 class="wordmark"><span class="crush">Crush</span> <span class="ai">AI</span></h1>

  <button class="new-chat" type="button">
    <Icon name="plus" />
    <span class="label">New chat</span>
  </button>

  <label class="search">
    <Icon name="search" />
    <input type="search" placeholder="Search chats…" aria-label="Search chats" />
    <kbd>⌘K</kbd>
  </label>

  <Rule marks="both" class="under-search" />

  {#each groups as group (group.label)}
    <section class="group" aria-label={group.label}>
      <h2 class="day">{group.label}</h2>
      <ul class="list">
        {#each group.chats as chat (chat.id)}
          <li class="chat" class:selected={chat.selected} class:expanded={chat.branches && chat.expanded}>
            <button class="row" type="button" aria-current={chat.selected ? "page" : undefined} aria-expanded={chat.branches ? chat.expanded : undefined}>
              <span class="title">{chat.title}</span>
              <span class="agent">{chat.agent}</span>
              {#if chat.time}
                <span class="time">{chat.time}</span>
              {/if}
              {#if chat.branches}
                <Icon name="chevron" class="chevron" />
              {/if}
              {#if chat.state}
                <span class="state {chat.state.kind}">
                  <Icon name={chat.state.kind === "working" ? "working" : "clock"} />
                  <span>{chat.state.text}</span>
                </span>
              {/if}
            </button>
            {#if chat.branches && chat.expanded}
              <ul class="branches" aria-label="Branches of {chat.title}">
                {#each chat.branches as branch (branch.id)}
                  <li class="branch">
                    <button class="branch-row" type="button">
                      <span class="title">{branch.title}</span>
                      <span class="agent">{branch.agent}</span>
                      <span class="time">{branch.time}</span>
                    </button>
                  </li>
                {/each}
              </ul>
              <Rule marks="start" class="after-branches" />
            {/if}
          </li>
        {/each}
      </ul>
    </section>
  {/each}

  <Rule marks="both" class="end-of-list" />
</nav>

<style>
  /* Vertical positions follow the approved picture at 12/12 (column top = 0). */
  .chats {
    position: relative;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }

  .wordmark {
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

  .new-chat {
    display: flex;
    align-items: flex-start;
    box-sizing: border-box;
    height: calc(52 * var(--px));
    margin: calc(22 * var(--px)) calc(20 * var(--px)) 0;
    padding-left: calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    color: var(--ink);
  }
  .new-chat :global(.icon) {
    margin-top: calc(15 * var(--px));
  }
  .new-chat .label {
    margin: calc(12 * var(--px)) 0 0 calc(13 * var(--px));
    font: var(--t-button);
  }

  .search {
    display: flex;
    align-items: flex-start;
    box-sizing: border-box;
    height: calc(50 * var(--px));
    margin: calc(17 * var(--px)) calc(20 * var(--px)) 0;
    padding: 0 calc(12 * var(--px)) 0 calc(11 * var(--px));
    border: 1px solid var(--rule-control);
    color: var(--ink);
    cursor: text;
  }
  .search :global(.icon) {
    margin-top: calc(14 * var(--px));
  }
  .search input {
    flex: 1;
    min-width: 0;
    height: calc(26 * var(--px));
    margin: calc(13 * var(--px)) 0 0 calc(12 * var(--px));
    padding: 0;
    border: 0;
    outline: 0;
    background: none;
    font: var(--t-field);
    appearance: none;
  }
  .search input::placeholder {
    color: var(--slate);
    opacity: 1;
  }
  .search input::-webkit-search-cancel-button {
    appearance: none;
  }
  .search:focus-within {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .search kbd {
    margin-top: calc(14 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }

  .chats :global(.rule.under-search) {
    margin-top: calc(25 * var(--px));
  }

  .day {
    margin: calc(14 * var(--px)) 0 0 calc(20 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }

  .list {
    margin-top: calc(13 * var(--px));
  }

  /* A chat row: title over agent, time on the right, state on the agent line. */
  .chat + .chat {
    border-top: 1px solid var(--rule);
  }
  .chat.expanded + .chat {
    border-top: 0;
  }
  /* Rows are flex columns so the button does not centre its text vertically. */
  .row {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    justify-content: flex-start;
    box-sizing: border-box;
    width: 100%;
    height: calc(81 * var(--px));
    padding: calc(12 * var(--px)) 0 0 calc(23 * var(--px));
  }
  .row .title {
    display: block;
    font: var(--t-item);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    padding-right: calc(80 * var(--px));
  }
  .row .agent {
    display: block;
    margin-top: calc(5 * var(--px));
    font: var(--t-meta);
    color: var(--slate);
  }
  .row .time {
    position: absolute;
    right: calc(48 * var(--px));
    top: calc(29 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .row :global(.chevron) {
    position: absolute;
    right: calc(19 * var(--px));
    top: calc(37 * var(--px));
    color: var(--ink);
  }
  .state {
    position: absolute;
    right: calc(24 * var(--px));
    top: calc(45 * var(--px));
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }

  /* The open chat: a band between faint lines, with a vermilion edge. */
  .selected .row {
    background: var(--band);
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
    height: calc(77 * var(--px) + 2px);
  }
  .selected .row .agent {
    margin-top: calc(3 * var(--px));
  }
  .selected .row::before {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: -1px;
    bottom: -1px;
    width: calc(4 * var(--px));
    background: var(--vermilion);
  }

  /* Branches hang from a thin tree under the open chat. */
  .branches {
    padding-bottom: calc(12 * var(--px));
  }
  .branch {
    position: relative;
  }
  .branch::before {
    content: "";
    position: absolute;
    left: calc(35 * var(--px));
    top: 0;
    bottom: 0;
    width: max(1px, calc(1.5 * var(--px)));
    background: var(--figure);
  }
  .branch:last-child::before {
    bottom: auto;
    height: calc(35 * var(--px));
  }
  .branch::after {
    content: "";
    position: absolute;
    left: calc(35 * var(--px));
    top: calc(34 * var(--px));
    width: calc(16 * var(--px));
    height: max(1px, calc(1.5 * var(--px)));
    background: var(--figure);
  }
  .branch-row {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    justify-content: flex-start;
    box-sizing: border-box;
    width: 100%;
    height: calc(72 * var(--px));
    padding: calc(13 * var(--px)) 0 0 calc(64 * var(--px));
  }
  .branch-row .title {
    display: block;
    font: var(--t-branch);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    padding-right: calc(80 * var(--px));
  }
  .branch-row .agent {
    display: block;
    font: var(--t-agent-mono);
    line-height: calc(20 * var(--px));
    color: var(--slate);
  }
  .branch-row .time {
    position: absolute;
    right: calc(48 * var(--px));
    top: calc(25 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }

  .chats :global(.rule.after-branches) {
    color: var(--rule-frame);
  }
  .chats :global(.rule.end-of-list) {
    margin-top: 0;
  }

  .row:hover .title,
  .branch-row:hover .title {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
</style>
