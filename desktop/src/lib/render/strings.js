// Small string rules shared by the ports of terminal code, written to behave
// exactly like the Go functions they stand for.

// Go's unicode.IsSpace (Go's strings.TrimSpace trims these; JavaScript's
// trim() would also take U+FEFF, which Go keeps).
export function isGoSpace(ch) {
  switch (ch) {
    case "\t":
    case "\n":
    case "\v":
    case "\f":
    case "\r":
    case " ":
    case "\u0085":
    case " ":
    case " ":
    case " ":
    case " ":
    case " ":
    case " ":
    case "　":
      return true;
    default:
      return ch >= " " && ch <= " ";
  }
}

// The range [start, end) of s that Go's strings.TrimSpace keeps.
export function trimSpaceRange(s) {
  let start = 0;
  let end = s.length;
  while (start < end && isGoSpace(s[start])) start++;
  while (end > start && isGoSpace(s[end - 1])) end--;
  return [start, end];
}

// Go's strings.TrimSpace.
export function trimSpace(s) {
  const [start, end] = trimSpaceRange(String(s));
  return String(s).slice(start, end);
}
