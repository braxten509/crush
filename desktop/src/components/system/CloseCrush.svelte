<script>
  // The close question (owner, 2026-10-27; sketch "A · inside the app"; asked
  // by lib/closeask.js): the last window closes while chats or sub-agents
  // still work. "Close window only" is selected when it opens; ←/→ and Tab
  // switch; Enter and Space press the selected button; W, S and C press
  // theirs; Esc cancels. Leaving the sheet any other way cancels too, so the
  // close never waits on a sheet that is gone.
  //   chats, helpers (counts), answer("window" | "stop" | "cancel")
  import { onDestroy } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { CLOSE_SHEET, workLine } from "../../lib/closeask.js";
  import { closeDialog } from "../../lib/dialogs.js";

  let { chats = 0, helpers = 0, answer = () => {} } = $props();

  const BUTTONS = [
    { id: "cancel", label: "Cancel", key: "c" },
    { id: "stop", label: "Stop engine", key: "s" },
    { id: "window", label: "Close window only", key: "w" },
  ];
  const ORDER = BUTTONS.map((b) => b.id);

  let selected = $state("window");
  let answered = false;

  function press(id) {
    if (!answered) {
      answered = true;
      answer(id);
    }
    closeDialog(CLOSE_SHEET);
  }

  onDestroy(() => press("cancel"));

  function step(delta) {
    selected = ORDER[(ORDER.indexOf(selected) + delta + ORDER.length) % ORDER.length];
  }

  function onKey(event, combo) {
    switch (combo) {
      case "ArrowLeft":
      case "Shift+Tab":
        step(-1);
        return true;
      case "ArrowRight":
      case "Tab":
        step(1);
        return true;
      case "Enter":
      case "Space":
        press(selected);
        return true;
      case "Escape":
      case "Alt+Escape":
        press("cancel");
        return true;
      default:
        return false;
    }
  }
</script>

<Sheet id={CLOSE_SHEET} title="Close Crush" width="small" help={[{ key: "←/→", text: "switch options" }, { key: "enter", text: "confirm" }, { key: "esc", text: "cancel" }]} {onKey}>
  <div class="close">
    <p class="line">{workLine(chats, helpers)} Keep the engine running, or stop everything?</p>
    <div class="buttons"><Buttons buttons={BUTTONS} bind:selected onPress={press} /></div>
  </div>
</Sheet>

<style>
  .close {
    display: flex;
    flex-direction: column;
  }
  .line {
    margin: 0;
    font: var(--t-item);
    color: var(--slate);
  }
  .buttons {
    display: flex;
    justify-content: flex-end;
    margin-top: calc(18 * var(--px));
  }
</style>
