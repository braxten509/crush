<script>
  // The close box (desktop-design-5-6 2.25 LIST-014, -015, -043; look 5.17):
  // a small sheet with no title line, 'Close “Terminal”?' in --t-heading, the
  // detail in --t-body-small --slate, then "Close" (selected first) and
  // "Keep" (internal/ui/sessionhost/draw.go:535-559, terminal.go:82-87,
  // host.go:473-496). Every key and paste goes to it while it is open
  // (input.go:13-26, :137-142): ←, →, Tab, Shift+Tab, H and L switch; Enter
  // and Space press the selected button; Y closes; Esc and N keep.
  //
  // KIT's sheet hands the keys over through onKey; this box also takes them
  // itself while it has the focus, so it works the same with the kit's
  // placeholder sheet.
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { closeTerminal, shownTab, terminals } from "../../lib/terminal/tabs.svelte.js";

  let { id } = $props();

  const DETAIL = "This closes the terminal. Its output is not saved."; // terminal.go:84
  const title = $derived(terminals.find((tab) => tab.id === id)?.title ?? "Terminal");
  let selected = $state("close");
  let box = $state();

  const buttons = [
    { id: "close", label: "Close", key: "c" },
    { id: "keep", label: "Keep", key: "k" },
  ];

  function answer(close) {
    closeDialog("terminal_close");
    if (close) closeTerminal(id);
    else shownTab()?.focus?.();
  }

  // The terminal's keys for the box (input.go:13-26), each as the terminal
  // names it ("h" plain, "shift+tab", "Y"); every other key is taken too and
  // does nothing.
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

<Sheet id="terminal_close" title={null} width="small" {onKey}>
  <!-- The box holds the focus, so its keys never reach the terminal behind it. -->
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="close-box" bind:this={box} tabindex="-1" role="group" aria-labelledby="terminal-close-question" onkeydown={onOwnKey} onpaste={(event) => event.preventDefault()}>
    <h2 class="question" id="terminal-close-question">Close “{title}”?</h2>
    <p class="detail">{DETAIL}</p>
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
