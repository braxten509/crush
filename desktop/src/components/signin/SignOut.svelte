<script>
  // Sign out's question (desktop-design-5-6 2.4 "Settings rows"; MISC-013):
  // "Are you sure you want to log out of <name>?" with No selected, as
  // `crush logout` asks (internal/cmd/logout.go:102). Y/N answer at once.
  //   name, onAnswer(yes)
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";

  const ID = "signout_confirm";

  let { name = "", onAnswer = () => {} } = $props();
  let selected = $state("no");

  function answer(yes) {
    closeDialog(ID);
    onAnswer(yes);
  }

  function onKey(event, combo) {
    if (combo === "Escape" || combo === "Alt+Escape") {
      answer(false);
      return true;
    }
    return false;
  }
</script>

<Sheet id={ID} title="Sign Out" width="small" help={[{ key: "←/→", text: "choose" }, { key: "enter", text: "confirm" }, { key: "esc", text: "cancel" }]} {onKey}>
  <p class="question">Are you sure you want to log out of {name}?</p>
  <Buttons
    buttons={[
      { id: "yes", label: "Yes", key: "y" },
      { id: "no", label: "No", key: "n" },
    ]}
    bind:selected
    onPress={(id) => answer(id === "yes")}
  />
</Sheet>

<style>
  .question {
    margin: 0 0 calc(20 * var(--px));
    font: var(--t-reading);
    color: var(--ink);
  }
</style>
