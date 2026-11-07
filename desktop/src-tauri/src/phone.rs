//! The composer's phone button (owner, 2026-11-01): whether an Android phone
//! is connected over adb (USB or wireless), and a picture of its screen
//! (`adb exec-out screencap -p`). With several, the first real phone listed.
//! adb is looked up in the login shell's PATH (an app started from the menu
//! may not have it), then in the usual SDK folders.
use crate::engine_cli::{run_limited, Ran};
use crate::error::{AppError, AppResult};
use base64::Engine as _;
use serde::Serialize;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

const NO_PHONE: &str = "No phone connected";
const LIST_LIMIT: Duration = Duration::from_secs(4);
const SHOT_LIMIT: Duration = Duration::from_secs(15);
const FIND_LIMIT: Duration = Duration::from_secs(10);

#[derive(Serialize)]
pub struct PhoneShot {
    mime: String,
    base64: String,
}

fn adb() -> Option<PathBuf> {
    tool("adb")
}

fn tool(name: &str) -> Option<PathBuf> {
    let path = crate::login_env::login_env()
        .into_iter()
        .find(|(k, _)| k == "PATH")
        .map(|(_, v)| v)
        .or_else(|| std::env::var("PATH").ok())
        .unwrap_or_default();
    let home = std::env::var("HOME").unwrap_or_default();
    let mut places: Vec<PathBuf> = std::env::split_paths(&path).map(|dir| dir.join(name)).collect();
    for var in ["ANDROID_HOME", "ANDROID_SDK_ROOT"] {
        if let Ok(dir) = std::env::var(var) {
            places.push(PathBuf::from(dir).join("platform-tools").join(name));
        }
    }
    places.push(PathBuf::from(&home).join("Android/Sdk/platform-tools").join(name));
    places.push(PathBuf::from("/opt/android-sdk/platform-tools").join(name));
    places.push(PathBuf::from("/usr/bin").join(name));
    places.into_iter().find(|p| p.is_file())
}

/// The serial of the first phone `adb devices` lists as ready ("device"); a
/// real phone before an emulator.
fn first_phone(adb: &PathBuf) -> Option<String> {
    let Ok(Ran::Exited { code: Some(0), stdout }) = run_limited(Command::new(adb).arg("devices"), LIST_LIMIT, false) else {
        return None;
    };
    let ready: Vec<String> = String::from_utf8_lossy(&stdout)
        .lines()
        .skip(1)
        .filter_map(|line| {
            let mut parts = line.split_whitespace();
            match (parts.next(), parts.next()) {
                (Some(serial), Some("device")) => Some(serial.to_string()),
                _ => None,
            }
        })
        .collect();
    ready.iter().find(|s| !s.starts_with("emulator-")).or(ready.first()).cloned()
}

pub fn connected() -> bool {
    adb().and_then(|adb| first_phone(&adb)).is_some()
}

/// The Find phone button: the phones adb sees on Wi-Fi (`adb mdns services`,
/// Wireless debugging's `_adb-tls-connect` entries), each `adb connect`ed.
/// True when a phone is connected afterwards.
pub fn find() -> AppResult<bool> {
    let adb = adb().ok_or_else(|| AppError::new("no_adb", "adb is not installed"))?;
    if first_phone(&adb).is_some() {
        return Ok(true);
    }
    if let Ok(Ran::Exited { stdout, .. }) = run_limited(Command::new(&adb).args(["mdns", "services"]), FIND_LIMIT, false) {
        for line in String::from_utf8_lossy(&stdout).lines() {
            let mut parts = line.split_whitespace();
            let (Some(_), Some(kind), Some(address)) = (parts.next(), parts.next(), parts.next()) else { continue };
            if kind.starts_with("_adb-tls-connect") {
                let _ = run_limited(Command::new(&adb).args(["connect", address]), FIND_LIMIT, false);
            }
        }
    }
    Ok(first_phone(&adb).is_some())
}

pub fn screenshot() -> AppResult<PhoneShot> {
    let adb = adb().ok_or_else(|| AppError::new("no_phone", NO_PHONE))?;
    let serial = first_phone(&adb).ok_or_else(|| AppError::new("no_phone", NO_PHONE))?;
    let ran = run_limited(Command::new(&adb).args(["-s", &serial, "exec-out", "screencap", "-p"]), SHOT_LIMIT, false)
        .map_err(|e| AppError::new("phone_failed", format!("Couldn't take the phone picture: {e}")))?;
    match ran {
        Ran::Exited { code: Some(0), stdout } if stdout.starts_with(b"\x89PNG") => Ok(PhoneShot {
            mime: "image/png".into(),
            base64: base64::engine::general_purpose::STANDARD.encode(stdout),
        }),
        Ran::TimedOut => Err(AppError::new("phone_failed", "The phone took too long to send its picture")),
        _ => Err(AppError::new("phone_failed", "Couldn't take the phone picture (is the phone unlocked?)")),
    }
}

const RECORD_LIMIT_SECS: &str = "600";
const STOP_LIMIT: Duration = Duration::from_secs(10);
const PULL_LIMIT: Duration = Duration::from_secs(120);
const PROBE_LIMIT: Duration = Duration::from_secs(30);

// One recording at a time. Stopping takes the recording out while the stop
// and the copy run, leaving Stopping so no second start or stop slips in.
// A copy that fails while the file is still on the phone puts the recording
// back, stopped, so clicking the red dot again retries it.
struct Recording {
    child: Child,
    adb: PathBuf,
    serial: String,
    remote: String,
    file: String,
    // The phone-side process id of this recording's screenrecord.
    pid: Option<u32>,
    stopped: bool,
}

enum State {
    Idle,
    Recording(Recording),
    Stopping,
}

static STATE: Mutex<State> = Mutex::new(State::Idle);

fn state() -> std::sync::MutexGuard<'static, State> {
    STATE.lock().unwrap_or_else(|e| e.into_inner())
}

#[derive(Serialize)]
pub struct PhoneVideo {
    path: String,
    file: String,
    bytes: u64,
    width: u32,
    height: u32,
    duration: f64,
    frames: Vec<f64>,
}

pub fn recordings_dir() -> PathBuf {
    std::env::var_os("XDG_CACHE_HOME")
        .map(PathBuf::from)
        .filter(|p| p.is_absolute())
        .or_else(|| dirs::home_dir().map(|h| h.join(".cache")))
        .unwrap_or_else(std::env::temp_dir)
        .join("crush")
        .join("recordings")
}

/// True while a recording runs, is being saved, or waits for a retried copy
/// (the button stays the red dot until it is saved).
pub fn recording() -> bool {
    !matches!(*state(), State::Idle)
}

pub fn record_start() -> AppResult<()> {
    let mut held = state();
    match *held {
        State::Idle => {}
        State::Stopping => return Err(AppError::new("phone_recording", "The last recording is still being saved")),
        State::Recording(_) => return Err(AppError::new("phone_recording", "The phone is already recording")),
    }
    let adb = adb().ok_or_else(|| AppError::new("no_phone", NO_PHONE))?;
    let serial = first_phone(&adb).ok_or_else(|| AppError::new("no_phone", NO_PHONE))?;
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_millis()).unwrap_or_default();
    let file = format!("phone-recording-{stamp}.mp4");
    let remote = format!("/data/local/tmp/crush-{file}");
    // The phone's shell prints its own id, then becomes screenrecord (exec
    // keeps the id), so the stop signals this recording and nothing else.
    let script = format!("echo $$; exec screenrecord --time-limit {RECORD_LIMIT_SECS} --bit-rate 8M {remote}");
    let mut child = Command::new(&adb)
        .args(["-s", &serial, "shell", &script])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|e| AppError::new("phone_failed", format!("Couldn't start recording: {e}")))?;
    let pid = child.stdout.take().and_then(|out| {
        let (send, got) = std::sync::mpsc::channel();
        std::thread::spawn(move || {
            use std::io::BufRead;
            let mut reader = std::io::BufReader::new(out);
            let mut line = String::new();
            let _ = reader.read_line(&mut line);
            let _ = send.send(line.trim().parse::<u32>().ok());
            // Keep reading so the phone's output never blocks the recorder.
            let _ = std::io::copy(&mut reader, &mut std::io::sink());
        });
        got.recv_timeout(STOP_LIMIT).ok().flatten()
    });
    let Some(pid) = pid else {
        let _ = child.kill();
        let _ = child.wait();
        return Err(AppError::new("phone_failed", "Couldn't start recording on the phone"));
    };
    *held = State::Recording(Recording { child, adb, serial, remote, file, pid: Some(pid), stopped: false });
    Ok(())
}

pub fn record_stop() -> AppResult<PhoneVideo> {
    let mut rec = {
        let mut held = state();
        match std::mem::replace(&mut *held, State::Stopping) {
            State::Recording(rec) => rec,
            other => {
                let busy = matches!(other, State::Stopping);
                *held = other;
                return Err(if busy {
                    AppError::new("phone_recording", "The recording is already being saved")
                } else {
                    AppError::new("phone_failed", "The phone isn't recording")
                });
            }
        }
    };
    let result = finish(&mut rec);
    let mut held = state();
    match &result {
        Err(Kept::Retry(_)) => *held = State::Recording(rec),
        _ => *held = State::Idle,
    }
    result.map_err(|kept| match kept {
        Kept::Retry(e) | Kept::Gone(e) => e,
    })
}

enum Kept {
    // The file is still on the phone: the recording stays for another try.
    Retry(AppError),
    // Nothing left to retry.
    Gone(AppError),
}

fn finish(rec: &mut Recording) -> Result<PhoneVideo, Kept> {
    if !rec.stopped {
        if let (Some(pid), Ok(None)) = (rec.pid, rec.child.try_wait()) {
            let _ = run_limited(
                Command::new(&rec.adb).args(["-s", &rec.serial, "shell", "kill", "-2", &pid.to_string()]),
                STOP_LIMIT,
                false,
            );
        }
        let until = Instant::now() + STOP_LIMIT;
        while matches!(rec.child.try_wait(), Ok(None)) && Instant::now() < until {
            std::thread::sleep(Duration::from_millis(100));
        }
        if matches!(rec.child.try_wait(), Ok(None)) {
            let _ = rec.child.kill();
            let _ = rec.child.wait();
        }
        rec.stopped = true;
    }
    let dir = recordings_dir();
    std::fs::create_dir_all(&dir).map_err(|e| Kept::Retry(AppError::new("phone_failed", format!("Couldn't save the recording: {e}"))))?;
    let local = dir.join(&rec.file);
    if !local.is_file() {
        pull(rec, &local)?;
    }
    // Only once the copy is safe here is the phone's copy removed.
    let _ = run_limited(Command::new(&rec.adb).args(["-s", &rec.serial, "shell", "rm", "-f", &rec.remote]), STOP_LIMIT, false);
    probe(&local, &rec.file).map_err(Kept::Gone)
}

fn pull(rec: &Recording, local: &Path) -> Result<(), Kept> {
    // Into a side file first, so a half copy is never taken for the video.
    let part = local.with_extension("mp4.part");
    let pulled = run_limited(Command::new(&rec.adb).args(["-s", &rec.serial, "pull", &rec.remote]).arg(&part), PULL_LIMIT, false);
    if matches!(pulled, Ok(Ran::Exited { code: Some(0), .. })) && part.is_file() && std::fs::rename(&part, local).is_ok() {
        return Ok(());
    }
    let _ = std::fs::remove_file(&part);
    let check = run_limited(
        Command::new(&rec.adb).args(["-s", &rec.serial, "shell", &format!("test -s {} && echo yes || echo no", rec.remote)]),
        STOP_LIMIT,
        false,
    );
    match check {
        Ok(Ran::Exited { code: Some(0), stdout }) if stdout.starts_with(b"yes") => Err(Kept::Retry(AppError::new(
            "phone_failed",
            "Couldn't copy the recording from the phone. Click the red dot to try again",
        ))),
        Ok(Ran::Exited { code: Some(0), stdout }) if stdout.starts_with(b"no") => {
            Err(Kept::Gone(AppError::new("phone_failed", "The phone didn't save a recording")))
        }
        _ => Err(Kept::Gone(AppError::new(
            "phone_failed",
            format!("Lost the phone before the recording was copied. It is still on the phone at {}", rec.remote),
        ))),
    }
}

fn probe(local: &Path, file: &str) -> AppResult<PhoneVideo> {
    let bytes = std::fs::metadata(local).map(|m| m.len()).unwrap_or(0);
    let mut video = PhoneVideo {
        path: local.to_string_lossy().into_owned(),
        file: file.to_string(),
        bytes,
        width: 0,
        height: 0,
        duration: 0.0,
        frames: Vec::new(),
    };
    let Some(ffprobe) = tool("ffprobe") else { return Ok(video) };
    let size = run_limited(
        Command::new(&ffprobe)
            .args(["-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height:format=duration", "-of", "default=nw=1"])
            .arg(local),
        PROBE_LIMIT,
        false,
    );
    if let Ok(Ran::Exited { code: Some(0), stdout }) = size {
        for line in String::from_utf8_lossy(&stdout).lines() {
            match line.split_once('=') {
                Some(("width", v)) => video.width = v.trim().parse().unwrap_or(0),
                Some(("height", v)) => video.height = v.trim().parse().unwrap_or(0),
                Some(("duration", v)) => video.duration = v.trim().parse().unwrap_or(0.0),
                _ => {}
            }
        }
    }
    let times = run_limited(
        Command::new(&ffprobe)
            .args(["-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pts_time", "-of", "csv=p=0"])
            .arg(local),
        PROBE_LIMIT,
        false,
    );
    if let Ok(Ran::Exited { code: Some(0), stdout }) = times {
        let mut frames: Vec<f64> = String::from_utf8_lossy(&stdout)
            .lines()
            .filter_map(|l| l.trim().trim_end_matches(',').parse::<f64>().ok())
            .filter(|t| t.is_finite() && *t >= 0.0)
            .collect();
        frames.sort_by(|a, b| a.total_cmp(b));
        frames.dedup();
        video.frames = frames;
    }
    Ok(video)
}
