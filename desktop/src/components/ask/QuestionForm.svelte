<script>
  // The question form in the typing box's place (desktop-design-5-6 2.6,
  // 2.7, look 5.8; DLG-141 to DLG-201): a port of
  // internal/ui/dialog/questionform. A batch has a tab row ending in
  // "Confirm"; one question has neither. [ and ] (or Ctrl+←/→) switch tabs,
  // keeping live answers; Enter answers and moves on (the last question goes
  // to Confirm, a lone one submits); Esc cancels the batch, or only closes an
  // open note. Yes/no starts on No; choices wrap through their rows and the
  // "Something else?" field; multi choice toggles with Space or a click. Tab
  // gives the focus to the chat, and the form folds to one line when it is
  // taller than 2/5 of the column; clicking it brings it back. With `secure`
  // (SecureForm), secure_entry questions are masked fields whose values stay
  // in this component until submit.
  // With `local` (lib/ask/localform.js), the answers go to local.onSubmit and
  // nothing is sent to the engine; local.onGone runs if the form leaves
  // unanswered (another form took the slot).
  //   request (QuestionRequest), secure? {fields, onSubmit(answers, values)
  //   → {questionId, text} | null, onCancel()}, local? {onSubmit(answers),
  //   onCancel(), onGone()}
  import { onMount, tick, untrack } from "svelte";
  import Area from "../kit/Area.svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Field from "../kit/Field.svelte";
  import MarkdownView from "../render/MarkdownView.svelte";
  import Rule from "../Rule.svelte";
  import Picture from "./Picture.svelte";
  import SecureField from "./SecureField.svelte";
  import { cancelQuestions, submitAnswers } from "../../lib/ask/answers.js";
  import { CUSTOM_MAX, FREE_MAX, initialState, isAnswered, NOTE_MAX, respond, summaryLine, tabLabels, typeOf } from "../../lib/ask/form.js";
  import { statusText, valueError } from "../../lib/ask/secure.js";
  import { dialogs } from "../../lib/dialogs.js";
  import { bindKey, comboOf as comboFor } from "../../lib/keys.js";
  import { attachSlot, focusTimeline } from "../../lib/stores/composer.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import { setFocus, ui } from "../../lib/stores/ui.svelte.js";

  let { request, secure = null, local = null } = $props();

  const items = untrack(() => request.questions ?? []);
  const labels = untrack(() => tabLabels(request));
  const hasConfirm = items.length > 1;
  const confirmTab = items.length;

  let states = $state(items.map(initialState));
  let answers = $state(items.map(() => null));
  let active = $state(0);
  let confirmChoice = $state("yup");
  let noteKey = $state(null); // the choice id (or question id) whose note is open
  let noteText = $state("");
  let hovered = $state(-1);
  let secureValues = $state(Object.fromEntries(items.filter((i) => typeOf(i) === "secure_entry").map((i) => [i.id, ""])));
  let secureErrors = $state({});
  let root = $state(null);
  let tall = $state(false);
  let sending = false;
  let finished = false;

  const item = $derived(active < items.length ? items[active] : null);
  const state = $derived(active < items.length ? states[active] : null);
  const kind = $derived(item ? typeOf(item) : "confirm");
  const focused = $derived(ui.focus === "slot");
  const collapsed = $derived(!focused && tall);
  const answeredCount = $derived(items.filter((it, i) => answeredAt(i)).length);
  const unanswered = $derived(items.length - answeredCount);

  // A page's picture: the hovered row's on the active page, else the cursor's.
  function pictureOf(i) {
    const it = items[i];
    if (!it?.choices?.some((c) => c.image)) return null;
    const at = i === active && hovered >= 0 ? hovered : (states[i]?.cursor ?? 0);
    return it.choices[at] ?? { image: "" };
  }

  function answeredAt(i) {
    const it = items[i];
    if (typeOf(it) === "secure_entry") return secureValues[it.id] !== "" || settled(it.id);
    return isAnswered(answers[i]);
  }

  const fieldOf = (questionId) => secure?.fields?.find((f) => f.question_id === questionId) ?? null;
  // A secure field the engine already saved, or can no longer take: no value is asked for.
  const settled = (questionId) => ["saved", "unavailable"].includes(fieldOf(questionId)?.status);

  // The limits of 2.6 (DLG-166, DLG-173), whatever way the text came in.
  $effect(() => {
    for (const st of states) {
      if (st.custom.length > CUSTOM_MAX) st.custom = st.custom.slice(0, CUSTOM_MAX);
      if (st.text.length > FREE_MAX) st.text = st.text.slice(0, FREE_MAX);
    }
  });

  // -- Moving between tabs (form.go:370-400) --

  // The live answer always replaces the stored one, so an erased answer is gone (form.go:378).
  function snapshot() {
    if (active < items.length && typeOf(items[active]) !== "secure_entry") answers[active] = respond(items[active], states[active]);
  }

  function switchTab(to) {
    const total = labels.length;
    if (total === 0) return;
    snapshot();
    noteKey = null;
    active = ((to % total) + total) % total;
    hovered = -1;
    focusActive();
  }

  async function focusActive() {
    await tick();
    if (!root) return;
    if (kind === "free_text") root.querySelector(".page.active .free textarea")?.focus();
    else if (kind === "secure_entry") root.querySelector(".page.active .secure input")?.focus();
    else root.focus({ preventScroll: true });
  }

  // A question is answered: on to the next tab, Confirm, or the submit.
  function answered(resp) {
    if (resp) answers[active] = resp;
    if (active < labels.length - 1) switchTab(active + 1);
    else if (!hasConfirm) submit();
  }

  // -- Submit and cancel --

  async function submit() {
    if (sending) return;
    if (secure) {
      for (const it of items) {
        if (typeOf(it) !== "secure_entry" || settled(it.id)) continue;
        const problem = valueError(secureValues[it.id]);
        if (problem) {
          secureErrors = { [it.id]: problem };
          active = items.indexOf(it);
          focusActive();
          return;
        }
      }
    }
    sending = true;
    try {
      if (local) {
        finished = true;
        local.onSubmit($state.snapshot(answers));
      } else if (secure) {
        const values = {};
        for (const [id, value] of Object.entries(secureValues)) if (value !== "" && !settled(id)) values[id] = value.replace(/\r?\n$/, "");
        const problem = await secure.onSubmit(answers, values);
        for (const id of Object.keys(values)) values[id] = "";
        if (problem?.questionId) {
          secureErrors = { [problem.questionId]: problem.text };
          active = Math.max(0, items.findIndex((it) => it.id === problem.questionId));
          focusActive();
          return;
        }
        for (const id of Object.keys(secureValues)) secureValues[id] = "";
      } else await submitAnswers(request, answers);
    } finally {
      sending = false;
    }
  }

  function cancel() {
    for (const id of Object.keys(secureValues)) secureValues[id] = "";
    finished = true;
    if (local) local.onCancel();
    else if (secure) secure.onCancel();
    else cancelQuestions(request);
  }

  // Esc or "Not yet": the first unanswered question, else the last (confirm.go:207-236).
  function back() {
    const first = items.findIndex((_, i) => !answeredAt(i));
    switchTab(first >= 0 ? first : items.length - 1);
  }

  // -- Notes (Alt+N, DLG-168, DLG-169) --

  function openNote() {
    if (!item || kind === "free_text" || kind === "secure_entry") return;
    const choices = item.choices ?? [];
    const key = kind === "yes_no" || state.cursor >= choices.length ? item.id : choices[state.cursor].id;
    noteKey = key;
    noteText = state.notes[key] ?? "";
    tick().then(() => root?.querySelector(".page.active .note input")?.focus());
  }

  function saveNote() {
    const text = noteText.trim().slice(0, NOTE_MAX);
    if (text) states[active].notes[noteKey] = text;
    else delete states[active].notes[noteKey];
    noteKey = null;
    root?.focus({ preventScroll: true });
  }

  // -- Keys (the slot scope while mounted) --

  const inEditor = () => {
    const el = document.activeElement;
    return Boolean(el && root?.contains(el) && (el.tagName === "INPUT" || el.tagName === "TEXTAREA"));
  };

  function choiceKey(combo, event) {
    const choices = item.choices ?? [];
    const rows = choices.length + 1; // the "Something else?" row last
    const realArrow = combo === "ArrowUp" || combo === "ArrowDown";
    // Typing in "Something else?": only real arrows and Enter act; typed J/K and digits stay text.
    const editing = state.cursor === choices.length && inEditor();
    if (editing && !realArrow && combo !== "Enter") return false;
    // The hovered row is the target, as the terminal adopts it before acting (multi.go:100).
    if (!editing && hovered >= 0 && hovered < choices.length) {
      states[active].cursor = hovered;
      hovered = -1;
    }
    const onCustom = states[active].cursor === choices.length;
    const move = (to) => {
      states[active].cursor = ((to % rows) + rows) % rows;
      hovered = -1;
      if (states[active].cursor === choices.length) tick().then(() => root?.querySelector(".page.active .custom textarea")?.focus());
      else if (inEditor()) root?.focus({ preventScroll: true });
    };
    switch (combo) {
      case "ArrowUp":
      case "k":
        move(state.cursor - 1);
        return true;
      case "ArrowDown":
      case "j":
        move(state.cursor + 1);
        return true;
      case "Space":
        if (kind === "multi_choice" && !onCustom) states[active].picked[states[active].cursor] = !states[active].picked[states[active].cursor];
        return kind === "multi_choice";
      case "Enter": {
        if (kind === "single_choice") {
          if (onCustom && states[active].custom.trim() === "") return true;
          answered(respond(item, states[active]));
          return true;
        }
        if (onCustom && states[active].custom.trim() === "" && inEditor()) return true;
        answered(respond(item, states[active]));
        return true;
      }
      default: {
        const digit = /^[1-9]$/.test(combo) ? Number(combo) - 1 : -1;
        if (digit >= 0 && digit < choices.length) {
          states[active].cursor = digit;
          if (kind === "multi_choice") states[active].picked[digit] = !states[active].picked[digit];
          return true;
        }
        return false;
      }
    }
  }

  function key(event) {
    const combo = event.__combo;
    if (noteKey) {
      if (combo === "Escape" || combo === "Alt+Escape") {
        noteKey = null;
        root?.focus({ preventScroll: true });
        return true;
      }
      if (combo === "Enter") {
        saveNote();
        return true;
      }
      return false;
    }
    const inSecure = kind === "secure_entry";
    if (hasConfirm && ((!inSecure && (combo === "[" || combo === "]")) || combo === "Ctrl+ArrowLeft" || combo === "Ctrl+ArrowRight")) {
      switchTab(active + (combo === "[" || combo === "Ctrl+ArrowLeft" ? -1 : 1));
      return true;
    }
    if (combo === "Tab") {
      snapshot();
      focusTimeline();
      return true;
    }
    if (combo === "Escape" || combo === "Alt+Escape") {
      if (kind === "confirm") back();
      else cancel();
      return true;
    }
    if (combo === "Alt+N") {
      openNote();
      return true;
    }
    switch (kind) {
      case "yes_no":
        if (["ArrowLeft", "ArrowRight", "h", "l"].includes(combo)) {
          states[active].no = !state.no;
          return true;
        }
        if (combo === "Enter") {
          answered(respond(item, state));
          return true;
        }
        if (combo === "y" || combo === "n" || combo === "Y" || combo === "N") {
          const yes = combo.toLowerCase() === "y";
          states[active].no = !yes;
          answered(respond(item, states[active], { yes }));
          return true;
        }
        return false;
      case "single_choice":
      case "multi_choice":
        return choiceKey(combo, event);
      case "free_text":
        if (combo === "Enter") {
          if (state.text.trim() !== "") answered(respond(item, state));
          return true;
        }
        if (combo === "Ctrl+J") {
          const area = root?.querySelector(".page.active .free textarea");
          if (area) {
            const at = area.selectionStart;
            states[active].text = `${state.text.slice(0, at)}\n${state.text.slice(area.selectionEnd)}`.slice(0, FREE_MAX);
            tick().then(() => area.setSelectionRange(at + 1, at + 1));
          }
          return true;
        }
        return false;
      case "secure_entry":
        if (combo === "Enter") {
          const problem = valueError(secureValues[item.id]);
          if (problem && !settled(item.id)) secureErrors = { [item.id]: problem };
          else {
            secureErrors = {};
            answered(null);
          }
          return true;
        }
        if (combo === "Ctrl+U") {
          secureValues[item.id] = "";
          secureErrors = {};
          return true;
        }
        return false;
      case "confirm":
        if (["ArrowLeft", "ArrowRight", "y", "n", "Y", "N"].includes(combo)) {
          confirmChoice = combo === "ArrowLeft" || combo === "ArrowRight" ? (confirmChoice === "yup" ? "notyet" : "yup") : combo.toLowerCase() === "y" ? "yup" : "notyet";
          return true;
        }
        if (combo === "Enter") {
          if (confirmChoice === "yup") submit();
          else back();
          return true;
        }
        return false;
      default:
        return false;
    }
  }

  // Every page sits in one grid cell (only the active one shows), so the
  // tallest page decides the height and a tab switch never moves the chat
  // (form_draw.go:15-35).
  function measure() {
    if (!root || collapsed) return;
    const column = root.closest("main")?.clientHeight ?? innerHeight;
    tall = root.scrollHeight > column * 0.4;
  }

  onMount(() => {
    const off = bindKey("slot", "*", (event) => {
      event.__combo = comboFor(event);
      return key(event);
    });
    const detach = attachSlot(() => {
      setFocus("slot");
      focusActive();
    });
    setFocus("slot");
    focusActive();
    const observer = new ResizeObserver(measure);
    if (root) observer.observe(root);
    // DLG-200: while any sheet covers the window, a paste (or a middle-click
    // paste) goes nowhere, so a secret never lands in the sheet.
    const covered = () => dialogs.length > 0;
    const dropPaste = (event) => {
      if (!covered()) return;
      event.preventDefault();
      event.stopImmediatePropagation();
    };
    const dropMiddle = (event) => {
      if (event.button === 1 && covered()) event.preventDefault();
    };
    if (secure) {
      window.addEventListener("paste", dropPaste, true);
      window.addEventListener("mousedown", dropMiddle, true);
      window.addEventListener("auxclick", dropMiddle, true);
    }
    return () => {
      off();
      detach();
      observer.disconnect();
      window.removeEventListener("paste", dropPaste, true);
      window.removeEventListener("mousedown", dropMiddle, true);
      window.removeEventListener("auxclick", dropMiddle, true);
      if (local && !finished) local.onGone?.();
      for (const id of Object.keys(secureValues)) secureValues[id] = "";
    };
  });

  // Clicking a folded form, or anywhere in it, brings the keys back.
  function wake() {
    if (ui.focus !== "slot") {
      setFocus("slot");
      focusActive();
    }
  }

  function clickChoice(at) {
    states[active].cursor = at; // a click selects without advancing (DLG-162)
    if (kind === "multi_choice") states[active].picked[at] = !states[active].picked[at];
    if (inEditor()) root?.focus({ preventScroll: true });
  }

  // Focus in "Something else?" (a click, or Tab inside the form) puts the cursor there.
  function customFocused() {
    const choices = items[active]?.choices ?? [];
    states[active].cursor = choices.length;
    hovered = -1;
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
<div class="form" class:focused class:collapsed bind:this={root} tabindex="-1" data-probe="question-form" onmousedown={wake} onfocusin={() => ui.focus !== "slot" && setFocus("slot")}>
  {#if collapsed}
    <div class="folded">
      <span class="folded-text">{item ? item.question : "Ready to go?"}</span>
      {#if items.length > 1}<span class="count">({answeredCount}/{items.length} answered)</span>{/if}
    </div>
    <Rule />
  {:else}
    {#if hasConfirm}
      <div class="tabs" role="tablist">
        {#each labels as label, i (i)}
          <button type="button" role="tab" class="tab" class:active={i === active} aria-selected={i === active} tabindex="-1" onclick={() => switchTab(i)}>{label}</button>
        {/each}
      </div>
    {/if}
    <div class="pages">
      {#each items as it, i (it.id)}
        {@const st = states[i]}
        {@const k = typeOf(it)}
        {@const picture = pictureOf(i)}
        <div class="page" class:active={i === active} aria-hidden={i !== active} inert={i !== active}>
          <p class="question">{it.question}</p>
          {#if it.description}
            <div class="description"><MarkdownView text={it.description} messageId="" sessionId={sessions.currentId ?? ""} finished={true} plan="none" /></div>
          {/if}
          <div class="body" class:with-picture={Boolean(picture)}>
            <div class="answers">
              {#if k === "yes_no"}
                <div class="yesno">
                  {#each [["yes", "Yes"], ["no", "No"]] as [id, label] (id)}
                    <button type="button" class="pill" class:on={(id === "no") === st.no} tabindex="-1" onclick={() => {
                      states[i].no = id !== "yes";
                      answered(respond(it, states[i], { yes: id === "yes" }));
                    }}>{label}</button>
                  {/each}
                </div>
              {:else if k === "single_choice" || k === "multi_choice"}
                <div class="choices" onmouseleave={() => (hovered = -1)}>
                  {#each it.choices ?? [] as choice, at (choice.id)}
                    <div class="choice" class:cursor={at === (i === active && hovered >= 0 ? hovered : st.cursor)} onmouseenter={() => (hovered = at)} onclick={() => clickChoice(at)} role="option" aria-selected={k === "multi_choice" ? st.picked[at] : at === st.cursor} tabindex="-1">
                      <span class="mark" class:check={k === "multi_choice"} class:on={k === "multi_choice" ? st.picked[at] : at === st.cursor}></span>
                      <span class="label">{choice.label}</span>
                      {#if choice.description}<span class="detail"> · {choice.description}</span>{/if}
                    </div>
                    {#if i === active && noteKey === choice.id}
                      <div class="note"><Field bind:value={noteText} placeholder="Add a note" label="Note" /></div>
                    {:else if st.notes[choice.id]}
                      <p class="note-text">Note: {st.notes[choice.id]}</p>
                    {/if}
                  {/each}
                  <div class="custom" class:cursor={st.cursor === (it.choices ?? []).length} onfocusin={customFocused}>
                    <Area bind:value={states[i].custom} minLines={1} maxLines={4} placeholder="Something else?" label="Something else" />
                  </div>
                </div>
              {:else if k === "free_text"}
                <div class="free"><Area bind:value={states[i].text} minLines={1} maxLines={6} placeholder="Type your answer..." label="Your answer" /></div>
              {:else if k === "secure_entry"}
                <div class="secure">
                  <SecureField bind:value={secureValues[it.id]} status={statusText(fieldOf(it.id), secureValues[it.id] !== "")} locked={settled(it.id)} error={secureErrors[it.id] ?? ""} />
                </div>
              {/if}
              {#if i === active && noteKey === it.id}
                <div class="note"><Field bind:value={noteText} placeholder="Add a note" label="Note" /></div>
              {/if}
            </div>
            {#if picture}
              <Picture path={picture.image} />
            {/if}
          </div>
        </div>
      {/each}
      {#if hasConfirm}
        <div class="page" class:active={active === confirmTab} aria-hidden={active !== confirmTab} inert={active !== confirmTab}>
          <p class="question heading">{request.confirm_title || "Ready to go?"}</p>
          {#if request.confirm_description}<p class="confirm-description">{request.confirm_description}</p>{/if}
          <ul class="summary">
            {#each items as it, i (it.id)}
              <li>{summaryLine(it, answers[i], typeOf(it) === "secure_entry" ? statusText(fieldOf(it.id), secureValues[it.id] !== "") : "")}</li>
            {/each}
          </ul>
          {#if unanswered > 0}
            <p class="warn"><span class="tag">WARN</span> {unanswered} question(s) unanswered</p>
          {/if}
          <Buttons
            buttons={[
              { id: "yup", label: "Yup!", key: "y" },
              { id: "notyet", label: "Not yet", key: "n" },
            ]}
            bind:selected={confirmChoice}
            onPress={(id) => (id === "yup" ? submit() : back())}
          />
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: calc(10 * var(--px));
    padding: calc(16 * var(--px)) calc(20 * var(--px));
    overflow-y: auto;
    min-height: 0;
    outline: none;
    border: 1px solid transparent;
  }
  .form.focused {
    border-color: var(--vermilion);
  }
  .form.collapsed {
    padding-top: 0;
    padding-bottom: 0;
    gap: 0;
  }
  .folded {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    height: calc(44 * var(--px));
    cursor: pointer;
  }
  .folded-text {
    font: var(--t-item);
    color: var(--ink);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .count {
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
  }
  .tabs {
    display: flex;
    gap: calc(24 * var(--px));
    flex-wrap: wrap;
  }
  .tab {
    padding: 0 0 calc(4 * var(--px));
    border: 0;
    border-bottom: 2px solid transparent;
    background: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--slate);
    cursor: pointer;
  }
  .tab.active {
    color: var(--ink);
    border-bottom-color: var(--ink);
  }
  p {
    margin: 0;
  }
  .question {
    font: var(--t-reading);
    color: var(--ink);
    user-select: text;
  }
  .question.heading {
    font: var(--t-heading);
  }
  .description,
  .confirm-description {
    font: var(--t-body-small);
    color: var(--slate);
    user-select: text;
  }
  .pages {
    display: grid;
  }
  .page {
    grid-area: 1 / 1;
    display: flex;
    flex-direction: column;
    gap: calc(10 * var(--px));
    min-width: 0;
    visibility: hidden;
  }
  .page.active {
    visibility: visible;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: calc(16 * var(--px));
  }
  @container (min-width: 900px) {
    .body.with-picture {
      flex-direction: row;
    }
  }
  .answers {
    flex: 1;
    min-width: 0;
  }
  .yesno {
    display: flex;
    gap: calc(12 * var(--px));
  }
  .pill {
    min-width: calc(112 * var(--px));
    height: calc(44 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
  }
  .pill.on {
    border-color: var(--ink);
    background: var(--band);
  }
  .choice {
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
    min-height: calc(40 * var(--px));
    padding: 0 calc(8 * var(--px));
    cursor: pointer;
  }
  .choice:hover {
    background: var(--control-hover);
  }
  .choice.cursor {
    background: var(--band);
  }
  .mark {
    flex: none;
    box-sizing: border-box;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    border: 1.6px solid var(--figure);
    border-radius: 50%;
    position: relative;
  }
  .mark.on::after {
    content: "";
    position: absolute;
    inset: calc(2.4 * var(--px));
    border-radius: 50%;
    background: var(--choice);
  }
  .mark.check {
    border-radius: 0;
  }
  .mark.check.on {
    background: var(--choice);
    border-color: var(--choice);
  }
  .mark.check.on::after {
    inset: auto;
    left: calc(4.5 * var(--px));
    top: calc(1 * var(--px));
    width: calc(4 * var(--px));
    height: calc(8 * var(--px));
    border: solid var(--paper);
    border-width: 0 1.6px 1.6px 0;
    border-radius: 0;
    background: none;
    transform: rotate(45deg);
  }
  .label {
    font: var(--t-item);
    color: var(--ink);
  }
  .detail,
  .note-text {
    font: var(--t-small);
    color: var(--slate);
  }
  .note,
  .note-text {
    margin-left: calc(32 * var(--px));
  }
  .custom {
    margin-top: calc(6 * var(--px));
  }
  .summary {
    margin: 0;
    padding-left: calc(18 * var(--px));
    font: var(--t-body-small);
    color: var(--ink);
  }
  .warn {
    font: var(--t-small);
    color: var(--ink);
  }
  .tag {
    padding: calc(2 * var(--px)) calc(6 * var(--px));
    background: var(--attention);
    color: var(--paper);
    font: var(--t-label);
    letter-spacing: var(--track-label);
  }
</style>
