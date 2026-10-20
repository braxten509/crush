// The start screens' shared state (desktop-design 2.5): what they read once
// (the app's version, START-002's question, which providers are set up), the
// chat a launch names (-s / -C, START-006 and START-021), and the init answer.
// EngineGate calls watchStart() once; the screens read `start` and screen().
import { tick, untrack } from "svelte";
import { hasDialog, openDialog } from "../../lib/dialogs.js";
import { api } from "../../lib/engine/api.js";
import { app_info } from "../../lib/engine/commands.js";
import { probeSection } from "../../lib/probe.js";
import { resolveSessionId } from "../../lib/sessionid.js";
import { conn } from "../../lib/stores/connection.svelte.js";
import { focusField } from "../../lib/stores/composer.svelte.js";
import { addPending, finishPending } from "../../lib/stores/messages.svelte.js";
import { ensureSession, openChat, sessions } from "../../lib/stores/sessions.svelte.js";
import { notify, ui } from "../../lib/stores/ui.svelte.js";
import { ws } from "../../lib/stores/workspace.svelte.js";
import { focusSearch } from "../list/listfocus.js";
import { startMarks } from "./marks.js";
import { shown } from "./shown.js";
import { chooseScreen } from "./startmodel.js";

const LIST_WAIT_MS = 5000; // the screens stop waiting for a chat list that never comes
const LAUNCH_WAIT_MS = 15000; // nor for a launch chat that never opens
const LAUNCH_NOTICE_MS = 15000; // START-021's error stays this long

export const start = $state({
  version: "", // app_info's version (START-004)
  platform: "", // "linux" | "macos"
  needsInit: null, // GET project/needs-init, read once after the first join; null until then
  initSelected: "yes", // the init prompt's chosen button: Initialize first (internal/ui/model/ui.go:520)
  answering: false,
  configured: null, // some provider is set up and not disabled (config.IsConfigured); null until read
  providerNames: {}, // provider id → its configured name; nothing else of the config is kept
  listGaveUp: false,
  launch: "none", // "none" | "pending" | "done": the -s / -C chat
});

// The middle column's start screen (startmodel.js chooseScreen).
export function screen() {
  return chooseScreen({
    currentId: sessions.currentId,
    opening: sessions.opening,
    loaded: sessions.loaded || start.listGaveUp,
    launch: start.launch,
    needsInit: start.needsInit,
    agentReady: ws.agent ? ws.agent.is_ready : undefined,
    configured: start.configured,
  });
}

function errorText(error) {
  return error?.message ?? String(error);
}

async function readAppInfo() {
  try {
    const info = await app_info();
    start.version = info?.version ?? "";
    start.platform = info?.platform ?? "";
  } catch (error) {
    console.error("app info read failed", errorText(error));
  }
}

async function readNeedsInit() {
  try {
    start.needsInit = (await api.needsInit())?.needs_init === true;
  } catch (error) {
    // Unknown: never hold the landing page on it.
    console.error("needs-init read failed", errorText(error));
    start.needsInit = false;
  }
}

// One config read at a time; a change during it makes one more after it.
// Only provider names and whether any provider is enabled are kept: the
// config's keys and headers never leave this function.
const configRead = { running: false, again: false };
async function readConfig() {
  if (configRead.running) {
    configRead.again = true;
    return;
  }
  configRead.running = true;
  try {
    do {
      configRead.again = false;
      try {
        const providers = (await api.config())?.providers;
        const names = {};
        let enabled = false;
        for (const [id, p] of Object.entries(providers && typeof providers === "object" ? providers : {})) {
          if (typeof p?.name === "string" && p.name) names[id] = p.name;
          if (p?.disable !== true) enabled = true;
        }
        start.providerNames = names;
        start.configured = enabled;
      } catch (error) {
        console.error("config read failed", errorText(error));
        if (start.configured === null) start.configured = true;
      }
    } while (configRead.again);
  } finally {
    configRead.running = false;
  }
}

// -s / -C. sessions.openLaunchChat (P3.3) opens a whole id that is in the
// list, or the newest chat for -C, as soon as the list is in; any other -s
// value is resolved here the terminal's way (START-021). Until the launch
// settles the screens hold, so no landing page flashes first.
function beginLaunch({ session, continueLast }) {
  if ((!session && !continueLast) || sessions.currentId || sessions.opening) {
    start.launch = "done";
    return;
  }
  start.launch = "pending";
  setTimeout(() => {
    if (start.launch === "pending") start.launch = "done";
  }, LAUNCH_WAIT_MS);
  if (session) {
    if (!sessions.byId[session]) resolveLaunch(session);
  } else if (sessions.order.length === 0) {
    start.launch = "done";
  }
}

async function resolveLaunch(id) {
  const found = await resolveSessionId(id, { getSession: (sid) => api.getSession(sid), ids: () => [...sessions.order] });
  if (start.launch !== "pending") return;
  // Nothing happens once the user opened a chat (sessions.svelte.js:512-515).
  if (sessions.currentId || sessions.opening) {
    start.launch = "done";
    return;
  }
  if (found.id) {
    openChat(found.id);
    if (!sessions.opening && !sessions.currentId) start.launch = "done";
    return;
  }
  start.launch = "done";
  notify("error", found.error, LAUNCH_NOTICE_MS);
  // The Sessions sheet with the given ID in its filter (PLAN.md:134;
  // desktop-design-5-6 2.5); the chat-list search while no Sessions sheet is
  // registered (SES).
  queueMicrotask(() => {
    openDialog("sessions", { query: id });
    if (!hasDialog("sessions")) focusSearch();
  });
}

// START-002 (initializeProject and skipInitializeProject,
// internal/ui/model/onboarding.go:49-84): Yes makes a new chat and sends the
// engine's init prompt in it; both answers mark the project initialized.
export function selectInit(choice) {
  start.initSelected = choice === "no" ? "no" : "yes";
}

// The prompt stays (its buttons off) until Yes has its chat, so the landing
// page does not flash between the two; as in the terminal's tea.Sequence, the
// project is marked initialized even when the init prompt could not be sent.
export async function answerInit(yes) {
  if (start.answering || start.needsInit !== true) return false;
  start.answering = true;
  try {
    if (yes) await initializeProject();
    start.needsInit = false;
    try {
      await api.initProject();
    } catch (error) {
      notify("error", `Failed to mark project as initialized: ${errorText(error)}`, 15000);
    }
  } finally {
    start.answering = false;
  }
  return true;
}

async function initializeProject() {
  let prompt;
  try {
    prompt = (await api.initPrompt())?.prompt ?? "";
  } catch (error) {
    notify("error", `Failed to initialize project: ${errorText(error)}`);
    return;
  }
  let sid;
  try {
    sid = await ensureSession();
  } catch (error) {
    notify("error", errorText(error));
    return;
  }
  // The terminal sends it as a typed prompt (sendMessageMsg), bypassing the
  // field, so the field's draft stays as it was.
  const submissionId = addPending({ sessionId: sid, text: prompt });
  try {
    await api.send({ session_id: sid, prompt, submission_id: submissionId });
  } catch (error) {
    finishPending(submissionId, false);
    notify("error", errorText(error));
  }
}

function rectOf(el) {
  if (!el) return null;
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height };
}

// The landing block's content box (CHAT-001 measures it against the column).
function contentRect(el) {
  const r = rectOf(el);
  if (!r) return null;
  const style = getComputedStyle(el);
  const left = parseFloat(style.paddingLeft) || 0;
  const right = parseFloat(style.paddingRight) || 0;
  return { x: r.x + left, y: r.y, w: r.w - left - right, h: r.h };
}

// Test runs only (desktop-design 1.9; this section is not pinned): which start
// screen shows and what it says. The config fields are the ones START-017 and
// START-019 check: the large model's id and provider, and the theme setting.
function registerProbe() {
  probeSection(
    "start",
    () => ({
      screen: screen(),
      needs_init: start.needsInit,
      launch: start.launch,
      version: start.version,
      configured: start.configured,
      agent_ready: ws.agent ? ws.agent.is_ready === true : null,
      large_model: ws.config?.models?.large ? { model: ws.config.models.large.model ?? "", provider: ws.config.models.large.provider ?? "" } : null,
      active_theme: ws.config?.options?.tui?.active_theme ?? null,
      head: shown.head,
      landing: shown.landing,
      init: shown.init,
      opening: shown.opening,
      onboarding: shown.onboarding,
    }),
    () => {
      const column = document.querySelector(".conversation");
      return {
        column: rectOf(column),
        landing: {
          block: rectOf(document.querySelector(".landing .block")),
          content: contentRect(document.querySelector(".landing .block")),
          columns: rectOf(document.querySelector(".landing .columns")),
          // Where each column's rows end: START-003's rows never run past the area.
          rows_end: Object.fromEntries(
            [...document.querySelectorAll(".landing .column")].map((c) => [
              c.dataset.column,
              Math.max(0, ...[...c.querySelectorAll("li, .none")].map((el) => el.getBoundingClientRect().bottom)),
            ]),
          ),
        },
        init: {
          block: rectOf(document.querySelector(".init .block")),
          content: contentRect(document.querySelector(".init .block")),
          yes: rectOf(document.querySelector(".init .yes")),
          no: rectOf(document.querySelector(".init .no")),
        },
        opening: rectOf(document.querySelector(".opening .line")),
        wordmark: rectOf(document.querySelector(".head .mark")),
        // START-005: animations running anywhere in the window, with what runs them.
        animations: document.getAnimations().map((a) => ({
          name: a.animationName ?? a.transitionProperty ?? a.constructor?.name ?? "",
          target: a.effect?.target?.className?.baseVal ?? a.effect?.target?.className ?? "",
        })),
        align: startMarks(),
      };
    },
  );
}

let watching = null;

// Starts the reads and the launch watch; EngineGate calls it once.
export function watchStart() {
  if (watching) return watching;
  registerProbe();
  readAppInfo();
  let joined = false;
  watching = $effect.root(() => {
    // The first join: what the start screens need, read once.
    $effect(() => {
      if (conn.state !== "connected" || joined) return;
      joined = true;
      untrack(() => {
        readNeedsInit();
        readConfig();
        setTimeout(() => {
          if (!sessions.loaded) start.listGaveUp = true;
        }, LIST_WAIT_MS);
      });
    });
    // Provider names follow config changes.
    $effect(() => {
      ws.config;
      if (joined) untrack(() => readConfig());
    });
    // The launch chat, once the list is in.
    $effect(() => {
      if (!(sessions.loaded || start.listGaveUp) || start.launch !== "none") return;
      untrack(() => beginLaunch(conn.launch));
    });
    $effect(() => {
      if (start.launch === "pending" && (sessions.currentId || sessions.opening)) start.launch = "done";
    });
    // Focus starts in the typing box, as the terminal's editor has it
    // (internal/ui/model/ui.go:521-530): once the screens settle on a chat or
    // the landing page, the field takes the page's focus while nothing else
    // holds it. The init prompt's answer focuses it itself (InitPrompt).
    $effect(() => {
      const s = screen();
      if (s !== "chat" && s !== "landing") return;
      tick().then(() => {
        const el = document.activeElement;
        if (ui.focus === "composer" && (!el || el === document.body || el === document.documentElement)) focusField();
      });
    });
  });
  return watching;
}
