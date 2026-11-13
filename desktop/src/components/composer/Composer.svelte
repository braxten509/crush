<script>
  // The typing box (desktop-design 4.9; TYPE-001 to TYPE-061): the chip row,
  // the field with its mode mark and the model button, the send button, and
  // the `@` popup. A permission prompt or the plan handoff takes the place of
  // all of it (the slot, Q8). One block: its top is where the timeline's rail
  // ends. {sessionId} is the shown chat ("" on the landing page).
  import { tick, untrack } from "svelte";
  import { settle } from "../../lib/motion.js";
  import Icon from "../Icon.svelte";
  import ChipRow from "./ChipRow.svelte";
  import Field from "./Field.svelte";
  import ModelButton from "./ModelButton.svelte";
  import ModeMark from "./ModeMark.svelte";
  import PermissionPrompt from "./PermissionPrompt.svelte";
  import PlanHandoff from "./PlanHandoff.svelte";
  import QueueLine from "./QueueLine.svelte";
  import Popup from "./Popup.svelte";
  import PhoneMenu from "./PhoneMenu.svelte";
  import Slot from "./Slot.svelte";
  import { attachPhoneScreen, attachPhoneVideo, composer, currentMode, draftOf, focusField, hasChips, send, stop, working } from "../../lib/stores/composer.svelte.js";
  import { findPhone, phone, startRecording, stopRecording, watchPhone } from "../../lib/stores/phone.svelte.js";
  import { helperChats } from "../../lib/stores/helperchats.svelte.js";
  import { hasDialog, openDialog } from "../../lib/dialogs.js";
  import { askCloseChat } from "../../lib/stores/closechat.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import { prompts } from "../../lib/stores/prompts.svelte.js";
  import { closeSlot, openSlot, ui } from "../../lib/stores/ui.svelte.js";

  let { sessionId } = $props();

  let root = $state(null);
  let fullTop = $state(0);

  const mode = $derived(currentMode());

  // A sub-agent's chat keeps its typing box but takes no typing (owner,
  // 2026-10-22): what it is asked belongs to whoever started it.
  const readOnly = $derived(Boolean(helperChats.byId[sessionId]));

  // The send square shows Stop while the shown chat works and nothing is
  // typed or attached (owner, 2026-10-20, choice A); typing turns it back
  // into Send, which queues the message. Esc stops as before. One button
  // throughout, so nothing moves and the focus stays.
  const stopping = $derived(working() && draftOf().text.trim() === "" && draftOf().attachments.length === 0);

  function sendClick(event) {
    if (!stopping) return; // a submit: the form sends
    event.preventDefault();
    stop();
    focusField();
  }

  // DLG-119: the shown chat's permission request takes the slot; a newer one
  // replaces it, and one that waited behind another form opens when the slot frees.
  $effect(() => {
    const request = prompts.permission;
    const slot = ui.slot;
    if (!request || (slot?.kind === "permission" && slot.props?.request === request)) return;
    untrack(() => openSlot({ kind: "permission", component: PermissionPrompt, props: { request } }));
  });

  // TYPE-052: the ready plan's form, while it is meant to show for this chat
  // and nothing else holds the slot.
  $effect(() => {
    const handoff = composer.planHandoff;
    const wanted = Boolean(handoff?.open) && handoff.sessionId === sessionId;
    const holder = ui.slot?.kind ?? null;
    untrack(() => {
      if (wanted && !holder) openSlot({ kind: "plan", component: PlanHandoff, props: {} });
      else if (!wanted && holder === "plan") closeSlot("plan");
    });
  });

  // A closed slot gives the keys back to the field (and ends full mode).
  $effect(() => {
    if (ui.slot) return;
    untrack(() => {
      composer.slotFull = false;
      composer.slotTall = false;
      if (ui.focus === "slot") tick().then(focusField);
    });
  });

  // Full mode (DLG-138) covers the column from under the head's rule.
  $effect(() => {
    if (!composer.slotFull || !root) return;
    const column = root.closest(".conversation");
    const rule = column?.querySelector(".rule.under-head");
    fullTop = column && rule ? rule.getBoundingClientRect().bottom - column.getBoundingClientRect().top : 0;
  });

  $effect(() => watchPhone());

  async function phoneShot() {
    if (phone.busy) return;
    phone.busy = true;
    try {
      await attachPhoneScreen();
    } finally {
      phone.busy = false;
    }
    focusField();
  }

  let phoneMenu = $state(null);

  async function phoneClick(event) {
    if (phone.recording) {
      const sid = sessionId;
      const video = await stopRecording();
      if (video) attachPhoneVideo(video, sid);
      focusField();
      return;
    }
    phoneMenu = phoneMenu ? null : { button: event.currentTarget };
  }

  function phonePick(what) {
    if (what === "record") startRecording();
    else phoneShot();
  }

  // The lock button (owner, 2026-11-03): asks in the lock box first (with
  // the vault password while the vault is closed), then locks the chat into
  // the vault, or locks an unlocked locked chat again. Refused while the
  // chat works.
  const lockState = $derived(sessions.byId[sessionId]?.lock_state ?? "");
  const lockBusy = $derived(working());
  const locking = $derived(hasDialog("lock_chat"));
  function lock() {
    if (!sessionId || lockBusy || locking) return;
    openDialog("lock_chat", { sessionId });
  }

  function complete() {
    askCloseChat(sessionId);
  }

  function submit(event) {
    event.preventDefault();
    if (readOnly) return;
    send();
  }
</script>

<div
  class="composer"
  class:slotted={Boolean(ui.slot)}
  class:chipped={!ui.slot && hasChips() && !sessionId}
  class:tall={Boolean(ui.slot) && composer.slotTall}
  class:full={Boolean(ui.slot) && composer.slotFull}
  style:--full-top={fullTop ? `${fullTop}px` : null}
  data-probe="composer"
  bind:this={root}
>
  {#if ui.slot}
    <Slot entry={ui.slot} />
  {:else}
    {#if sessionId || hasChips() || phone.connected || phone.recording}
      <!-- Complete (owner, 2026-10-21, pick A): closes the shown chat for
           every app, as the list's × does. It sits on the right of the
           attached files' row (owner, 2026-10-21). -->
      <div class="complete-row">
        {#if sessionId && !readOnly}
          <button
            class="lock"
            type="button"
            title={lockBusy ? "Wait for the reply to finish before locking" : lockState === "unlocked" ? "Lock this chat again" : "Lock this chat"}
            aria-label={lockState === "unlocked" ? "Lock this chat again" : "Lock this chat"}
            disabled={lockBusy || locking}
            data-lock-chat={lockState || "none"}
            onclick={lock}
          >
            <Icon name="lock" size={20} />
          </button>
        {/if}
        <!-- The phone button (owner, 2026-11-01): the connected phone's
             screen as an attachment; with no phone, Find phone looks for it
             on Wi-Fi and connects. -->
        {#if phone.recording}
          <button class="phone" type="button" title={phone.stopping ? "Saving the recording…" : "Stop recording"} aria-label="Stop recording" disabled={phone.stopping} data-phone-recording onclick={phoneClick}>
            <span class="rec" class:saving={phone.stopping} aria-hidden="true"></span>
          </button>
        {:else if phone.connected}
          <button class="phone" type="button" title="Phone: take a picture or record" aria-label="Phone" aria-haspopup="menu" aria-expanded={Boolean(phoneMenu)} disabled={phone.busy} data-phone-shot onclick={phoneClick}>
            <Icon name="phone" size={20} />
          </button>
        {:else}
          <button class="phone find" type="button" disabled={phone.finding} data-phone-find onclick={findPhone}>
            <Icon name="phone" size={20} />
            <span>{phone.finding ? "Finding phone…" : "Find phone"}</span>
          </button>
        {/if}
        {#if hasChips()}<div class="chips-side"><ChipRow /></div>{:else if sessionId}<QueueLine />{/if}
        {#if sessionId}
          <button class="complete" type="button" onclick={complete}>
            <Icon name="check" />
            <span>Complete</span>
          </button>
        {/if}
      </div>
    {/if}
    <form class="row" onsubmit={submit} in:settle>
      <div class="field" class:marked={mode !== "normal"}>
        <ModeMark {mode} />
        <Field {readOnly} />
        <span class="divider" aria-hidden="true"></span>
        <ModelButton />
      </div>
      <button class="send" class:stopping type={stopping ? "button" : "submit"} disabled={readOnly && !stopping} aria-label={stopping ? "Stop" : "Send"} title={stopping ? "Stop (Esc)" : "Send (Enter)"} onclick={sendClick}>
        {#if stopping}<Icon name="stop" size={20} />{:else}<Icon name="send" />{/if}
      </button>
    </form>
  {/if}
</div>
{#if composer.popup && !ui.slot}<Popup />{/if}
{#if phoneMenu}<PhoneMenu button={phoneMenu.button} onpick={phonePick} onclose={() => (phoneMenu = null)} />{/if}

<style>
  /* Positioned: a leaving slot form lies over the returning row (Slot). */
  .composer {
    position: relative;
    flex: none;
    display: flex;
    flex-direction: column;
    min-height: 0;
    box-sizing: border-box;
    /* 15 below as above; the meter strip brings 18 more of its own while it
       shows, words midway in the 33 (MeterStrip; owner, 2026-10-20). */
    padding: calc(15 * var(--px)) calc(20 * var(--px));
  }
  /* The chip row brings its own 10 above and below in place of the top padding. */
  .composer.chipped {
    padding-top: 0;
  }
  /* A slot form grows upward to at most 60% of the middle column (4.9); a
     diff takes all of it. */
  .composer.slotted {
    max-height: 60%;
  }
  .composer.tall {
    height: 60%;
  }
  .composer.tall :global(.slot),
  .composer.full :global(.slot) {
    flex-grow: 1;
  }
  /* F: the form covers the column under the head's rule (DLG-138). */
  .composer.full {
    position: absolute;
    z-index: 2;
    inset: var(--full-top, 0) 0 0 0;
    height: auto;
    max-height: none;
    background: var(--paper);
  }
  .row {
    flex: none;
    display: flex;
    gap: calc(6 * var(--px));
  }
  .field {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: flex-start;
    border: 1px solid var(--rule-control);
  }
  /* No focus ring: the caret shows where typing goes (owner, 2026-10-19). */
  /* The text starts 8 picture px after the mode mark (TYPE-002). */
  .field.marked :global(.input) {
    margin-left: calc(8 * var(--px));
  }
  /* The model button stays centred as the field grows (owner, 2026-10-20);
     the divider runs the field's whole height, 7 short of each end, so it
     grows with it (owner, 2026-10-21). */
  .divider {
    flex: none;
    align-self: stretch;
    width: 1px;
    margin: calc(7 * var(--px)) 0;
    background: var(--rule);
  }
  /* Square, as tall as the one-line field; the plane's drawing is balanced
     in its own box, so it sits in the exact middle (owner, 2026-10-20: the
     old nudge to the right read as off-centre). */
  .phone {
    flex: none;
    display: flex;
    align-items: center;
    margin-right: auto;
    color: var(--slate);
  }
  /* The lock button, just left of the phone button and drawn like it
     (owner, 2026-11-03). */
  .lock {
    flex: none;
    display: flex;
    align-items: center;
    color: var(--slate);
  }
  .lock:hover {
    color: var(--ink);
  }
  .lock:disabled {
    opacity: 0.5;
  }
  .rec {
    display: block;
    width: calc(14 * var(--px));
    height: calc(14 * var(--px));
    margin: calc(3 * var(--px));
    border-radius: 50%;
    background: var(--vermilion);
    animation: rec-blink 1s steps(1, end) infinite;
  }
  .rec.saving {
    animation: none;
  }
  @keyframes rec-blink {
    50% {
      opacity: 0.15;
    }
  }
  .phone.find {
    gap: calc(6 * var(--px));
    font: var(--t-small);
  }
  .phone:hover {
    color: var(--ink);
  }
  .phone:disabled {
    opacity: 0.5;
  }
  .complete-row {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: calc(16 * var(--px));
    margin-bottom: calc(12 * var(--px));
  }
  .composer.chipped .complete-row {
    margin-bottom: 0;
  }
  /* Beside Complete the attachments fit its 36 px row, so adding one
     changes nothing above the field (owner, 2026-10-25: it moved a few px). */
  .chips-side {
    flex: 1;
    min-width: 0;
  }
  .chips-side :global(.chip-row) {
    padding: 0;
  }
  .complete {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    box-sizing: border-box;
    height: calc(36 * var(--px));
    padding: 0 calc(16 * var(--px));
    border: 0;
    background: var(--vermilion-fill);
    color: var(--on-accent);
    font: var(--t-button);
    white-space: nowrap;
    cursor: pointer;
  }
  .complete:hover span {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .complete:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }

  .send {
    flex: none;
    align-self: flex-end;
    width: calc(58 * var(--px));
    height: calc(58 * var(--px));
    display: grid;
    place-items: center;
    background: var(--vermilion-fill);
    color: var(--on-accent);
  }
</style>
