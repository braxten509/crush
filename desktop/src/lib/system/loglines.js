// One log line as the terminal's `crush logs` prints it (desktop-design-5-6
// 2.28, MISC-016; internal/cmd/logs.go:167-215): time, level and message,
// then the other keys sorted by name, `source` as "<file>:<line>". A line
// that is not JSON is skipped (null).
export function parseLogLine(text) {
  let data;
  try {
    data = JSON.parse(text);
  } catch {
    return null;
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) return null;
  const parts = [];
  for (const key of Object.keys(data).sort()) {
    if (key === "msg" || key === "level" || key === "time") continue;
    const value = data[key];
    if (key === "source") {
      if (value && typeof value === "object") parts.push(["source", `${value.file}:${Math.trunc(Number(value.line))}`]);
      continue;
    }
    parts.push([key, typeof value === "string" ? value : JSON.stringify(value)]);
  }
  const when = new Date(data.time);
  const time = Number.isNaN(when.getTime()) ? "" : when.toLocaleTimeString("en-US", { hour12: false });
  const level = ["INFO", "DEBUG", "ERROR", "WARN"].includes(data.level) ? data.level : "INFO";
  return { time, level, msg: String(data.msg ?? ""), parts };
}
