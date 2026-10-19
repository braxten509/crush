// The right panel's and the overlays' registrations (desktop-design 1.10,
// P3.8): the actions help.toggle, details.toggle and composer.customize; the
// keys Ctrl+G and Esc for the help sheet (SIDE-027), Ctrl+D for the details
// overlay (SIDE-003), → or l from the timeline into the panel and the
// panel's own keys (SIDE-012); and the probe's panel and notice sections
// (1.9). Loaded by lib/plugins.js.
import { alignRows } from "../../components/panel/align.js";
import CustomizeForm from "../../components/panel/CustomizeForm.svelte";
import { CUSTOMIZE_COLLAPSED, CUSTOMIZE_HELP } from "../../components/panel/customize.js";
import { helpGroups, helpLines } from "../../components/panel/help.js";
import {
  customizing,
  helpNow,
  meterNow,
  panel,
  panelBody,
  sectionRows,
  toggleDetails,
  toField,
  viewing,
} from "../../components/panel/panel.svelte.js";
import { registerAction } from "../actions.js";
import { dialogs } from "../dialogs.js";
import { bindKey } from "../keys.js";
import { probeSection } from "../probe.js";
import { sessions } from "../stores/sessions.svelte.js";
import { figuresFor, visuals } from "../stores/visuals.svelte.js";
import { openSlot, setFocus, setHelp, ui } from "../stores/ui.svelte.js";
import { ws } from "../stores/workspace.svelte.js";

// The terminal's sidebar scrolls 4 rows a key (sidebarKeyScroll); a row of
// the panel's lists is 24 picture px.
const KEY_SCROLL_ROWS = 4;
const ROW = 24;

const focusOnly = () => ws.config?.options?.tui?.composer_focus_only === true;
const scale = () => parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;
const detailsShown = () => panel.detailsOpen && ui.panelHidden && Boolean(sessions.currentId);

registerAction({
  id: "help.toggle",
  title: "Toggle Help",
  keys: ["Ctrl+G"],
  run: () => setHelp(!ui.help),
});

registerAction({
  id: "details.toggle",
  title: "Toggle Details",
  keys: ["Ctrl+D"],
  enabled: () => ui.panelHidden,
  run: toggleDetails,
});

// The terminal opens the form only while no other inline form is open
// (composer_footer.go:142-145); the slot refuses the rest.
registerAction({
  id: "composer.customize",
  title: "Customize Composer",
  run() {
    if (ui.slot) return;
    openSlot({ kind: "customize", component: CustomizeForm, props: { help: CUSTOMIZE_HELP, collapsedHelp: CUSTOMIZE_COLLAPSED } });
  },
});

// SIDE-027: Ctrl+G opens and closes the full help (keypress.go:578-581).
bindKey("global", "Ctrl+G", () => {
  setHelp(!ui.help);
  return true;
});

// Esc closes the help sheet when it is the top overlay; the viewer, a sheet
// or the menu takes Esc first (ui.closeOverlay's order).
bindKey("overlay", "Escape", () => {
  if (!ui.help || ui.viewer || ui.menu || dialogs.length > 0) return false;
  setHelp(false);
  return true;
});

// SIDE-003: only while the panel is hidden (keypress.go:588-591); the
// overlay draws only while a chat is shown (draw.go:155, :333).
bindKey("global", "Ctrl+D", () => {
  if (!ui.panelHidden) return false;
  toggleDetails();
  return true;
});

// SIDE-012: → or l from the timeline focuses the panel's body
// (keypress.go FocusSidebar). With composer_focus_only focus never leaves
// the field, so there is nothing to do.
function focusPanel() {
  const body = panelBody();
  if (ui.panelHidden || focusOnly() || !body?.isConnected) return false;
  body.focus({ preventScroll: true });
  setFocus("panel");
  return true;
}
bindKey("timeline", "ArrowRight", focusPanel);
bindKey("timeline", "l", focusPanel);

function scrollBody(rows) {
  const body = panelBody();
  if (!body) return false;
  body.scrollTop += rows * ROW * scale();
  return true;
}
function toTop() {
  const body = panelBody();
  if (!body) return false;
  body.scrollTop = 0;
  return true;
}
function toBottom() {
  const body = panelBody();
  if (!body) return false;
  body.scrollTop = body.scrollHeight;
  return true;
}
// ← or h back to the timeline: the timeline takes focus when ui.focus says
// so (as lib/tray.js hands it over).
function toTimeline() {
  const body = panelBody();
  if (body?.contains(document.activeElement)) document.activeElement.blur();
  setFocus("timeline");
  return true;
}
function tabToField() {
  toField(panelBody()?.closest("aside") ?? null);
  return true;
}

// The terminal's sidebar keys (keys.go: up, k; down, ctrl+j, j; g, home;
// G, end; h, left; tab). Its ctrl+k stays the desktop's chat-list search
// (PLAN.md:103), as in the timeline.
for (const combo of ["ArrowUp", "k"]) bindKey("panel", combo, () => scrollBody(-KEY_SCROLL_ROWS));
for (const combo of ["ArrowDown", "j", "Ctrl+J"]) bindKey("panel", combo, () => scrollBody(KEY_SCROLL_ROWS));
for (const combo of ["Home", "g"]) bindKey("panel", combo, toTop);
for (const combo of ["End", "G"]) bindKey("panel", combo, toBottom);
for (const combo of ["ArrowLeft", "h"]) bindKey("panel", combo, toTimeline);
bindKey("panel", "Tab", tabToField);

function rect(element) {
  if (!element) return null;
  const r = element.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height };
}

function describe(element) {
  if (!element || element === document.body) return "";
  if (element.closest?.('[data-probe="panel"]')) return "panel";
  if (element.closest?.("[data-image-viewer]")) return "viewer";
  if (element.closest?.("[data-customize]")) return "customize";
  if (element.closest?.("main.conversation")) return element.tagName === "TEXTAREA" ? "field" : "conversation";
  return element.tagName.toLowerCase();
}

// 1.9: light fields read the stores and the panel's own state; heavy ones
// read the page.
probeSection(
  "panel",
  () => {
    const sid = sessions.currentId;
    const meters = meterNow();
    const lists = sectionRows();
    return {
      hidden: ui.panelHidden,
      meters: meters.meters.map((m) => ({ id: m.id, label: m.label, text: m.text, warn: m.warn, resets: m.resets ?? "" })),
      detail: meters.lines,
      tokens: meters.tokens,
      visuals: figuresFor(sid).map((f) => ({ id: f.id, kind: f.kind, title: f.title, message_id: f.messageId })),
      marked: visuals.marked,
      sections: ["files", "lsp", "mcp", "skills"].map((name) => ({ name, rows: lists[name].rows, more: lists[name].more })),
      focus: ui.focus,
      dom_focus: describe(document.activeElement),
      focused: panel.focused,
      scrollbar: panel.focused || panel.wheeled,
      details: detailsShown(),
      help: ui.help ? helpLines(helpGroups(helpNow())) : null,
      customize: ui.slot?.kind === "customize" ? { cursor: customizing.cursor, picked: [...customizing.picked] } : null,
      viewer: ui.viewer ? { fit: viewing.fit, state: viewing.state, name: viewing.name, size: viewing.size } : null,
    };
  },
  () => {
    const body = panelBody();
    const figures = [...document.querySelectorAll("[data-figure-id]")];
    return {
      visuals: figures.map((el) => ({ id: el.dataset.figureId, rect: rect(el) })),
      panel: rect(document.querySelector('[data-probe="panel"]')),
      body: body ? { rect: rect(body), scroll_top: body.scrollTop, scroll_height: body.scrollHeight, client_height: body.clientHeight } : null,
      sections: [...document.querySelectorAll('[data-probe="panel"] [data-section]')].map((el) => ({ name: el.dataset.section, rect: rect(el) })),
      meters: rect(document.querySelector('[data-probe="panel"] .usage')),
      meter_detail: rect(document.querySelector("#meter-detail")),
      customize_button: rect(document.querySelector("#meter-detail .customize")),
      strip: rect(document.querySelector("[data-meter-strip]")),
      field: rect(document.querySelector("main.conversation .field")),
      timeline: rect(document.querySelector("[data-notice-anchor]")?.previousElementSibling),
      help: rect(document.querySelector("[data-help-sheet]")),
      details: rect(document.querySelector("[data-details-overlay]")),
      viewer: rect(document.querySelector("[data-image-viewer] img")),
      window: { w: window.innerWidth, h: window.innerHeight, scale: scale() },
      align: alignRows(),
    };
  },
);

probeSection(
  "notice",
  () => (ui.notice ? { kind: ui.notice.kind, text: ui.notice.text, action: ui.notice.action?.label ? { label: ui.notice.action.label } : null } : null),
  () => {
    const button = document.querySelector("[data-notice-action]");
    return { line: rect(document.querySelector("[data-notice-kind]")), action: button ? { rect: rect(button) } : null };
  },
);
