//! Terminal tabs (desktop-design-5-6 2.25, 6.4): a shell per tab in a
//! pseudo-terminal (`portable-pty`), a port of the session host's terminal
//! sessions (internal/ui/sessionhost/session.go, terminal.go, run.go).
//!
//! Each tab has three threads: the waiter (the process's end), the reader
//! (output to the page, in batches at most every 8 ms) and the writer (the
//! page's keys to the program, in order). The reader waits with poll(2), so
//! closing the tab can make it let go of the terminal even while a program
//! the shell started still holds the other end; only then is the terminal
//! really closed and its programs hung up, as the session host's tty.Close()
//! does. The environment is never logged, recorded or probed.

use crate::error::{codes, AppError, AppResult};
use base64::Engine as _;
use portable_pty::{native_pty_system, CommandBuilder, MasterPty, PtySize};
use serde::Serialize;
use serde_json::{json, Value};
use std::collections::{HashMap, HashSet};
use std::fs::File;
use std::io::{ErrorKind, Read, Write};
use std::os::fd::{AsRawFd, BorrowedFd, RawFd};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{mpsc, Arc, Condvar, Mutex};
use std::time::{Duration, Instant};
use tauri::ipc::Channel;
use tauri::{AppHandle, Manager};

/// How long a closed tab's programs get before they are killed (session.go:64).
const STOP_GRACE: Duration = Duration::from_secs(5);
/// How long the last output may take after the shell ended (session.go:67).
const OUTPUT_DRAIN: Duration = Duration::from_secs(2);
/// A window's tabs all end within this (stopGrace plus a second, run.go:37).
const CLOSE_ALL_LIMIT: Duration = Duration::from_secs(6);
/// Output goes to the page at most once per this time per tab (6.4, inferred).
const BATCH: Duration = Duration::from_millis(8);
/// How often an idle reader looks whether its tab is closing.
const IDLE_POLL_MS: i32 = 50;

/// The variables that describe the real terminal (run.go:53-58).
const TERMINAL_NAMES: [&str; 8] = [
    "TERM",
    "COLORTERM",
    "TERM_PROGRAM",
    "TERM_PROGRAM_VERSION",
    "LC_TERMINAL",
    "LC_TERMINAL_VERSION",
    "VTE_VERSION",
    "WT_SESSION",
];
/// Terminal-specific families (run.go:61).
const TERMINAL_PREFIXES: [&str; 6] = ["KONSOLE_", "KITTY_", "GHOSTTY_", "WEZTERM_", "ITERM_", "TMUX"];
/// What marks a process as a chat or an agent inside Crush (run.go:70-73,
/// session_host.go:43-45), and the launcher's window ID, so a `crush`
/// started in a tab never takes it (2.25, 9.4 D11).
const CRUSH_NAMES: [&str; 5] = [
    "CRUSH_SESSION_HOST",
    "CRUSH_TASKS_DIR",
    "CRUSH_SESSION_ID",
    "CRUSH_CRASH_REPORT",
    "CRUSH_REMOTE_LAUNCH",
];

/// The app's tabs, by id (managed by main.rs).
#[derive(Default)]
pub struct Ptys {
    tabs: Mutex<HashMap<String, Arc<Tab>>>,
    /// Windows whose tabs `pty_close_all` ended. A start still on its way
    /// when the window closed is refused here (window labels are never
    /// reused), so no shell outlives its window (2.25, 6.4). Locked after
    /// `tabs`, never before.
    closed: Mutex<HashSet<String>>,
    next: AtomicU64,
}

#[derive(Serialize)]
pub struct Spawned {
    pub id: String,
    pub pid: u32,
}

struct Tab {
    /// The window that started it (`pty_close_all` ends a window's tabs).
    label: String,
    pid: u32,
    /// Resizes the terminal; dropped when the tab lets go of it.
    master: Mutex<Option<Box<dyn MasterPty + Send>>>,
    /// To the writer thread; dropped when the tab lets go of the terminal.
    input: Mutex<Option<mpsc::Sender<Vec<u8>>>>,
    /// The reader stops and lets go of its end of the terminal.
    letting_go: AtomicBool,
    /// `pty_close` already ran (session.go:400-405).
    stopped: AtomicBool,
    /// The shell has exited.
    ended: Flag,
    /// The reader has stopped.
    output_done: Flag,
}

impl Tab {
    /// Closes this side of the terminal: the reader stops within
    /// IDLE_POLL_MS, and the master and the writer's copy are dropped, so the
    /// shell and its foreground job get the hang-up a closed window gives.
    fn let_go(&self) {
        self.letting_go.store(true, Ordering::SeqCst);
        self.master.lock().unwrap().take();
        self.input.lock().unwrap().take();
    }
}

/// A one-way flag threads can wait on.
#[derive(Default)]
struct Flag {
    set: Mutex<bool>,
    changed: Condvar,
}

impl Flag {
    fn set(&self) {
        *self.set.lock().unwrap() = true;
        self.changed.notify_all();
    }

    fn is_set(&self) -> bool {
        *self.set.lock().unwrap()
    }

    /// Waits until set or `limit` passed; true when set.
    fn wait(&self, limit: Duration) -> bool {
        let guard = self.set.lock().unwrap();
        let (guard, _) = self.changed.wait_timeout_while(guard, limit, |set| !*set).unwrap();
        *guard
    }
}

/// `pty_spawn`: `onData` gets `{data: <base64>}` batches, then `{exit: {code, signal}}` once.
pub fn spawn(
    app: &AppHandle,
    label: &str,
    cwd: &str,
    argv: Option<Vec<String>>,
    cols: u16,
    rows: u16,
    on_data: Channel<Value>,
) -> AppResult<Spawned> {
    let env = clean_env(crate::login_env::login_env());
    // No command: the user's shell, interactive (terminal.go:33-47).
    let (program, args) = match argv {
        None => (login_shell(&env), vec!["-i".to_string()]),
        Some(argv) => {
            let mut words = argv.into_iter();
            let first = words.next().filter(|w| !w.is_empty()).ok_or_else(|| failed("open: no command given"))?;
            // "crush" is the one the app itself uses (2.27).
            let program = if first == "crush" { crush_program(app, &env)? } else { first };
            (program, words.collect())
        }
    };
    // As terminal.go:55 words it: "open <program>: <why>", the why as Go's
    // os/exec gives it.
    let fail = |why: String| failed(format!("open {program}: {why}"));
    match std::fs::metadata(cwd) {
        Ok(meta) if meta.is_dir() => {}
        Ok(_) => return Err(fail(format!("chdir {cwd}: not a directory"))),
        Err(_) => return Err(fail(format!("chdir {cwd}: no such file or directory"))),
    }
    let size = PtySize { rows: rows.max(1), cols: cols.max(1), pixel_width: 0, pixel_height: 0 };
    let pair = native_pty_system().openpty(size).map_err(|e| fail(format!("{e:#}")))?;
    let mut cmd = CommandBuilder::new(&program);
    cmd.args(&args);
    cmd.cwd(cwd);
    cmd.env_clear();
    for (name, value) in &env {
        cmd.env(name, value);
    }
    let mut child = pair.slave.spawn_command(cmd).map_err(|e| fail(format!("{e:#}")))?;
    // The child has its own copies; ours would keep the terminal open after it ends.
    drop(pair.slave);
    let pid = child.process_id().unwrap_or(0);
    let master = pair.master;
    let (reader, writer) = match master.as_raw_fd().map(|fd| (copy_fd(fd), copy_fd(fd))) {
        Some((Ok(reader), Ok(writer))) => (reader, writer),
        _ => {
            signal(pid as i32, SIGKILL);
            let _ = child.wait();
            return Err(fail("could not read the terminal".to_string()));
        }
    };

    let (input, keys) = mpsc::channel::<Vec<u8>>();
    let tab = Arc::new(Tab {
        label: label.to_string(),
        pid,
        master: Mutex::new(Some(master)),
        input: Mutex::new(Some(input)),
        letting_go: AtomicBool::new(false),
        stopped: AtomicBool::new(false),
        ended: Flag::default(),
        output_done: Flag::default(),
    });
    let ptys = app.state::<Ptys>();
    let id = (ptys.next.fetch_add(1, Ordering::SeqCst) + 1).to_string();
    {
        let mut all = ptys.tabs.lock().unwrap();
        if ptys.closed.lock().unwrap().contains(label) {
            drop(all);
            signal(-(pid as i32), SIGKILL);
            signal(pid as i32, SIGKILL);
            let _ = child.wait();
            return Err(fail("the window closed".to_string()));
        }
        all.insert(id.clone(), tab.clone());
    }

    // The writer: the page's input, in order, without holding up the caller
    // while a program is slow to read (session.go:355-357).
    std::thread::spawn(move || {
        let mut writer = writer;
        for bytes in keys {
            if writer.write_all(&bytes).is_err() {
                break;
            }
        }
    });

    // The reader: output to the page (session.go:321-351).
    let out = on_data.clone();
    let reading = tab.clone();
    std::thread::spawn(move || {
        read_output(&reading, reader, &out);
        reading.output_done.set();
    });

    // The waiter (session.go:299-312): after the shell ends, let the last
    // output in, unless something it started still holds the terminal open;
    // then let go of the terminal and report the end.
    let app = app.clone();
    let waited = tab;
    let tab_id = id.clone();
    std::thread::spawn(move || {
        let status = child.wait();
        waited.ended.set();
        waited.output_done.wait(OUTPUT_DRAIN);
        waited.let_go();
        while !waited.output_done.wait(Duration::from_secs(1)) {}
        app.state::<Ptys>().tabs.lock().unwrap().remove(&tab_id);
        let (code, signal) = match &status {
            Ok(status) if status.signal().is_some() => (None, status.signal().map(str::to_string)),
            Ok(status) => (Some(status.exit_code()), None),
            Err(_) => (None, None),
        };
        let _ = on_data.send(json!({"exit": {"code": code, "signal": signal}}));
    });

    Ok(Spawned { id, pid })
}

/// `pty_write`: `data` is base64.
pub fn write(app: &AppHandle, id: &str, data: &str) -> AppResult<()> {
    let tab = live_tab(app, id)?;
    let bytes = base64::engine::general_purpose::STANDARD
        .decode(data)
        .map_err(|_| failed("pty_write takes base64 data"))?;
    if let Some(input) = tab.input.lock().unwrap().as_ref() {
        let _ = input.send(bytes);
    }
    Ok(())
}

pub fn resize(app: &AppHandle, id: &str, cols: u16, rows: u16) -> AppResult<()> {
    let tab = live_tab(app, id)?;
    let size = PtySize { rows: rows.max(1), cols: cols.max(1), pixel_width: 0, pixel_height: 0 };
    if let Some(master) = tab.master.lock().unwrap().as_ref() {
        master.resize(size).map_err(|e| failed(format!("resize: {e:#}")))?;
    }
    Ok(())
}

/// The shell's folder, or None where it cannot be read (terminal.go:89-98).
pub fn cwd(app: &AppHandle, id: &str) -> AppResult<Option<String>> {
    let tab = live_tab(app, id)?;
    if cfg!(target_os = "linux") {
        let link = std::fs::read_link(format!("/proc/{}/cwd", tab.pid));
        return Ok(link.ok().map(|dir| dir.to_string_lossy().into_owned()));
    }
    Ok(None)
}

/// Ends one tab: SIGHUP, SIGTERM, then SIGKILL to its process group after 5 s.
/// Returns once the shell ended (a tab that already ended is no error).
pub fn close(app: &AppHandle, id: &str) -> AppResult<()> {
    let Some(tab) = find(app, id) else {
        return Ok(());
    };
    stop(&tab);
    tab.ended.wait(STOP_GRACE + Duration::from_secs(1));
    Ok(())
}

/// Every tab of the window `label`, at most 6 s (run.go:32-51).
pub fn close_all(app: &AppHandle, label: &str) -> AppResult<()> {
    let tabs: Vec<Arc<Tab>> = {
        let ptys = app.state::<Ptys>();
        let all = ptys.tabs.lock().unwrap();
        ptys.closed.lock().unwrap().insert(label.to_string());
        all.values().filter(|tab| tab.label == label).cloned().collect()
    };
    for tab in &tabs {
        stop(tab);
    }
    let deadline = Instant::now() + CLOSE_ALL_LIMIT;
    for tab in &tabs {
        tab.ended.wait(deadline.saturating_duration_since(Instant::now()));
    }
    Ok(())
}

/// The session host's environment for shell tabs (TerminalEnviron,
/// run.go:63-100; session_host.go:43-45), plus CRUSH_REMOTE_LAUNCH removed.
pub(crate) fn clean_env(base: Vec<(String, String)>) -> Vec<(String, String)> {
    let mut env: Vec<(String, String)> = base
        .into_iter()
        .filter(|(name, _)| {
            !TERMINAL_NAMES.contains(&name.as_str())
                && !CRUSH_NAMES.contains(&name.as_str())
                && !TERMINAL_PREFIXES.iter().any(|prefix| name.starts_with(prefix))
        })
        .collect();
    env.push(("TERM".to_string(), "xterm-256color".to_string()));
    env.push(("COLORTERM".to_string(), "truecolor".to_string()));
    env
}

/// $SHELL, else /bin/sh (terminal.go:33-39).
fn login_shell(env: &[(String, String)]) -> String {
    env.iter()
        .find(|(name, value)| name == "SHELL" && !value.is_empty())
        .map(|(_, value)| value.clone())
        .unwrap_or_else(|| "/bin/sh".to_string())
}

/// The crush P3.1's `crush_bin::resolve` finds, with the login shell's PATH.
fn crush_program(app: &AppHandle, env: &[(String, String)]) -> AppResult<String> {
    let setting = app.state::<crate::state::AppState>().settings.lock().unwrap().crush_path.clone();
    let path = env.iter().find(|(name, _)| name == "PATH").map(|(_, value)| value.as_str());
    let bin = crate::crush_bin::resolve(setting.as_deref(), path).map_err(|e| failed(e.message))?;
    Ok(bin.path.to_string_lossy().into_owned())
}

fn find(app: &AppHandle, id: &str) -> Option<Arc<Tab>> {
    app.state::<Ptys>().tabs.lock().unwrap().get(id).cloned()
}

/// A tab whose shell still runs.
fn live_tab(app: &AppHandle, id: &str) -> AppResult<Arc<Tab>> {
    find(app, id)
        .filter(|tab| !tab.ended.is_set())
        .ok_or_else(|| AppError::new(codes::NO_SUCH_PTY, format!("There is no terminal {id}.")))
}

fn failed(message: impl Into<String>) -> AppError {
    AppError::new(codes::PTY_FAILED, message)
}

/// session.go:397-425 and proc_unix.go: hang the terminal up, SIGHUP and
/// SIGTERM to the shell, and kill its process group if it has not ended
/// after STOP_GRACE.
fn stop(tab: &Arc<Tab>) {
    if tab.stopped.swap(true, Ordering::SeqCst) || tab.ended.is_set() {
        return;
    }
    tab.let_go();
    // Interactive shells ignore SIGTERM, so hang the shell up directly; it
    // passes the hang-up on to its jobs.
    let pid = tab.pid as i32;
    signal(pid, SIGHUP);
    signal(pid, SIGTERM);
    let waiting = tab.clone();
    std::thread::spawn(move || {
        if !waiting.ended.wait(STOP_GRACE) {
            // The shell leads its own process group (portable-pty runs it with setsid).
            signal(-pid, SIGKILL);
            signal(pid, SIGKILL);
        }
    });
}

/// Reads the program's output and sends it in batches, at most one per
/// BATCH, until the terminal ends or the tab lets go of it.
fn read_output(tab: &Tab, reader: File, out: &Channel<Value>) {
    let fd = reader.as_raw_fd();
    let mut reader = reader;
    let mut chunk = vec![0u8; 32 * 1024];
    let mut pending: Vec<u8> = Vec::new();
    let mut last_sent = Instant::now() - BATCH;
    loop {
        if tab.letting_go.load(Ordering::SeqCst) {
            break;
        }
        let wait_ms = if pending.is_empty() {
            IDLE_POLL_MS
        } else {
            (last_sent + BATCH).saturating_duration_since(Instant::now()).as_millis() as i32
        };
        let mut ended = false;
        match readable(fd, wait_ms) {
            Readable::Yes => match reader.read(&mut chunk) {
                Ok(0) => ended = true,
                Ok(n) => pending.extend_from_slice(&chunk[..n]),
                Err(e) if matches!(e.kind(), ErrorKind::Interrupted | ErrorKind::WouldBlock) => {}
                // EIO: every program let go of the terminal.
                Err(_) => ended = true,
            },
            Readable::NotYet => {}
            Readable::Gone => ended = true,
        }
        if !pending.is_empty() && (ended || last_sent.elapsed() >= BATCH) {
            send_output(out, &mut pending);
            last_sent = Instant::now();
        }
        if ended {
            break;
        }
    }
    if !pending.is_empty() {
        send_output(out, &mut pending);
    }
}

fn send_output(out: &Channel<Value>, pending: &mut Vec<u8>) {
    let data = base64::engine::general_purpose::STANDARD.encode(pending.as_slice());
    pending.clear();
    let _ = out.send(json!({ "data": data }));
}

/// A second handle on the terminal's master side.
fn copy_fd(fd: RawFd) -> std::io::Result<File> {
    // SAFETY: fd is the master's, open for as long as the master (the caller's) lives.
    let borrowed = unsafe { BorrowedFd::borrow_raw(fd) };
    Ok(File::from(borrowed.try_clone_to_owned()?))
}

enum Readable {
    Yes,
    NotYet,
    Gone,
}

#[repr(C)]
struct PollFd {
    fd: std::ffi::c_int,
    events: std::ffi::c_short,
    revents: std::ffi::c_short,
}

#[cfg(target_os = "linux")]
type PollCount = std::ffi::c_ulong;
#[cfg(not(target_os = "linux"))]
type PollCount = std::ffi::c_uint;

extern "C" {
    fn poll(fds: *mut PollFd, count: PollCount, timeout_ms: std::ffi::c_int) -> std::ffi::c_int;
    fn kill(pid: i32, sig: i32) -> i32;
}

const POLLIN: std::ffi::c_short = 0x1;
const POLLERR: std::ffi::c_short = 0x8;
const POLLHUP: std::ffi::c_short = 0x10;
const POLLNVAL: std::ffi::c_short = 0x20;
const SIGHUP: i32 = 1;
const SIGKILL: i32 = 9;
const SIGTERM: i32 = 15;

/// Waits up to `wait_ms` for output (or the end) on fd.
fn readable(fd: RawFd, wait_ms: i32) -> Readable {
    let mut one = PollFd { fd, events: POLLIN, revents: 0 };
    // SAFETY: one valid pollfd for the length of the call.
    let n = unsafe { poll(&mut one, 1, wait_ms.max(0)) };
    if n < 0 {
        return if std::io::Error::last_os_error().kind() == ErrorKind::Interrupted { Readable::NotYet } else { Readable::Gone };
    }
    if n == 0 {
        return Readable::NotYet;
    }
    if one.revents & POLLNVAL != 0 {
        return Readable::Gone;
    }
    if one.revents & (POLLIN | POLLHUP | POLLERR) != 0 {
        // A read tells data from the end (EIO).
        return Readable::Yes;
    }
    Readable::NotYet
}

fn signal(pid: i32, sig: i32) {
    if pid == 0 || pid == -1 {
        return; // never this process's group or every process
    }
    // SAFETY: a plain system call.
    unsafe { kill(pid, sig) };
}

#[cfg(test)]
mod tests {
    use super::*;

    fn pairs(list: &[(&str, &str)]) -> Vec<(String, String)> {
        list.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect()
    }

    #[test]
    fn env_clean() {
        // Every case of 2.25: the terminal's names, each family by prefix
        // (TMUX itself and anything starting with it), the session host's
        // and the launcher's names; the rest kept in order, then TERM and
        // COLORTERM set once each.
        let removed = [
            "TERM", "COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "LC_TERMINAL", "LC_TERMINAL_VERSION",
            "VTE_VERSION", "WT_SESSION", "KONSOLE_DBUS_SESSION", "KONSOLE_VERSION", "KITTY_WINDOW_ID",
            "KITTY_PID", "GHOSTTY_RESOURCES_DIR", "WEZTERM_PANE", "ITERM_SESSION_ID", "TMUX", "TMUX_PANE",
            "TMUXINATOR_CONFIG", "CRUSH_SESSION_HOST", "CRUSH_TASKS_DIR", "CRUSH_SESSION_ID",
            "CRUSH_CRASH_REPORT", "CRUSH_REMOTE_LAUNCH",
        ];
        let kept = pairs(&[
            ("PATH", "/x/bin:/usr/bin"),
            ("HOME", "/home/u"),
            ("TERMINFO", "/usr/share/terminfo"),
            ("MY_TERM", "keep"),
            ("XKITTY_THING", "keep"),
            ("CRUSH_DESKTOP_ENV_MARK", "kept"),
            ("CRUSH_GLOBAL_CONFIG", "/c"),
            ("SHELL", "/bin/zsh"),
        ]);
        let mut base = Vec::new();
        for (i, name) in removed.iter().enumerate() {
            base.push((name.to_string(), format!("from-the-app-{i}")));
            if let Some(pair) = kept.get(i) {
                base.push(pair.clone());
            }
        }
        let mut want = kept.clone();
        want.push(("TERM".to_string(), "xterm-256color".to_string()));
        want.push(("COLORTERM".to_string(), "truecolor".to_string()));
        assert_eq!(clean_env(base), want);
        // An empty environment still gets the two.
        assert_eq!(clean_env(Vec::new()), pairs(&[("TERM", "xterm-256color"), ("COLORTERM", "truecolor")]));
    }
}
