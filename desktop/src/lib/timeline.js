// A chat's stored messages → the timeline's items (desktop-design 1.5.6, 2.2):
// one pure build per change, a port of the terminal's ExtractMessageItems,
// regroup and markLiveStatus (internal/ui/chat/messages.go:465-521,
// internal/ui/model/chat.go:325-455, internal/ui/model/chat_anim.go:15-44),
// with each terminal item split into the desktop's kinds: a reply's text, its
// banner and its live status are items of their own.
//
//   buildTimeline(input, state) → {items, status, refreshAt, groups}
//
// input: the chat ({ids, byId}), this window's pending prompts and live `!`
// commands, busy, plan mode, the permission marks, the providers list, the
// thought-line setting, the phase 4 key rule (hasKey(mid)) and the time.
// state (createTimelineState) is what the window keeps between builds: the
// items already built (an unchanged item keeps its object and version), the
// activity timers, the keys a pending prompt or a live command handed on.
// Messages are drawn through withInterrupt (CHAT-026), never as stored.
import { agentName } from "./agents.js";
import { planReadyPresent, planStartPresent } from "./render/plan.js";
import { trimSpace } from "./render/strings.js";
import { isHiddenInterruptedStep, withInterrupt } from "./stores/messages.svelte.js";
import { activityStart, goDuration } from "./timefmt.js";
import { isThinking, thoughtLine } from "./thought.js";

// -- Message parts (wire.js Part; internal/message/content.go) --

const partsOf = (m, type) => (m?.parts ?? []).filter((p) => p?.type === type);
// Message.Content(): the first text part.
export const contentOf = (m) => partsOf(m, "text")[0]?.data ?? {};
export const textOf = (m) => contentOf(m).text ?? "";
export const finishOf = (m) => partsOf(m, "finish")[0]?.data ?? null;
export const finished = (m) => finishOf(m) !== null;
const reasonOf = (m) => finishOf(m)?.reason ?? "";
export const toolCallsOf = (m) => partsOf(m, "tool_call").map((p) => p.data ?? {});
const isErrorLike = (m) => reasonOf(m) === "error" || reasonOf(m) === "content_filter";
// IsCompacting: AddFinish clears it in the engine (content.go:534-536); a
// finish the window added itself (withInterrupt) must clear it too.
const compacting = (m) => Boolean(m?.is_compacting) && !finished(m);

// ShouldRenderAssistantMessage (messages.go:514-521).
export function rendersAssistant(m) {
  return trimSpace(textOf(m)) !== "" || isErrorLike(m) || reasonOf(m) === "canceled" || (!finished(m) && toolCallsOf(m).length === 0);
}

// ShouldShowAssistantInfo (messages.go:297-303).
export function showsFooter(m) {
  const f = finishOf(m);
  return f !== null && (f.reason === "end_turn" || Boolean(m.prism_model_name));
}

// -- Notices (internal/ui/chat/user.go) --

const TASK_TAG = "crush-task-result";

// agent.ParseTaskNotification (internal/agent/tasks.go:538-549).
export function parseTaskNotification(text) {
  if (!text.startsWith(`<${TASK_TAG}>`)) return null;
  const field = (tag) => {
    const at = text.indexOf(`<${tag}>`);
    const rest = at < 0 ? "" : text.slice(at + tag.length + 2);
    const end = rest.indexOf(`</${tag}>`);
    return end < 0 ? rest : rest.slice(0, end);
  };
  return { name: field("name"), status: field("status") };
}

// The one line of a task's result (renderTaskNotification, user.go:138-160):
// question forms say how they ended; others are running, finished, failed
// or stopped.
export function taskNotice(name, status) {
  if (name === "Questions" && (status === "answered" || status === "cancelled")) {
    return { notice: "answers", state: status, name, text: `Your answers to the questions: ${status}` };
  }
  if (status === "running") return { notice: "task", state: "running", name, text: `Still running: ${name}` };
  const verb = status === "failed" ? "failed" : status === "stopped" ? "stopped" : "finished";
  const state = status === "failed" || status === "stopped" ? status : "done";
  return { notice: "task", state, name, text: `Task ${name} ${verb}` };
}

// A `<loaded_skill>` block (renderSkillInvocation, user.go:111-119): its name
// and description, or null when the XML does not parse (the text then shows
// as typed).
export function parseSkill(text) {
  if (typeof DOMParser === "undefined") return null;
  let doc;
  try {
    doc = new DOMParser().parseFromString(text, "application/xml");
  } catch {
    return null;
  }
  const root = doc?.documentElement;
  if (!root || root.nodeName !== "loaded_skill" || doc.getElementsByTagName("parsererror").length > 0) return null;
  const child = (tag) => [...root.children].find((el) => el.nodeName === tag)?.textContent ?? "";
  return { name: child("name"), description: child("description") };
}

// A system notice (renderSystemNotice, user.go:162-175): "<name> turned on|off
// <how>", or a plain line.
export function systemNotice(text) {
  const at = text.indexOf(" turned ");
  if (at < 0) return { notice: "system", state: "", name: "", text };
  const rest = text.slice(at + " turned ".length);
  return { notice: "system", state: rest.startsWith("off") ? "off" : "on", name: text.slice(0, at), text };
}

// -- Chips (CHAT-006) --

const chipsOf = new WeakMap(); // message → ChipView[]

function baseName(path) {
  return String(path ?? "").split(/[\\/]/).filter(Boolean).pop() ?? "";
}

// The chips of a sent message's binary and image_url parts.
export function chipsFor(m) {
  let chips = chipsOf.get(m);
  if (chips) return chips;
  chips = [];
  for (const part of m?.parts ?? []) {
    if (part?.type === "binary") {
      const d = part.data ?? {};
      const mime = d.MIMEType ?? "";
      const picture = mime.startsWith("image/");
      chips.push({
        kind: picture ? "picture" : mime.startsWith("text/") ? "text" : "file",
        name: baseName(d.Path) || "attachment",
        mime,
        viewable: picture && Boolean(d.Data),
        ...(picture && d.Data ? { src: `data:${mime};base64,${d.Data}` } : {}),
      });
    } else if (part?.type === "image_url") {
      const url = String(part.data?.url ?? "");
      const local = url.startsWith("data:image/");
      let name = "image";
      if (!local) {
        try {
          name = baseName(new URL(url).pathname) || new URL(url).host || "image";
        } catch {
          name = "image";
        }
      }
      chips.push({ kind: "picture", name, mime: local ? url.slice(5, url.indexOf(";")) : "", viewable: local, src: url });
    }
  }
  if (m) chipsOf.set(m, chips);
  return chips;
}

// -- Tools --

// The tools that never fold into a group (Foldable, toolgroup.go:107-119).
const UNFOLDED = new Set(["question", "agent", "agentic_fetch"]);

// movedToBackground (internal/ui/chat/bash.go:116-126).
export function movedToBackground(result) {
  if (!result || result.name !== "bash") return false;
  try {
    const meta = JSON.parse(result.metadata || "null");
    return Boolean(meta?.background) && (Boolean(meta.shell_id) || !meta.end_time);
  } catch {
    return false;
  }
}

// A call's status (computeStatus, tools.go:356-365, with the permission marks
// of 1.5.3): a result decides; else a stopped run, the permission mark, the
// arguments still streaming, or running.
export function callStatus(call, result, canceled, marks = {}) {
  if (result) return movedToBackground(result) ? "background" : result.is_error ? "error" : "done";
  const mark = marks[call.id];
  if (canceled || mark === "cancelled") return "cancelled";
  if (mark === "permission") return "permission";
  return call.finished ? "running" : "pending";
}

// What a running tool is doing (toolActivity, toolgroup.go:341-370).
export function toolActivity(name) {
  switch (name) {
    case "agent":
    case "task":
      return "Waiting for subagent";
    case "bash":
      return "Running a command";
    case "job_output":
    case "job_kill":
      return "Checking a command";
    case "view":
      return "Reading a file";
    case "edit":
    case "multiedit":
    case "write":
      return "Editing a file";
    case "grep":
    case "glob":
    case "ls":
    case "sourcegraph":
      return "Searching";
    case "web_search":
      return "Searching the web";
    case "fetch":
    case "web_fetch":
    case "download":
      return "Reading a web page";
    case "todos":
      return "Updating the plan";
  }
  if (name.startsWith("lsp_")) return "Checking code";
  if (name.startsWith("mcp_")) {
    const rest = name.slice(4);
    const at = rest.indexOf("_");
    if (at >= 0) return `Using ${rest.slice(0, at)}`;
  }
  return "Working";
}

// -- Live status words --

const RETRY = { rate_limited: "Rate limited — waiting to retry", retrying: "Waiting to retry" };
const RESPONDING_MS = 5000;

// activityStatus (assistant_render.go:176-196) for a step that is spinning.
export function activityStatus(m, now, compactingEarlier = false) {
  if (m.is_summary_message || compacting(m) || compactingEarlier) return "Compacting conversation";
  if (RETRY[m.activity]) return RETRY[m.activity];
  if (m.activity === "writing") return "Writing command";
  if (m.activity === "thinking" || isThinking(m)) return "Thinking";
  if (m.activity === "responding" && now - (m.activity_at ?? 0) * 1000 < RESPONDING_MS) return "Responding";
  return "Waiting for the model";
}

// -- Footers (AssistantInfoItem, messages.go:385-458) --

function modelName(providers, provider, model) {
  const p = providers.find((x) => x?.id === provider);
  return p?.models?.find((x) => x?.id === model)?.name || "";
}

// formatHypercreditSavings (messages.go:453-458): no decimals at 1 or more.
export function hypercredits(v) {
  return v >= 1 ? v.toFixed(0) : v.toFixed(1);
}

// {text, saved} of a footer: "Model via Provider in 12s" on the prompt's last
// turn, "Model → Routed" on a Prism turn; saved is the Prism savings, if any.
export function footerText(m, providers, lastUserTime) {
  const f = finishOf(m);
  if (!f) return { text: "", saved: "" };
  let model = modelName(providers, m.provider, m.model) || "Unknown Model";
  if (m.prism_model_name) model = `${model} → ${m.prism_model_name}`;
  let saved = "";
  if (m.prism_hypercredit_savings !== undefined && m.prism_hypercredit_savings !== null) saved = `◆ ${hypercredits(m.prism_hypercredit_savings)} Saved`;
  else if (m.prism_dollar_savings !== undefined && m.prism_dollar_savings !== null) saved = `• $${m.prism_dollar_savings.toFixed(2)} Saved`;
  if (f.reason !== "end_turn") return { text: model, saved };
  const provider = providers.find((x) => x?.id === m.provider)?.name || m.provider || "";
  return { text: `${model} via ${provider} in ${goDuration((f.time ?? 0) - lastUserTime)}`, saved };
}

// -- Keys (4.4, D16) --

// The letters of the n-th key (0-based): A is the rail's top dot, so keys
// run B, C, … Z, then AA, AB.
export function keyLetters(n) {
  let k = n + 2;
  let out = "";
  while (k > 0) {
    const r = (k - 1) % 26;
    out = String.fromCharCode(65 + r) + out;
    k = Math.floor((k - 1) / 26);
  }
  return out;
}

// -- Building --

export function createTimelineState() {
  return {
    built: new Map(), // key → {deps, item}
    timers: new Map(), // status key → {key, startedAt}
    shown: new Set(), // assistant ids that had an item in the last build
    shellKeys: new Map(), // "<mid>:<n>" → the live command's key it took over
    pendingKeys: new Map(), // submission id → "pending:<id>" once shown as pending
  };
}

const sameDeps = (a, b) => a.length === b.length && a.every((v, i) => Object.is(v, b[i]));

// The kept item for key when its inputs are unchanged; else a new one with
// the next version.
function keep(state, seen, key, deps, make) {
  seen.add(key);
  const old = state.built.get(key);
  if (old && sameDeps(old.deps, deps)) return old.item;
  const item = { ...make(), key, version: (old?.item.version ?? 0) + 1 };
  state.built.set(key, { deps, item });
  return item;
}

const userLike = (e) => e.t === "user" || e.t === "notice" || e.t === "pending";

export function buildTimeline(input, state = createTimelineState()) {
  const {
    sessionId = "",
    ids = [],
    byId = new Map(),
    pending = [],
    shells = [],
    busy = false,
    planMode = false,
    callMarks = {},
    providers = [],
    thoughtLine: thoughtOn = true,
    hasKey = null,
    now = Date.now(),
  } = input;
  const get = (id) => (typeof byId.get === "function" ? byId.get(id) : byId[id]);

  // Tool results by call (BuildToolResultMap, messages.go:526-538).
  const results = new Map();
  for (const id of ids) {
    const m = get(id);
    if (m?.role !== "tool") continue;
    for (const p of partsOf(m, "tool_result")) if (p.data?.tool_call_id) results.set(p.data.tool_call_id, p.data);
  }

  // 1. Each message's entries in chat order (ExtractMessageItems; the info
  //    row after an assistant message that shows one, session_load.go:381-389).
  const flat = [];
  const liveShells = shells.filter((s) => s.sessionId === sessionId);
  const usedShells = new Set();
  const shown = new Set();
  let lastUserTime = ids.length ? (get(ids[0])?.created_at ?? 0) : 0;
  for (const id of ids) {
    const stored = get(id);
    if (!stored) continue;
    if (stored.role === "user") {
      lastUserTime = stored.created_at ?? lastUserTime;
      const content = contentOf(stored);
      if (content.hidden) continue; // CHAT-005
      const shellParts = partsOf(stored, "shell_command");
      if (shellParts.length) {
        shellParts.forEach((p, n) => {
          const slot = `${stored.id}:${n}`;
          let key = state.shellKeys.get(slot);
          if (!key) {
            const live = liveShells.find((s) => !usedShells.has(s.requestId) && !s.running && s.command === p.data?.command);
            key = live ? `shell:${live.requestId}` : `shell:${slot}`;
            state.shellKeys.set(slot, key);
          }
          const req = key.startsWith(`shell:${stored.id}:`) ? "" : key.slice(6);
          if (req) usedShells.add(req);
          flat.push({ t: "shell", m: stored, part: p.data ?? {}, key, requestId: "" });
        });
        continue;
      }
      const text = trimSpace(content.text ?? "");
      const task = parseTaskNotification(text);
      if (task) {
        flat.push({ t: "notice", m: stored, key: `notice:${stored.id}`, ...taskNotice(task.name, task.status) });
        continue;
      }
      if (text.startsWith("<loaded_skill>")) {
        const skill = parseSkill(text);
        if (skill) {
          flat.push({ t: "notice", m: stored, key: `notice:${stored.id}`, notice: "skill", state: "", name: skill.name, text: skill.description });
          continue;
        }
      }
      const sub = content.submission_id ?? "";
      flat.push({ t: "user", m: stored, key: (sub && state.pendingKeys.get(sub)) || stored.id, submission: sub });
    } else if (stored.role === "system") {
      flat.push({ t: "notice", m: stored, key: `notice:${stored.id}`, ...systemNotice(trimSpace(textOf(stored))) });
    } else if (stored.role === "assistant") {
      const m = withInterrupt(stored);
      const hidden = isHiddenInterruptedStep(stored, state.shown.has(stored.id));
      if (rendersAssistant(m) && !hidden) {
        flat.push({ t: "step", m, key: stored.id });
        shown.add(stored.id);
      }
      const canceled = reasonOf(m) === "canceled";
      for (const call of toolCallsOf(m)) {
        if (!call.id) continue;
        flat.push({ t: "call", m, call, result: results.get(call.id) ?? null, canceled, key: `tool:${call.id}` });
      }
      if (showsFooter(m) && !hidden) flat.push({ t: "footer", m, lastUserTime, key: `footer:${stored.id}` });
    }
  }
  state.shown = shown;

  // This window's commands still running or not saved yet, and its prompts on
  // their way, after the saved messages in the order they started.
  const tail = [
    ...liveShells.filter((s) => !usedShells.has(s.requestId)).map((s) => ({ t: "liveshell", shell: s, key: `shell:${s.requestId}`, at: s.startedAt ?? 0 })),
    ...pending.map((p) => ({ t: "pending", p, key: `pending:${p.submissionId}`, at: p.createdAt ?? 0 })),
  ].sort((a, b) => a.at - b.at);
  for (const e of tail) {
    if (e.t === "pending") state.pendingKeys.set(e.p.submissionId, e.key);
    flat.push(e);
  }

  // 2. Groups (regroup, chat.go:346-399): a run of foldable entries that holds
  //    a tool call becomes one group; footers that would label nothing go.
  const foldable = (e) =>
    (e.t === "call" && !UNFOLDED.has(e.call.name)) || (e.t === "step" && onlyThinking(e.m, planMode));
  const list = [];
  for (let i = 0; i < flat.length; ) {
    let j = i;
    while (j < flat.length && foldable(flat[j])) j++;
    const run = flat.slice(i, j);
    if (run.some((e) => e.t === "call")) {
      // Named after its first step, as the terminal's group is ("toolgroup:" +
      // children[0].ID(), toolgroup.go:133-138): a thinking step's message id
      // or a call's id, so two runs of one message never share a key.
      const id = run[0].t === "call" ? run[0].call.id : run[0].m.id;
      list.push({ t: "group", children: run, id, key: `group:${id}` });
      i = j;
      continue;
    }
    if (j === i) j = i + 1;
    for (const e of flat.slice(i, j)) {
      if (e.t === "footer") {
        const prev = list[list.length - 1];
        if (!prev || prev.t === "footer" || userLike(prev) || (prev.t === "step" && (prev.m.is_summary_message || compacting(prev.m)))) continue;
      }
      list.push(e);
    }
    i = j;
  }
  // While the agent works, a prompt with nothing after it yet gets the
  // waiting status (chat.go:400-428).
  const lastFlat = flat[flat.length - 1];
  if (busy && lastFlat && userLike(lastFlat)) {
    const forKey = lastFlat.t === "pending" ? lastFlat.p.submissionId : lastFlat.submission || lastFlat.m.id;
    list.push({ t: "waiting", key: `status:waiting:${forKey}` });
  }

  // 3. One live status, at the end (markLiveStatus, chat_anim.go:15-44).
  let last = list.length - 1;
  while (last >= 0 && list[last].t === "footer") last--;
  let compactingEarlier = false;
  for (let i = 0; i < last; i++) {
    const e = list[i];
    if (e.t === "step" && compacting(e.m)) compactingEarlier = true;
  }

  // 4. The items.
  const seen = new Set();
  const items = [];
  let status = "";
  let refreshAt = Infinity;
  let keyed = 0;
  const letterFor = (kind, mid) => {
    const on = kind === "tools" || ((kind === "user" || kind === "reply") && hasKey?.(mid));
    return on ? keyLetters(keyed++) : "";
  };
  const base = (kind, messageId) => ({ kind, sessionId, messageId });
  const statusItem = (key, m, words, timerKey, thought) => {
    const startedAt = activityStart(state.timers, key, timerKey, now);
    if (words === "Responding") refreshAt = Math.min(refreshAt, (m.activity_at ?? 0) * 1000 + RESPONDING_MS);
    return keep(state, seen, key, [words, thought, startedAt], () => ({ ...base("status", m?.id ?? ""), letter: "", words, startedAt, thought }));
  };

  list.forEach((e, index) => {
    const isLast = index === last;
    switch (e.t) {
      case "user": {
        const letter = letterFor("user", e.m.id);
        items.push(keep(state, seen, e.key, [e.m, letter], () => ({ ...base("user", e.m.id), letter, text: textOf(e.m), chips: chipsFor(e.m) })));
        break;
      }
      case "notice":
        items.push(keep(state, seen, e.key, [e.m, e.notice, e.state, e.name, e.text], () => ({ ...base("notice", e.m.id), letter: "", notice: e.notice, state: e.state, name: e.name, text: e.text })));
        break;
      case "shell":
        items.push(
          keep(state, seen, e.key, [e.part.command, e.part.output, e.part.exit_code], () => ({
            ...base("shell", e.m.id),
            letter: "",
            command: e.part.command ?? "",
            output: e.part.output ?? "",
            exitCode: e.part.exit_code ?? null,
            running: false,
            requestId: "",
          })),
        );
        break;
      case "liveshell": {
        const s = e.shell;
        items.push(
          keep(state, seen, e.key, [s.command, s.output, s.exitCode, s.running], () => ({
            ...base("shell", ""),
            letter: "",
            command: s.command ?? "",
            output: s.output ?? "",
            exitCode: s.exitCode ?? null,
            running: Boolean(s.running),
            requestId: s.requestId,
          })),
        );
        break;
      }
      case "pending": {
        const p = e.p;
        items.push(keep(state, seen, e.key, [p.text, p.attachments, p.state], () => ({ ...base("pending", ""), letter: "", submissionId: p.submissionId, text: p.text ?? "", chips: pendingChips(p.attachments), state: p.state })));
        break;
      }
      case "footer": {
        const { text, saved } = footerText(e.m, providers, e.lastUserTime);
        items.push(keep(state, seen, e.key, [text, saved], () => ({ ...base("footer", e.m.id), letter: "", text, saved })));
        break;
      }
      case "waiting": {
        const words = "Waiting for the model";
        status = words;
        items.push(statusItem(e.key, null, words, words, ""));
        break;
      }
      case "step":
        stepItems(e.m, { superseded: index < last, compactingEarlier: isLast && compactingEarlier });
        break;
      case "call": {
        // A tool that never folds (Question, Agent, Agentic Fetch).
        const view = callView(e);
        items.push(keep(state, seen, e.key, [e.call, e.result, view.status], () => ({ ...base("tool", e.m.id), letter: "", call: view })));
        break;
      }
      case "group":
        groupItem(e, isLast);
        break;
    }
  });

  function callView(e) {
    return {
      id: e.call.id,
      name: e.call.name ?? "",
      input: e.call.input ?? "",
      messageId: e.m.id,
      status: callStatus(e.call, e.result, e.canceled, callMarks),
      result: e.result
        ? {
            content: e.result.content ?? "",
            ...(e.result.data !== undefined ? { data: e.result.data } : {}),
            ...(e.result.mime_type !== undefined ? { mime_type: e.result.mime_type } : {}),
            metadata: e.result.metadata ?? "",
            is_error: Boolean(e.result.is_error),
            ...(e.result.review ? { review: e.result.review } : {}),
          }
        : null,
      hooks: [],
      children: [],
    };
  }

  // A reply's text, its banner, then its live status (RawRender and
  // renderMessageContent, assistant.go:223-425).
  function stepItems(m, { superseded, compactingEarlier: earlier }) {
    const spinning = busy && !superseded && !finished(m) && toolCallsOf(m).length === 0;
    const mid = m.id;
    if (compacting(m) || m.is_summary_message) {
      if (spinning) {
        const words = "Compacting conversation";
        status = words;
        items.push(statusItem(`status:${mid}`, m, words, words, ""));
        return;
      }
      if (m.is_summary_message) {
        const f = finishOf(m);
        if (f?.reason === "error") items.push(bannerItem(m, "error", f.message ?? "", f.details ?? ""));
        else if (f?.reason === "canceled") items.push(bannerItem(m, "compaction_canceled", "Compaction canceled", ""));
        else items.push(bannerItem(m, "compacted", "Conversation compacted", ""));
        return;
      }
    }
    const text = textOf(m);
    if (trimSpace(text) !== "") {
      const plan = planReadyPresent(text) ? "closed" : planMode && !finished(m) && planStartPresent(text) ? "open" : "none";
      const author = agentName(m.provider, providers) || "Assistant";
      const letter = letterFor("reply", mid);
      items.push(keep(state, seen, mid, [m, plan, author, letter], () => ({ ...base("reply", mid), letter, text, finished: finished(m), plan, author, time: m.created_at ?? 0 })));
    }
    if (finished(m)) {
      const f = finishOf(m);
      if (f.reason === "canceled") {
        if (toolCallsOf(m).length === 0) items.push(bannerItem(m, "canceled", "Canceled", ""));
      } else if (f.reason === "content_filter") {
        items.push(bannerItem(m, "refused", f.message || REFUSAL_TITLE, f.details || REFUSAL_DETAILS));
      } else if (f.reason === "error") {
        items.push(bannerItem(m, "error", f.message ?? "", f.details ?? ""));
      }
    }
    if (spinning) {
      const words = activityStatus(m, now, earlier);
      status = words;
      const thought = words === "Thinking" ? thoughtLine(m, { enabled: thoughtOn }) : "";
      items.push(statusItem(`status:${mid}`, m, words, words, thought));
    }
  }

  function bannerItem(m, banner, title, details) {
    return keep(state, seen, `banner:${m.id}`, [banner, title, details], () => ({ ...base("banner", m.id), letter: "", banner, title, details }));
  }

  // A folded run (ToolGroupItem, toolgroup.go): its calls, what it is doing
  // while it is the chat's live end, and its counts.
  function groupItem(e, live) {
    const calls = [];
    const callEntries = e.children.filter((c) => c.t === "call");
    for (const c of callEntries) calls.push(callView(c));
    let words = "";
    let lastCallId = "";
    for (let i = e.children.length - 1; i >= 0; i--) {
      if (e.children[i].t === "call") {
        lastCallId = e.children[i].call.id;
        break;
      }
    }
    const tail = e.children[e.children.length - 1];
    // status() (toolgroup.go:295-339): the last step says what is happening;
    // a finished step or tool leaves "Thinking" while the agent still works.
    if (live && busy) {
      if (tail.t === "step") {
        const m = tail.m;
        if (compacting(m)) words = "Compacting conversation";
        else if (reasonOf(m) === "canceled") words = "";
        else if (!finished(m) && toolCallsOf(m).length === 0) words = RETRY[m.activity] ?? (m.activity === "writing" ? "Writing command" : "Thinking");
        else words = compactingEarlier ? "Compacting conversation" : "Thinking";
      } else {
        const st = calls[calls.length - 1].status;
        if (st === "cancelled") words = "";
        else if (st === "permission") words = "Waiting for approval";
        else if (st === "running" || st === "pending") words = toolActivity(tail.call.name ?? "");
        else words = compactingEarlier ? "Compacting conversation" : "Thinking";
      }
    }
    let liveItem = null;
    if (words) {
      status = words;
      const thought = words === "Thinking" && tail.t === "step" ? thoughtLine(tail.m, { enabled: thoughtOn }) : "";
      const startedAt = activityStart(state.timers, e.key, `${words}\u0000${lastCallId}`, now);
      liveItem = keep(state, seen, `status:${e.key}`, [words, thought, startedAt], () => ({ ...base("status", tail.m.id), letter: "", words, startedAt, thought }));
    }
    const failed = calls.filter((c) => c.status === "error").length;
    const backgrounded = calls.filter((c) => c.status === "background").length;
    const lastFailed = calls[calls.length - 1]?.status === "error";
    const working = liveItem !== null || (busy && calls.some((c) => c.status === "pending"));
    const withChanges = callEntries.filter((c) => c.result && (c.result.review || (!c.result.is_error && FILE_TOOLS.has(c.call.name))));
    const changes = withChanges.length
      ? { sessionId, messageId: withChanges[withChanges.length - 1].m.id, toolCallIds: withChanges.map((c) => c.call.id), title: ranTools(calls.length) }
      : null;
    const letter = letterFor("tools", e.children[0].m.id);
    const deps = [letter, liveItem, working, ...e.children.flatMap((c) => (c.t === "call" ? [c.m, c.result, calls.find((v) => v.id === c.call.id).status] : [c.m]))];
    items.push(
      keep(state, seen, e.key, deps, () => ({
        ...base("tools", e.children[0].m.id),
        letter,
        groupId: e.id,
        calls,
        working,
        lastFailed,
        backgrounded,
        failed,
        summary: "",
        live: liveItem,
        changes,
        members: e.children.map((c) => (c.t === "call" ? c.call.id : c.m.id)),
      })),
    );
  }

  for (const key of [...state.built.keys()]) if (!seen.has(key)) state.built.delete(key);
  for (const key of [...state.timers.keys()]) if (!seen.has(key) && !seen.has(`status:${key}`)) state.timers.delete(key);
  for (const [sub, key] of [...state.pendingKeys]) if (!seen.has(key)) state.pendingKeys.delete(sub);
  for (const [slot, key] of [...state.shellKeys]) if (!seen.has(key)) state.shellKeys.delete(slot);
  return { items, status, refreshAt };
}

const FILE_TOOLS = new Set(["edit", "multiedit", "write"]);
export const REFUSAL_TITLE = "Model refused to continue";
export const REFUSAL_DETAILS =
  "The provider's safety classifier stopped this response before any usable content was produced. Rephrase the request, start a fresh session, or try a different model.";

// The group row's words (PLAN.md:147): "Ran 1 tool", "Ran 4 tools".
export function ranTools(n) {
  return n === 1 ? "Ran 1 tool" : `Ran ${n} tools`;
}

// onlyThinking (assistant.go:773-781): a step that shows nothing but
// thinking folds into a group; in plan mode no step folds.
export function onlyThinking(m, planMode) {
  const canceledBanner = reasonOf(m) === "canceled" && toolCallsOf(m).length === 0;
  return trimSpace(textOf(m)) === "" && !isErrorLike(m) && !canceledBanner && !m.is_summary_message && !m.is_compacting && !planMode;
}

// A pending prompt's chips, from its attachments (lib/types.js Attachment).
const pendingChipsOf = new WeakMap();
function pendingChips(attachments) {
  if (!Array.isArray(attachments)) return [];
  let chips = pendingChipsOf.get(attachments);
  if (chips) return chips;
  chips = attachments.map((a) => {
    const picture = a.kind === "picture" || String(a.mime ?? "").startsWith("image/");
    return {
      kind: picture ? "picture" : a.kind === "text" ? "text" : "file",
      name: a.name ?? "",
      mime: a.mime ?? "",
      viewable: picture && Boolean(a.base64),
      ...(picture && a.base64 ? { src: `data:${a.mime};base64,${a.base64}` } : {}),
    };
  });
  pendingChipsOf.set(attachments, chips);
  return chips;
}
