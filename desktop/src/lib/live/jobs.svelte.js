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
import { onResync } from "../engine/events.js";
import { sessions } from "../stores/sessions.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { since, timeOf } from "./helpers.svelte.js";

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
  return job.job_id ? `job ${job.job_id} · ${since(job.started, now)}` : `pid ${job.pid} · ${since(job.started, now)}`;
}

// The tray entry's label (tasks.go:259-264).
export function jobsLabel(n) {
  return n === 1 ? "1 background process" : `${n} background processes`;
}

function isServer(job, servers) {
  return servers.some((s) => (job.job_id && s.job_id === job.job_id) || (job.pid && s.pid === job.pid));
}

const held = new SvelteMap(); // job key → ms the engine answered its kill (0 while on its way)
let readTurn = 0;
let timer = null;

// The chat's jobs that are no server, in the engine's order, without the ones
// killed here and not yet gone.
export function trayJobs() {
  const sid = jobs.sid;
  if (!sid || sid !== sessions.currentId) return [];
  const servers = jobs.servers;
  return jobs.list.filter((job) => !isServer(job, servers) && !held.has(jobKey(job)));
}

// The jobs the right panel's Background list shows: those past their 4th
// second (owner, 2026-10-25: a quick check's wave of 1-2 second jobs stays
// out). A job with no start time is shown.
const SHOW_AFTER_MS = 4000;
export function lastingJobs(list, now = Date.now()) {
  return list.filter((job) => !(now - timeOf(job.started) <= SHOW_AFTER_MS));
}

// The server jobs left out of the tray (for the probe).
export function serverJobs() {
  return jobs.list.filter((job) => isServer(job, jobs.servers));
}

function schedule() {
  clearTimeout(timer);
  timer = jobs.sid ? setTimeout(read, READ_EVERY_MS) : null;
}

async function read() {
  clearTimeout(timer);
  timer = null;
  const sid = jobs.sid;
  if (!sid) return;
  const turn = ++readTurn;
  const startedAt = Date.now();
  const [list, servers] = await Promise.all([api.jobs(sid).catch(() => null), api.servers().catch(() => null)]);
  if (turn !== readTurn) return;
  if (Array.isArray(list)) {
    jobs.list = list.filter((job) => job && (job.job_id || job.pid));
    prune(startedAt);
  }
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
  clearTimeout(timer);
  timer = null;
  jobs.sid = sid;
  jobs.list = [];
  jobs.servers = [];
  held.clear();
  if (sid) read();
}

// POST …/jobs/kill with {"job_id"} when the job has one, else {"pid"} (5.4),
// at once and with no confirmation; the row leaves at once. Success says
// "Killed <command>", failure the engine's message (background.go:179-182).
export async function killJob(job) {
  const sid = jobs.sid;
  if (!sid || !job) return false;
  const key = jobKey(job);
  held.set(key, 0);
  try {
    await api.killJob(sid, job.job_id ? { job_id: job.job_id } : { pid: job.pid });
  } catch (error) {
    held.delete(key);
    notify("error", error?.message ?? String(error));
    return false;
  }
  if (held.has(key)) held.set(key, Date.now());
  notify("info", `Killed ${job.command}`);
  return true;
}

let started = false;

export function startJobs() {
  if (started) return;
  started = true;
  $effect.root(() => {
    $effect(() => {
      const sid = sessions.currentId;
      untrack(() => showChat(sid));
    });
  });
  onResync(() => read());
}
