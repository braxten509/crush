//! The app's own log (owner, 2026-10-19): one line per event, from Rust and
//! from the page (`app_log`), in `$XDG_STATE_HOME/crush-desktop/desktop.log`
//! (else ~/.local/state/…). At 5 MB the file moves to `desktop.log.1` and a
//! new one starts, so it never grows without end. Keys, tokens and passwords
//! never go in: callers log what happened, not what was typed or sent.
use std::io::Write;
use std::path::PathBuf;
use std::sync::Mutex;

const LIMIT: u64 = 5 * 1024 * 1024;
static LOCK: Mutex<()> = Mutex::new(());

fn path() -> Option<PathBuf> {
    std::env::var_os("XDG_STATE_HOME")
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".local/state")))
        .map(|d| d.join("crush-desktop").join("desktop.log"))
}

/// Appends one line: Unix time, level, window (or "app") and the message,
/// with line breaks folded so one event stays one line.
pub fn write(level: &str, window: &str, message: &str) {
    let Some(path) = path() else { return };
    let _guard = LOCK.lock().unwrap_or_else(|e| e.into_inner());
    if let Some(dir) = path.parent() {
        if std::fs::create_dir_all(dir).is_err() {
            return;
        }
    }
    if std::fs::metadata(&path).map(|m| m.len() >= LIMIT).unwrap_or(false) {
        let _ = std::fs::rename(&path, path.with_extension("log.1"));
    }
    let secs = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_secs_f64()).unwrap_or(0.0);
    let text: String = message.chars().take(2000).map(|c| if c == '\n' || c == '\r' { ' ' } else { c }).collect();
    if let Ok(mut file) = std::fs::OpenOptions::new().create(true).append(true).open(&path) {
        let _ = writeln!(file, "{secs:.3} {level} {window} {text}");
    }
}

/// `app_log`: a line from the page ("info", "warn" or "error").
pub fn app_log(window: crate::host::Window, level: String, message: String) {
    let level = match level.as_str() {
        "warn" | "error" => level.as_str(),
        _ => "info",
    };
    write(level, window.label(), &message);
}
