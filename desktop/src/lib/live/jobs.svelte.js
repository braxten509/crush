// The shown chat's background jobs (desktop-design-5-6 2.15; LIVE-001,
// LIVE-016, DLG-223, DLG-225, DLG-229): GET …/sessions/{sid}/jobs every
// second while the chat shows (internal/ui/model/tasks.go:22, :161-172; there
// is no job event), with GET …/servers (G-18) beside it: a job that listens on
// a port is a server row of the left list (2.22), not a tray entry. When the
// servers read fails, every job counts. Jobs come from the process table, so
// on a Mac the list stays empty, as the terminal's (PLAN Q11).
import { untrack } from "svelte";
import { SvelteMap } from "svelte/reactivity";
import { api } from "../engine/api.js";
import { onEvent, onFolderEvent, onResync } from "../engine/events.js";
import { messages } from "../stores/messages.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { since } from "./helpers.svelte.js";

const READ_EVERY_MS = 1000;
// As for helpers (helpers.svelte.js): a killed row stays out until a read
// that started after the engine answered no longer lists it, or this long
// after the answer.
const HOLD_AFTER_ANSWER_MS = 5000;

class JobList {
  sid = $state(""); // the chat these jobs belong to
  list = $state.raw([]); // proto.Job[], as the engine last listed them
  servers = $state.raw([]); // ServerPort[] of the project (G-18), [] when unknown
}

export const jobs = new JobList();

// A row's identity (background.go:238-246): "job:<job_id>", else the pid.
export function jobKey(job) {
  return job?.job_id ? `job:${job.job_id}` : String(job?.pid ?? "");
}

// The Background Processes row's info (background.go:269-272).
export function jobInfo(job, now = Date.now()) {
  if (job.foreground) return `Running · ${since(job.started, now)}`;
  return job.job_id ? `job ${job.job_id} · ${since(job.started, now)}` : `pid ${job.pid} · ${since(job.started, now)}`;
}

// The tray entry's label (tasks.go:259-264).
export function jobsLabel(n) {
  return n === 1 ? "1 background process" : `${n} background processes`;
}

function isServer(job, servers) {
  return servers.some((s) => (job.job_id && s.job_id === job.job_id) || (job.pid && s.pid === job.pid));
}

// Keep job rows for as long as the desktop keeps the chat loaded, including
// live job events received while another tab or folder is shown.
const saved = new WeakMap();
let held = new SvelteMap(); // job key → ms the engine answered its kill (0 while on its way)
let eventAt = new Map(); // job key → { seq, deleted, job } for events during a read
let eventSeq = 0;
let readTurn = 0;
let reading = false;
let timer = null;

// The chat the jobs follow: the one being opened, else the one shown. The
// read starts at the pick, not after the chat's messages have loaded (owner,
// 2026-11-22), so the rows are there when the chat shows.
const pickedSid = () => sessions.opening?.id || sessions.currentId;

// The chat's jobs that are no server, in the engine's order, without the ones
// killed here and not yet gone.
export function trayJobs() {
  const sid = jobs.sid;
  if (!sid || sid !== pickedSid()) return [];
  const servers = jobs.servers;
  return jobs.list.filter((job) => !isServer(job, servers) && !held.has(jobKey(job)));
}

// The right panel shows every non-server background job right away.
export function lastingJobs(list) {
  return list;
}

// The server jobs left out of the tray (for the probe).
export function serverJobs() {
  return jobs.list.filter((job) => isServer(job, jobs.servers));
}

function schedule() {
  clearTimeout(timer);
  timer = jobs.sid ? setTimeout(read, READ_EVERY_MS) : null;
}

function upsertJob(list, job) {
  const key = jobKey(job);
  const idx = list.findIndex((item) => jobKey(item) === key);
  if (idx >= 0) {
    const next = list.slice();
    next[idx] = job;
    return next;
  }
  return [...list, job];
}

function removeJob(list, key) {
  if (!list.some((item) => jobKey(item) === key)) return list;
  return list.filter((item) => jobKey(item) !== key);
}

function mergeRead(list, startedSeq) {
  let next = list.filter((job) => job && (job.job_id || job.pid));
  for (const [key, note] of eventAt) {
    if (note.seq <= startedSeq) continue;
    next = note.deleted ? removeJob(next, key) : upsertJob(next, note.job);
  }
  eventAt.clear();
  jobs.list = next;
}

async function read() {
  clearTimeout(timer);
  timer = null;
  const sid = jobs.sid;
  if (!sid) return;
  const turn = ++readTurn;
  const startedAt = Date.now();
  const startedSeq = eventSeq;
  reading = true;
  const jobsCall = api.jobs(sid).catch(() => null);
  const serversCall = api.servers().catch(() => null);
  const list = await jobsCall;
  if (turn !== readTurn) return;
  reading = false;
  if (Array.isArray(list)) {
    mergeRead(list, startedSeq);
    prune(startedAt);
  }
  const servers = await serversCall;
  if (turn !== readTurn) return;
  jobs.servers = Array.isArray(servers) ? servers : [];
  schedule();
}

function prune(readStartedAt, now = Date.now()) {
  const listed = new Set(jobs.list.map(jobKey));
  for (const [key, answeredAt] of held) {
    const gone = answeredAt > 0 && answeredAt <= readStartedAt && !listed.has(key);
    if (gone || (answeredAt > 0 && now - answeredAt >= HOLD_AFTER_ANSWER_MS)) held.delete(key);
  }
}

function showChat(sid) {
  if (sid === jobs.sid) return;
  readTurn += 1;
  reading = false;
  clearTimeout(timer);
  timer = null;
  const previous = messages.bySession[jobs.sid];
  if (previous && jobs.list.length) saved.set(previous, { list: jobs.list, servers: jobs.servers, held });
  else if (previous) saved.delete(previous);
  const snapshot = saved.get(messages.bySession[sid]);
  jobs.sid = sid;
  jobs.list = snapshot?.list ?? [];
  jobs.servers = snapshot?.servers ?? [];
  held = snapshot?.held ?? new SvelteMap();
  eventAt = new Map();
  if (sid) read();
}

function updateJob(kind, job) {
  if (!job || !(job.job_id || job.pid) || !job.session_id) return;
  const key = jobKey(job);
  const deleted = kind === "deleted";
  if (job.session_id === jobs.sid) {
    if (reading) eventAt.set(key, { seq: ++eventSeq, deleted, job });
    jobs.list = deleted ? removeJob(jobs.list, key) : upsertJob(jobs.list, job);
    if (deleted) held.delete(key);
    return;
  }
  const chat = messages.bySession[job.session_id];
  if (!chat) return;
  const snapshot = saved.get(chat) ?? { list: [], servers: [], held: new SvelteMap() };
  snapshot.list = deleted ? removeJob(snapshot.list, key) : upsertJob(snapshot.list, job);
  if (deleted) snapshot.held.delete(key);
  if (snapshot.list.length) saved.set(chat, snapshot);
  else saved.delete(chat);
}

// POST …/jobs/kill with {"job_id"} when the job has one, else {"pid"} (5.4),
// at once and with no confirmation; the row leaves at once. Success says
// "Killed <command>", failure the engine's message (background.go:179-182).
export async function killJob(job) {
  const sid = jobs.sid;
  if (!sid || !job) return false;
  const key = jobKey(job);
  const chatHeld = held;
  chatHeld.set(key, 0);
  try {
    await api.killJob(sid, job.job_id ? { job_id: job.job_id } : { pid: job.pid });
  } catch (error) {
    chatHeld.delete(key);
    notify("error", error?.message ?? String(error));
    return false;
  }
  if (chatHeld.has(key)) chatHeld.set(key, Date.now());
  notify("info", `Killed ${job.command}`);
  return true;
}

let started = false;

export function startJobs() {
  if (started) return;
  started = true;
  $effect.root(() => {
    $effect.pre(() => {
      const sid = pickedSid();
      untrack(() => showChat(sid));
    });
  });
  onEvent("job", updateJob);
  onFolderEvent("job", updateJob);
  onResync(() => read());
}
