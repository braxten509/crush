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

struct Recording {
    child: Child,
    adb: PathBuf,
    serial: String,
    remote: String,
    file: String,
}

static RECORDING: Mutex<Option<Recording>> = Mutex::new(None);

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

pub fn recording() -> bool {
    let mut held = RECORDING.lock().unwrap_or_else(|e| e.into_inner());
    if let Some(rec) = held.as_mut() {
        if matches!(rec.child.try_wait(), Ok(None)) {
            return true;
        }
        let local = recordings_dir().join(&rec.file);
        return local.exists() || pull(rec, &local).is_ok();
    }
    false
}

pub fn record_start() -> AppResult<()> {
    let mut held = RECORDING.lock().unwrap_or_else(|e| e.into_inner());
    if held.is_some() {
        return Err(AppError::new("phone_recording", "The phone is already recording"));
    }
    let adb = adb().ok_or_else(|| AppError::new("no_phone", NO_PHONE))?;
    let serial = first_phone(&adb).ok_or_else(|| AppError::new("no_phone", NO_PHONE))?;
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_millis()).unwrap_or_default();
    let file = format!("phone-recording-{stamp}.mp4");
    let remote = format!("/data/local/tmp/crush-{file}");
    let child = Command::new(&adb)
        .args(["-s", &serial, "shell", "screenrecord", "--time-limit", RECORD_LIMIT_SECS, "--bit-rate", "8M", &remote])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|e| AppError::new("phone_failed", format!("Couldn't start recording: {e}")))?;
    *held = Some(Recording { child, adb, serial, remote, file });
    Ok(())
}

pub fn record_stop() -> AppResult<PhoneVideo> {
    let mut rec = RECORDING
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .take()
        .ok_or_else(|| AppError::new("phone_failed", "The phone isn't recording"))?;
    let _ = run_limited(
        Command::new(&rec.adb).args(["-s", &rec.serial, "shell", "kill -2 $(pidof screenrecord)"]),
        STOP_LIMIT,
        false,
    );
    let until = Instant::now() + STOP_LIMIT;
    while matches!(rec.child.try_wait(), Ok(None)) && Instant::now() < until {
        std::thread::sleep(Duration::from_millis(100));
    }
    if matches!(rec.child.try_wait(), Ok(None)) {
        let _ = rec.child.kill();
        let _ = rec.child.wait();
    }
    let dir = recordings_dir();
    std::fs::create_dir_all(&dir).map_err(|e| AppError::new("phone_failed", format!("Couldn't save the recording: {e}")))?;
    let local = dir.join(&rec.file);
    if !local.exists() {
        pull(&mut rec, &local)?;
    }
    probe(&local, &rec.file)
}

fn pull(rec: &mut Recording, local: &Path) -> AppResult<()> {
    let _ = rec.child.try_wait();
    let pulled = run_limited(
        Command::new(&rec.adb).args(["-s", &rec.serial, "pull", &rec.remote]).arg(local),
        PULL_LIMIT,
        false,
    );
    let _ = run_limited(Command::new(&rec.adb).args(["-s", &rec.serial, "shell", "rm", "-f", &rec.remote]), STOP_LIMIT, false);
    match pulled {
        Ok(Ran::Exited { code: Some(0), .. }) if local.is_file() => Ok(()),
        _ => Err(AppError::new("phone_failed", "Couldn't copy the recording from the phone")),
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
