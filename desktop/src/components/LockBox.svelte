<script>
  // The lock box (owner, 2026-11-03): the lock button asks first, since a
  // locked chat only opens again with the vault password. Like the quit
  // box: the question, a line under it, Cancel (selected first) and Lock.
  // While the vault is closed it also takes the vault password, so Crush
  // opens the vault itself (no KDE box to find); a wrong one shows under
  // the field and the box stays. The password is kept nowhere.
  import Buttons from "./kit/Buttons.svelte";
  import Field from "./kit/Field.svelte";
  import Sheet from "./kit/Sheet.svelte";
  import { closeDialog } from "../lib/dialogs.js";
  import { focusField } from "../lib/stores/composer.svelte.js";
  import { lockChat, sessions, vaultState } from "../lib/stores/sessions.svelte.js";

  const ID = "lock_chat";
  let { sessionId } = $props();

  const again = $derived(sessions.byId[sessionId]?.lock_state === "unlocked");
  let vault = $state(null); // {exists, open}, null until known
  let password = $state("");
  let error = $state("");
  let busy = $state(false);
  let selected = $state("cancel");

  // The field shows only while an existing vault is closed.
  const needsPassword = $derived(vault?.exists === true && vault?.open === false);

  $effect(() => {
    const sid = sessionId;
    vaultState(sid).then((state) => {
      if (sid !== sessionId) return;
      vault = state ?? { exists: true, open: false };
      if (needsPassword) queueMicrotask(() => document.querySelector(`[data-sheet="${ID}"] input`)?.focus());
    });
  });

  const buttons = $derived([
    { id: "cancel", label: "Cancel", key: "c", disabled: busy },
    { id: "lock", label: busy ? "Locking…" : "Lock", key: "l", disabled: busy || vault === null || (needsPassword && !password) },
  ]);

  async function press(id) {
    if (id !== "lock") {
      closeDialog(ID);
      focusField();
      return;
    }
    if (busy || vault === null || (needsPassword && !password)) return;
    busy = true;
    error = "";
    const done = await lockChat(sessionId, { password: needsPassword ? password : "" });
    busy = false;
    password = "";
    if (done === "wrong_password") {
      error = "Wrong password";
      return;
    }
    closeDialog(ID);
  }

  // Enter in the password field locks; the rest is the buttons' and the
  // sheet's (Esc cancels).
  function onKey(event, combo) {
    if (combo === "Enter" && event.target?.tagName === "INPUT") {
      press("lock");
      return true;
    }
    return false;
  }
</script>

<Sheet id={ID} title={null} label="Lock this chat" width="small" {onKey}>
  <div class="lock-box" data-lock-box={needsPassword ? "password" : "plain"}>
    <h2 class="question">{again ? "Lock this chat again?" : "Lock this chat?"}</h2>
    <p class="detail">It moves into the vault. You will need the vault password to open it again.</p>
    {#if needsPassword}
      <div class="field">
        <Field bind:value={password} masked placeholder="Vault password" label="Vault password" {error} />
      </div>
    {/if}
    <div class="buttons" data-selected={selected}>
      <Buttons {buttons} bind:selected onPress={press} />
    </div>
  </div>
</Sheet>

<style>
  .question {
    margin: 0;
    font: var(--t-heading);
    color: var(--ink);
  }
  .detail {
    margin: calc(8 * var(--px)) 0 0;
    font: var(--t-body-small);
    color: var(--slate);
  }
  .field {
    margin-top: calc(16 * var(--px));
  }
  .buttons {
    margin-top: calc(24 * var(--px));
  }
</style>
