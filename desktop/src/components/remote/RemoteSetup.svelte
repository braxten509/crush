<script>
  // Remote Control setup (desktop-design-5-6 2.27, look 5.20; REM-002 to
  // REM-005): internal/ui/dialog/remotecontrol/setup.go. Shows the engine's
  // setup step and checks again every 2 s while open (not for
  // "unsupported"). Buttons: while checking "Cancel"; else "Check again" and
  // "Cancel", with the step's own button first when it has one. A step runs
  // as `crush remote setup <action>` in a new terminal tab, so the system's
  // password prompt and Tailscale's sign-in reach the owner.
  import { onMount, untrack } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { cancelStart, startShare } from "../../lib/remote/share.js";
  import { share } from "../../lib/remote/store.svelte.js";
  import { projectDir } from "../../lib/stores/composer.svelte.js";
  import { openTerminal, showTerminal } from "../../lib/terminal/tabs.svelte.js";

  const ID = "remote_setup";

  let stepFailed = $state(false);
  let stepping = $state(false); // a setup step is running
  let selected = $state("check");
  let timer = null;
  let left = false;

  const checking = $derived(share.state === "starting" && !share.status);
  const setup = $derived(share.status?.setup ?? null);
  const title = $derived.by(() => {
    if (stepFailed) return "Setup did not finish";
    if (share.status?.state === "off" && share.status?.error) return "Sharing could not start";
    if (checking || !setup) return "Checking Tailscale…";
    return setup.title ?? "";
  });
  const detail = $derived.by(() => {
    if (stepFailed) return "The step was cancelled or failed. You can retry it, choose Check again if you finished elsewhere, or cancel setup.";
    if (share.status?.state === "off" && share.status?.error) return `${share.status.error}. Choose Check again to retry.`;
    if (checking || !setup) return "Looking for a private connection to Pocket Agents.";
    return setup.detail ?? "";
  });
  const buttons = $derived.by(() => {
    if (checking) return [{ id: "cancel", label: "Cancel", key: "c" }];
    const list = [];
    if (setup?.action && !(share.status?.state === "off")) list.push({ id: "step", label: setup.button || "Continue" });
    list.push({ id: "check", label: "Check again", key: "a" }, { id: "cancel", label: "Cancel", key: "c" });
    return list;
  });
  const help = [
    { key: "←/→", text: "choose" },
    { key: "enter", text: "confirm" },
    { key: "esc", text: "cancel" },
  ];

  // A new state selects the first button again (setup.go:38-43).
  $effect(() => {
    const first = buttons[0]?.id;
    untrack(() => (selected = first));
  });

  // Checks again every 2 s while the engine says setup (remote.go:227-253),
  // but not while a setup step runs.
  $effect(() => {
    const again = share.state === "setup" && setup?.state !== "unsupported" && !stepping;
    clearInterval(timer);
    if (again) timer = setInterval(() => share.state === "setup" && startShare(), 2000);
  });

  // One setup step at a time, and no check while it runs (setupBusy,
  // remote.go:227-229, 264-267).
  async function runStep() {
    const action = setup?.action;
    if (!action || stepping) return;
    stepping = true;
    stepFailed = false;
    clearInterval(timer);
    try {
      const tab = openTerminal({ cwd: projectDir(), argv: ["crush", "remote", "setup", action], title: "Remote setup" });
      showTerminal(tab.id);
      const exit = await tab.exited;
      if (left) return;
      if (exit?.code === 0) startShare();
      else stepFailed = true;
    } finally {
      stepping = false;
    }
  }

  function press(id) {
    if (id === "cancel") {
      cancelStart();
      closeDialog(ID);
    } else if (id === "check") {
      if (stepping) return;
      stepFailed = false;
      startShare();
    } else if (id === "step") runStep();
  }

  function onKey(event, combo) {
    const ids = buttons.map((b) => b.id);
    const at = Math.max(0, ids.indexOf(selected));
    switch (combo) {
      case "Tab":
      case "ArrowRight":
      case "ArrowDown":
        selected = ids[(at + 1) % ids.length];
        return true;
      case "Shift+Tab":
      case "ArrowLeft":
      case "ArrowUp":
        selected = ids[(at - 1 + ids.length) % ids.length];
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

  onMount(() => () => {
    left = true;
    clearInterval(timer);
  });
</script>

<Sheet id={ID} title="Remote control" width="small" {help} {onKey}>
  <p class="title">{title}</p>
  {#if checking}
    <p class="loading"><Icon name="working" size={16} /> {detail}</p>
  {:else}
    <p class="detail">{detail}</p>
  {/if}
  <div class="buttons"><Buttons {buttons} bind:selected onPress={press} /></div>
</Sheet>

<style>
  p {
    margin: 0;
  }
  .title {
    font: var(--t-heading);
    color: var(--ink);
  }
  .detail,
  .loading {
    margin-top: calc(8 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
    overflow-wrap: anywhere;
  }
  .loading {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
  }
  .buttons {
    margin-top: calc(20 * var(--px));
  }
</style>
