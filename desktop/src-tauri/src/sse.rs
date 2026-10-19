//! The event stream's line splitter (desktop-design 1.2.5). The engine writes
//! each event as one `data: <json>` line and a blank line
//! (internal/server/handlers_workspace.go:206); the socket hands the bytes over
//! in chunks that may cut a line, or a UTF-8 character, anywhere.

/// The longest line kept, in bytes. A longer one is dropped with a log line,
/// so a runaway stream cannot fill the memory.
const MAX_LINE: usize = 64 << 20;

/// Splits a byte stream into lines (LF, an optional CR before it dropped). Holds a partial
/// line across chunks; a line longer than 64 MiB is dropped with a log line.
pub struct LineSplitter {
    buffer: Vec<u8>,
    /// The longest line kept (MAX_LINE; smaller in tests).
    max: usize,
    /// Set while the rest of a too-long line is being skipped.
    skipping: bool,
}

impl LineSplitter {
    pub fn new() -> Self {
        Self::with_limit(MAX_LINE)
    }

    fn with_limit(max: usize) -> Self {
        LineSplitter {
            buffer: Vec::new(),
            max,
            skipping: false,
        }
    }

    pub fn push(&mut self, chunk: &[u8], out: &mut Vec<String>) {
        let mut rest = chunk;
        while let Some(end) = rest.iter().position(|&b| b == b'\n') {
            let head = &rest[..end];
            rest = &rest[end + 1..];
            if std::mem::take(&mut self.skipping) {
                // The end of a dropped line.
                continue;
            }
            if self.buffer.len() + head.len() > self.max {
                drop_line(&mut self.buffer);
                continue;
            }
            self.buffer.extend_from_slice(head);
            if self.buffer.last() == Some(&b'\r') {
                self.buffer.pop();
            }
            out.push(text(&self.buffer));
            self.buffer.clear();
        }
        if self.skipping || rest.is_empty() {
            return;
        }
        if self.buffer.len() + rest.len() > self.max {
            drop_line(&mut self.buffer);
            self.skipping = true;
            return;
        }
        self.buffer.extend_from_slice(rest);
    }
}

fn drop_line(buffer: &mut Vec<u8>) {
    eprintln!("crush-desktop: dropped an event line longer than 64 MiB");
    // Give the memory back, not just the length.
    *buffer = Vec::new();
}

/// A complete line as text. The engine writes JSON, which is UTF-8; anything
/// else is kept with replacement characters and fails the JSON parse later.
fn text(line: &[u8]) -> String {
    match std::str::from_utf8(line) {
        Ok(s) => s.to_string(),
        Err(_) => String::from_utf8_lossy(line).into_owned(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn split(chunks: &[&[u8]]) -> Vec<String> {
        let mut s = LineSplitter::new();
        let mut out = Vec::new();
        for chunk in chunks {
            s.push(chunk, &mut out);
        }
        out
    }

    #[test]
    fn sse_lines_split_across_chunks() {
        let lines = split(&[
            b"data: {\"type\":\"mess",
            b"age\",\"payload\":{}}\n",
            b"\ndata: {\"a\":1}\r",
            b"\n\r\n",
            b"data: tail",
        ]);
        assert_eq!(lines, vec![
            "data: {\"type\":\"message\",\"payload\":{}}",
            "",
            "data: {\"a\":1}",
            "",
        ]);
    }

    #[test]
    fn byte_by_byte_gives_the_same_lines() {
        let stream = "data: {\"t\":\"é → ✓\"}\n\ndata: x\r\n\r\n: comment\n\n".as_bytes();
        let whole = split(&[stream]);
        let bytes: Vec<&[u8]> = stream.chunks(1).collect();
        assert_eq!(split(&bytes), whole, "a UTF-8 character cut between chunks survives");
        assert_eq!(whole[0], "data: {\"t\":\"é → ✓\"}");
        assert_eq!(whole.len(), 6);
    }

    #[test]
    fn a_partial_line_waits_for_its_end() {
        let mut s = LineSplitter::new();
        let mut out = Vec::new();
        s.push(b"data: {\"id\":", &mut out);
        assert!(out.is_empty(), "nothing before the line feed");
        s.push(b"1}\n", &mut out);
        assert_eq!(out, vec!["data: {\"id\":1}"]);
    }

    #[test]
    fn a_line_over_the_limit_is_dropped_and_the_next_one_kept() {
        let mut s = LineSplitter::with_limit(16);
        let mut out = Vec::new();
        s.push(b"data: short\n", &mut out);
        s.push(b"data: this line is far too long", &mut out);
        s.push(b" and goes on\ndata: ok\n", &mut out);
        s.push(b"0123456789abcdefXYZ\ndata: again\n", &mut out);
        assert_eq!(out, vec!["data: short", "data: ok", "data: again"]);
    }
}
