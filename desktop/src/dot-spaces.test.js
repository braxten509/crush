// A separator dot always has a space on each side (owner, 2026-10-21: "add a
// space before the bullet so it's not touching the account name - make that a
// general design rule"). Svelte throws away the spaces at the very start and
// end of an element's or a block's text, so " · Claude Code" inside a <span>
// drew "Claude 1· Claude Code". This reads every component's markup and fails
// where a space beside a dot sits in such a place. Write it between pieces
// instead ({name} <span>· …</span>), or as text Svelte keeps ({" · "}).
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const SRC = new URL(".", import.meta.url).pathname;

function files(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return files(path);
    return entry.name.endsWith(".svelte") ? [path] : [];
  });
}

// The drawn markup only: scripts, styles and comments become spaces (newlines
// kept, so line numbers still match).
function markup(text) {
  return text.replace(/<script[\s\S]*?<\/script>|<style[\s\S]*?<\/style>|<!--[\s\S]*?-->/g, (block) => block.replace(/[^\n]/g, " "));
}

// The places Svelte drops a space beside a dot (· or •).
const WAYS = [
  // the start of an element's text: <span> · x</span> (not after a tag that
  // closes itself, where the space lies between two pieces and stays)
  /<(?!(?:br|hr|img|input|wbr)\b)[A-Za-z](?:[^<>]*[^/<>])?>\s+[·•]/g,
  // the start of a block's text: {#if x} · y{/if}, {:else} · y
  /\{[#:][^{}]*\}\s+[·•]/g,
  // the end of an element's or a block's text: x · </span>, x · {/if}
  /[·•]\s+(?:<\/|\{[/:])/g,
];

// Each lost space in a component's text, as "line: text".
function lostSpaces(text) {
  const drawn = markup(text);
  const found = [];
  for (const way of WAYS) {
    for (const match of drawn.matchAll(way)) {
      const line = drawn.slice(0, match.index).split("\n").length;
      found.push(`${line}: ${match[0].replace(/\s+/g, " ").trim()}`);
    }
  }
  return found.sort((a, b) => parseInt(a) - parseInt(b));
}

describe("dot spaces", () => {
  it("finds each way a space beside a dot is thrown away, and only those", () => {
    expect(lostSpaces(`{name}<span class="tool"> · {tool}</span>`)).toHaveLength(1);
    expect(lostSpaces(`<p>{text}<span class="sep"> · </span></p>`)).toHaveLength(2);
    expect(lostSpaces(`Charm™{#if v} · v{v}{/if}`)).toHaveLength(1);
    expect(lostSpaces(`{#if a}x · {:else}y{/if}`)).toHaveLength(1);
    expect(lostSpaces(`{name} <span class="tool">· {tool}</span>`)).toEqual([]);
    expect(lostSpaces(`{name}<span class="tool">{" · "}{tool}</span>`)).toEqual([]);
    expect(lostSpaces(`<span class="e">&nbsp;· {x}</span>`)).toEqual([]);
    expect(lostSpaces(`<Icon name="x" /> · <span>a</span>`)).toEqual([]);
    expect(lostSpaces(`<span class="sep">·</span>`)).toEqual([]);
    expect(lostSpaces(`<script>const a = " · ";</script><p>{a}</p>`)).toEqual([]);
  });

  it("no component loses a space beside a dot", () => {
    const lost = files(SRC).flatMap((path) => lostSpaces(readFileSync(path, "utf8")).map((place) => `${path.slice(SRC.length)}:${place}`));
    expect(lost).toEqual([]);
  });
});
