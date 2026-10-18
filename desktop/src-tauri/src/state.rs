//! The app's managed state (desktop-design 1.2.9): the per-window connection
//! registry, the settings handle and how the app was launched.

use crate::{connection, settings};
use std::collections::HashMap;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

pub struct AppState {
    /// Window label → that window's engine connection (P3.1).
    #[allow(dead_code)] // read and filled by connection.rs (P3.1)
    pub windows: Mutex<HashMap<String, Arc<connection::WindowConn>>>,
    pub settings: Mutex<settings::Settings>,
    pub launch: Launch,
}

/// The command line (main.rs parses it; the terminal's flags, internal/cmd/root.go:37-48).
#[derive(Debug, Default, Clone, PartialEq)]
pub struct Launch {
    /// Test runs only: where the page writes its on-screen report.
    pub probe: Option<PathBuf>,
    /// Test runs only: plays a recorded engine instead of joining one (6.3).
    pub fixture: Option<PathBuf>,
    /// -c/--cwd, resolved against the process's folder; the process's folder by default.
    pub cwd: PathBuf,
    /// -s/--session.
    pub session: Option<String>,
    /// -C/--continue.
    pub continue_last: bool,
    /// -D/--data-dir.
    pub data_dir: Option<String>,
    /// -d/--debug.
    pub debug: bool,
    /// -H/--host.
    pub host: Option<String>,
    /// -y/--yolo[=false]; None when not given (the settings file decides).
    pub yolo: Option<bool>,
    /// --channels server:name, repeatable.
    pub channels: Vec<String>,
    /// CRUSH_UI_DEBUG == "true" (PLAN.md:144).
    pub ui_debug: bool,
}

impl Launch {
    /// A file beside the probe file, named "<probe file>.<suffix>" (request.json,
    /// opened.jsonl, api.jsonl). None outside probe mode.
    pub fn probe_side_file(&self, suffix: &str) -> Option<PathBuf> {
        self.probe.as_ref().map(|probe| {
            let mut name = probe.clone().into_os_string();
            name.push(".");
            name.push(suffix);
            PathBuf::from(name)
        })
    }
}

/// Appends one JSON value as one line. Test-run side files only; a failure is
/// logged and otherwise ignored.
pub fn append_json_line(path: &Path, value: &serde_json::Value) {
    let line = format!("{value}\n");
    let result = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .and_then(|mut f| f.write_all(line.as_bytes()));
    if let Err(e) = result {
        eprintln!("crush-desktop: could not append to {}: {e}", path.display());
    }
}

/// The number of lines in a side file; 0 while it does not exist.
pub fn count_lines(path: &Path) -> usize {
    std::fs::read(path)
        .map(|bytes| bytes.split(|&b| b == b'\n').filter(|line| !line.is_empty()).count())
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn probe_side_files_sit_beside_the_probe() {
        let launch = Launch {
            probe: Some(PathBuf::from("/tmp/probe.json")),
            ..Default::default()
        };
        assert_eq!(
            launch.probe_side_file("request.json"),
            Some(PathBuf::from("/tmp/probe.json.request.json"))
        );
        assert_eq!(Launch::default().probe_side_file("api.jsonl"), None);
    }

    #[test]
    fn side_file_lines_are_counted() {
        let dir = std::env::temp_dir().join(format!("crush-desktop-count-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("probe.json.opened.jsonl");
        let _ = std::fs::remove_file(&path);
        assert_eq!(count_lines(&path), 0);
        append_json_line(&path, &serde_json::json!({"url": "https://example.com/"}));
        append_json_line(&path, &serde_json::json!({"refused_navigation": "http:"}));
        assert_eq!(count_lines(&path), 2);
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
