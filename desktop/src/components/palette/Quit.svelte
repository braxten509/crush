<script>
  // The quit sheet (desktop-design-5-6 2.24, look 5.19; DLG-288 to DLG-293):
  // a port of internal/ui/dialog/quit.go. "Nope" is selected when it opens;
  // ←/→ and Tab switch; Enter and Space press the selected button; Y, Shift+Y
  // and Ctrl+C quit (so Ctrl+C twice quits without looking); N, Shift+N and
  // Esc cancel. Hover only shows the hover look; a click on the other button
  // selects it, a click on the selected one presses it. "Yep!" closes this
  // window as 2.23 does; the app ends after its last window.
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeWindow } from "../../lib/closing.js";
  import { closeDialog } from "../../lib/dialogs.js";

  const ID = "quit";
  const BUTTONS = [
    { id: "yes", label: "Yep!", key: "y" },
    { id: "no", label: "Nope", key: "n" },
  ];

  let selected = $state("no");

  function press(id) {
    closeDialog(ID);
    if (id === "yes") closeWindow();
  }

  function onKey(event, combo) {
    switch (combo) {
      case "ArrowLeft":
      case "ArrowRight":
      case "Tab":
      case "Shift+Tab":
        selected = selected === "yes" ? "no" : "yes";
        return true;
      case "Enter":
      case "Space":
        press(selected);
        return true;
      case "y":
      case "Shift+Y":
      case "Y":
      case "Ctrl+C":
        press("yes");
        return true;
      case "n":
      case "Shift+N":
      case "N":
      case "Escape":
      case "Alt+Escape":
        press("no");
        return true;
      default:
        return false;
    }
  }

  // Clicks only (the keys are onKey's).
  function click(id) {
    if (id !== selected) selected = id;
    else press(id);
  }
</script>

<Sheet id={ID} title="" label="Quit Crush" width="small" help={[{ key: "←/→", text: "switch options" }, { key: "enter/space", text: "confirm" }]} {onKey}>
  <div class="quit">
    <p class="question">Are you sure you want to quit?</p>
    <div class="buttons"><Buttons buttons={BUTTONS} bind:selected onPress={click} /></div>
    <p class="hint">To quit without confirmation</p>
    <p class="hint">press ctrl+c twice.</p>
  </div>
</Sheet>

<style>
  .quit {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
  }
  .question {
    margin: 0;
    font: var(--t-heading);
    color: var(--ink);
  }
  .buttons {
    margin: calc(24 * var(--px)) 0 calc(20 * var(--px));
  }
  .hint {
    margin: 0;
    font: var(--t-body-small);
    color: var(--slate);
  }
</style>
