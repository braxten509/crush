<script>
  // The ready plan's form in the slot (TYPE-052 to TYPE-060; a port of
  // internal/ui/dialog/questionform/plan_handoff_inline.go): "Ready to start
  // coding?" with Start coding, Code with YOLO and Revise plan. Revise opens
  // "What should change?", a field of at most 1,000 characters, 3 to 8 lines
  // tall. With focus elsewhere the buttons stay, inactive. The keys are the
  // slot scope's while the form is mounted.
  import { tick } from "svelte";
  import { bindKey } from "../../lib/keys.js";
  import { attachSlot, confirmPlan, dismissPlan, revisePlan, setSlotButtons } from "../../lib/stores/composer.svelte.js";
  import { setFocus, ui } from "../../lib/stores/ui.svelte.js";

  // The terminal's buttons and underlined letters (plan_handoff_inline.go:252-275).
  const CHOICES = [
    { label: "Start coding", underline: 6 },
    { label: "Code with YOLO", underline: 10 },
    { label: "Revise plan", underline: 10 },
  ];
  const REVISE = 2;
  const MAX_FEEDBACK = 1000;
  const MIN_LINES = 3;
  const MAX_LINES = 8;
  // ShortHelp (plan_handoff_inline.go:393-398).
  const CHOOSING_HINT = "←/→ switch · enter confirm · c start coding · y yolo coding · n revise plan";
  const EDITING_HINT = "enter confirm · ctrl+j newline · esc exit";

  let root = $state(null);
  let editor = $state(null);
  let selected = $state(0);
  let editing = $state(false);
  let feedback = $state("");
  let lines = $state(MIN_LINES);
  let confirming = false;

  const focused = $derived(ui.focus === "slot");

  $effect(() => {
    setSlotButtons(editing ? [] : CHOICES.map((choice, index) => ({ label: choice.label, selected: focused && index === selected })));
  });

  $effect(() => {
    if (!root) return;
    const unbind = [
      ...["Escape", "Enter", "Shift+Enter", "Ctrl+J"].map((combo) => bindKey("slot", combo, () => key(combo))),
      ...["ArrowLeft", "ArrowRight", "h", "l", "H", "L", "c", "C", "y", "Y", "n", "N"].map((combo) => bindKey("slot", combo, () => key(combo))),
    ];
    const detach = attachSlot(() => (editing ? editor : root)?.focus({ preventScroll: true }));
    setFocus("slot");
    root.focus({ preventScroll: true });
    return () => {
      for (const off of unbind) off();
      detach();
      setSlotButtons([]);
    };
  });

  // The feedback field's height follows its text, 3 to 8 lines (TYPE-056).
  $effect(() => {
    void feedback;
    if (!editor) return;
    const line = parseFloat(getComputedStyle(editor).lineHeight) || 26;
    editor.style.height = "0px";
    const needed = Math.round(editor.scrollHeight / line);
    editor.style.height = "";
    lines = Math.min(MAX_LINES, Math.max(MIN_LINES, needed));
  });

  // A drag selection in the field clears when the form changes size (TYPE-059).
  $effect(() => {
    if (!root) return;
    const observer = new ResizeObserver(() => collapse());
    observer.observe(root);
    return () => observer.disconnect();
  });

  function collapse() {
    if (editor && editor.selectionStart !== editor.selectionEnd) editor.setSelectionRange(editor.selectionEnd, editor.selectionEnd);
  }

  function key(combo) {
    if (editing) {
      switch (combo) {
        case "Escape":
          editing = false;
          root?.focus({ preventScroll: true });
          return true;
        case "Enter": {
          const text = feedback.trim();
          if (text) revisePlan(text);
          return true;
        }
        case "Ctrl+J":
          insertNewline();
          return true;
        default:
          return false; // Shift+Enter and the letters type in the field
      }
    }
    switch (combo) {
      case "Escape":
        dismissPlan();
        return true;
      case "ArrowLeft":
      case "h":
      case "H":
        selected = (selected + CHOICES.length - 1) % CHOICES.length;
        return true;
      case "ArrowRight":
      case "l":
      case "L":
        selected = (selected + 1) % CHOICES.length;
        return true;
      case "Enter":
        choose(selected);
        return true;
      case "c":
      case "C":
        choose(0);
        return true;
      case "y":
      case "Y":
        choose(1);
        return true;
      case "n":
      case "N":
        choose(REVISE);
        return true;
      default:
        return false;
    }
  }

  async function choose(index) {
    selected = index;
    if (index === REVISE) {
      editing = true;
      await tick();
      editor?.focus({ preventScroll: true });
      return;
    }
    if (confirming) return;
    confirming = true;
    try {
      await confirmPlan(index === 1);
    } finally {
      confirming = false;
    }
  }

  function insertNewline() {
    if (!editor) return;
    const start = editor.selectionStart ?? feedback.length;
    const end = editor.selectionEnd ?? start;
    if (feedback.length - (end - start) >= MAX_FEEDBACK) return;
    feedback = feedback.slice(0, start) + "\n" + feedback.slice(end);
    tick().then(() => editor?.setSelectionRange(start + 1, start + 1));
  }

  function regain() {
    setFocus("slot");
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="handoff" class:inactive={!focused} role="group" aria-label="Plan ready" tabindex="-1" bind:this={root} onpointerdown={regain}>
  <header class="title">
    <span class="label">Plan</span>
    <span class="question">{editing ? "What should change?" : "Ready to start coding?"}</span>
  </header>
  {#if editing}
    <textarea
      bind:this={editor}
      bind:value={feedback}
      class="feedback"
      style:--lines={lines}
      maxlength={MAX_FEEDBACK}
      placeholder="Describe the changes..."
      aria-label="What should change?"
      onfocus={regain}
      onblur={collapse}
    ></textarea>
  {/if}
  <p class="hint">{editing ? EDITING_HINT : CHOOSING_HINT}</p>
  {#if !editing}
    <div class="buttons">
      {#each CHOICES as choice, index (choice.label)}
        <button
          type="button"
          class="choice"
          class:selected={focused && index === selected}
          tabindex="-1"
          data-slot-button={choice.label}
          aria-pressed={focused && index === selected}
          onclick={() => choose(index)}
        >
          {choice.label.slice(0, choice.underline)}<u>{choice.label[choice.underline]}</u>{choice.label.slice(choice.underline + 1)}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .handoff {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: calc(10 * var(--px));
    padding: calc(14 * var(--px)) calc(16 * var(--px)) calc(16 * var(--px));
    container-type: inline-size;
  }
  .handoff:focus-visible {
    outline: none;
  }
  .title {
    display: flex;
    align-items: baseline;
    gap: calc(12 * var(--px));
  }
  .label {
    flex: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .question {
    font: var(--t-body-small);
    color: var(--ink);
  }
  .feedback {
    box-sizing: content-box;
    height: calc(var(--lines) * 26 * var(--px));
    padding: calc(8 * var(--px)) calc(12 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
    resize: none;
    font: var(--t-field);
    color: var(--ink);
    caret-color: var(--ink);
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
  }
  .feedback:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .feedback::placeholder {
    color: var(--slate);
    opacity: 1;
  }
  .feedback::selection {
    background: var(--selection);
    color: var(--ink);
  }
  .feedback::-webkit-scrollbar {
    width: calc(6 * var(--px));
  }
  .feedback::-webkit-scrollbar-thumb {
    background: var(--rule-control);
  }
  .feedback::-webkit-scrollbar-track {
    background: transparent;
  }
  .hint {
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-small);
    color: var(--slate);
  }
  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: calc(8 * var(--px));
  }
  .choice {
    flex: none;
    box-sizing: border-box;
    height: calc(44 * var(--px));
    padding: 0 calc(18 * var(--px));
    border: 1px solid var(--rule-control);
    font: var(--t-body-small);
    color: var(--ink);
    white-space: nowrap;
  }
  .handoff:not(.inactive) .choice:hover {
    border-color: var(--ink);
  }
  .choice.selected {
    border-color: var(--ink);
    background: var(--band);
  }
  .choice u {
    text-decoration-thickness: 1px;
    text-underline-offset: calc(3 * var(--px));
  }
  /* Focus elsewhere: the buttons stay, slate (TYPE-060). */
  .inactive .choice,
  .inactive .question,
  .inactive .label {
    color: var(--slate);
  }
  /* A narrow column stacks the buttons (TYPE-053). */
  @container (max-width: 32rem) {
    .buttons {
      flex-direction: column;
      align-items: stretch;
    }
  }
</style>
