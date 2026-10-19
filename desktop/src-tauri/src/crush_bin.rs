//! Finding `crush` (desktop-design 1.2.3; PLAN.md:51): the `crush` on PATH by
//! default, the same one the terminal app uses; `CRUSH_DESKTOP_CRUSH` (tests)
//! or the `crush_path` setting can point elsewhere.

use crate::error::{AppError, AppResult};
use std::ffi::OsStr;
use std::path::{Path, PathBuf};

pub struct CrushBin {
    pub path: std::path::PathBuf,
    pub source: BinSource,
}

pub enum BinSource {
    Env,
    Setting,
    Path,
}

/// Order: CRUSH_DESKTOP_CRUSH (tests), then settings.crush_path, then `crush` found on PATH
/// (the PATH of the login-shell environment, section 1.2.7, else the app's own PATH).
pub fn resolve(setting: Option<&str>, login_path: Option<&str>) -> AppResult<CrushBin> {
    let env = std::env::var_os("CRUSH_DESKTOP_CRUSH");
    let own_path = std::env::var_os("PATH");
    let path = login_path.map(OsStr::new).or(own_path.as_deref());
    resolve_from(env.as_deref(), setting, path)
}

/// `resolve` with its inputs given, so tests need no process environment.
/// A variable or setting that is set is the answer, found or not: falling back
/// to PATH would hide a wrong path behind another crush.
fn resolve_from(env: Option<&OsStr>, setting: Option<&str>, path: Option<&OsStr>) -> AppResult<CrushBin> {
    if let Some(given) = env.filter(|p| !p.is_empty()) {
        return named(PathBuf::from(given), BinSource::Env);
    }
    if let Some(given) = setting.map(str::trim).filter(|p| !p.is_empty()) {
        return named(PathBuf::from(given), BinSource::Setting);
    }
    // Only absolute PATH entries: an empty or relative one means a folder
    // relative to wherever the app happened to start.
    let found = path
        .into_iter()
        .flat_map(std::env::split_paths)
        .filter(|dir| dir.is_absolute())
        .map(|dir| dir.join("crush"))
        .find(|candidate| is_executable(candidate));
    match found {
        Some(path) => Ok(CrushBin {
            path,
            source: BinSource::Path,
        }),
        None => Err(not_found(&BinSource::Path, Path::new("crush"))),
    }
}

fn named(path: PathBuf, source: BinSource) -> AppResult<CrushBin> {
    if is_executable(&path) {
        Ok(CrushBin { path, source })
    } else {
        Err(not_found(&source, &path))
    }
}

/// The `crush_not_found` error (1.2.3): "on your PATH" when PATH was searched,
/// else the path the variable or the setting named.
pub fn not_found(source: &BinSource, path: &Path) -> AppError {
    let message = match source {
        BinSource::Path => "Crush Desktop could not find crush on your PATH.".to_string(),
        BinSource::Env | BinSource::Setting => {
            format!("Crush Desktop could not find crush at {}.", path.display())
        }
    };
    AppError::new("crush_not_found", message)
}

/// A file (after symlinks) that someone may run.
fn is_executable(path: &Path) -> bool {
    use std::os::unix::fs::PermissionsExt;
    std::fs::metadata(path).is_ok_and(|m| m.is_file() && m.permissions().mode() & 0o111 != 0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    /// A fresh folder for one test, removed by the test at its end.
    fn scratch(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("crush-desktop-p31-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    fn file(path: &Path, mode: u32) {
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(path, "#!/bin/sh\n").unwrap();
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode)).unwrap();
    }

    fn source(bin: &CrushBin) -> &'static str {
        match bin.source {
            BinSource::Env => "env",
            BinSource::Setting => "setting",
            BinSource::Path => "path",
        }
    }

    #[test]
    fn resolve_order_env_then_setting_then_path() {
        let dir = scratch("resolve");
        let (env_bin, set_bin) = (dir.join("env/crush"), dir.join("set/crush"));
        let (first, second) = (dir.join("a"), dir.join("b"));
        file(&env_bin, 0o755);
        file(&set_bin, 0o755);
        file(&first.join("crush"), 0o644); // not executable: skipped
        file(&second.join("crush"), 0o755);
        std::fs::create_dir_all(dir.join("c/crush")).unwrap(); // a folder named crush: skipped
        let path = std::env::join_paths(["relative", dir.join("c").to_str().unwrap(), first.to_str().unwrap(), second.to_str().unwrap()]).unwrap();
        let set = set_bin.to_str();

        let bin = resolve_from(Some(env_bin.as_os_str()), set, Some(&path)).unwrap();
        assert_eq!((source(&bin), bin.path.clone()), ("env", env_bin.clone()), "the variable comes first");
        let bin = resolve_from(Some(OsStr::new("")), set, Some(&path)).unwrap();
        assert_eq!((source(&bin), bin.path.clone()), ("setting", set_bin.clone()), "then the setting");
        let bin = resolve_from(None, Some("  "), Some(&path)).unwrap();
        assert_eq!((source(&bin), bin.path.clone()), ("path", second.join("crush")), "then the first runnable crush on PATH");

        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn resolve_names_what_it_could_not_find() {
        let dir = scratch("notfound");
        let missing = dir.join("nowhere/crush");
        let other = dir.join("other");
        file(&other.join("crush"), 0o755);
        let path = other.as_os_str();

        let e = resolve_from(Some(missing.as_os_str()), None, Some(path)).err().unwrap();
        assert_eq!(e.code, "crush_not_found");
        assert_eq!(e.message, format!("Crush Desktop could not find crush at {}.", missing.display()));
        let e = resolve_from(None, missing.to_str(), Some(path)).err().unwrap();
        assert_eq!(e.message, format!("Crush Desktop could not find crush at {}.", missing.display()));
        let empty_dir = dir.join("empty");
        for empty in [None, Some(OsStr::new("")), Some(empty_dir.as_os_str())] {
            let e = resolve_from(None, None, empty).err().unwrap();
            assert_eq!((e.code.as_str(), e.message.as_str()), ("crush_not_found", "Crush Desktop could not find crush on your PATH."));
        }
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
