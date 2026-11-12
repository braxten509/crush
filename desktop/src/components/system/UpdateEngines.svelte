<script>
  // The update's question (owner, 2026-11-12: "when updating crush it should
  // ask me if I want to restart the engine or not"; asked by
  // lib/system/appupdate.svelte.js when Update is pressed), in the close
  // question's form. "Keep engines running" is selected when it opens, as an
  // update did before; ←/→ and Tab switch; Enter and Space press the
  // selected button; R, K and C press theirs; Esc cancels. Leaving the sheet
  // any other way cancels too, so the update never waits on a sheet that is
  // gone.
  //   chats, helpers, waiting (counts), answer("restart" | "keep" | "cancel")
  import { onDestroy } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { workLine } from "../../lib/closeask.js";
  import { closeDialog } from "../../lib/dialogs.js";
  import { UPDATE_SHEET } from "../../lib/system/appupdate.svelte.js";

  let { chats = 0, helpers = 0, waiting = 0, answer = () => {} } = $props();

  const BUTTONS = [
    { id: "cancel", label: "Cancel", key: "c" },
    { id: "restart", label: "Restart engines", key: "r" },
    { id: "keep", label: "Keep engines running", key: "k" },
  ];
  const ORDER = BUTTONS.map((b) => b.id);

  let selected = $state("keep");
  let answered = false;

  function press(id) {
    if (!answered) {
      answered = true;
      answer(id);
    }
    closeDialog(UPDATE_SHEET);
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

<Sheet id={UPDATE_SHEET} title="Update Crush" width="small" help={[{ key: "←/→", text: "switch options" }, { key: "enter", text: "confirm" }, { key: "esc", text: "cancel" }]} {onKey}>
  <div class="ask">
    <p class="line">{workLine(chats, helpers, waiting)} Restart the engines on the new version too? A restart stops the work still running.</p>
    <div class="buttons"><Buttons buttons={BUTTONS} bind:selected onPress={press} /></div>
  </div>
</Sheet>

<style>
  .ask {
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
