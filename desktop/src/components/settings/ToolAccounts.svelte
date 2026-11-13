<script>
  // Settings > Accounts, one tool's sign-ins (owner, 2026-10-21): each tool's
  // own login is account 1; more accounts each get a folder of their own that
  // shares account 1's settings, skills, tools and saved chats. "Add" makes
  // one and opens a terminal tab that signs it in. Every account, the tools'
  // own logins too, is renamed in place; one not renamed shows the email it
  // signed in with (owner, 2026-10-21). A row is just the account's name
  // (owner, 2026-10-24: no tool or sign-in subtext). Removing asks once more
  // on the row itself. With more than one account, each row's handle drags
  // it to a new place (owner, 2026-11-13, pick B): the order is saved and
  // every account list in the app follows it; Alt+Up/Down moves it from the
  // keyboard.
  //   kind, name, ownLogin? (argv), ownSignedIn? (state read by the engine)
  import { onMount, tick } from "svelte";
  import { moveBlock } from "../../lib/fallback/blocks.js";
  import { addAccount, fallback, loadFallback, removeAccount, renameAccount, saveAccountOrder, signInAccount } from "../../lib/fallback/store.svelte.js";
  import { MOTION_MS, reducedMotion } from "../../lib/motion.js";
  import { sign_in_run } from "../../lib/engine/commands.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  let { kind, name, ownLogin = null, ownSignedIn = null } = $props();

  let renaming = $state(""); // "kind/id" of the row being renamed
  let draft = $state("");
  let removing = $state(""); // the account asked about
  let busy = $state(false);

  onMount(() => {
    if (!fallback.loaded) loadFallback();
  });

  const accounts = $derived(fallback.accounts.filter((a) => a.kind === kind));

  // The drag, as Fallback order's: the held row follows the pointer (kept
  // within the list) while the others slide past it; let go, it settles in.
  let list = $state(null);
  let drag = $state(null); // {from, to}
  let held = $state(null); // {id, el, startY, top, height, mids, first, last, dy}
  const shown = $derived(drag ? moveBlock(accounts, drag.from, drag.to) : accounts);
  const movable = $derived(accounts.length > 1);

  const ease = () => getComputedStyle(list).getPropertyValue("--ease-paper").trim() || "ease-out";
  function slide(el, dy) {
    const motion = el.animate([{ transform: `translateY(${dy}px)` }, { transform: "translateY(0)" }], { duration: MOTION_MS, easing: ease() });
    motion.id = "slide";
  }
  function drawnTops() {
    return new Map([...(list?.children ?? [])].map((el) => [el.dataset.id, el.getBoundingClientRect().top]));
  }
  async function slideFrom(before, skip = null) {
    await tick();
    if (!list || reducedMotion()) return;
    for (const el of list.children) {
      if (el.dataset.id === skip || !before.has(el.dataset.id)) continue;
      for (const motion of el.getAnimations()) if (motion.id === "slide") motion.cancel();
      const dy = before.get(el.dataset.id) - el.getBoundingClientRect().top;
      if (Math.abs(dy) >= 0.5) slide(el, dy);
    }
  }
  const save = (ordered) => saveAccountOrder(kind, ordered.map((a) => a.id));

  async function move(i, to, id) {
    if (to < 0 || to >= accounts.length) return;
    const before = drawnTops();
    save(moveBlock(accounts, i, to));
    await slideFrom(before);
    list?.querySelector(`[data-id="${CSS.escape(id)}"] .handle`)?.focus({ preventScroll: true });
  }
  function handleKey(event, i, id) {
    if (!event.altKey || (event.key !== "ArrowUp" && event.key !== "ArrowDown")) return;
    event.preventDefault();
    move(i, event.key === "ArrowUp" ? i - 1 : i + 1, id);
  }

  function grab(event, i, id) {
    if (event.button !== 0 || !list) return;
    event.preventDefault();
    const els = [...list.children];
    const el = els.find((e) => e.dataset.id === id);
    if (!el) return;
    for (const motion of el.getAnimations()) if (motion.id === "slide") motion.cancel();
    const end = els[els.length - 1];
    held = {
      id,
      el,
      startY: event.clientY,
      top: el.offsetTop,
      height: el.offsetHeight,
      mids: els.map((e) => e.offsetTop + e.offsetHeight / 2),
      first: els[0].offsetTop,
      last: end.offsetTop + end.offsetHeight,
      dy: 0,
    };
    drag = { from: i, to: i };
    window.addEventListener("pointermove", pull);
    window.addEventListener("pointerup", drop);
    window.addEventListener("pointercancel", drop);
  }
  function follow() {
    if (held) held.el.style.transform = `translateY(${held.top + held.dy - held.el.offsetTop}px)`;
  }
  function pull(event) {
    if (!held || !drag) return;
    held.dy = Math.max(held.first - held.top, Math.min(held.last - held.top - held.height, event.clientY - held.startY));
    const middle = held.top + held.height / 2 + held.dy;
    let to = 0;
    held.mids.forEach((mid, n) => {
      if (n !== drag.from && mid < middle) to += 1;
    });
    if (to === drag.to) {
      follow();
      return;
    }
    const before = drawnTops();
    drag = { ...drag, to };
    slideFrom(before, held.id).then(follow);
    follow();
  }
  function drop() {
    window.removeEventListener("pointermove", pull);
    window.removeEventListener("pointerup", drop);
    window.removeEventListener("pointercancel", drop);
    if (!held || !drag) return;
    const { from, to } = drag;
    const { el } = held;
    const offset = held.top + held.dy - el.offsetTop;
    held = null;
    drag = null;
    if (from !== to) save(moveBlock(accounts, from, to));
    el.style.transform = "";
    if (Math.abs(offset) >= 0.5 && !reducedMotion()) slide(el, offset);
  }

  // Account 1's state comes from the CLI's own login when the engine read it.
  const signedIn = (a) => (a.id ? a.signed_in : (ownSignedIn ?? a.signed_in));

  function startRename(a) {
    renaming = `${a.kind}/${a.id}`;
    draft = a.name;
  }
  function finishRename(a, event) {
    if (event?.key && event.key !== "Enter" && event.key !== "Escape") return;
    if (event?.key === "Escape") {
      renaming = "";
      return;
    }
    renaming = "";
    // A tool's own login goes by the tool's name.
    if (draft.trim() !== a.name) renameAccount(a.id || a.kind, draft.trim());
  }

  // Account 1 signs in with the tool itself; the app runs that command with
  // no terminal, as it does for an added account (sign_in_run).
  async function signInOwn() {
    if (busy || !ownLogin?.length) return;
    busy = true;
    try {
      const ok = await sign_in_run(ownLogin);
      if (ok) notify("info", `Signed in to ${name}.`);
      else notify("error", `Signing in to ${name} didn't finish.`);
    } catch (error) {
      notify("error", `Couldn't start the sign-in: ${error?.message ?? String(error)}`);
    } finally {
      busy = false;
      await loadFallback();
    }
  }
</script>

<div class="list" class:dragging={Boolean(drag)} bind:this={list}>
{#each shown as a, i (a.id)}
  <div class="row account" class:held={held?.id === a.id} data-id={a.id} data-account={`${a.kind}/${a.id}`}>
    {#if movable}
      <button type="button" class="handle" aria-label="Move {a.name}" title="Drag to move (Alt+Up/Down)" onpointerdown={(e) => grab(e, i, a.id)} onkeydown={(e) => handleKey(e, i, a.id)}><span class="grip" aria-hidden="true"><span></span><span></span><span></span><span></span><span></span><span></span></span></button>
    {/if}
    <span class="name" title={renaming === `${a.kind}/${a.id}` ? undefined : a.name}>
      {#if renaming === `${a.kind}/${a.id}`}
        <!-- svelte-ignore a11y_autofocus -->
        <input class="rename" type="text" bind:value={draft} aria-label="Account name" autofocus onkeydown={(e) => finishRename(a, e)} onblur={() => finishRename(a)} />
      {:else}
        {a.name}
      {/if}
    </span>
    {#if removing === a.id && a.id}
      <span class="value">Remove {a.name}? Its sign-in is deleted.</span>
      <button type="button" class="act" onclick={() => ((removing = ""), removeAccount(a.id))}>Yes, remove</button>
      <button type="button" class="act" onclick={() => (removing = "")}>No</button>
    {:else}
      {#if a.id}
        <button type="button" class="act" onclick={() => signInAccount(a.id)}>{a.signed_in ? "Sign in again" : "Sign in"}</button>
        <button type="button" class="act" onclick={() => (removing = a.id)}>Remove</button>
      {:else if ownLogin}
        <button type="button" class="act" disabled={busy} onclick={signInOwn}>{signedIn(a) ? "Sign in again" : "Sign in"}</button>
      {/if}
      <!-- Last on every row, so it keeps one place (my-ui-design). -->
      <button type="button" class="act" onclick={() => startRename(a)}>Rename</button>
    {/if}
  </div>
{/each}
</div>
<div class="row">
  <span class="name quiet-name">Another {name} account</span>
  <button type="button" class="act" onclick={() => addAccount(kind)}>Add…</button>
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    min-height: calc(56 * var(--px));
    padding: calc(6 * var(--px)) 0;
    border-bottom: 1px solid var(--rule-control);
  }
  /* A tool's accounts and its "Another account" row stand together; the
     line comes after the group (owner, 2026-10-21). The clear line keeps
     every row the same height. */
  .account {
    position: relative;
    border-bottom-color: transparent;
  }
  .dragging {
    user-select: none;
  }
  .held {
    z-index: 2;
    background: var(--band);
  }
  /* Fallback order's handle: six drawn dots, grabbed with the pointer. */
  .handle {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(20 * var(--px));
    align-self: stretch;
    margin-right: calc(-6 * var(--px));
    padding: 0;
    border: 0;
    background: none;
    color: var(--slate);
    cursor: grab;
    touch-action: none;
  }
  .held .handle {
    cursor: grabbing;
  }
  .handle:hover {
    color: var(--ink);
  }
  .grip {
    display: grid;
    grid-template-columns: repeat(2, round(calc(3 * var(--px)), 1px));
    gap: round(calc(3 * var(--px)), 1px);
  }
  .grip span {
    width: round(calc(3 * var(--px)), 1px);
    height: round(calc(3 * var(--px)), 1px);
    background: currentColor;
  }
  /* A long name ends in "…" before the buttons; hovering shows it whole
     (owner, 2026-10-24, pick A). */
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-item);
    color: var(--ink);
  }
  .quiet-name {
    color: var(--slate);
  }
  .value {
    font: var(--t-small);
    color: var(--slate);
  }
  .act {
    flex: none;
    height: calc(36 * var(--px));
    padding: 0 calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
  }
  .act:hover {
    background: var(--control-hover);
  }
  .act:disabled {
    color: var(--slate);
    cursor: default;
  }
  .rename {
    width: calc(220 * var(--px));
    height: calc(36 * var(--px));
    padding: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
    font: var(--t-field);
    color: var(--ink);
  }
  .act:focus-visible,
  .handle:focus-visible,
  .rename:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
</style>
