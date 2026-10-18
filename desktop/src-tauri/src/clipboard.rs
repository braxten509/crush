//! Clipboard writes and reads through Rust (desktop-design 1.2.8, D22), so
//! copying works the same from keys, menus and auto-copy (CHAT-090), and
//! Ctrl+Shift+V reads plain text (TYPE-010). Ctrl+V stays the web view's own
//! paste event (TYPE-014).
//!
//! A write follows the terminal (internal/clipboard/clipboard_supported.go:
//! 34-66): it is read back to confirm it landed, and tried three times 5 ms
//! apart before it counts as failed, because another program writing at the
//! same moment can make a good write look lost. Only a verified failure is an
//! error ("Failed to copy to clipboard", internal/ui/common/common.go:265).
//!
//! The commands that call these run off the main thread (`#[tauri::command(async)]`
//! in commands.rs), so a body may wait for `app.run_on_main_thread`.

use crate::error::{AppError, AppResult};
use std::time::Duration;

/// Tries of one write before it counts as failed, and the pause between them.
const WRITE_ATTEMPTS: usize = 3;
const RETRY_DELAY: Duration = Duration::from_millis(5);

/// The longest a clipboard call waits for an answer.
const WAIT: Duration = Duration::from_secs(5);

/// Linux: gtk::Clipboard (CLIPBOARD selection) on the main thread via app.run_on_main_thread;
/// Mac: the general pasteboard through `pbcopy` and `pbpaste` (no new library).
pub fn write_text(app: &tauri::AppHandle, text: &str) -> AppResult<()> {
    for attempt in 0..WRITE_ATTEMPTS {
        if attempt > 0 {
            std::thread::sleep(RETRY_DELAY);
        }
        if platform::write_and_check(app, text)? {
            return Ok(());
        }
    }
    Err(copy_failed())
}

/// Ctrl+Shift+V (TYPE-010). None when the clipboard is empty or holds no text.
pub fn read_text(app: &tauri::AppHandle) -> AppResult<Option<String>> {
    Ok(platform::read(app)?.filter(|text| !text.is_empty()))
}

/// The terminal's words for a copy that did not land (CHAT-090).
fn copy_failed() -> AppError {
    AppError::new("clipboard_failed", "Failed to copy to clipboard")
}

/// The terminal's words for a paste with nothing to paste (TYPE-010).
fn read_failed() -> AppError {
    AppError::new("clipboard_failed", "Clipboard is empty or does not contain text")
}

#[cfg(target_os = "linux")]
mod platform {
    use super::{copy_failed, read_failed, WAIT};
    use crate::error::AppResult;
    use gtk::gdk::SELECTION_CLIPBOARD;
    use std::sync::mpsc;

    /// Sets the text, then reads it back. Ok(false): the clipboard does not
    /// hold the text afterwards (the caller tries again).
    pub fn write_and_check(app: &tauri::AppHandle, text: &str) -> AppResult<bool> {
        let text = text.to_string();
        let landed = on_main(app, move |done: Box<dyn FnOnce(bool) + Send>| {
            let clipboard = gtk::Clipboard::get(&SELECTION_CLIPBOARD);
            clipboard.set_text(&text);
            // Writing nothing empties the clipboard, so there is nothing to read back.
            if text.is_empty() {
                done(true);
                return;
            }
            clipboard.request_text(move |_, got| done(got == Some(text.as_str())));
        });
        landed.ok_or_else(copy_failed)
    }

    pub fn read(app: &tauri::AppHandle) -> AppResult<Option<String>> {
        let text = on_main(app, |done: Box<dyn FnOnce(Option<String>) + Send>| {
            let clipboard = gtk::Clipboard::get(&SELECTION_CLIPBOARD);
            clipboard.request_text(move |_, text| done(text.map(str::to_string)));
        });
        text.ok_or_else(read_failed)
    }

    /// Runs `work` on the main thread (GTK lives there) and waits up to WAIT
    /// for the answer it hands to `done`. None when the main thread could not
    /// take the work or no answer came in time.
    fn on_main<T: Send + 'static>(
        app: &tauri::AppHandle,
        work: impl FnOnce(Box<dyn FnOnce(T) + Send>) + Send + 'static,
    ) -> Option<T> {
        let (tx, rx) = mpsc::channel();
        let done: Box<dyn FnOnce(T) + Send> = Box::new(move |value| {
            let _ = tx.send(value);
        });
        if let Err(e) = app.run_on_main_thread(move || work(done)) {
            eprintln!("crush-desktop: the clipboard could not reach the main thread: {e}");
            return None;
        }
        rx.recv_timeout(WAIT).ok()
    }
}

#[cfg(target_os = "macos")]
mod platform {
    use super::{copy_failed, read_failed};
    use crate::error::AppResult;
    use std::io::Write;
    use std::process::{Command, Stdio};

    /// pbcopy and pbpaste read and write the general pasteboard as plain text;
    /// a GUI app's environment may lack LANG, so UTF-8 is named explicitly.
    pub fn write_and_check(app: &tauri::AppHandle, text: &str) -> AppResult<bool> {
        let mut child = Command::new("/usr/bin/pbcopy")
            .env("LANG", "en_US.UTF-8")
            .stdin(Stdio::piped())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|_| copy_failed())?;
        let wrote = child.stdin.take().map(|mut stdin| stdin.write_all(text.as_bytes()));
        let status = child.wait().map_err(|_| copy_failed())?;
        if !matches!(wrote, Some(Ok(()))) || !status.success() {
            return Ok(false);
        }
        if text.is_empty() {
            return Ok(true);
        }
        Ok(read(app)?.as_deref() == Some(text))
    }

    pub fn read(_app: &tauri::AppHandle) -> AppResult<Option<String>> {
        let out = Command::new("/usr/bin/pbpaste")
            .env("LANG", "en_US.UTF-8")
            .stdin(Stdio::null())
            .stderr(Stdio::null())
            .output()
            .map_err(|_| read_failed())?;
        if !out.status.success() {
            return Err(read_failed());
        }
        Ok(String::from_utf8(out.stdout).ok())
    }
}

#[cfg(not(any(target_os = "linux", target_os = "macos")))]
mod platform {
    use super::{copy_failed, read_failed};
    use crate::error::AppResult;

    pub fn write_and_check(_app: &tauri::AppHandle, _text: &str) -> AppResult<bool> {
        Err(copy_failed())
    }

    pub fn read(_app: &tauri::AppHandle) -> AppResult<Option<String>> {
        Err(read_failed())
    }
}
