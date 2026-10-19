<script>
  // The Usage sheet (desktop-design-5-6 2.3 "Usage sheet", look 5.5; DLG-281
  // to DLG-287, CMD-017): a port of internal/ui/dialog/usage.go and
  // internal/ui/model/usage.go. For a CLI-backed model: each usage window of
  // its kind (GET /v1/limits, newer readings from `limits` events), the
  // countdown ticking each second; then the shown chat's context against its
  // compaction limit; then the refresh line. R asks for a fresh reading
  // (202; the answer comes as an event). ↑/↓, j/k, PgUp/PgDn and the wheel
  // scroll; Esc closes.
  import { onMount, untrack } from "svelte";
  import Sheet from "../kit/Sheet.svelte";
  import Rule from "../Rule.svelte";
  import { compactionLimit } from "../../lib/compaction.js";
  import { api } from "../../lib/engine/api.js";
  import { cliKind } from "../../lib/footer.js";
  import { modelData, watchModels } from "../../lib/models/store.svelte.js";
  import { limits, readTime } from "../../lib/stores/limits.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { secondNow } from "../../lib/live/helpers.svelte.js";

  const ID = "usage";
  const REFRESH_LIMIT_MS = 25000; // usage.go: RefreshLimitsNow's 25 s

  let body = $state(null);
  let refreshing = $state(false);
  let refreshError = $state("");
  let askedBefore = null; // the reading's read_at when the refresh was asked
  let refreshTimer = null;

  const agent = $derived(ws.agent);
  const modelId = $derived(agent?.model_cfg?.model ?? "");
  const kind = $derived(cliKind(agent, ws.providers));
  const providerName = $derived.by(() => {
    const id = agent?.model_cfg?.provider;
    if (!id) return "Current model";
    return modelData.cfg?.providers.get(id)?.name || "Current model";
  });
  const reading = $derived(kind ? limits.byKind[kind] : null);
  const windows = $derived((reading?.windows ?? []).filter((w) => !w.model || w.model === modelId));
  const session = $derived(sessions.currentId ? sessions.byId[sessions.currentId] : null);
  const compactAt = $derived(compactionLimit({ agent, session, providers: ws.providers, options: ws.config?.options }));
  const context = $derived(session ? (Number(session.prompt_tokens) || 0) + (Number(session.completion_tokens) || 0) : 0);
  const estimated = $derived(Boolean(session?.estimated_usage || session?.context_budget?.estimated));
  const now = $derived(secondNow());
  const help = $derived([
    { key: "esc", text: "exit" },
    { key: "r", text: "refresh", disabled: !kind || refreshing },
    { key: "↑/↓", text: "scroll" },
  ]);

  // -- Words and numbers as the terminal writes them --

  const comma = (n) => Math.trunc(Number(n) || 0).toLocaleString("en-US");
  const pct = (n) => (Math.round(n * 10) / 10).toFixed(1);
  const two = (n) => String(n).padStart(2, "0");

  function zoneName(date) {
    const part = new Intl.DateTimeFormat("en-US", { timeZoneName: "short" }).formatToParts(date).find((p) => p.type === "timeZoneName");
    return part?.value ?? "";
  }

  function clock(date) {
    const h = date.getHours() % 12 || 12;
    return `${h}:${two(date.getMinutes())}:${two(date.getSeconds())} ${date.getHours() < 12 ? "AM" : "PM"} ${zoneName(date)}`;
  }

  // "Mon, Jan 2, 2006 · 3:04:05 PM MST (UTC-07:00)"
  function resetText(ms) {
    const date = new Date(ms);
    const day = date.toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric", year: "numeric" });
    const offset = -date.getTimezoneOffset();
    const sign = offset < 0 ? "-" : "+";
    const abs = Math.abs(offset);
    return `${day} · ${clock(date)} (UTC${sign}${two(Math.floor(abs / 60))}:${two(abs % 60)})`;
  }

  // usageCountdown (usage.go:143-151).
  function countdown(ms) {
    const seconds = Math.round(ms / 1000);
    const days = Math.floor(seconds / 86400);
    const text = `${two(Math.floor(seconds / 3600) % 24)}h ${two(Math.floor(seconds / 60) % 60)}m ${two(seconds % 60)}s`;
    return days > 0 ? `${days}d ${text}` : text;
  }

  function windowLines(w) {
    const used = Math.max(0, Math.min(100, Number(w.used) || 0));
    const lines = [];
    if (w.model) lines.push(`Model: ${w.model}`);
    const resets = readTime(w.resets_at);
    if (!resets) lines.push("Reset time not reported");
    else {
      lines.push(`Resets: ${resetText(resets[0])}`);
      const left = resets[0] - now;
      lines.push(left > 0 ? `In: ${countdown(left)}` : "Reset time passed; percentages above are last reported.");
    }
    return { used, figure: `${pct(used)}% used · ${pct(100 - used)}% left`, lines };
  }

  const footer = $derived.by(() => {
    if (refreshing) return { text: "Refreshing usage…", warn: false };
    if (refreshError) return { text: `Refresh failed: ${refreshError}`, warn: true };
    const at = readTime(reading?.read_at);
    return at ? { text: `Checked: ${clock(new Date(at[0]))}`, warn: false } : null;
  });

  // -- Refreshing (usage.go: refreshUsage, applyUsageRefresh) --

  function refresh() {
    if (!kind || refreshing) return;
    refreshing = true;
    refreshError = "";
    askedBefore = reading?.read_at ?? null;
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => {
      if (!refreshing) return;
      refreshing = false;
      refreshError = "context deadline exceeded";
    }, REFRESH_LIMIT_MS);
    api.refreshLimits(kind).catch((error) => {
      refreshing = false;
      refreshError = error?.message ?? String(error);
    });
  }

  // A newer reading, or a failed one, ends the refresh.
  $effect(() => {
    const r = reading;
    untrack(() => {
      if (!refreshing || !r) return;
      if (r.state === "error" && r.error) {
        refreshing = false;
        refreshError = r.error;
      } else if (r.read_at && r.read_at !== askedBefore && r.state !== "checking") {
        refreshing = false;
        refreshError = "";
      }
    });
  });

  function scroll(by) {
    if (body) body.scrollTop += by;
  }

  function onKey(event, combo) {
    const line = 24 * (parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1);
    switch (combo) {
      case "r":
        refresh();
        return true;
      case "ArrowUp":
      case "k":
        scroll(-line);
        return true;
      case "ArrowDown":
      case "j":
        scroll(line);
        return true;
      case "PageUp":
        scroll(-5 * line);
        return true;
      case "PageDown":
        scroll(5 * line);
        return true;
      default:
        return false;
    }
  }

  onMount(() => {
    const stop = watchModels();
    refresh();
    return () => {
      stop();
      clearTimeout(refreshTimer);
    };
  });
</script>

<Sheet id={ID} title="Usage" width="small" maxHeight={640} {help} {onKey}>
  <div class="body" bind:this={body}>
    <p class="head">{providerName}</p>
    <p class="line">{agent?.model?.name ?? ""}</p>
    {#if !kind}
      <p class="empty">Subscription usage is available for CLI-backed models.</p>
    {:else if windows.length === 0 && !refreshing}
      <p class="empty">No usage windows reported by this CLI.</p>
    {/if}
    {#each windows as w, i (w.name + i)}
      {@const shown = windowLines(w)}
      <Rule marks="start" />
      <p class="label">{w.name}</p>
      <p class="figure">{shown.figure}</p>
      <div class="meter"><span style:width="{100 - shown.used}%"></span></div>
      {#each shown.lines as text}
        <p class="line">{text}</p>
      {/each}
    {/each}
    {#if session}
      <Rule marks="start" />
      <p class="label">{estimated ? "Context (estimated)" : "Context"}</p>
      {#if compactAt > 0}
        <p class="line">{comma(context)} / {comma(compactAt)} tokens before compaction</p>
        <p class="line">{comma(Math.max(0, compactAt - context))} tokens remaining</p>
      {:else}
        <p class="line">{comma(context)} tokens used</p>
        <p class="line">Compaction limit not reported</p>
      {/if}
    {/if}
    {#if footer}
      <p class="footer" class:warn={footer.warn}>{footer.text}</p>
    {/if}
  </div>
</Sheet>

<style>
  .body {
    overflow-y: auto;
    min-height: 0;
    padding-left: calc(4 * var(--px));
  }
  p {
    margin: 0;
  }
  .head {
    font: var(--t-item);
    color: var(--ink);
  }
  .label {
    margin-top: calc(10 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .figure {
    margin-top: calc(4 * var(--px));
    font: var(--t-item);
    color: var(--ink);
  }
  .meter {
    box-sizing: border-box;
    height: calc(13 * var(--px));
    margin: calc(6 * var(--px)) 0;
    border: 1px solid var(--meter-edge);
    background: var(--meter-track);
  }
  .meter span {
    display: block;
    height: 100%;
    background: var(--meter);
  }
  .line,
  .empty {
    font: var(--t-small);
    color: var(--slate);
  }
  .empty {
    margin-top: calc(14 * var(--px));
  }
  .footer {
    margin-top: calc(14 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .footer.warn {
    color: var(--amber-ink);
  }
</style>
