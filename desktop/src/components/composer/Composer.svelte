<script>
  // The typing box (desktop-design 4.9; TYPE-001 to TYPE-061): the chip row,
  // the field with its mode mark and the model button, the send button, and
  // the `@` popup. A permission prompt or the plan handoff takes the place of
  // all of it (the slot, Q8). One block: its top is where the timeline's rail
  // ends. {sessionId} is the shown chat ("" on the landing page).
  import { tick, untrack } from "svelte";
  import Icon from "../Icon.svelte";
  import ChipRow from "./ChipRow.svelte";
  import Field from "./Field.svelte";
  import ModelButton from "./ModelButton.svelte";
  import ModeMark from "./ModeMark.svelte";
  import PermissionPrompt from "./PermissionPrompt.svelte";
  import PlanHandoff from "./PlanHandoff.svelte";
  import Popup from "./Popup.svelte";
  import Slot from "./Slot.svelte";
  import { composer, currentMode, focusField, hasChips, send } from "../../lib/stores/composer.svelte.js";
  import { prompts } from "../../lib/stores/prompts.svelte.js";
  import { closeSlot, openSlot, ui } from "../../lib/stores/ui.svelte.js";

  let { sessionId } = $props();

  let root = $state(null);
  let fullTop = $state(0);

  const mode = $derived(currentMode());

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

  function submit(event) {
    event.preventDefault();
    send();
  }
</script>

<div
  class="composer"
  class:slotted={Boolean(ui.slot)}
  class:chipped={!ui.slot && hasChips()}
  class:tall={Boolean(ui.slot) && composer.slotTall}
  class:full={Boolean(ui.slot) && composer.slotFull}
  style:--full-top={fullTop ? `${fullTop}px` : null}
  data-probe="composer"
  bind:this={root}
>
  {#if ui.slot}
    <Slot entry={ui.slot} />
  {:else}
    {#if hasChips()}<ChipRow />{/if}
    <form class="row" onsubmit={submit}>
      <div class="field" class:marked={mode !== "normal" || composer.badges.length > 0}>
        <ModeMark {mode} badges={composer.badges} />
        <Field />
        <span class="divider" aria-hidden="true"></span>
        <ModelButton />
      </div>
      <button class="send" type="submit" aria-label="Send">
        <Icon name="send" />
      </button>
    </form>
  {/if}
</div>
{#if composer.popup && !ui.slot}<Popup />{/if}

<style>
  .composer {
    flex: none;
    display: flex;
    flex-direction: column;
    min-height: 0;
    box-sizing: border-box;
    padding: calc(15 * var(--px)) calc(20 * var(--px)) calc(25 * var(--px));
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
  .field:focus-within {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  /* The text starts 8 picture px after the mode mark (TYPE-002). */
  .field.marked :global(.input) {
    margin-left: calc(8 * var(--px));
  }
  /* The divider and the controls keep the one-line row's place at the bottom as the field grows. */
  .divider {
    flex: none;
    align-self: flex-end;
    width: 1px;
    height: calc(42 * var(--px));
    margin: calc(7 * var(--px)) 0;
    background: var(--rule);
  }
  .send {
    flex: none;
    align-self: flex-end;
    width: calc(55 * var(--px));
    height: calc(58 * var(--px));
    display: grid;
    place-items: center;
    background: var(--vermilion-fill);
    color: var(--paper);
    border-radius: calc(2 * var(--px));
  }
  .send :global(.icon) {
    /* A paper plane reads centred a little right of its box. */
    transform: translateX(calc(3 * var(--px)));
  }
</style>
