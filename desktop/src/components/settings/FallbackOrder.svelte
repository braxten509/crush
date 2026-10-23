<script>
  // Settings > Fallback order (owner, 2026-10-21; look C, one sentence per
  // block): the order chats move through when an account runs out. An on/off
  // switch, then the blocks top to bottom, then "+ Add block". Each block is
  // one sentence (lib/fallback/blocks.js sentence) whose underlined words open
  // a menu under them (WordMenu); every pick is saved at once
  // (lib/fallback/store.svelte.js). A block's handle drags it: it follows the
  // pointer and the others slide out of its way, and when let go it settles
  // into its place (owner's pick A); Alt+Up/Down moves it from the keyboard,
  // sliding the same way; × removes it. A block that is added grows in and
  // one that is removed shrinks away. With the system's reduce-motion
  // setting on, nothing slides.
  import { onMount, tick } from "svelte";
  import { fallback, loadFallback, saveOrder } from "../../lib/fallback/store.svelte.js";
  import { accountChoices, addList, blockKind, changeBlock, CODEX, effortsOf, fallbackProviders, listBlocks, listsOf, menuProviders, modelName, moveBlock, newBlock, removeList, sentence, withListBlocks } from "../../lib/fallback/blocks.js";
  import { modelData, watchModels } from "../../lib/models/store.svelte.js";
  import { MOTION_MS, reducedMotion, settle } from "../../lib/motion.js";
  import WordMenu from "./WordMenu.svelte";

  // The model choices: every configured provider, as the engine checks blocks
  // against them, the agent CLIs (accounts live there) first.
  const providers = $derived(fallbackProviders(modelData.cfg, modelData.catalog));
  const tools = $derived(menuProviders(providers));
  const ctx = $derived({ accounts: fallback.accounts, providers });

  const LABELS = { model: "Model", account: "Account", effort: "Effort", leave: "Move on", wait: "How long", meanwhile: "Meanwhile" };

  // The list shown (layout C, owner 2026-10-21): each picked model's own
  // list on the left, then every other model's (key ""); its blocks on the
  // right.
  let shownList = $state("");
  const lists = $derived(listsOf(fallback.order));
  const current = $derived(lists.some((l) => l.key === shownList) ? shownList : "");
  const blocks = $derived(listBlocks(fallback.order, current));
  const listName = (l) => (l.key ? modelName(providers, l.provider, l.model) : "Every other model");

  let adding = $state(false);
  let menu = $state(""); // the open word menu: "<block key>:<menu>"
  let drag = $state(null); // {from, to} while a block is held
  let ready = $state(false); // blocks grow in only once the first ones are drawn

  // Each block's key, kept across saves (the engine answers with new
  // objects), so a block keeps its row: first the same words at the same
  // place, then the same words anywhere (a move), then the same place (an
  // edit); any other block is new and grows in.
  let made = 0;
  let lastBlocks = [];
  let lastKeys = [];
  const empty = (value) => value === "" || value === 0 || value === false || value === null || value === undefined;
  const words = (block) =>
    JSON.stringify(
      Object.keys(block ?? {})
        .sort()
        .filter((field) => !empty(block[field]))
        .map((field) => [field, block[field]]),
    );
  function keysOf(blocks) {
    const before = lastBlocks.map((block, i) => ({ words: words(block), key: lastKeys[i], used: false }));
    const now = blocks.map(words);
    const keys = blocks.map(() => "");
    const take = (i, found) => {
      found.used = true;
      keys[i] = found.key;
    };
    now.forEach((w, i) => before[i] && !before[i].used && before[i].words === w && take(i, before[i]));
    now.forEach((w, i) => {
      const found = !keys[i] && before.find((b) => !b.used && b.words === w);
      if (found) take(i, found);
    });
    now.forEach((_, i) => {
      if (keys[i]) return;
      if (before[i] && !before[i].used) take(i, before[i]);
      else keys[i] = `b${made++}`;
    });
    lastBlocks = blocks;
    lastKeys = keys;
    return keys;
  }

  const shown = $derived(drag ? moveBlock(blocks, drag.from, drag.to) : blocks);
  const rows = $derived.by(() => {
    const keys = keysOf(shown);
    return shown.map((block, i) => ({ key: keys[i], block }));
  });

  onMount(() => {
    loadFallback();
    const stop = watchModels();
    return () => {
      stop();
      window.removeEventListener("pointermove", pull);
      window.removeEventListener("pointerup", drop);
      window.removeEventListener("pointercancel", drop);
    };
  });

  $effect(() => {
    if (fallback.loaded && !ready) requestAnimationFrame(() => (ready = true));
  });

  function save(nextBlocks, on = fallback.order.on) {
    saveOrder(withListBlocks({ ...fallback.order, on }, current, nextBlocks));
  }

  function showList(key) {
    menu = "";
    adding = false;
    shownList = key;
  }

  function newList(provider, model) {
    saveOrder(addList(fallback.order, provider, model));
    showList(`${provider}\n${model}`);
  }

  function dropList(key) {
    if (current === key) showList("");
    saveOrder(removeList(fallback.order, key));
  }

  // The models that have no list yet, by tool.
  const unlisted = $derived(tools.map((p) => ({ ...p, models: p.models.filter((m) => !lists.some((l) => l.provider === p.id && l.model === m.id)) })).filter((p) => p.models.length));

  function change(i, fields) {
    const next = [...blocks];
    next[i] = changeBlock(next[i], fields);
    save(next);
  }

  function add(type) {
    adding = false;
    save([...blocks, newBlock(type, { providers: tools })]);
  }

  function remove(i) {
    menu = "";
    save(blocks.filter((_, n) => n !== i));
  }

  // The motion. Rows that change places slide from where they were drawn to
  // their new places (the house slide: --motion on --ease-paper, with the
  // --paper-tick every animation that ends runs, app.css).
  let list = $state(null);
  const ease = () => getComputedStyle(list).getPropertyValue("--ease-paper").trim() || "ease-out";
  function slide(li, dy) {
    const motion = li.animate([{ transform: `translateY(${dy}px)`, "--paper-tick": 0 }, { transform: "translateY(0)", "--paper-tick": 1 }], { duration: MOTION_MS, easing: ease() });
    motion.id = "slide";
  }
  // Where each row is drawn now, mid-slide included.
  function drawnTops() {
    return new Map([...(list?.children ?? [])].map((li) => [li.dataset.key, li.getBoundingClientRect().top]));
  }
  // Once the rows have moved: each that moved slides from where it was drawn
  // (but `skip`, the row under the pointer).
  async function slideFrom(before, skip = "") {
    await tick();
    if (!list || reducedMotion()) return;
    for (const li of list.children) {
      if (li.dataset.key === skip || !before.has(li.dataset.key)) continue;
      for (const motion of li.getAnimations()) if (motion.id === "slide") motion.cancel();
      const dy = before.get(li.dataset.key) - li.getBoundingClientRect().top;
      if (Math.abs(dy) >= 0.5) slide(li, dy);
    }
  }

  async function move(i, to, key) {
    if (to < 0 || to >= blocks.length) return;
    menu = "";
    const before = drawnTops();
    save(moveBlock(blocks, i, to));
    await slideFrom(before);
    // The keys follow the block, so its handle keeps the focus.
    list?.querySelector(`[data-key="${key}"] .handle`)?.focus({ preventScroll: true });
  }

  function handleKey(event, i, key) {
    if (event.altKey && event.key === "ArrowUp") {
      event.preventDefault();
      move(i, i - 1, key);
    } else if (event.altKey && event.key === "ArrowDown") {
      event.preventDefault();
      move(i, i + 1, key);
    }
  }

  // The handle: the held block is drawn under the pointer (kept within the
  // list) while its row moves past every block whose middle it passes.
  let held = $state(null); // {key, li, startY, top, height, mids, first, last, dy}
  function grab(event, i, key) {
    if (event.button !== 0 || !list) return;
    event.preventDefault();
    menu = "";
    const lis = [...list.children];
    const li = lis.find((el) => el.dataset.key === key);
    if (!li) return;
    for (const motion of li.getAnimations()) if (motion.id === "slide") motion.cancel();
    const end = lis[lis.length - 1];
    held = {
      key,
      li,
      startY: event.clientY,
      top: li.offsetTop,
      height: li.offsetHeight,
      mids: lis.map((el) => el.offsetTop + el.offsetHeight / 2),
      first: lis[0].offsetTop,
      last: end.offsetTop + end.offsetHeight,
      dy: 0,
    };
    drag = { from: i, to: i };
    window.addEventListener("pointermove", pull);
    window.addEventListener("pointerup", drop);
    window.addEventListener("pointercancel", drop);
  }
  function follow() {
    if (held) held.li.style.transform = `translateY(${held.top + held.dy - held.li.offsetTop}px)`;
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
    slideFrom(before, held.key).then(follow);
    follow();
  }
  function drop() {
    window.removeEventListener("pointermove", pull);
    window.removeEventListener("pointerup", drop);
    window.removeEventListener("pointercancel", drop);
    if (!held || !drag) return;
    const { from, to } = drag;
    const { li } = held;
    const offset = held.top + held.dy - li.offsetTop;
    held = null;
    drag = null;
    if (from !== to) save(moveBlock(blocks, from, to));
    // It settles from under the pointer into its place.
    li.style.transform = "";
    if (Math.abs(offset) >= 0.5 && !reducedMotion()) slide(li, offset);
  }

  // A menu's number field (percent used, minutes): saved when it changes, or
  // on Enter, which also closes the menu; each value is saved once.
  function numberField(i, field, most, done) {
    const keep = (input) => {
      const value = Math.min(most, Math.max(1, Math.round(Number(input.value) || 0)));
      if (input.dataset.saved === String(value)) return;
      input.dataset.saved = String(value);
      change(i, { [field]: value });
    };
    return {
      onchange: (event) => keep(event.currentTarget),
      onkeydown: (event) => {
        if (event.key !== "Enter") return;
        event.preventDefault();
        keep(event.currentTarget);
        done();
      },
    };
  }
</script>

{#snippet pick(on, text, choose)}
  <button type="button" class="pick" role="menuitemradio" aria-checked={on} data-item onclick={choose}>{text}</button>
{/snippet}

{#snippet check(on, text, toggle)}
  <button type="button" class="pick" role="menuitemcheckbox" aria-checked={on} data-item onclick={toggle}>
    <span class="box" class:on aria-hidden="true">{#if on}<svg viewBox="0 0 12 12" focusable="false"><path d="M2.2 6.3 4.9 9 9.8 3.2" fill="none" stroke="currentColor" stroke-width="1.6" /></svg>{/if}</span>
    {text}
  </button>
{/snippet}

{#snippet menuOf(name, i, block, done)}
  {#if name === "model"}
    {#each tools as p (p.id)}
      <span class="heading" role="presentation">{p.name}</span>
      {#each p.models as m (m.id)}
        {@render pick(block.provider === p.id && block.model === m.id, m.name || m.id, () => {
          change(i, p.id === block.provider ? { model: m.id, effort: "" } : { provider: p.id, model: m.id, account: "", effort: "" });
          done();
        })}
      {/each}
    {/each}
  {:else if name === "account"}
    {#each accountChoices(block, ctx) as a (`${a.kind}/${a.id}`)}
      {@render pick(blockKind(block, providers) === a.kind && (block.account ?? "") === a.id, a.label || a.name, () => {
        change(i, block.type === "use" ? { account: a.id } : { kind: a.kind, account: a.id });
        done();
      })}
    {/each}
  {:else if name === "effort"}
    {#each ["", ...effortsOf(block, providers)] as level (level)}
      {@render pick((block.effort ?? "") === level, level || "default", () => {
        change(i, { effort: level });
        done();
      })}
    {/each}
  {:else if name === "leave"}
    {@const percent = Number(block.move_at_percent) > 0 ? Number(block.move_at_percent) : 0}
    {@render pick(!percent, "when it runs out", () => {
      change(i, { move_at_percent: 0 });
      done();
    })}
    <span class="number-row" class:on={percent > 0}>
      <button type="button" class="pick" role="menuitemradio" aria-checked={percent > 0} data-item onclick={() => (change(i, { move_at_percent: percent || 95 }), done())}>at</button>
      <input type="number" min="1" max="100" class="small" data-item aria-label="Move on at percent used" value={percent || 95} {...numberField(i, "move_at_percent", 100, done)} />
      <span class="unit">% used</span>
    </span>
    <span class="rule" role="separator"></span>
    {#if blockKind(block, providers) === CODEX}
      {@render check(Boolean(block.refill_first), "use a free refill first", () => change(i, { refill_first: !block.refill_first }))}
    {/if}
    {@render check(Boolean(block.come_back), "come back when it refills", () => change(i, { come_back: !block.come_back }))}
  {:else if name === "wait"}
    {@const minutes = Number(block.max_wait_minutes) > 0 ? Number(block.max_wait_minutes) : 0}
    {@render pick(!minutes, "with no time limit", () => {
      change(i, { max_wait_minutes: 0 });
      done();
    })}
    <span class="number-row" class:on={minutes > 0}>
      <button type="button" class="pick" role="menuitemradio" aria-checked={minutes > 0} data-item onclick={() => (change(i, { max_wait_minutes: minutes || 60 }), done())}>at most</button>
      <input type="number" min="1" class="small" data-item aria-label="Wait at most minutes" value={minutes || 60} {...numberField(i, "max_wait_minutes", Infinity, done)} />
      <span class="unit">minutes</span>
    </span>
  {:else if name === "meanwhile"}
    {@render pick(block.meanwhile !== "next", "just waiting", () => {
      change(i, { meanwhile: "" });
      done();
    })}
    {@render pick(block.meanwhile === "next", "using the next blocks meanwhile", () => {
      change(i, { meanwhile: "next" });
      done();
    })}
  {/if}
{/snippet}

<div class="fallback" data-probe="fallback-order">
  <div class="row head">
    <span class="name">Use the fallback order</span>
    <button type="button" class="switch" role="switch" aria-checked={fallback.order.on} aria-label="Use the fallback order" onclick={() => save(blocks, !fallback.order.on)}></button>
  </div>
  <p class="quiet">When a chat's account runs out, it moves down the list of the model it started on: the one you picked, or the one a sub-agent was started on. A model with no list of its own uses Every other model.</p>
  {#if fallback.error}<p class="error" role="alert">{fallback.error}</p>{/if}

  <div class="lists">
  <nav class="starts" aria-label="Lists by starting model" data-probe="fallback-lists">
    <span class="starts-head">Starts on</span>
    {#each lists as l (l.key)}
      <div class="start" class:on={l.key === current}>
        <button type="button" class="start-pick" aria-current={l.key === current ? "true" : undefined} data-list={l.key} onclick={() => showList(l.key)}>{listName(l)}</button>
        {#if l.key}<button type="button" class="start-remove" aria-label="Remove the {listName(l)} list" onclick={() => dropList(l.key)}>×</button>{/if}
      </div>
    {/each}
    {#if unlisted.length}
      <span class="start-add"><WordMenu text="+ Add a model" label="Add a list for a model" open={menu === "add-list"} ontoggle={(on) => (menu = on ? "add-list" : "")}
          >{#snippet children(done)}{#each unlisted as p (p.id)}<span class="heading" role="presentation">{p.name}</span>{#each p.models as m (m.id)}{@render pick(false, m.name || m.id, () => {
                  done();
                  newList(p.id, m.id);
                })}{/each}{/each}{/snippet}</WordMenu
        ></span>
    {/if}
  </nav>
  <div class="list-body">
  {#key current}
  <ol class="blocks" bind:this={list} class:off={!fallback.order.on} class:dragging={Boolean(drag)} aria-label="Fallback order">
    {#each rows as { key, block }, i (key)}
      <li class="item" data-block={i} data-key={key} class:lead={i === 0} class:raised={menu.startsWith(`${key}:`)} class:held={held?.key === key} in:settle={{ fold: true, off: !ready }} out:settle={{ fold: true }}>
        <div class="block" class:wait={block.type === "wait"}>
          <button type="button" class="handle" aria-label="Move block {i + 1}" title="Drag to move (Alt+Up/Down)" onpointerdown={(e) => grab(e, i, key)} onkeydown={(e) => handleKey(e, i, key)}><span class="grip" aria-hidden="true"><span></span><span></span><span></span><span></span><span></span><span></span></span></button>
          <div class="summary">
            <span class="number">{i + 1}</span>
            <span class="text">{#each sentence(block, ctx) as piece, p (piece.menu || `t${p}`)}{#if piece.menu}<WordMenu
                  text={piece.text}
                  label={LABELS[piece.menu]}
                  open={menu === `${key}:${piece.menu}`}
                  ontoggle={(on) => (menu = on ? `${key}:${piece.menu}` : "")}
                  >{#snippet children(done)}{@render menuOf(piece.menu, i, block, done)}{/snippet}</WordMenu
                >{:else}{piece.text}{/if}{/each}</span>
          </div>
          <button type="button" class="remove" aria-label="Remove block {i + 1}" onclick={() => remove(i)}>×</button>
        </div>
      </li>
    {/each}
  </ol>
  {/key}

  <div class="adder">
    {#if adding}
      <span class="choices" role="group" aria-label="Add a block">
        <button type="button" class="choice" disabled={!providers.length} title={providers.length ? undefined : "No models are set up yet"} onclick={() => add("use")}>Use an account</button>
        <button type="button" class="choice" onclick={() => add("wait")}>Wait for a refill</button>
        <button type="button" class="choice" onclick={() => add("refill")}>Use a free refill</button>
        <button type="button" class="choice" onclick={() => (adding = false)}>Cancel</button>
      </span>
    {:else}
      <button type="button" class="add" onclick={() => (adding = true)}>+ Add block</button>
    {/if}
  </div>
  </div>
  </div>
</div>

<style>
  .fallback {
    max-width: calc(1060 * var(--px));
  }
  .row {
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    min-height: calc(56 * var(--px));
    border-top: 1px solid var(--rule-control);
    border-bottom: 1px solid var(--rule-control);
  }
  .name {
    flex: 1;
    font: var(--t-item);
    color: var(--ink);
  }
  .quiet {
    margin: calc(12 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .error {
    margin: calc(12 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--rose, var(--vermilion));
  }
  .lists {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
  }
  .starts {
    display: flex;
    flex-direction: column;
    padding: calc(28 * var(--px)) calc(12 * var(--px)) 0 0;
    border-right: 1px solid var(--rule-faint);
  }
  .starts-head {
    padding: 0 calc(10 * var(--px)) calc(6 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .start {
    display: flex;
    align-items: center;
  }
  .start.on {
    background: var(--band);
    box-shadow: inset 2px 0 var(--vermilion);
  }
  .start-pick {
    flex: 1;
    min-width: 0;
    padding: calc(8 * var(--px)) calc(10 * var(--px));
    border: 0;
    background: none;
    font: var(--t-item);
    color: var(--ink);
    text-align: left;
    white-space: nowrap;
    cursor: pointer;
  }
  .start-remove {
    flex: none;
    width: calc(28 * var(--px));
    border: 0;
    background: none;
    font: var(--t-item);
    color: var(--slate);
    cursor: pointer;
    opacity: 0;
  }
  .start:hover .start-remove,
  .start-remove:focus-visible {
    opacity: 1;
  }
  .start-add {
    position: relative;
    padding: calc(8 * var(--px)) calc(10 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .list-body {
    min-width: 0;
    padding-left: calc(20 * var(--px));
  }
  .blocks {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .blocks.off .summary {
    color: var(--slate);
  }
  .blocks.off .block {
    border-color: var(--rule-faint);
  }
  .blocks.dragging {
    user-select: none;
  }
  /* Every block owns the room above it, where the ↓ from the block before
     sits (hidden over the first), so a block that grows in or shrinks away
     takes its room with it and nothing else jumps. */
  .item {
    position: relative;
    padding-top: calc(28 * var(--px));
  }
  .item::before {
    content: "↓";
    position: absolute;
    top: 0;
    left: calc(44 * var(--px));
    font: var(--t-small);
    line-height: calc(28 * var(--px));
    color: var(--slate);
  }
  .item.lead::before,
  .item.held::before {
    visibility: hidden;
  }
  .item.held {
    z-index: 2;
  }
  .item.raised {
    z-index: 30;
  }
  .block {
    position: relative;
    display: flex;
    min-height: calc(44 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
  }
  .block.wait {
    border-style: dashed;
  }
  .held .block {
    border-color: var(--ink);
    background: var(--band);
  }
  /* Both slots centre what they hold, both ways (the button reset leaves
     text at the left). */
  .handle,
  .remove {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(36 * var(--px));
    border: 0;
    background: none;
    font: var(--t-item);
    color: var(--slate);
    cursor: grab;
    touch-action: none;
  }
  .remove {
    cursor: pointer;
  }
  .held .handle {
    cursor: grabbing;
  }
  /* The handle's six dots, drawn: the font's ⠿ sat high in its line, by as
     much as its fallback font decides. Whole-pixel dots and gaps, as the
     busy dots; square and solid, as the font drew them (round ones this
     small come out paler). */
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
  .handle:hover,
  .remove:hover {
    color: var(--ink);
  }
  .summary {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    padding: calc(8 * var(--px)) calc(4 * var(--px));
    font: var(--t-item);
    color: var(--ink);
  }
  .number {
    flex: none;
    font: var(--t-small);
    color: var(--slate);
  }
  .text {
    min-width: 0;
  }
  /* The word menus' items (WordMenu draws the box). */
  .pick {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    box-sizing: border-box;
    width: 100%;
    min-height: calc(32 * var(--px));
    padding: 0 calc(14 * var(--px));
    border: 0;
    border-radius: 0;
    background: none;
    font: var(--t-button);
    color: var(--ink);
    text-align: left;
    cursor: pointer;
  }
  .pick:hover {
    background: var(--control-hover);
  }
  .pick[role="menuitemradio"][aria-checked="true"] {
    background: var(--pick);
  }
  .number-row.on {
    background: var(--band);
  }
  .pick:focus-visible,
  .small:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: -1px;
  }
  .heading {
    display: block;
    padding: calc(8 * var(--px)) calc(14 * var(--px)) calc(2 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .number-row {
    display: flex;
    align-items: center;
    gap: calc(6 * var(--px));
    padding-right: calc(14 * var(--px));
  }
  .number-row .pick {
    width: auto;
    padding-right: 0;
  }
  .number-row.on .pick {
    background: none;
  }
  .rule {
    display: block;
    height: 1px;
    margin: calc(4 * var(--px)) 0;
    background: var(--rule-faint);
  }
  .box {
    flex: none;
    display: grid;
    place-items: center;
    box-sizing: border-box;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
  }
  .box.on {
    border-color: var(--ink);
    color: var(--ink);
  }
  .box svg {
    width: calc(12 * var(--px));
    height: calc(12 * var(--px));
  }
  .choices {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: calc(6 * var(--px));
  }
  .choice,
  .add {
    height: calc(32 * var(--px));
    padding: 0 calc(12 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
  }
  .choice:disabled {
    color: var(--slate);
    cursor: default;
  }
  .choice:hover:not(:disabled),
  .add:hover {
    background: var(--control-hover);
  }
  .add {
    border-style: dashed;
    color: var(--slate);
  }
  .small {
    width: calc(64 * var(--px));
    height: calc(28 * var(--px));
    padding: 0 calc(8 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
    font: var(--t-field);
    color: var(--ink);
  }
  .unit {
    font: var(--t-small);
    color: var(--slate);
  }
  .adder {
    margin-top: calc(10 * var(--px));
  }
  .adder .add {
    width: 100%;
    text-align: left;
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
  .handle:focus-visible,
  .remove:focus-visible,
  .switch:focus-visible,
  .choice:focus-visible,
  .add:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
</style>
