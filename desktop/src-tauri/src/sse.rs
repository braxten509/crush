//! The event stream's line splitter (desktop-design 1.2.5). Scaffold S3
//! placeholder: piece P3.1 writes the bodies; the public signatures are
//! pinned.
#![allow(dead_code, unused_variables)]

/// Splits a byte stream into lines (LF, an optional CR before it dropped). Holds a partial
/// line across chunks; a line longer than 64 MiB is dropped with a log line.
pub struct LineSplitter {
    buffer: Vec<u8>,
}

impl LineSplitter {
    pub fn new() -> Self {
        LineSplitter { buffer: Vec::new() }
    }

    pub fn push(&mut self, chunk: &[u8], out: &mut Vec<String>) {}
}
