<script>
  // The Settings page (desktop-design-5-6 2.13, look 5.13; SET-003 to
  // SET-037): a sheet of the rows the plan and the checklist ask for; every
  // other option stays a crush.json or crushrc setting the engine honors.
  // Each row shows the effective value from the config (re-read on
  // config_changed, so the terminal's changes show too); the small sheets of
  // 2.12 open from their rows. Tab and Shift+Tab move between the controls.
  import { onMount } from "svelte";
  import Sheet, { section } from "../kit/Sheet.svelte";
  import { openDialog, closeDialog } from "../../lib/dialogs.js";
  import { runAction } from "../../lib/actions.js";
  import { crushDirs, remotePasswordExists } from "../../lib/engine/commands.js";
  import { refreshModels, modelData } from "../../lib/models/store.svelte.js";
  import { platform } from "../../lib/palette/entries.js";
  import { signIn, signOut } from "../../lib/signin/accounts.js";
  import {
    DEFAULT_AUTOCOMPACT,
    DEFAULT_CHECK_IN,
    NOTIFICATION_STYLES,
    notificationStyle,
    options,
    setAgentCpus,
    setCheckInMinutes,
    setLaunchTarget,
    setSidebarShown,
    toggleAgentCpus,
    toggleComposerFocusOnly,
    toggleThoughtLine,
    tui,
  } from "../../lib/settings/store.svelte.js";
  import { chosenTheme, theme } from "../../lib/themes/current.svelte.js";

  const ID = "settings";
  const ACCOUNTS = [
    { platform: "hyper", provider: "hyper", name: "Hyper" },
    { platform: "copilot", provider: "copilot", name: "GitHub Copilot" },
    { platform: "openai", provider: "openai", name: "ChatGPT" },
    { platform: "grok", provider: "xai", name: "Grok" },
  ];

  let checkIn = $state(String(options().check_in_minutes ?? DEFAULT_CHECK_IN));
  let cpus = $state(String(options().agent_cpus ?? ""));
  let hasPassword = $state(false);
  let editing = $state(""); // the focused field: "check_in" | "cpus" | ""
  let atFocus = ""; // its text when it took the focus

  // A field not being typed in follows the config, so leaving it untouched
  // never writes an old value over a change made elsewhere.
  $effect(() => {
    const minutes = String(options().check_in_minutes ?? DEFAULT_CHECK_IN);
    const set = String(options().agent_cpus ?? "");
    if (editing !== "check_in") checkIn = minutes;
    if (editing !== "cpus") cpus = set;
  });

  const help = [
    { key: "tab", text: "next" },
    { key: "enter", text: "choose" },
    { key: "esc", text: "close" },
  ];

  const signedIn = (provider) => {
    const p = modelData.cfg?.providers.get(provider);
    return Boolean(p && (p.has_oauth || p.has_api_key));
  };
  const memoryText = $derived(Number(options().memory_limit_gb) > 0 ? `${options().memory_limit_gb} GB` : "No limit");
  const styleLabel = $derived(NOTIFICATION_STYLES.find((s) => s.id === notificationStyle())?.label ?? "Auto");

  // A field saves on Enter and when it loses the focus.
  async function saveCheckIn() {
    editing = "";
    if (checkIn === atFocus) return; // focused but not changed: nothing to save
    if (String(options().check_in_minutes ?? DEFAULT_CHECK_IN) === checkIn.trim()) return;
    if (!(await setCheckInMinutes(checkIn))) checkIn = String(options().check_in_minutes ?? DEFAULT_CHECK_IN);
  }

  async function saveCpus() {
    editing = "";
    if (cpus === atFocus) return;
    if (String(options().agent_cpus ?? "") === cpus.trim()) return;
    await setAgentCpus(cpus);
  }

  function onKey(event, combo) {
    if (combo === "Enter" && event.target?.tagName === "INPUT") {
      event.target.blur();
      return true;
    }
    return false;
  }

  async function account(row) {
    if (signedIn(row.provider)) await signOut(row.platform);
    else await signIn(row.platform);
    refreshModels();
  }

  let configDirs = $state([]);

  onMount(() => {
    refreshModels();
    remotePasswordExists()
      .then((exists) => (hasPassword = exists))
      .catch(() => {});
    // The config folders, as About shows them (2.13, 2.28).
    crushDirs()
      .then((dirs) => (configDirs = (dirs?.lines ?? []).slice(0, 2)))
      .catch(() => {});
  });
</script>

<Sheet id={ID} title="Settings" width="wide" maxHeight={760} {help} {onKey}>
  <div class="page">
    {@render section("Appearance")}
    <div class="row">
      <span class="name">Theme</span>
      <span class="value">{chosenTheme()}{theme.name !== chosenTheme() ? ` (showing ${theme.name})` : ""}</span>
      <button type="button" class="act" onclick={() => openDialog("theme")}>Change…</button>
    </div>
    <div class="row">
      <span class="name">Show sidebar</span>
      <button type="button" class="switch" role="switch" aria-checked={tui().compact_mode !== true} aria-label="Show sidebar" onclick={() => setSidebarShown(tui().compact_mode === true)}></button>
    </div>

    {@render section("Typing")}
    <div class="row">
      <span class="name">Keep focus on the typing box</span>
      <button type="button" class="switch" role="switch" aria-checked={tui().composer_focus_only === true} aria-label="Keep focus on the typing box" onclick={toggleComposerFocusOnly}></button>
    </div>
    <div class="row">
      <span class="name">Show the model's newest thought while busy</span>
      <button type="button" class="switch" role="switch" aria-checked={tui().thought_line !== false} aria-label="Show the model's newest thought while busy" onclick={toggleThoughtLine}></button>
    </div>
    <div class="row">
      <span class="name">Composer footer</span>
      <button
        type="button"
        class="act"
        onclick={() => {
          closeDialog(ID);
          runAction("composer.customize");
        }}>Customize…</button
      >
    </div>

    {@render section("Work")}
    <div class="row">
      <span class="name">Fallback compaction</span>
      <span class="value">{(Number(options().auto_compact_token_limit) || DEFAULT_AUTOCOMPACT).toLocaleString("en-US")} tokens</span>
      <button type="button" class="act" onclick={() => openDialog("autocompact")}>Change…</button>
    </div>
    <div class="row">
      <span class="name">Check-in messages every</span>
      <span class="field"><input type="text" inputmode="numeric" bind:value={checkIn} onfocus={() => ((editing = "check_in"), (atFocus = checkIn))} onblur={saveCheckIn} aria-label="Check-in minutes" /> minutes</span>
    </div>
    <div class="row">
      <span class="name">Sub-agent instructions</span>
      <button type="button" class="act" onclick={() => openDialog("subagent_instructions")}>Edit…</button>
    </div>

    {#if platform.linux}
      {@render section("System")}
      <div class="row">
        <span class="name">Memory limit</span>
        <span class="value">{memoryText}</span>
        <button type="button" class="act" onclick={() => openDialog("memory_limit")}>Change…</button>
      </div>
      <div class="row">
        <span class="name">Run AI work on part of the CPU</span>
        <button type="button" class="switch" role="switch" aria-checked={options().agent_cpus_enabled === true} aria-label="Run AI work on part of the CPU" onclick={toggleAgentCpus}></button>
      </div>
      <div class="row">
        <span class="name">CPUs for AI work</span>
        <span class="field"><input type="text" bind:value={cpus} onfocus={() => ((editing = "cpus"), (atFocus = cpus))} onblur={saveCpus} placeholder="detect" aria-label="CPUs for AI work" /></span>
      </div>
    {/if}

    {@render section("Notifications")}
    <div class="row">
      <span class="name">Notification style</span>
      <span class="value">{styleLabel}</span>
      <button type="button" class="act" onclick={() => openDialog("notifications")}>Change…</button>
    </div>

    {@render section("Accounts")}
    {#each ACCOUNTS as row (row.platform)}
      <div class="row">
        <span class="name">{row.name}</span>
        <span class="value">{signedIn(row.provider) ? "Signed in" : "Not signed in"}</span>
        <button type="button" class="act" onclick={() => account(row)}>{signedIn(row.provider) ? "Sign out" : "Sign in"}</button>
      </div>
    {/each}

    {@render section("Phone")}
    <div class="row">
      <span class="name">Phone opens new chats in</span>
      <span class="choices" role="radiogroup" aria-label="Phone opens new chats in">
        {#each [["terminal", "Terminal"], ["desktop", "Desktop"]] as [id, label] (id)}
          <button type="button" class="choice" role="radio" aria-checked={(options().remote_launch_target || "terminal") === id} onclick={() => setLaunchTarget(id)}>{label}</button>
        {/each}
      </span>
    </div>
    <div class="row">
      <span class="name">Launcher password</span>
      <span class="value">{hasPassword ? "Set" : "Not set"}</span>
      <button type="button" class="act" onclick={() => openDialog("launcher_password")}>Set password…</button>
    </div>

    {@render section("Providers")}
    <div class="row">
      <span class="name">Provider list</span>
      <button type="button" class="act" onclick={() => runAction("providers.refresh")}>Refresh</button>
    </div>

    <p class="quiet">Other settings are read from crush.json and crushrc{configDirs.length ? ` in ${configDirs.join(" and ")}` : ""}.</p>
  </div>
</Sheet>

<style>
  .page {
    overflow-y: auto;
    min-height: 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    min-height: calc(48 * var(--px));
  }
  .name {
    flex: 1;
    min-width: 0;
    font: var(--t-item);
    color: var(--ink);
  }
  .value {
    font: var(--t-small);
    color: var(--slate);
  }
  .act,
  .choice {
    height: calc(36 * var(--px));
    padding: 0 calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-small);
    color: var(--ink);
    cursor: pointer;
  }
  .act:hover,
  .choice:hover {
    background: var(--control-hover);
  }
  .choice[aria-checked="true"] {
    background: var(--band);
    border-color: var(--ink);
  }
  .choices {
    display: flex;
    gap: calc(8 * var(--px));
  }
  .switch {
    position: relative;
    flex: none;
    width: calc(44 * var(--px));
    height: calc(24 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    cursor: pointer;
  }
  .switch::after {
    content: "";
    position: absolute;
    top: calc(3 * var(--px));
    left: calc(3 * var(--px));
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    background: var(--rule-control);
  }
  .switch[aria-checked="true"] {
    border-color: var(--ink);
  }
  .switch[aria-checked="true"]::after {
    left: calc(23 * var(--px));
    background: var(--choice);
  }
  .field {
    font: var(--t-small);
    color: var(--slate);
  }
  .field input {
    width: calc(120 * var(--px));
    height: calc(36 * var(--px));
    padding: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
    font: var(--t-field);
    color: var(--ink);
  }
  .act:focus-visible,
  .choice:focus-visible,
  .switch:focus-visible,
  .field input:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .quiet {
    margin: calc(20 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--slate);
  }
</style>
