//! A folder's entries for the pickers (desktop-design-5-6 2.10, 2.21, 6.4).
//! `~` is expanded and the answer's `path` is absolute; `is_dir` follows
//! links; an entry the app cannot read is marked, not dropped. The pickers
//! do their own sorting and hiding. VIEW owns this file (7.3).

use crate::error::{codes, AppError, AppResult};
use serde::Serialize;
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Serialize, Debug)]
pub struct DirEntry {
    pub name: String,
    pub is_dir: bool,
    pub is_symlink: bool,
    pub readable: bool,
}

#[derive(Serialize, Debug)]
pub struct DirList {
    pub path: String,
    pub entries: Vec<DirEntry>,
}

/// `dir_list`: `~` expanded; errors `bad_path` and `not_a_folder`.
pub fn list(path: &str) -> AppResult<DirList> {
    list_in(path, home())
}

fn home() -> Option<PathBuf> {
    std::env::var_os("HOME").map(PathBuf::from).filter(|p| p.is_absolute()).or_else(dirs::home_dir)
}

fn expand(path: &str, home: Option<PathBuf>) -> AppResult<PathBuf> {
    let bad = || AppError::new("bad_path", format!("\"{path}\" is not an absolute folder path"));
    let full = if path == "~" {
        home.ok_or_else(bad)?
    } else if let Some(rest) = path.strip_prefix("~/") {
        home.ok_or_else(bad)?.join(rest)
    } else {
        PathBuf::from(path)
    };
    if path.is_empty() || !full.is_absolute() {
        return Err(bad());
    }
    Ok(full)
}

fn list_in(path: &str, home: Option<PathBuf>) -> AppResult<DirList> {
    let full = expand(path, home)?;
    let meta = fs::metadata(&full).map_err(|e| AppError::new("bad_path", format!("{}: {e}", full.display())))?;
    if !meta.is_dir() {
        return Err(AppError::new(codes::NOT_A_FOLDER, format!("{} is not a folder", full.display())));
    }
    let reader = fs::read_dir(&full).map_err(|e| AppError::new("bad_path", format!("{}: {e}", full.display())))?;
    let mut entries = Vec::new();
    for item in reader.flatten() {
        let name = item.file_name().to_string_lossy().into_owned();
        let own = item.path();
        let is_symlink = item.file_type().map(|t| t.is_symlink()).unwrap_or(false);
        let target = fs::metadata(&own); // follows links
        let is_dir = target.as_ref().map(|m| m.is_dir()).unwrap_or(false);
        let readable = match &target {
            Ok(_) if is_dir => fs::read_dir(&own).is_ok(),
            // Only regular files are opened: opening a pipe with no writer
            // (or a device) can block forever.
            Ok(m) if m.is_file() => fs::File::open(&own).is_ok(),
            Ok(_) => false,
            Err(_) => false,
        };
        entries.push(DirEntry { name, is_dir, is_symlink, readable });
    }
    Ok(DirList { path: shown(&full), entries })
}

fn shown(path: &Path) -> String {
    path.to_string_lossy().into_owned()
}
