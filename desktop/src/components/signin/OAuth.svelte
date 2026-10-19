<script>
  // The sign-in sheet (desktop-design-5-6 2.4, look 5.6; DLG-054 to DLG-070):
  // a port of internal/ui/dialog/auth/oauth.go, driven by G-5's flow routes
  // (the engine runs the flow and saves the credential; nothing secret comes
  // to the page). States: initializing, display (a device code to copy, or a
  // browser flow, Grok's with a code field), saving, success, error. C copies
  // the code, U the link; Enter/Ctrl+Y opens the browser (a device flow
  // copies its code first); Tab goes to Grok's code field, where Enter sends
  // the code (empty: opens the browser) and Esc leaves the field. Esc in the
  // display or error state cancels the flow; on success Enter/Ctrl+Y/Esc goes
  // on with the model choice (onDone("done")).
  //   providerId, providerName?, onboarding?, onDone(result)
  import { onDestroy, untrack } from "svelte";
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { api } from "../../lib/engine/api.js";
  import { clipboard_write } from "../../lib/engine/commands.js";
  import { openLink } from "../../lib/render/links.js";
  import { PLATFORMS, startFlow, stopListening } from "../../lib/signin/flows.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "oauth";

  let props = $props();

  // The provider is fixed for the sheet's life.
  const platform = untrack(() => PLATFORMS[props.providerId] ?? { platform: props.providerId, name: props.providerName || props.providerId });
  let state = $state("initializing"); // "initializing" | "display" | "saving" | "success" | "error"
  let flow = $state(null); // {flow_id, url, user_code, code_entry}
  let error = $state("");
  let code = $state("");
  let codeFocused = $state(false);
  let finished = false;

  const codeEntry = $derived(state === "display" && flow?.code_entry === true && !flow?.user_code);
  const help = $derived.by(() => {
    if (state === "error") return [{ key: "esc", text: "exit" }];
    if (state === "success") return [{ key: "enter", text: "finish" }];
    if (state === "saving" || state === "initializing") return [];
    if (codeEntry && codeFocused)
      return [
        { key: "enter", text: "submit code" },
        { key: "esc", text: "back" },
      ];
    const pairs = [
      { key: "u", text: "copy url" },
      { key: "enter", text: flow?.user_code ? "copy & open" : "open browser" },
      { key: "esc", text: "exit" },
    ];
    if (flow?.user_code) pairs.unshift({ key: "c", text: "copy code" });
    if (codeEntry) pairs.unshift({ key: "tab", text: "enter code" });
    return pairs;
  });

  function onState(event) {
    if (finished) return;
    if (event.state === "waiting") state = "display";
    else if (event.state === "saving") state = "saving";
    else if (event.state === "done") state = "success";
    else if (event.state === "failed") {
      state = "error";
      error = event.error ?? "";
    }
  }

  (async () => {
    try {
      const started = await startFlow(platform.platform, onState);
      if (finished) {
        // Esc came while the flow was starting: stop it now.
        api.cancelAuthFlow(started.flow_id).catch(() => {});
        stopListening(started.flow_id);
        return;
      }
      flow = started;
      if (state === "initializing") state = "display";
    } catch (e) {
      if (finished) return;
      state = "error";
      error = e?.message ?? String(e);
    }
  })();

  async function copy(text, notice) {
    try {
      await clipboard_write(text);
      notify("info", notice);
    } catch (e) {
      notify("error", e?.message ?? String(e));
    }
  }

  async function openBrowser() {
    if (!flow?.url) return;
    if (flow.user_code) {
      try {
        await clipboard_write(flow.user_code);
      } catch {
        // The link still opens.
      }
      await openLink(flow.url);
      notify("info", "Code copied and URL opened");
      return;
    }
    openLink(flow.url);
  }

  function finish(result) {
    if (finished) return;
    finished = true;
    if (flow) stopListening(flow.flow_id);
    closeDialog(ID);
    props.onDone?.(result);
  }

  function cancel() {
    if (flow && (state === "display" || state === "initializing")) api.cancelAuthFlow(flow.flow_id).catch(() => {});
    finish("cancelled");
  }

  async function submitCode() {
    const text = code.trim();
    if (!text) {
      openBrowser();
      return;
    }
    try {
      await api.sendAuthCode(flow.flow_id, text);
      code = "";
    } catch (e) {
      notify("error", e?.message ?? String(e));
    }
  }

  function codeField() {
    return document.querySelector(`[data-sheet="${ID}"] .code-field input`);
  }

  function onKey(event, combo) {
    const enter = combo === "Enter" || combo === "Ctrl+Y";
    const esc = combo === "Escape" || combo === "Alt+Escape";
    if (state === "saving") return true;
    if (state === "success") {
      if (enter || esc) finish("done");
      return true;
    }
    if (state === "error") {
      if (esc || enter) finish("cancelled");
      return true;
    }
    if (codeEntry && codeFocused) {
      if (enter) {
        submitCode();
        return true;
      }
      if (esc) {
        codeField()?.blur();
        return true;
      }
      return false;
    }
    if (esc) {
      cancel();
      return true;
    }
    if (state !== "display") return true;
    switch (combo) {
      case "c":
        if (flow?.user_code) copy(flow.user_code, "Code copied to clipboard");
        return true;
      case "u":
        if (flow?.url) copy(flow.url, "URL copied to clipboard");
        return true;
      case "Tab":
        if (codeEntry) codeField()?.focus();
        return true;
      default:
        if (enter) {
          openBrowser();
          return true;
        }
        return false;
    }
  }

  onDestroy(() => {
    if (!finished && flow) {
      if (state === "display") api.cancelAuthFlow(flow.flow_id).catch(() => {});
      stopListening(flow.flow_id);
    }
  });
</script>

<Sheet id={ID} title={`Let’s authenticate with ${platform.name}`} width="standard" {help} {onKey} onboarding={props.onboarding === true}>
  <div class="body">
    {#if state === "initializing"}
      <p class="status"><Icon name="working" size={16} /> Initializing...</p>
    {:else if state === "display"}
      <p class="lead">Press <span class="key">enter</span> to {flow?.user_code ? "copy the code below and open the browser." : "open the browser and authenticate."}</p>
      {#if flow?.user_code}
        <div class="code">{flow.user_code}</div>
      {/if}
      <p class="small">Browser not opening? Pay a visit to:</p>
      <!-- svelte-ignore a11y_invalid_attribute -->
      <a class="link" href={flow?.url ?? "#"} onclick={(event) => (event.preventDefault(), flow?.url && openLink(flow.url))}>{flow?.url ?? ""}</a>
      <p class="status"><Icon name="working" size={16} /> {flow?.user_code ? "Verifying..." : "Waiting for browser authentication..."}</p>
      {#if codeEntry}
        <p class="small">Declined or browser didn't connect? Enter the code from the page:</p>
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="code-field" onfocusin={() => (codeFocused = true)} onfocusout={() => (codeFocused = false)}>
          <Field bind:value={code} placeholder="Paste the code from the browser..." label="Code from the browser" />
        </div>
      {/if}
    {:else if state === "saving"}
      <p class="status"><Icon name="working" size={16} /> Fetching models...</p>
    {:else if state === "success"}
      <p class="done"><Icon name="check" size={16} /> Authentication successful!</p>
    {:else}
      <p class="failed">Authentication failed.</p>
      {#if error}<p class="error">{error}</p>{/if}
    {/if}
  </div>
</Sheet>

<style>
  .body p {
    margin: 0;
  }
  .lead {
    font: var(--t-reading);
    color: var(--ink);
  }
  .key {
    color: var(--keyword);
  }
  .code {
    position: relative;
    display: grid;
    place-items: center;
    height: calc(72 * var(--px));
    margin: calc(16 * var(--px)) 0;
    border: 1px solid var(--rule-table);
    font: 400 calc(28 * var(--px)) / calc(34 * var(--px)) var(--face-mono);
    letter-spacing: 0.12em;
    color: var(--ink);
    user-select: all;
  }
  .small {
    margin-top: calc(12 * var(--px)) !important;
    font: var(--t-small);
    color: var(--slate);
  }
  .link {
    display: inline-block;
    margin-top: calc(4 * var(--px));
    font: var(--t-small);
    color: var(--link);
    text-decoration: underline;
    overflow-wrap: anywhere;
  }
  .status {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    margin-top: calc(16 * var(--px)) !important;
    font: var(--t-small);
    color: var(--slate);
  }
  .code-field {
    margin-top: calc(8 * var(--px));
  }
  .done {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    font: var(--t-reading);
    color: var(--ink);
  }
  .failed {
    font: var(--t-reading);
    color: var(--error-ink);
  }
  .error {
    margin-top: calc(6 * var(--px)) !important;
    font: var(--t-body-small);
    color: var(--ink);
  }
</style>
