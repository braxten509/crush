<script>
  // MCP sign-in (desktop-design-5-6 2.9, look 5.10; DLG-239 to DLG-244):
  // internal/ui/dialog/mcpservers/auth.go. One server at a time from the
  // queue (" (n/total)" when several wait). Enter starts the flow: POST
  // mcp/auth blocks until it ends (so it carries a callId), while the sheet
  // asks for the auth URL every 200 ms and opens it once it appears; Enter
  // while waiting opens it again. C or U copies the auth URL (else the
  // server URL); S skips to the next server. Esc closes and stops the flow.
  import { onMount } from "svelte";
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { api, uuid } from "../../lib/engine/api.js";
  import { clipboard_write } from "../../lib/engine/commands.js";
  import { ext } from "../../lib/extensions/store.svelte.js";
  import { openLink } from "../../lib/render/links.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "mcp_auth";

  let at = $state(0);
  let state = $state("ready"); // "ready" | "waiting" | "success" | "error"
  let error = $state("");
  let authUrl = $state("");
  let callId = null;
  let poll = null;
  let alive = true;

  const total = $derived(ext.authQueue.length);
  const server = $derived(ext.authQueue[at] ?? null);
  const help = $derived.by(() => {
    if (state === "success") return [{ key: "enter", text: "finish" }];
    if (state === "error") return [{ key: "esc", text: "close" }];
    const pairs = [
      { key: "enter", text: state === "waiting" ? "open again" : "open browser" },
      { key: "c", text: "copy url" },
    ];
    if (total > 1) pairs.push({ key: "s", text: "skip" });
    pairs.push({ key: "esc", text: "cancel" });
    return pairs;
  });

  function stopPolling() {
    clearInterval(poll);
    poll = null;
  }

  async function start() {
    if (!server) return;
    const name = server.name;
    state = "waiting";
    error = "";
    authUrl = "";
    callId = uuid();
    const mine = callId;
    let opened = false;
    poll = setInterval(async () => {
      try {
        const answer = await api.mcpAuthUrl(name);
        if (!alive || mine !== callId) return;
        if (answer?.auth_url && !opened) {
          opened = true;
          authUrl = answer.auth_url;
          openLink(authUrl);
        }
      } catch {
        // Not ready yet.
      }
    }, 200);
    try {
      await api.mcpAuth(name, mine);
      if (!alive || mine !== callId) return;
      stopPolling();
      state = "success";
    } catch (e) {
      if (!alive || mine !== callId) return;
      stopPolling();
      state = "error";
      error = e?.code === "aborted" ? "authentication timed out" : (e?.message ?? String(e));
    }
  }

  function next() {
    stopPolling();
    if (callId) api.abort(callId);
    callId = null;
    state = "ready";
    if (at + 1 < total) at += 1;
    else finish();
  }

  function finish() {
    const done = server?.name;
    ext.authQueue = ext.authQueue.filter((s) => s.name !== done);
    if (ext.authQueue.length === 0) closeDialog(ID);
    else {
      at = Math.min(at, ext.authQueue.length - 1);
      state = "ready";
    }
  }

  async function copy() {
    const text = authUrl || server?.url || "";
    if (!text) return;
    try {
      await clipboard_write(text);
      notify("info", "URL copied to clipboard");
    } catch (e) {
      notify("error", e?.message ?? String(e));
    }
  }

  function onKey(event, combo) {
    switch (combo) {
      case "Enter":
      case "Ctrl+Y":
        if (state === "ready") start();
        else if (state === "waiting" && authUrl) openLink(authUrl);
        else if (state === "success") finish();
        return true;
      case "c":
      case "u":
        if (state !== "success") copy();
        return true;
      case "s":
        if (total > 1 && state !== "success") next();
        return true;
      default:
        return false;
    }
  }

  onMount(() => () => {
    alive = false;
    stopPolling();
    if (callId) api.abort(callId);
  });
</script>

<Sheet id={ID} title={`Authenticate with ${server?.name ?? ""}`} width="small" {help} {onKey}>
  {#if state === "ready"}
    <p class="body">Press enter to open your browser.{#if total > 1}<span class="count"> ({at + 1}/{total})</span>{/if}</p>
  {:else if state === "waiting"}
    <p class="status"><Icon name="working" size={16} /> Waiting for authorization...</p>
  {:else if state === "success"}
    <p class="done"><Icon name="check" size={16} /> Authentication successful!</p>
  {:else}
    <p class="failed">Authentication failed.</p>
    {#if error}<p class="error">{error}</p>{/if}
  {/if}
</Sheet>

<style>
  p {
    margin: 0;
  }
  .body,
  .done {
    font: var(--t-reading);
    color: var(--ink);
  }
  .count {
    color: var(--slate);
  }
  .status,
  .done {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
  }
  .status {
    font: var(--t-small);
    color: var(--slate);
  }
  .failed {
    font: var(--t-reading);
    color: var(--error-ink);
  }
  .error {
    margin-top: calc(6 * var(--px));
    font: var(--t-body-small);
    color: var(--ink);
  }
</style>
