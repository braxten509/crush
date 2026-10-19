// What the tool surfaces tell the probe (desktop-design 1.9; test runs
// only). Each mounted component adds a reader here; lib/plugins/tools.js
// collects them into the "tools" section. Readers return plain values the
// component already keeps (light) or read the page (heavy).
const readers = new Map(); // kind → Map(id → {light, heavy})

export function addProbeReader(kind, id, light, heavy = null) {
  if (!readers.has(kind)) readers.set(kind, new Map());
  const entry = { light, heavy };
  readers.get(kind).set(id, entry);
  return () => {
    if (readers.get(kind)?.get(id) === entry) readers.get(kind).delete(id);
  };
}

export function probeRead(kind, which = "light") {
  const out = [];
  for (const [id, entry] of readers.get(kind) ?? []) {
    const read = entry[which];
    if (!read) continue;
    try {
      out.push({ id, ...read() });
    } catch (error) {
      out.push({ id, probe_error: String(error) });
    }
  }
  return out;
}
