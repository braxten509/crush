<script>
  // The AWS SSO sheet (desktop-design-5-6 2.4, look 5.6; DLG-071 to DLG-074):
  // a port of internal/ui/dialog/auth/aws_sso.go. It opens (with key grace)
  // when an agent_event "aws_sso_auth" arrives for this window's chat:
  // "Starting <command>..." until a link arrives, then the link and the wait;
  // a newer event's link replaces the old one. Enter/Ctrl+Y opens the link;
  // Esc closes the sheet without stopping the refresh. "aws_sso_auth_result"
  // closes it, or shows the failure until dismissed.
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { openLink } from "../../lib/render/links.js";
  import { awsSso } from "../../lib/signin/store.svelte.js";

  const ID = "aws_sso";

  const help = $derived(
    awsSso.state === "error"
      ? [{ key: "enter", text: "close" }]
      : [
          { key: "enter", text: "open in browser", disabled: !awsSso.url },
          { key: "esc", text: "cancel" },
        ],
  );

  function onKey(event, combo) {
    const enter = combo === "Enter" || combo === "Ctrl+Y";
    if (awsSso.state === "error" && (enter || combo === "Escape" || combo === "Alt+Escape")) {
      closeDialog(ID);
      return true;
    }
    if (enter) {
      if (awsSso.url) openLink(awsSso.url);
      return true;
    }
    return false;
  }
</script>

<Sheet id={ID} title="AWS SSO authentication" width="standard" {help} {onKey}>
  <div class="body">
    {#if awsSso.state === "error"}
      <p class="failed">Authentication failed.</p>
      {#if awsSso.error}<p class="error">{awsSso.error}</p>{/if}
    {:else if !awsSso.url}
      <p class="status"><Icon name="working" size={16} /> Starting <span class="mono">{awsSso.command}</span>...</p>
    {:else}
      <p class="lead">Press <span class="key">enter</span> to open the authorization page.</p>
      <!-- svelte-ignore a11y_invalid_attribute -->
      <a class="link" href={awsSso.url} onclick={(event) => (event.preventDefault(), openLink(awsSso.url))}>{awsSso.url}</a>
      <p class="status"><Icon name="working" size={16} /> Waiting for authentication...</p>
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
  .mono {
    font: var(--t-mono);
  }
  .link {
    display: inline-block;
    margin-top: calc(8 * var(--px));
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
