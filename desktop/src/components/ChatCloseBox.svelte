<script>
  // The chat close box (owner, 2026-11-13): asked before closing a chat
  // that still runs something, since the close stops it all
  // (lib/stores/closechat.js). The terminal close box's look and keys
  // (terminal/CloseBox.svelte): 'Close “title”?', the detail, then "Close"
  // (selected first) and "Keep". ←, →, Tab, Shift+Tab, H and L switch;
  // Enter and Space press the selected button; Y closes; Esc and N keep.
  import Buttons from "./kit/Buttons.svelte";
  import Sheet from "./kit/Sheet.svelte";
  import { closeDialog } from "../lib/dialogs.js";
  import { closeListed } from "../lib/stores/openchats.svelte.js";
  import { sessions } from "../lib/stores/sessions.svelte.js";

  let { id, kind } = $props();

  const ID = "chat_close";
  // The terminal's words for the same box (internal/ui/sessionhost/draw.go
  // confirmDetail).
  const DETAILS = {
    working: "It is still working. Closing stops it and its helpers.",
    waiting: "It is waiting for your answer. Closing stops it and its helpers.",
    background: "Its helpers or background jobs are still running. Closing stops them.",
  };
  const detail = $derived(`${DETAILS[kind] ?? DETAILS.working} The chat stays saved and can be reopened later.`);
  const title = $derived(sessions.byId[id]?.title || "New chat");
  let selected = $state("close");
  let box = $state();

  const buttons = [
    { id: "close", label: "Close", key: "c" },
    { id: "keep", label: "Keep", key: "k" },
  ];

  function answer(close) {
    closeDialog(ID);
    if (close) closeListed(id, { mustStop: true }).catch((error) => console.error("closing a chat failed", error));
  }

  function onKey(event) {
    const plain = !event.ctrlKey && !event.altKey && !event.metaKey;
    const bare = plain && !event.shiftKey;
    const key = event.key;
    if ((bare && ["ArrowLeft", "ArrowRight", "h", "l"].includes(key)) || (plain && key === "Tab")) {
      selected = selected === "close" ? "keep" : "close";
    } else if (bare && (key === "Enter" || key === " ")) {
      answer(selected === "close");
    } else if (plain && (key === "y" || key === "Y")) {
      answer(true);
    } else if ((bare && key === "Escape") || (plain && (key === "n" || key === "N"))) {
      answer(false);
    }
    return true;
  }

  function onOwnKey(event) {
    if (event.defaultPrevented) return;
    onKey(event);
    event.preventDefault();
    event.stopPropagation();
  }

  $effect(() => {
    box?.focus({ preventScroll: true });
  });
</script>

<Sheet id={ID} title={null} width="small" {onKey}>
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="close-box" bind:this={box} tabindex="-1" role="group" aria-labelledby="chat-close-question" data-testid="chat-close-box" onkeydown={onOwnKey} onpaste={(event) => event.preventDefault()}>
    <h2 class="question" id="chat-close-question">Close “{title}”?</h2>
    <p class="detail">{detail}</p>
    <div class="buttons" data-selected={selected}>
      <Buttons {buttons} bind:selected onPress={(button) => answer(button === "close")} />
    </div>
  </div>
</Sheet>

<style>
  .close-box {
    outline: none;
  }
  .question {
    margin: 0;
    font: var(--t-heading);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .detail {
    margin: calc(8 * var(--px)) 0 0;
    font: var(--t-body-small);
    color: var(--slate);
  }
  .buttons {
    margin-top: calc(24 * var(--px));
  }
</style>
