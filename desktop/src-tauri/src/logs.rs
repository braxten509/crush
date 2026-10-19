//! The log viewer's reads (desktop-design-5-6 2.28, 6.4): the window's
//! project log, `<data folder>/logs/crush.log`, with the data folder from the
//! window's `Ensured.data_dir`. `logs_tail` reads the last lines; a follow
//! polls the file every 250 ms and sends the new whole lines. SYS owns this
//! file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use serde_json::json;
use std::collections::HashMap;
use std::fs::File;
use std::io::{Read, Seek, SeekFrom};
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tauri::ipc::Channel;
use tauri::{AppHandle, Manager};

/// The running follows, by id, with their window's label (managed by main.rs).
#[derive(Default)]
pub struct Follows {
    stops: Mutex<HashMap<String, (String, Arc<AtomicBool>)>>,
}

#[derive(Serialize)]
pub struct Tail {
    pub lines: Vec<String>,
    pub truncated: bool,
    pub missing: bool,
    pub path: String,
    /// The byte offset just after the last whole line read; a follow starts
    /// there, so no line written in between is lost.
    pub end: u64,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Followed {
    pub follow_id: String,
}

const TAIL_CHUNK: u64 = 64 * 1024;

pub fn log_path(app: &AppHandle, label: &str) -> AppResult<PathBuf> {
    let ensured = crate::connection::ensured(app, label).ok_or_else(|| AppError::new("engine_unavailable", "not connected to Crush yet"))?;
    Ok(PathBuf::from(ensured.data_dir).join("logs").join("crush.log"))
}

/// `logs_tail`: the window's project log (its `Ensured.data_dir`, `connection::ensured`).
pub fn tail(app: &AppHandle, label: &str, lines: u32) -> AppResult<Tail> {
    let path = log_path(app, label)?;
    let shown = path.to_string_lossy().into_owned();
    let mut file = match File::open(&path) {
        Ok(file) => file,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Tail { lines: vec![], truncated: false, missing: true, path: shown, end: 0 }),
        Err(e) => return Err(AppError::new("read_failed", format!("{shown}: {e}"))),
    };
    let size = file.metadata().map(|m| m.len()).unwrap_or(0);
    let want = lines as usize;
    // Read backwards in chunks until enough lines are in.
    let mut start = size;
    let mut buf: Vec<u8> = Vec::new();
    loop {
        let step = TAIL_CHUNK.min(start);
        start -= step;
        let mut chunk = vec![0u8; step as usize];
        file.seek(SeekFrom::Start(start)).map_err(|e| AppError::new("read_failed", e.to_string()))?;
        file.read_exact(&mut chunk).map_err(|e| AppError::new("read_failed", e.to_string()))?;
        chunk.extend_from_slice(&buf);
        buf = chunk;
        let count = buf.iter().filter(|&&b| b == b'\n').count();
        if start == 0 || count > want {
            break;
        }
    }
    // A last line still being written is left for the follow.
    let unfinished = buf.iter().rev().take_while(|&&b| b != b'\n').count();
    let unfinished = if unfinished == buf.len() && start > 0 { 0 } else { unfinished };
    buf.truncate(buf.len() - unfinished);
    let end = size - unfinished as u64;
    let text = String::from_utf8_lossy(&buf);
    let mut all: Vec<String> = text.lines().filter(|l| !l.trim().is_empty()).map(String::from).collect();
    if start > 0 && !all.is_empty() {
        all.remove(0); // a partial first line
    }
    let truncated = all.len() > want || start > 0;
    if all.len() > want {
        all.drain(..all.len() - want);
    }
    let truncated = truncated && all.len() >= want;
    Ok(Tail { lines: all, truncated, missing: false, path: shown, end })
}

/// `logs_follow`: new lines as the file grows.
/// `from` is the tail's `end`; None starts at the file's end. A window has
/// one follow: a new one (after a reload) stops the old one, and closing the
/// window stops it too (`stop_window`).
pub fn follow(app: &AppHandle, label: &str, from: Option<u64>, on_lines: Channel<serde_json::Value>) -> AppResult<Followed> {
    let path = log_path(app, label)?;
    stop_window(app, label);
    let id = uuid::Uuid::new_v4().to_string();
    let stop = Arc::new(AtomicBool::new(false));
    app.state::<Follows>().stops.lock().unwrap().insert(id.clone(), (label.to_string(), stop.clone()));
    let handle = app.clone();
    let owner = label.to_string();
    let own_id = id.clone();
    std::thread::spawn(move || {
        let mut offset = from.unwrap_or_else(|| std::fs::metadata(&path).map(|m| m.len()).unwrap_or(0));
        let mut partial = String::new();
        while !stop.load(Ordering::SeqCst) {
            std::thread::sleep(Duration::from_millis(250));
            if handle.get_webview_window(&owner).is_none() {
                break;
            }
            let Ok(meta) = std::fs::metadata(&path) else { continue };
            if meta.len() < offset {
                offset = 0; // the log was replaced
                partial.clear();
            }
            if meta.len() == offset {
                continue;
            }
            let Ok(mut file) = File::open(&path) else { continue };
            if file.seek(SeekFrom::Start(offset)).is_err() {
                continue;
            }
            let mut fresh = Vec::new();
            if file.read_to_end(&mut fresh).is_err() {
                continue;
            }
            offset += fresh.len() as u64;
            partial.push_str(&String::from_utf8_lossy(&fresh));
            let mut lines = Vec::new();
            while let Some(at) = partial.find('\n') {
                let line: String = partial.drain(..=at).collect();
                let line = line.trim_end().to_string();
                if !line.is_empty() {
                    lines.push(line);
                }
            }
            if !lines.is_empty() && on_lines.send(json!({ "lines": lines })).is_err() {
                break;
            }
        }
        handle.state::<Follows>().stops.lock().unwrap().remove(&own_id);
    });
    Ok(Followed { follow_id: id })
}

pub fn unfollow(app: &AppHandle, follow_id: &str) -> AppResult<()> {
    if let Some((_, stop)) = app.state::<Follows>().stops.lock().unwrap().remove(follow_id) {
        stop.store(true, Ordering::SeqCst);
    }
    Ok(())
}

/// Stops every follow of a window (a new follow, or the window is gone).
pub fn stop_window(app: &AppHandle, label: &str) {
    app.state::<Follows>().stops.lock().unwrap().retain(|_, (owner, stop)| {
        let mine = owner == label;
        if mine {
            stop.store(true, Ordering::SeqCst);
        }
        !mine
    });
}
