//! Saved projects in `projects.json` (desktop-design-5-6 2.5, 2.21), read and
//! written here as the terminal does in-process (`internal/projects`; no
//! engine route). The file sits beside the global data config
//! (`config.GlobalConfigData`, `internal/config/paths.go:65-85`) and holds
//! `{"projects":[{path, data_dir, last_accessed, saved}]}`, the
//! `crush projects --json` shape. Writes keep every field this app does not
//! know, go to a temporary file in the same folder with mode 0600, and rename
//! it over the old one; the terminal writes in place (9.2 lists the race).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use serde_json::{json, Map, Value};
use std::collections::HashSet;
use std::io::Write;
use std::path::{Path, PathBuf};

const FILE_NAME: &str = "projects.json";

#[derive(Serialize)]
pub struct Projects {
    /// `{path, data_dir, last_accessed, saved}` each, as `crush projects --json`.
    pub projects: Vec<Value>,
}

/// `projects_list`: every entry, in the file's order (latest first).
pub fn list() -> AppResult<Projects> {
    let doc = load(&file_path())?;
    Ok(Projects { projects: entries(&doc) })
}

/// `project_save`: marks the folder saved (MarkSaved, projects.go), adding
/// an entry at the top when it has none.
pub fn save(path: &str) -> AppResult<()> {
    let file = file_path();
    let mut doc = load(&file)?;
    mark_saved(&mut doc, path, &now());
    write(&file, &doc)
}

/// `project_remove`: the entry stops being saved (Unsave, projects.go); the
/// folder and its record stay, so it can still show in Recent.
pub fn remove(path: &str) -> AppResult<()> {
    let file = file_path();
    let mut doc = load(&file)?;
    if unsave(&mut doc, path) {
        write(&file, &doc)?;
    }
    Ok(())
}

/// `recent_folders`: up to `limit` folders from every entry, latest first,
/// without missing folders or repeats (2.21; `internal/cmd/session_host.go:50-61`).
pub fn recent_folders(limit: u32) -> AppResult<Vec<String>> {
    let doc = load(&file_path())?;
    Ok(recent(&doc, limit as usize, |p| Path::new(p).is_dir()))
}

/// `<global data dir>/projects.json`, the engine's rule with the login
/// environment the engine itself gets.
fn file_path() -> PathBuf {
    let env = crate::login_env::login_env();
    let var = |name: &str| {
        env.iter()
            .find(|(k, v)| k == name && !v.is_empty())
            .map(|(_, v)| v.clone())
            .or_else(|| std::env::var(name).ok().filter(|v| !v.is_empty()))
    };
    global_data_dir(var("CRUSH_GLOBAL_DATA"), var("XDG_DATA_HOME"), var("HOME").map(PathBuf::from).or_else(dirs::home_dir)).join(FILE_NAME)
}

fn global_data_dir(crush_data: Option<String>, xdg: Option<String>, home: Option<PathBuf>) -> PathBuf {
    if let Some(dir) = crush_data {
        return PathBuf::from(dir);
    }
    if let Some(dir) = xdg {
        return PathBuf::from(dir).join("crush");
    }
    home.unwrap_or_default().join(".local").join("share").join("crush")
}

fn load(file: &Path) -> AppResult<Value> {
    match std::fs::read(file) {
        Ok(bytes) => {
            let doc: Value = serde_json::from_slice(&bytes)
                .map_err(|e| AppError::new("projects_unreadable", format!("{} could not be read: {e}", file.display())))?;
            if doc.is_object() {
                Ok(doc)
            } else {
                Err(AppError::new("projects_unreadable", format!("{} is not a projects file", file.display())))
            }
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(json!({ "projects": [] })),
        Err(e) => Err(AppError::new("projects_unreadable", format!("{} could not be read: {e}", file.display()))),
    }
}

fn entries(doc: &Value) -> Vec<Value> {
    doc.get("projects").and_then(Value::as_array).cloned().unwrap_or_default()
}

fn list_mut(doc: &mut Value) -> &mut Vec<Value> {
    let root = doc.as_object_mut().expect("a projects file is an object");
    if !root.get("projects").is_some_and(Value::is_array) {
        root.insert("projects".into(), Value::Array(Vec::new()));
    }
    root.get_mut("projects").and_then(Value::as_array_mut).expect("projects is an array")
}

fn path_of(entry: &Value) -> &str {
    entry.get("path").and_then(Value::as_str).unwrap_or("")
}

fn mark_saved(doc: &mut Value, path: &str, now: &str) {
    let list = list_mut(doc);
    if let Some(entry) = list.iter_mut().find(|e| path_of(e) == path) {
        if let Some(fields) = entry.as_object_mut() {
            fields.insert("saved".into(), Value::Bool(true));
        }
        return;
    }
    let mut fields = Map::new();
    fields.insert("path".into(), Value::String(path.to_string()));
    fields.insert("data_dir".into(), Value::String(String::new()));
    fields.insert("last_accessed".into(), Value::String(now.to_string()));
    fields.insert("saved".into(), Value::Bool(true));
    list.insert(0, Value::Object(fields));
}

fn unsave(doc: &mut Value, path: &str) -> bool {
    let list = list_mut(doc);
    match list.iter_mut().find(|e| path_of(e) == path).and_then(Value::as_object_mut) {
        // Go writes saved with omitempty, so false leaves the field out.
        Some(fields) => fields.remove("saved").is_some(),
        None => false,
    }
}

fn recent(doc: &Value, limit: usize, exists: impl Fn(&str) -> bool) -> Vec<String> {
    let mut seen = HashSet::new();
    let mut out = Vec::new();
    for entry in entries(doc) {
        if out.len() >= limit {
            break;
        }
        let path = path_of(&entry);
        if path.is_empty() || !seen.insert(path.to_string()) || !exists(path) {
            continue;
        }
        out.push(path.to_string());
    }
    out
}

fn write(file: &Path, doc: &Value) -> AppResult<()> {
    let fail = |e: std::io::Error| AppError::new("projects_unwritable", format!("{} could not be saved: {e}", file.display()));
    let dir = file.parent().unwrap_or(Path::new("."));
    std::fs::create_dir_all(dir).map_err(fail)?;
    let data = serde_json::to_vec_pretty(doc).map_err(|e| AppError::new("projects_unwritable", e.to_string()))?;
    let tmp = dir.join(format!(".{FILE_NAME}.{}.tmp", std::process::id()));
    {
        use std::os::unix::fs::OpenOptionsExt;
        let mut out = std::fs::OpenOptions::new().write(true).create(true).truncate(true).mode(0o600).open(&tmp).map_err(fail)?;
        out.write_all(&data).map_err(fail)?;
        out.sync_all().map_err(fail)?;
    }
    std::fs::rename(&tmp, file).map_err(|e| {
        let _ = std::fs::remove_file(&tmp);
        fail(e)
    })
}

/// RFC 3339 in UTC, as Go's time.Time marshals.
fn now() -> String {
    let secs = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0);
    let (days, rest) = (secs / 86_400, secs % 86_400);
    let (y, m, d) = civil_from_days(days as i64);
    format!("{y:04}-{m:02}-{d:02}T{:02}:{:02}:{:02}Z", rest / 3600, rest % 3600 / 60, rest % 60)
}

/// Howard Hinnant's days-to-date.
fn civil_from_days(z: i64) -> (i64, u32, u32) {
    let z = z + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    (if m <= 2 { yoe + era * 400 + 1 } else { yoe + era * 400 }, m, d)
}
