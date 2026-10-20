<script>
  // Remote Control status (desktop-design-5-6 2.27, look 5.20; REM-006 to
  // REM-008): internal/ui/dialog/remotecontrol/remote.go. The computer and
  // the phones (kept current by remote_status); "Turn off" stops the share,
  // "Keep on" (selected first) and Esc close without stopping.
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { stopShare } from "../../lib/remote/share.js";
  import { share } from "../../lib/remote/store.svelte.js";

  const ID = "remote";

  let selected = $state("keep");

  const computer = $derived(share.status?.host ? `${share.status.host} · port ${share.status.port}` : "");
  const phones = $derived(share.phones.length > 0 ? share.phones.join(", ") : "none yet");
  const help = [
    { key: "←/→", text: "choose" },
    { key: "enter", text: "confirm" },
    { key: "esc", text: "close" },
  ];

  function press(id) {
    closeDialog(ID);
    if (id === "off") stopShare();
  }

  function onKey(event, combo) {
    switch (combo) {
      case "ArrowLeft":
      case "ArrowRight":
      case "Tab":
      case "Shift+Tab":
        selected = selected === "keep" ? "off" : "keep";
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

<Sheet id={ID} title="Remote control" width="small" {help} {onKey}>
  <p class="lead">On. Your phone can see this chat.</p>
  <table>
    <tbody>
      <tr><th>Computer</th><td>{computer}</td></tr>
      <tr><th>Phones</th><td>{phones}</td></tr>
    </tbody>
  </table>
  <p class="note">Open Pocket Agents › PC on your phone.</p>
  <p class="note">Only your own Tailscale devices can connect.</p>
  <div class="buttons">
    <Buttons
      buttons={[
        { id: "off", label: "Turn off", key: "t" },
        { id: "keep", label: "Keep on", key: "k" },
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
  .lead {
    font: var(--t-reading);
    color: var(--ink);
  }
  table {
    width: 100%;
    margin: calc(14 * var(--px)) 0;
    border-collapse: collapse;
    border-top: 1px solid var(--rule-table);
    border-bottom: 1px solid var(--rule-table);
  }
  tr + tr th,
  tr + tr td {
    border-top: 1px solid var(--rule-table-inner);
  }
  th {
    width: calc(130 * var(--px));
    padding: calc(8 * var(--px)) 0;
    text-align: left;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  td {
    padding: calc(8 * var(--px)) 0;
    font: var(--t-small);
    color: var(--ink);
    max-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .note {
    font: var(--t-small);
    color: var(--slate);
  }
  .buttons {
    margin-top: calc(18 * var(--px));
  }
</style>
