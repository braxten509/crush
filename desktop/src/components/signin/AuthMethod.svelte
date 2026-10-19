<script>
  // The sign-in entry point and method sheet (desktop-design-5-6 2.4, 7.4,
  // look 5.6; DLG-041 to DLG-045). openDialog("auth_method", props) opens the
  // sheet the provider needs (lib/signin/flows.js pickSheet): this one only
  // for OpenAI and xAI without any credential; otherwise it gives way at once
  // to the API key or sign-in sheet with the same props. Here: two cards,
  // the subscription first; arrows, Tab, H and L switch; Enter/Ctrl+Y goes
  // on; Esc goes back (onDone("cancelled")). A click selects, a second click
  // within 400 ms confirms (auth_method.go:36, :134).
  //   providerId, providerName?, reAuthenticate?, onboarding?, model?,
  //   modelType?, onDone(result: "done" | "cancelled")
  import { onDestroy } from "svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog, openDialog } from "../../lib/dialogs.js";
  import { configuredProvider, pickSheet } from "../../lib/signin/flows.js";
  import { modelData } from "../../lib/models/store.svelte.js";

  const ID = "auth_method";
  const DOUBLE_CLICK_MS = 400;

  let props = $props();

  let ready = $state(false);
  let selected = $state(0); // 0 the subscription, 1 the API key
  let lastClick = { card: -1, at: 0 };

  const name = $derived(props.providerName || modelData.catalog.find((p) => p.id === props.providerId)?.name || props.providerId);
  const oauthLabel = $derived(props.providerId === "xai" ? "Grok Account\nwith Subscription" : "ChatGPT Account\nwith Subscription");
  const help = [
    { key: "←/→", text: "choose" },
    { key: "enter", text: "accept" },
    { key: "esc", text: "back" },
  ];

  let gone = false;
  onDestroy(() => (gone = true));

  (async () => {
    const configured = await configuredProvider(props.providerId);
    if (gone) return; // closed while the provider was read: nothing opens
    const sheet = pickSheet(props.providerId, configured, props.model);
    if (sheet === ID) {
      ready = true;
      return;
    }
    closeDialog(ID);
    openDialog(sheet, { ...props }, { grace: true });
  })();

  function accept(card = selected) {
    closeDialog(ID);
    openDialog(card === 0 ? "oauth" : "api_key_input", { ...props }, { grace: true });
  }

  function back() {
    closeDialog(ID);
    props.onDone?.("cancelled");
  }

  function onKey(event, combo) {
    switch (combo) {
      case "ArrowLeft":
      case "ArrowRight":
      case "ArrowUp":
      case "ArrowDown":
      case "Tab":
      case "Shift+Tab":
      case "h":
      case "l":
        selected = selected === 0 ? 1 : 0;
        return true;
      case "Enter":
      case "Ctrl+Y":
        accept();
        return true;
      case "Escape":
      case "Alt+Escape":
        back();
        return true;
      default:
        return false;
    }
  }

  function click(card) {
    const now = Date.now();
    const again = lastClick.card === card && now - lastClick.at <= DOUBLE_CLICK_MS;
    lastClick = { card, at: now };
    selected = card;
    if (again) accept(card);
  }
</script>

{#if ready}
  <Sheet id={ID} title={`Let’s Auth ${name}`} width="standard" {help} {onKey} onboarding={props.onboarding === true}>
    <!-- A click outside the cards clears the double-click memory (auth_method.go:134). -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="body" onclick={(event) => !event.target.closest(".card") && (lastClick = { card: -1, at: 0 })}>
      <p class="prompt">How would you like to authenticate?</p>
      <div class="cards">
        {#each [oauthLabel, "API Key"] as label, card (card)}
          <button type="button" class="card" class:selected={selected === card} aria-pressed={selected === card} tabindex="-1" onclick={() => click(card)}>{label}</button>
        {/each}
      </div>
    </div>
  </Sheet>
{/if}

<style>
  .prompt {
    margin: 0 0 calc(16 * var(--px));
    font: var(--t-reading);
    color: var(--ink);
  }
  .cards {
    display: flex;
    flex-wrap: wrap;
    gap: calc(16 * var(--px));
  }
  .card {
    flex: 1 1 calc(220 * var(--px));
    min-width: calc(220 * var(--px));
    min-height: calc(120 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-reading);
    color: var(--ink);
    white-space: pre-line;
    cursor: pointer;
  }
  .card:hover {
    background: var(--control-hover);
  }
  .card.selected {
    border: 2px solid var(--ink);
    background: var(--band);
  }
</style>
