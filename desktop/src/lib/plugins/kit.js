// KIT's registrations (desktop-design-5-6 2.1, 7.3; loaded by lib/plugins.js).
//
// The one overlay binding: every key of S3's overlay scope goes to
// lib/kit/sheetkeys.js, which hands it to the front sheet's onKey, then to
// its kit parts, then closes the front sheet on Esc and Alt+Esc (DLG-001,
// DLG-003). The probe's "kit" section (8.2): the stack, each open kit sheet
// with its title, controls and parts (list rows, buttons, help pairs, fields
// without their text), and in heavy fields their rects.
//
// Test runs only (the app started with --probe): sample sheets built from the
// kit's own parts, which the window checks (tests/driver/p5_kit.py) open with
// Ctrl+Shift+F1 to F5 before any surface that uses the kit exists. Without
// --probe nothing of them is registered and those keys do nothing.
import { createRawSnippet, mount, unmount } from "svelte";
import { SvelteMap } from "svelte/reactivity";
import Area from "../../components/kit/Area.svelte";
import Buttons from "../../components/kit/Buttons.svelte";
import Field from "../../components/kit/Field.svelte";
import FilterList from "../../components/kit/FilterList.svelte";
import Sheet from "../../components/kit/Sheet.svelte";
import Rule from "../../components/Rule.svelte";
import { dialogs, frontDialog, hasDialog, openDialog, registerDialog } from "../dialogs.js";
import { probe_request } from "../engine/commands.js";
import { bindKey } from "../keys.js";
import { dialogBox, openSheets, partsIn, sheetKey } from "../kit/sheetkeys.js";
import { probeSection } from "../probe.js";

bindKey("overlay", "*", sheetKey);

// -- The probe's "kit" section --

function openKitSheets() {
  const open = new Map(openSheets().map(({ id, entry }) => [id, entry]));
  return dialogs.map((entry) => entry.id).filter((id) => open.has(id)).map((id) => ({ id, entry: open.get(id) }));
}

probeSection(
  "kit",
  () => {
    const front = frontDialog();
    return {
      front,
      stack: dialogs.map((entry) => entry.id),
      sheets: openKitSheets().map(({ id, entry }) => ({
        id,
        front: id === front,
        ...entry.report(),
        parts: partsIn(entry.root()).map(({ entry: part }) => (part.report ? part.report() : { kind: part.kind })),
      })),
      samples: samples ? Object.fromEntries(samples) : null,
    };
  },
  () => ({
    sheets: openKitSheets().map(({ id, entry }) => ({
      id,
      inert: dialogBox(id)?.hasAttribute("inert") ?? null,
      ...entry.heavy(),
      parts: partsIn(entry.root()).map(({ entry: part }) => (part.heavy ? part.heavy() : { kind: part.kind })),
    })),
    focus: focusPlace(document.activeElement),
    window: { w: innerWidth, h: innerHeight, px: parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 },
  }),
);

function focusPlace(element) {
  if (!element || element === document.body) return "body";
  const sheet = element.closest("[data-sheet]");
  const where = sheet ? sheet.dataset.sheet : element.closest("main.conversation") ? "conversation" : "page";
  const what = element.closest("[data-kit-field]") ? "field" : element.closest("[data-kit-area]") ? "area" : element.dataset?.button ? `button:${element.dataset.button}` : element.tagName.toLowerCase();
  return `${where}/${what}`;
}

// -- Sample sheets (probe runs only) --

let samples = null; // SvelteMap: the sample sheets' state, null outside probe runs

const A = "kit_sample_a";
const B = "kit_sample_b";
const GRACE = "kit_grace";
const GRACE_B = "kit_grace_b";
const ONBOARDING = "kit_onboarding";

const CATEGORIES = ["System", "User", "MCP"];
const LONG_TITLE = "A sample sheet whose title is long enough that the controls beside it cannot fit";
const WIDE_INFO = "alt+shift+ctrl+c, then y, then enter";
const SYSTEM = [
  ["Switch Model", "ctrl+l"],
  ["Session Tree", ""],
  ["Summarize Session", ""],
  ["New Session", "ctrl+n"],
  ["Toggle Sidebar", ""],
  ["Settings", ""],
  ["sessions", ""],
  ["Open Session", "ctrl+s"],
  ["Toggle Help", "ctrl+g"],
  ["Toggle Yolo Mode", "ctrl+y"],
  ["Open File Picker", "ctrl+f"],
  ["Open External Editor", "ctrl+o"],
  ["Toggle Thinking Mode", ""],
  ["Select Reasoning Effort", ""],
  ["Initialize Project", ""],
  ["Show Usage", ""],
  ["Refresh Providers", ""],
  ["Copy Last Reply", "ctrl+r"],
  ["Background Processes", "ctrl+b"],
  ["Sub-agents", ""],
  ["Toggle To-dos", "ctrl+t"],
  ["Change Theme", ""],
  ["Manage Skills", ""],
  ["MCP Servers", ""],
  ["Phone Sharing", ""],
  ["View Logs", ""],
  ["About Crush", ""],
  ["Check for Updates", ""],
  ["An entry whose name is long enough that it cannot fit in its row and ends with an ellipsis", ""],
  ["Quit", "ctrl+c"],
];
const LISTS = {
  System: { short: SYSTEM.map(([name, info]) => ({ name, info })), wide: SYSTEM.map(([name, info]) => ({ name, info: name === "Copy Last Reply" ? WIDE_INFO : info })) },
  User: { short: [{ name: "user:review", info: "" }, { name: "user:deploy", info: "" }] },
  MCP: { short: [{ name: "mcp: fetch page", info: "" }] },
};
const HELP_A = [
  { key: "tab", text: "switch selection" },
  { key: "↑/↓", text: "choose" },
  { key: "enter", text: "confirm" },
  { key: "ctrl+o", text: "open the second sheet" },
  { key: "ctrl+l", text: "long title" },
  { key: "ctrl+i", text: "wide info" },
  { key: "ctrl+d", text: "delete", disabled: true },
  { key: "ctrl+e", text: "count a screen key" },
  { key: "esc", text: "cancel" },
];
const MODELS = [
  { name: "Large Model", group: "Fake Provider", info: "current" },
  { name: "Small Model", group: "Fake Provider", info: "" },
  { name: "Short", group: "Second Provider", info: "an information text that is much longer than half of the row it sits in" },
  { name: "Other Model", group: "Second Provider", info: "configured" },
];
const BUTTONS_B = [
  { id: "yes", label: "Yes", key: "y" },
  { id: "no", label: "No", key: "n" },
  { id: "delete", label: "Delete", key: "d", destructive: true },
];

const get = (key, fallback) => samples.get(key) ?? fallback;
const bump = (key) => samples.set(key, get(key, 0) + 1);

// A body made of kit parts, mounted into one flex column.
function body(build) {
  return createRawSnippet(() => ({
    render: () => '<div class="kit-sample-body"></div>',
    setup(element) {
      Object.assign(element.style, { display: "flex", flexDirection: "column", flex: "1 1 auto", minHeight: "0", gap: "calc(12 * var(--px))" });
      const mounted = build(element);
      return () => mounted.forEach((instance) => unmount(instance));
    },
  }));
}

// The palette's System / User / MCP selector, as plain buttons.
const controlsA = createRawSnippet(() => ({
  render: () => '<span class="kit-sample-controls"></span>',
  setup(element) {
    element.style.display = "contents";
    const buttons = CATEGORIES.map((name, i) => {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = name;
      button.dataset.control = name;
      button.addEventListener("click", () => setCategory(i));
      element.appendChild(button);
      return button;
    });
    const show = () => buttons.forEach((button, i) => button.setAttribute("aria-pressed", String(i === get("a.category", 0))));
    show();
    categoryViews.add(show);
    return () => categoryViews.delete(show);
  },
}));
const categoryViews = new Set();

function setCategory(i) {
  samples.set("a.category", (i + CATEGORIES.length) % CATEGORIES.length);
  samples.set("a.query", "");
  samples.set("a.selected", 0);
  categoryViews.forEach((show) => show());
}

function itemsA() {
  const lists = LISTS[CATEGORIES[get("a.category", 0)]];
  return get("a.wide", false) && lists.wide ? lists.wide : lists.short;
}

const propsA = {
  id: A,
  get title() {
    return get("a.long", false) ? LONG_TITLE : "Kit Sample";
  },
  width: "standard",
  controls: controlsA,
  help: HELP_A,
  onKey(event, combo) {
    switch (combo) {
      case "Tab":
        setCategory(get("a.category", 0) + 1);
        return true;
      case "Shift+Tab":
        setCategory(get("a.category", 0) - 1);
        return true;
      case "Ctrl+E":
        bump("a.screen_key");
        return true;
      case "Ctrl+O":
        openB();
        return true;
      case "Ctrl+L":
        samples.set("a.long", !get("a.long", false));
        return true;
      case "Ctrl+I":
        samples.set("a.wide", !get("a.wide", false));
        return true;
      default:
        return false;
    }
  },
  children: body((target) => [
    mount(Field, {
      target,
      props: {
        get value() {
          return get("a.query", "");
        },
        set value(text) {
          samples.set("a.query", text);
        },
        icon: "search",
        placeholder: "Type to filter",
      },
    }),
    mount(FilterList, {
      target,
      props: {
        get items() {
          return itemsA();
        },
        filterText: (item) => item.name,
        info: (item) => item.info,
        infoBudget: 25,
        get query() {
          return get("a.query", "");
        },
        get selected() {
          return get("a.selected", 0);
        },
        set selected(index) {
          samples.set("a.selected", index);
        },
        onChoose: (item) => samples.set("a.chosen", item.name),
        label: "Commands",
      },
    }),
  ]),
};

// A section header as Sheet's `section` snippet draws it: label, then rule.
function sectionHeader(target, label) {
  const box = document.createElement("div");
  box.className = "kit-section";
  const heading = document.createElement("h3");
  heading.className = "kit-section-label";
  heading.textContent = label;
  box.appendChild(heading);
  target.appendChild(box);
  return mount(Rule, { target: box, props: { class: "kit-section-rule" } });
}

const propsB = {
  id: B,
  get title() {
    return get("b.page", 1) === 2 ? "Second Sheet · Page 2" : "Second Sheet";
  },
  width: "small",
  help: [
    { key: "ctrl+n", text: "next page" },
    { key: "esc", text: "back or close" },
  ],
  onKey(event, combo) {
    if (combo === "Ctrl+N") {
      samples.set("b.page", 2);
      return true;
    }
    // Page 2 takes Esc to go back, as a sheet with a back step does.
    if ((combo === "Escape" || combo === "Alt+Escape") && get("b.page", 1) === 2) {
      samples.set("b.page", 1);
      return true;
    }
    return false;
  },
  children: body((target) => [
    sectionHeader(target, "Details"),
    mount(Field, {
      target,
      props: {
        get value() {
          return get("b.field", "");
        },
        set value(text) {
          samples.set("b.field", text);
        },
        prompt: "> ",
        placeholder: "Type here",
      },
    }),
    mount(Area, {
      target,
      props: {
        get value() {
          return get("b.area", "");
        },
        set value(text) {
          samples.set("b.area", text);
        },
        minLines: 3,
        maxLines: 5,
        placeholder: "Notes",
      },
    }),
    mount(Buttons, {
      target,
      props: {
        buttons: BUTTONS_B,
        get selected() {
          return get("b.button", "no");
        },
        set selected(id) {
          samples.set("b.button", id);
        },
        onPress: (id) => samples.set("b.pressed", [...get("b.pressed", []), id]),
      },
    }),
    mount(FilterList, {
      target,
      props: {
        items: MODELS,
        filterText: (item) => item.name,
        info: (item) => item.info,
        groups: (item) => item.group,
        query: "",
        get selected() {
          return get("b.list", 0);
        },
        set selected(index) {
          samples.set("b.list", index);
        },
        label: "Models",
      },
    }),
  ]),
};

function graceProps(id, title) {
  return {
    id,
    get title() {
      return get(`${id}.result`, false) ? `${title} · arrived` : title;
    },
    width: "small",
    help: [
      { key: "a", text: "allow" },
      { key: "d", text: "deny" },
      { key: "esc", text: "close" },
    ],
    // Hears every key that reaches the sheet; the grace swallows the rest.
    onKey(event) {
      bump(`${id}.keys`);
      samples.set(`${id}.heard`, get(`${id}.heard`, "") + (event.key.length === 1 ? event.key : `<${event.key}>`));
      return false;
    },
    children: body((target) => [
      mount(Field, {
        target,
        props: {
          get value() {
            return get(`${id}.field`, "");
          },
          set value(text) {
            samples.set(`${id}.field`, text);
          },
          placeholder: "Type here",
        },
      }),
      mount(Buttons, {
        target,
        props: {
          buttons: [
            { id: "allow", label: "Allow", key: "a" },
            { id: "deny", label: "Deny", key: "d" },
          ],
          get selected() {
            return get(`${id}.button`, "allow");
          },
          set selected(button) {
            samples.set(`${id}.button`, button);
          },
          onPress: (button) => bump(`${id}.${button}`),
        },
      }),
    ]),
  };
}

const propsGrace = graceProps(GRACE, "Kit Grace");
const propsGraceB = graceProps(GRACE_B, "Kit Grace B");

const propsOnboarding = {
  id: ONBOARDING,
  title: "Kit Onboarding",
  width: "small",
  onboarding: true,
  help: [{ key: "esc", text: "close" }],
  children: body((target) => [
    mount(Field, {
      target,
      props: {
        get value() {
          return get("o.field", "");
        },
        set value(text) {
          samples.set("o.field", text);
        },
        placeholder: "Type here",
      },
    }),
  ]),
};

function reset(prefix, values) {
  for (const key of [...samples.keys()]) if (key.startsWith(`${prefix}.`)) samples.delete(key);
  for (const [key, value] of Object.entries(values)) samples.set(`${prefix}.${key}`, value);
}

function openA() {
  if (!hasDialog(A)) reset("a", { query: "", selected: 0, category: 0, long: false, wide: false, screen_key: 0, chosen: null });
  openDialog(A, propsA);
}

function openB() {
  if (!hasDialog(B)) reset("b", { field: "", area: "", button: "no", pressed: [], page: 1, list: 0 });
  openDialog(B, propsB);
}

function openGrace(id, props) {
  if (!hasDialog(id)) {
    reset(id, { field: "", keys: 0, heard: "", allow: 0, deny: 0, button: "allow", result: false, opened_at: Date.now() });
    // A result arriving while the grace runs (DLG-006).
    setTimeout(() => samples.set(`${id}.result`, true), 300);
  }
  openDialog(id, props, { grace: true });
}

function openOnboarding() {
  if (!hasDialog(ONBOARDING)) reset("o", { field: "" });
  openDialog(ONBOARDING, propsOnboarding);
}

const TRIGGERS = {
  F1: openA,
  F2: openB,
  F3: () => openGrace(GRACE, propsGrace),
  F4: () => openGrace(GRACE_B, propsGraceB),
  F5: openOnboarding,
};

// Before S3's key router (this file loads before main.js starts it), so a
// trigger is never taken by a sheet or swallowed by a grace.
function onTrigger(event) {
  if (!samples || !event.ctrlKey || !event.shiftKey || event.altKey || event.metaKey) return;
  const open = TRIGGERS[event.key];
  if (!open) return;
  event.preventDefault();
  event.stopImmediatePropagation();
  if (!event.repeat) open();
}

async function startSamples() {
  let answer = null;
  try {
    answer = await probe_request();
  } catch {
    return; // not in the app window (unit tests)
  }
  if (!answer) return; // not a probe run
  samples = new SvelteMap();
  for (const id of [A, B, GRACE, GRACE_B, ONBOARDING]) registerDialog(id, Sheet);
}

if (typeof window !== "undefined") {
  window.addEventListener("keydown", onTrigger, { capture: true });
  startSamples();
}
