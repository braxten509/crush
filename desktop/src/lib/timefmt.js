// The times the timeline shows: a byline's clock time (CHAT-015), the elapsed
// time after a live status (CHAT-021) and a footer's reply time (CHAT-035).
import { createSubscriber } from "svelte/reactivity";

// The elapsed time after a live status, as the terminal's activity timer
// writes it (internal/ui/chat/activity_timer.go:22-37): nothing under 10 s,
// then "12s", "3m 4s", "1h 2m".
export function elapsedText(seconds) {
  const s = Math.max(0, Math.trunc(Number(seconds) || 0));
  if (s < 10) return "";
  if (s >= 3600) return `${Math.floor(s / 3600)}h ${Math.floor(s / 60) % 60}m`;
  if (s >= 60) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return `${s}s`;
}

// The elapsed text of a status whose activity the window first saw at
// startedAt (ms), at now (ms).
export function elapsedSince(startedAt, now) {
  if (!startedAt) return "";
  return elapsedText((now - startedAt) / 1000);
}

// One activity's start, kept per status: a new activity (a different key)
// starts the timer again; the same key keeps it, so provider heartbeats never
// reset it (activityTimer.update, activity_timer.go:15-20). timers is a Map
// the caller keeps between builds: status key → {key, startedAt}.
export function activityStart(timers, statusKey, activityKey, now) {
  const kept = timers.get(statusKey);
  if (kept && kept.key === activityKey) return kept.startedAt;
  timers.set(statusKey, { key: activityKey, startedAt: now });
  return now;
}

// Go's time.Duration.String() for whole seconds, which is what a footer's
// duration is: both ends are Unix seconds (internal/ui/chat/messages.go:419-420).
export function goDuration(seconds) {
  let s = Math.trunc(Number(seconds) || 0);
  if (s === 0) return "0s";
  const sign = s < 0 ? "-" : "";
  s = Math.abs(s);
  const h = Math.floor(s / 3600);
  const m = Math.floor(s / 60) % 60;
  const sec = s % 60;
  if (h > 0) return `${sign}${h}h${m}m${sec}s`;
  if (m > 0) return `${sign}${m}m${sec}s`;
  return `${sign}${sec}s`;
}

// A byline's time: the message's local clock time, "10:42".
export function clockTime(unixSeconds) {
  if (!unixSeconds) return "";
  const d = new Date(unixSeconds * 1000);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

// One shared 1 s clock for every visible timer (CHAT-021): it ticks only
// while some status line on the page reads it, and all of them share the one
// interval.
const subscribe = createSubscriber((update) => {
  const id = setInterval(update, 1000);
  return () => clearInterval(id);
});

export function clockNow() {
  subscribe();
  return Date.now();
}
