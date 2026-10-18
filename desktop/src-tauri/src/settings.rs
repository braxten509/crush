//! The desktop settings file and its watcher (desktop-design 1.2.8). Scaffold
//! S3 placeholder: piece P3.2 writes the bodies; the public signatures and
//! the Settings shape are pinned.
#![allow(dead_code, unused_variables)]

use crate::error::{AppError, AppResult};

/// `dirs::config_dir()/crush-desktop/settings.json`. Phases 5 and 6 add keys;
/// unknown keys are kept on write (`other`).
#[derive(serde::Serialize, serde::Deserialize, Clone)]
#[serde(default)]
pub struct Settings {
    pub version: u32,
    pub crush_path: Option<String>,
    pub yolo: bool,
    pub closed_chats: std::collections::BTreeMap<String, Vec<String>>,
    #[serde(flatten)]
    pub other: serde_json::Map<String, serde_json::Value>,
}

impl Default for Settings {
    fn default() -> Self {
        Settings {
            version: 1,
            crush_path: None,
            yolo: true,
            closed_chats: Default::default(),
            other: Default::default(),
        }
    }
}

/// Defaults when missing or broken.
pub fn load() -> Settings {
    Settings::default()
}

/// Known keys only: crush_path, yolo.
pub fn set_key(app: &tauri::AppHandle, key: &str, value: serde_json::Value) -> AppResult<Settings> {
    Err(AppError::not_built("settings::set_key"))
}

pub fn set_closed(
    app: &tauri::AppHandle,
    project: &str,
    session_id: &str,
    closed: bool,
) -> AppResult<Settings> {
    Err(AppError::not_built("settings::set_closed"))
}

/// Started once by main.rs setup (S3 wires the call, P3.2 writes the body). One thread for the
/// app's life: every 500 ms it reads the settings file's inode, modification time and length
/// (std::os::unix::fs::MetadataExt). When any of the three differs from the last seen value, it
/// re-reads and parses the file; when the parsed Settings differ from AppState.settings, it
/// stores them and emits `settings-changed` to every window of this process.
pub fn watch(app: tauri::AppHandle) {}
