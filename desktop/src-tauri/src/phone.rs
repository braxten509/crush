//! The composer's phone button (owner, 2026-11-01): whether an Android phone
//! is connected over adb (USB or wireless), and a picture of its screen
//! (`adb exec-out screencap -p`). With several, the first real phone listed.
//! adb is looked up in the login shell's PATH (an app started from the menu
//! may not have it), then in the usual SDK folders.
use crate::engine_cli::{run_limited, Ran};
use crate::error::{AppError, AppResult};
use base64::Engine as _;
use serde::Serialize;
use std::path::PathBuf;
use std::process::Command;
use std::time::Duration;

const NO_PHONE: &str = "No phone connected";
const LIST_LIMIT: Duration = Duration::from_secs(4);
const SHOT_LIMIT: Duration = Duration::from_secs(15);

#[derive(Serialize)]
pub struct PhoneShot {
    mime: String,
    base64: String,
}

fn adb() -> Option<PathBuf> {
    let path = crate::login_env::login_env()
        .into_iter()
        .find(|(k, _)| k == "PATH")
        .map(|(_, v)| v)
        .or_else(|| std::env::var("PATH").ok())
        .unwrap_or_default();
    let home = std::env::var("HOME").unwrap_or_default();
    let mut places: Vec<PathBuf> = std::env::split_paths(&path).map(|dir| dir.join("adb")).collect();
    for var in ["ANDROID_HOME", "ANDROID_SDK_ROOT"] {
        if let Ok(dir) = std::env::var(var) {
            places.push(PathBuf::from(dir).join("platform-tools/adb"));
        }
    }
    places.push(PathBuf::from(&home).join("Android/Sdk/platform-tools/adb"));
    places.push(PathBuf::from("/opt/android-sdk/platform-tools/adb"));
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
