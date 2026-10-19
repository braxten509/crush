<script>
  // The init prompt (START-002; the terminal's initializeView and its keys,
  // internal/ui/model/onboarding.go:32-119, keys.go:323-338): shown while
  // GET project/needs-init says so. Yep! makes a new chat and sends the
  // engine's init prompt in it; Nope goes to the landing page; both mark the
  // project initialized (start.svelte.js answerInit). Enter answers with the
  // chosen button, ← → Tab switch, y Y yes, n N Esc no. Like the terminal's,
  // the block sits at the bottom of the space, above the composer.
  //
  // The keys work while the prompt has the focus or nothing has it (the
  // terminal gives them to the prompt; here the chat-list search and the
  // composer keep their typing). The prompt takes the focus when it shows,
  // unless the chat-list search has it (START-021 focuses it on a bad -s).
  import { onMount, tick } from "svelte";
  import { bindKey } from "../../lib/keys.js";
  import { focusField } from "../../lib/stores/composer.svelte.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { setFocus } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { searchFocused } from "../list/listfocus.js";
  import StartLayer from "./StartLayer.svelte";
  import { answerInit, screen, selectInit, start } from "./start.svelte.js";
  import { initKey } from "./startmodel.js";
  import { shown } from "./shown.js";

  const QUESTION = "Would you like to initialize this project?";
  const HINT_KEY = "Ctrl+P";
  const ASK = "Would you like to initialize now?";

  const active = $derived(screen() === "init");
  const initFile = $derived(ws.config?.options?.initialize_as || "AGENTS.md");
  const desc = $derived(`When I initialize your codebase I examine the project and put the result into an ${initFile} file which serves as general context.`);

  let block = $state(null);
  let yes = $state(null);
  let no = $state(null);

  function ownsFocus() {
    const el = document.activeElement;
    return !el || el === document.body || el === document.documentElement || Boolean(block?.contains(el));
  }

  async function answer(choice) {
    const done = await answerInit(choice === "yes");
    if (done) focusField();
  }

  function onKey(event) {
    if (!active || start.answering || !ownsFocus()) return false;
    const action = initKey(event);
    if (!action) return false;
    if (action === "switch") {
      selectInit(start.initSelected === "yes" ? "no" : "yes");
      (start.initSelected === "yes" ? yes : no)?.focus();
    } else if (action === "enter") {
      answer(start.initSelected);
    } else {
      answer(action);
    }
    return true;
  }

  onMount(() => {
    const combos = ["y", "Y", "n", "N", "Escape", "Alt+Escape", "Enter", "ArrowLeft", "ArrowRight", "Tab"];
    const unbind = combos.map((combo) => bindKey("global", combo, onKey));
    return () => unbind.forEach((off) => off());
  });

  // Take the focus once when the prompt shows.
  $effect(() => {
    if (!active || !block) return;
    tick().then(() => {
      if (!searchFocused() && ownsFocus()) (start.initSelected === "yes" ? yes : no)?.focus();
    });
  });

  $effect(() => {
    shown.init = active
      ? { question: QUESTION, path: conn.project.path, desc, hint: `You can also initialize anytime via ${HINT_KEY}.`, ask: ASK, buttons: ["Yep!", "Nope"], selected: start.initSelected }
      : null;
    return () => {
      shown.init = null;
    };
  });
</script>

{#if active}
  <StartLayer kind="init" label="Initialize this project">
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <div class="block" bind:this={block} role="group" aria-label={QUESTION} onfocusin={() => setFocus("start")}>
      <h2 class="question">{QUESTION}</h2>
      <p class="path" title={conn.project.path}>{conn.project.path}</p>
      <p class="desc">{desc}</p>
      <p class="hint">You can also initialize anytime via <kbd>{HINT_KEY}</kbd>.</p>
      <p class="ask">{ASK}</p>
      <div class="buttons">
        <button
          class="yes"
          class:selected={start.initSelected === "yes"}
          type="button"
          aria-pressed={start.initSelected === "yes"}
          bind:this={yes}
          onclick={() => answer("yes")}
          disabled={start.answering}
        >
          <span class="label"><span class="key">Y</span>ep!</span>
        </button>
        <button
          class="no"
          class:selected={start.initSelected === "no"}
          type="button"
          aria-pressed={start.initSelected === "no"}
          bind:this={no}
          onclick={() => answer("no")}
          disabled={start.answering}
        >
          <span class="label"><span class="key">N</span>ope</span>
        </button>
      </div>
    </div>
  </StartLayer>
{/if}

<style>
  /* CHAT-001: centred, its content at most 720 picture px wide and at least
     60 from each edge of the column; at the bottom of the space, as the
     terminal draws it. */
  .block {
    box-sizing: border-box;
    width: 100%;
    max-width: calc(840 * var(--px));
    margin: auto auto 0;
    padding: calc(24 * var(--px)) calc(60 * var(--px)) calc(30 * var(--px));
  }

  .question {
    font: var(--t-figure);
    color: var(--ink);
  }
  .path {
    margin-top: calc(12 * var(--px));
    font: var(--t-stamp);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .desc {
    margin-top: calc(18 * var(--px));
    font: var(--t-reading);
    color: var(--ink);
  }
  .hint {
    margin-top: calc(12 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
  }
  .hint kbd {
    font: inherit;
    color: var(--ink);
  }
  .ask {
    margin-top: calc(18 * var(--px));
    font: var(--t-reading);
    color: var(--ink);
  }

  .buttons {
    display: flex;
    gap: calc(12 * var(--px));
    margin-top: calc(20 * var(--px));
  }
  /* The New chat button's look (ChatList.svelte); the chosen one as the
     slot's chosen button (4.9): an ink line and the band. */
  .buttons button {
    display: flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
    min-width: calc(128 * var(--px));
    height: calc(52 * var(--px));
    padding: 0 calc(22 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    color: var(--ink);
  }
  .buttons button.selected {
    border-color: var(--ink);
    background: var(--band);
  }
  .buttons .label {
    /* As on New chat: Spectral's capitals sit high; one picture pixel down
       centres them in the button (measured in the real window). */
    position: relative;
    top: calc(1 * var(--px));
    font: var(--t-button);
  }
  .buttons .key {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .buttons button:disabled {
    color: var(--slate);
  }
</style>
