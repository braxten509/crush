<script>
  // The crash notice (desktop-design-5-6 2.28, START-020, MISC-025): shown
  // once per report. "Open report" opens the report file; "Restart" reloads
  // the window (a panic already ended the last run, so this run is the
  // restart; a crashed web view was reloaded and shows it at once).
  //   path, kind
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { crashRestart, open_link } from "../../lib/engine/commands.js";
  import { projectDir } from "../../lib/stores/composer.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "crash";

  let { path = "", kind = "panic" } = $props();
  let selected = $state("open");

  const help = [
    { key: "←/→", text: "choose" },
    { key: "enter", text: "confirm" },
    { key: "esc", text: "close" },
  ];

  function press(id) {
    if (id === "open") {
      open_link(path, projectDir()).catch((error) => notify("error", error?.message ?? String(error)));
      return;
    }
    closeDialog(ID);
    if (kind === "webview") crashRestart().catch((error) => notify("error", error?.message ?? String(error)));
  }

  function onKey(event, combo) {
    switch (combo) {
      case "ArrowLeft":
      case "ArrowRight":
      case "Tab":
      case "Shift+Tab":
        selected = selected === "open" ? "restart" : "open";
        return true;
      case "Enter":
      case "Space":
        press(selected);
        return true;
      default:
        return false;
    }
  }
</script>

<Sheet id={ID} title="Crush Desktop crashed" width="small" {help} {onKey}>
  <p class="body">If you'd like to report it, please open the report and open an issue at https://github.com/charmbracelet/crush/issues/new?template=bug.yml</p>
  <p class="path">{path}</p>
  <div class="buttons">
    <Buttons
      buttons={[
        { id: "open", label: "Open report", key: "o" },
        { id: "restart", label: "Restart", key: "r" },
      ]}
      bind:selected
      onPress={press}
    />
  </div>
</Sheet>

<style>
  p {
    margin: 0;
  }
  .body {
    font: var(--t-reading);
    color: var(--ink);
    overflow-wrap: anywhere;
  }
  .path {
    margin: calc(12 * var(--px)) 0 calc(18 * var(--px));
    font: var(--t-agent-mono);
    color: var(--slate);
    overflow-wrap: anywhere;
  }
</style>
