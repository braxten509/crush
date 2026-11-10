<script>
  // One folder's todos (owner, 2026-11-09, pick C), drawn in the folder after
  // its chats and terminals, or under "No project" (dir ""). A row has a tick
  // box and the words; ticking takes the todo away. A double-click edits the
  // words in place: Enter or leaving the box keeps them, Esc drops the edit,
  // empty words take the todo away. A right-click opens a small menu (the
  // folder "+" menu's look, ChatList.svelte): Edit, Delete, and Move to each
  // other folder or No project. The folder "+" menu's New Todo (owner,
  // 2026-11-09) opens a box under the rows for a new one: Enter adds it and
  // leaves the box open for the next, Esc or leaving it closes it (words
  // left in it are kept).
  import { unfold } from "../../lib/motion.js";
  import { bindKey } from "../../lib/keys.js";
  import { addTodo, dropTodo, editTodo, listTodos, moveTodo, stopTodo, todosIn } from "../../lib/stores/listtodos.svelte.js";

  // folders: every heading of the list, {dir, label}, for "Move to".
  let { dir = "", folders = [] } = $props();

  const items = $derived(todosIn(dir));

  let editing = $state(""); // the todo being edited
  let draft = $state("");
  const adding = $derived(listTodos.addingIn === dir);
  let fresh = $state("");

  function edit(todo) {
    closeMenu();
    editing = todo.id;
    draft = todo.text;
  }

  function keepEdit() {
    if (!editing) return;
    editTodo(editing, draft);
    editing = "";
  }

  function onEditKey(event) {
    event.stopPropagation();
    if (event.key === "Enter") {
      event.preventDefault();
      keepEdit();
    } else if (event.key === "Escape") {
      event.preventDefault();
      editing = "";
    }
  }

  function onAddKey(event) {
    event.stopPropagation();
    if (event.key === "Enter") {
      event.preventDefault();
      addTodo(fresh, dir);
      fresh = "";
    } else if (event.key === "Escape") {
      event.preventDefault();
      fresh = "";
      stopTodo();
    }
  }

  function leaveAdd() {
    addTodo(fresh, dir);
    fresh = "";
    if (adding) stopTodo();
  }

  function focusSelect(node) {
    node.focus();
    node.select();
  }

  // -- The right-click menu: at the pointer, kept inside the window. --
  let menu = $state(null); // {todo, x, y}
  let menuBox = $state(null);
  let menuPlace = $state(null);

  const moveTargets = $derived(menu ? [...folders.filter((f) => f.dir !== dir), ...(dir ? [{ dir: "", label: "No project" }] : [])] : []);

  function openMenu(event, todo) {
    event.preventDefault();
    menu = { todo, x: event.clientX, y: event.clientY };
  }

  function closeMenu() {
    menu = null;
  }

  function pick(fn) {
    const todo = menu?.todo;
    closeMenu();
    if (todo) fn(todo);
  }

  $effect(() => {
    if (!menu || !menuBox) {
      menuPlace = null;
      return;
    }
    const w = menuBox.offsetWidth;
    const h = menuBox.offsetHeight;
    menuPlace = {
      x: Math.max(0, Math.min(menu.x, window.innerWidth - w)),
      y: menu.y + h > window.innerHeight ? Math.max(0, menu.y - h) : menu.y,
    };
    menuBox.querySelector("button")?.focus({ preventScroll: true });
    const outside = (event) => {
      if (!menuBox?.contains(event.target)) closeMenu();
    };
    const away = () => closeMenu();
    window.addEventListener("mousedown", outside, true);
    window.addEventListener("wheel", away, { capture: true, passive: true });
    window.addEventListener("blur", away);
    window.addEventListener("resize", away);
    const unbind = [
      bindKey("overlay", "ArrowDown", () => step(1)),
      bindKey("overlay", "ArrowUp", () => step(-1)),
      bindKey("overlay", "Enter", enter),
      bindKey("overlay", "Escape", () => (closeMenu(), true)),
      bindKey("overlay", "Tab", () => (closeMenu(), false)),
      bindKey("overlay", "Shift+Tab", () => (closeMenu(), false)),
    ];
    return () => {
      for (const fn of unbind) fn();
      window.removeEventListener("mousedown", outside, true);
      window.removeEventListener("wheel", away, { capture: true });
      window.removeEventListener("blur", away);
      window.removeEventListener("resize", away);
    };
  });

  function entries() {
    return [...(menuBox?.querySelectorAll("button") ?? [])];
  }

  function step(by) {
    const list = entries();
    const at = list.indexOf(document.activeElement);
    list[(at + by + list.length) % list.length]?.focus({ preventScroll: true });
    return true;
  }

  function enter() {
    const item = entries().find((b) => b === document.activeElement);
    if (!item) return false;
    item.click();
    return true;
  }
</script>

<ul class="todos" aria-label="Todos">
  {#each items as todo (todo.id)}
    <li class="todo" data-todo={todo.id}>
      <button class="tick" type="button" aria-label="Done: {todo.text}" title="Done" onclick={() => dropTodo(todo.id)}></button>
      {#if editing === todo.id}
        <input class="box" type="text" aria-label="Todo" maxlength="300" bind:value={draft} use:focusSelect onkeydown={onEditKey} onblur={keepEdit} />
      {:else}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <span class="words" title={todo.text} ondblclick={() => edit(todo)} oncontextmenu={(event) => openMenu(event, todo)}>{todo.text}</span>
      {/if}
    </li>
  {/each}
  {#if adding}
    <li class="todo add-line">
      <span class="tick blank" aria-hidden="true"></span>
      <input class="box" type="text" aria-label="New todo" placeholder="Write a todo…" maxlength="300" bind:value={fresh} use:focusSelect onkeydown={onAddKey} onblur={leaveAdd} />
    </li>
  {/if}
</ul>

{#if menu}
  <!-- svelte-ignore a11y_interactive_supports_focus -->
  <div
    class="todo-menu"
    role="menu"
    aria-label="Todo"
    bind:this={menuBox}
    style:left="{menuPlace?.x ?? 0}px"
    style:top="{menuPlace?.y ?? 0}px"
    style:visibility={menuPlace ? null : "hidden"}
    transition:unfold={{ from: "top" }}
  >
    <button class="entry" type="button" role="menuitem" onclick={() => pick(edit)}>Edit</button>
    <button class="entry" type="button" role="menuitem" onclick={() => pick((t) => dropTodo(t.id))}>Delete</button>
    {#if moveTargets.length > 0}
      <div class="split" role="separator"></div>
      {#each moveTargets as target (target.dir)}
        <button class="entry" type="button" role="menuitem" title={target.dir || undefined} onclick={() => pick((t) => moveTodo(t.id, target.dir))}>Move to {target.label}</button>
      {/each}
    {/if}
  </div>
{/if}

<style>
  /* Under the folder's chats and terminals, 2 px between rows as between
     chats (ChatRow.svelte); text from 23 like theirs. */
  .todos {
    margin-top: 2px;
  }
  .todo {
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
    box-sizing: border-box;
    height: calc(38 * var(--px));
    padding: 0 calc(16 * var(--px)) 0 calc(23 * var(--px));
  }
  /* Centered on the words' capitals, not their line box: 1 px up
     (measured, owner 2026-11-09). */
  .tick {
    flex: none;
    position: relative;
    top: calc(-1.5 * var(--px));
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    padding: 0;
    border: 1px solid var(--slate);
    background: none;
    cursor: pointer;
  }
  .tick:hover {
    background: var(--control-hover);
  }
  .tick:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .words {
    flex: 1 1 auto;
    min-width: 0;
    font: var(--t-body-small);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: default;
  }
  .box {
    flex: 1 1 auto;
    min-width: 0;
    height: calc(30 * var(--px));
    padding: 0 calc(6 * var(--px));
    margin-left: calc(-7 * var(--px));
    box-sizing: border-box;
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    font: var(--t-body-small);
    color: var(--ink);
    outline: none;
  }
  .box:focus-visible {
    border-color: var(--vermilion);
  }
  /* The new todo's row keeps the tick box's room, so its box starts where
     an edited todo's does. */
  .tick.blank {
    visibility: hidden;
  }

  /* The folder "+" menu's paper and hairline (ChatList.svelte .plus-menu). */
  .todo-menu {
    position: fixed;
    z-index: 30;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    padding: calc(6 * var(--px)) 0;
    background: var(--paper-raised);
    border: 1px solid var(--rule-control);
  }
  .entry {
    box-sizing: border-box;
    min-height: calc(36 * var(--px));
    padding: calc(5 * var(--px)) calc(16 * var(--px));
    border: 0;
    background: none;
    font: var(--t-body-small);
    color: var(--ink);
    text-align: left;
    white-space: nowrap;
    cursor: default;
  }
  .entry:hover,
  .entry:focus-visible {
    background: var(--pick);
    outline: none;
  }
  .split {
    height: 1px;
    margin: calc(6 * var(--px)) 0;
    background: var(--rule-faint);
  }
</style>
