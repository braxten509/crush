// Code highlighting (CHAT-014 code blocks, CHAT-057 tool bodies; desktop-design
// 1.7 item 7). highlight.js gives escaped HTML; it goes through DOMPurify with
// spans and classes only (sanitize.js) and comes back as nodes. The classes map
// onto the --hl-* tokens in code.css.
//
//   highlight(code, lang)        one fragment of spans and text; the text is
//                                exactly code (with control characters shown)
//   highlightLines(code, lang)   one fragment per line ("\n" not included), the
//                                spans of a multi-line token repeated per line
//   languageFor(name)            a fence's info string or a language name → the
//                                registered language, or null
//   languageForPath(path)        by the file's name or extension, or null
//   showControls(text)           control characters other than \n, \t and \r drawn
//                                as their Unicode pictures (U+2400 + code, DEL
//                                U+2421), so a stray escape never hides text
//   gutterWidth(lastNumber)      the line-number gutter's width in ch: the
//                                digits of the last number
import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import c from "highlight.js/lib/languages/c";
import clojure from "highlight.js/lib/languages/clojure";
import cmake from "highlight.js/lib/languages/cmake";
import cpp from "highlight.js/lib/languages/cpp";
import csharp from "highlight.js/lib/languages/csharp";
import css from "highlight.js/lib/languages/css";
import dart from "highlight.js/lib/languages/dart";
import diff from "highlight.js/lib/languages/diff";
import dockerfile from "highlight.js/lib/languages/dockerfile";
import elixir from "highlight.js/lib/languages/elixir";
import erlang from "highlight.js/lib/languages/erlang";
import go from "highlight.js/lib/languages/go";
import graphql from "highlight.js/lib/languages/graphql";
import haskell from "highlight.js/lib/languages/haskell";
import http from "highlight.js/lib/languages/http";
import ini from "highlight.js/lib/languages/ini";
import java from "highlight.js/lib/languages/java";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import kotlin from "highlight.js/lib/languages/kotlin";
import latex from "highlight.js/lib/languages/latex";
import lua from "highlight.js/lib/languages/lua";
import makefile from "highlight.js/lib/languages/makefile";
import markdown from "highlight.js/lib/languages/markdown";
import nginx from "highlight.js/lib/languages/nginx";
import nix from "highlight.js/lib/languages/nix";
import objectivec from "highlight.js/lib/languages/objectivec";
import ocaml from "highlight.js/lib/languages/ocaml";
import perl from "highlight.js/lib/languages/perl";
import php from "highlight.js/lib/languages/php";
import plaintext from "highlight.js/lib/languages/plaintext";
import powershell from "highlight.js/lib/languages/powershell";
import protobuf from "highlight.js/lib/languages/protobuf";
import python from "highlight.js/lib/languages/python";
import r from "highlight.js/lib/languages/r";
import ruby from "highlight.js/lib/languages/ruby";
import rust from "highlight.js/lib/languages/rust";
import scala from "highlight.js/lib/languages/scala";
import scss from "highlight.js/lib/languages/scss";
import shell from "highlight.js/lib/languages/shell";
import sql from "highlight.js/lib/languages/sql";
import swift from "highlight.js/lib/languages/swift";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";
import { highlightFragment } from "./sanitize.js";
import "./code.css";

const LANGUAGES = {
  bash, c, clojure, cmake, cpp, csharp, css, dart, diff, dockerfile, elixir, erlang, go, graphql, haskell, http, ini,
  java, javascript, json, kotlin, latex, lua, makefile, markdown, nginx, nix, objectivec, ocaml, perl, php, plaintext,
  powershell, protobuf, python, r, ruby, rust, scala, scss, shell, sql, swift, typescript, xml, yaml,
};
for (const [name, language] of Object.entries(LANGUAGES)) hljs.registerLanguage(name, language);
// Names the languages' own alias lists lack.
hljs.registerAliases(["sh", "zsh", "console", "shellsession"], { languageName: "bash" });
hljs.registerAliases(["toml", "conf", "cfg", "properties", "env"], { languageName: "ini" });
hljs.registerAliases(["jsonc", "json5"], { languageName: "json" });
hljs.registerAliases(["html", "svg", "xhtml", "plist", "vue", "svelte"], { languageName: "xml" });
hljs.registerAliases(["patch"], { languageName: "diff" });
hljs.registerAliases(["text", "txt", "plain", "log"], { languageName: "plaintext" });

// Guessing a fence's language costs a run of every candidate; only short
// blocks get one, among the languages replies use most.
const GUESS_MAX = 4096;
const GUESS_FROM = ["bash", "python", "javascript", "typescript", "go", "rust", "json", "yaml", "xml", "css", "sql", "c", "cpp", "java"];

const BY_EXTENSION = {
  sh: "bash", bash: "bash", zsh: "bash", fish: "bash", c: "c", h: "c", clj: "clojure", cljs: "clojure", cmake: "cmake",
  cc: "cpp", cpp: "cpp", cxx: "cpp", hpp: "cpp", hh: "cpp", cs: "csharp", css: "css", dart: "dart", diff: "diff",
  patch: "diff", ex: "elixir", exs: "elixir", erl: "erlang", go: "go", graphql: "graphql", gql: "graphql",
  hs: "haskell", ini: "ini", toml: "ini", cfg: "ini", conf: "ini", java: "java", js: "javascript", mjs: "javascript",
  cjs: "javascript", jsx: "javascript", json: "json", jsonc: "json", kt: "kotlin", kts: "kotlin", tex: "latex",
  lua: "lua", mk: "makefile", md: "markdown", markdown: "markdown", nix: "nix", m: "objectivec", mm: "objectivec",
  ml: "ocaml", mli: "ocaml", pl: "perl", pm: "perl", php: "php", ps1: "powershell", proto: "protobuf", py: "python",
  pyi: "python", r: "r", rb: "ruby", rs: "rust", scala: "scala", scss: "scss", sql: "sql", swift: "swift",
  ts: "typescript", tsx: "typescript", mts: "typescript", html: "xml", htm: "xml", xml: "xml", svg: "xml",
  svelte: "xml", vue: "xml", yaml: "yaml", yml: "yaml", txt: "plaintext", log: "plaintext",
};
const BY_NAME = { dockerfile: "dockerfile", containerfile: "dockerfile", makefile: "makefile", gnumakefile: "makefile", "nginx.conf": "nginx" };

const canonical = new Map(); // a registered language object → its name

export function languageFor(name) {
  if (typeof name !== "string") return null;
  // A fence's info string: its first word, without braces or a leading dot ({.python}).
  const word = name.trim().split(/\s+/)[0]?.replace(/^\{?\.?/, "").replace(/\}$/, "").toLowerCase() ?? "";
  if (!word) return null;
  const language = hljs.getLanguage(word);
  if (!language) return null;
  if (canonical.size === 0) for (const n of hljs.listLanguages()) canonical.set(hljs.getLanguage(n), n);
  return canonical.get(language) ?? word;
}

export function languageForPath(path) {
  if (typeof path !== "string" || path === "") return null;
  const base = path.split(/[\\/]/).pop().toLowerCase();
  if (BY_NAME[base]) return BY_NAME[base];
  if (base.startsWith("dockerfile")) return "dockerfile";
  const dot = base.lastIndexOf(".");
  return dot > 0 ? (BY_EXTENSION[base.slice(dot + 1)] ?? null) : null;
}

export function showControls(text) {
  return String(text).replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g, (ch) => {
    const code = ch.charCodeAt(0);
    return String.fromCharCode(code === 0x7f ? 0x2421 : 0x2400 + code);
  });
}

export function gutterWidth(lastNumber) {
  return String(Math.max(1, Math.floor(Math.abs(lastNumber)))).length;
}

// The highlighted HTML for code, or null when nothing should color it.
function highlightedHtml(code, lang) {
  const language = languageFor(lang);
  try {
    if (language) return language === "plaintext" ? null : hljs.highlight(code, { language, ignoreIllegals: true }).value;
    if (lang == null || lang === "") {
      if (code.length > GUESS_MAX) return null;
      const guess = hljs.highlightAuto(code, GUESS_FROM);
      return guess.relevance >= 5 ? guess.value : null;
    }
  } catch {
    return null;
  }
  return null;
}

// lang: a language name or a fence's info string; null or "" guesses.
export function highlight(code, lang = null, doc = document) {
  const text = showControls(code ?? "");
  const html = highlightedHtml(text, lang);
  const fragment = html === null ? null : highlightFragment(html);
  // The cleaned nodes must say exactly what the code says; anything else
  // (a cleaner that dropped text) falls back to plain text.
  if (fragment && fragment.textContent === text) return adopt(doc, fragment);
  const plain = doc.createDocumentFragment();
  if (text) plain.append(doc.createTextNode(text));
  return plain;
}

function adopt(doc, fragment) {
  if (fragment.ownerDocument === doc) return fragment;
  return doc.importNode(fragment, true);
}

// Each line as its own fragment: a span that crosses a line end is repeated
// on the next line with the same classes.
export function highlightLines(code, lang = null, doc = document) {
  return splitLines(highlight(code, lang, doc), doc);
}

export function splitLines(fragment, doc = document) {
  const lines = [doc.createDocumentFragment()];
  const walk = (node, classes) => {
    for (const child of node.childNodes) {
      if (child.nodeType === 3) {
        const parts = child.data.split("\n");
        parts.forEach((part, i) => {
          if (i > 0) lines.push(doc.createDocumentFragment());
          if (!part) return;
          const text = doc.createTextNode(part);
          if (classes.length === 0) lines[lines.length - 1].append(text);
          else {
            const span = doc.createElement("span");
            span.className = classes.join(" ");
            span.append(text);
            lines[lines.length - 1].append(span);
          }
        });
      } else if (child.nodeType === 1) {
        const own = child.getAttribute("class");
        walk(child, own ? [...classes, own] : classes);
      }
    }
  };
  walk(fragment, []);
  return lines;
}
