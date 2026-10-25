//! The open chats the desktop shares with the terminal's session list
//! (owner, 2026-10-20: "the two should be in sync"). The same file and the
//! same rules as internal/openchats (Go): $XDG_STATE_HOME/crush/open-chats.json
//! lists the chats and the apps that have it open; every change takes the
//! lock file beside it, reads, changes and writes through a temporary file.
//! The chats stay until they are closed in either app; quitting every app
//! keeps them (owner, 2026-10-21). This helper signs in when it
//! starts and out when Electron goes.

use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

#[derive(Serialize, Deserialize, Clone, Debug, Default, PartialEq)]
pub struct Chat {
    pub id: String,
    pub dir: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub title: String,
    #[serde(default)]
    pub opened: i64,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
struct App {
    pid: i32,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    start: String,
    kind: String,
    /// The folders whose chats this desktop hears and rings for; a terminal
    /// there stays quiet (watch.rs; internal/openchats DesktopHears).
    #[serde(default, deserialize_with = "null_as_empty", skip_serializing_if = "Vec::is_empty")]
    dirs: Vec<String>,
}

#[derive(Serialize, Deserialize, Default)]
struct File {
    #[serde(default)]
    version: u32,
    #[serde(default, deserialize_with = "null_as_empty")]
    apps: Vec<App>,
    #[serde(default, deserialize_with = "null_as_empty")]
    chats: Vec<Chat>,
}

/// Go writes an empty list as null.
fn null_as_empty<'de, D: serde::Deserializer<'de>, T: Deserialize<'de>>(d: D) -> Result<Vec<T>, D::Error> {
    Ok(Option::<Vec<T>>::deserialize(d)?.unwrap_or_default())
}

/// $XDG_STATE_HOME/crush/open-chats.json, or ~/.local/state/crush/open-chats.json.
pub fn path() -> Option<PathBuf> {
    let state = std::env::var_os("XDG_STATE_HOME")
        .filter(|v| !v.is_empty())
        .map(PathBuf::from)
        .or_else(|| dirs::home_dir().map(|h| h.join(".local").join("state")))?;
    Some(state.join("crush").join("open-chats.json"))
}

/// The file, or an empty one when there is none yet; Err when it is there
/// but cannot be read, so change never writes over it (owner, 2026-10-24).
fn read(path: &Path) -> Result<File, String> {
    let bytes = match std::fs::read(path) {
        Ok(bytes) => bytes,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(File::default()),
        Err(e) => return Err(format!("could not read {}: {e}", path.display())),
    };
    if bytes.iter().all(u8::is_ascii_whitespace) {
        return Ok(File::default());
    }
    serde_json::from_slice(&bytes).map_err(|e| format!("could not read {}: {e}", path.display()))
}

/// The open chats, oldest first, whatever apps run (owner, 2026-10-21:
/// the chats stay until closed manually, even when every app quits).
pub fn chats() -> Vec<Chat> {
    let Some(path) = path() else { return Vec::new() };
    read(&path).unwrap_or_default().chats
}

/// The marker of an update's restart (appupdate.rs): while it is fresh, the
/// app leaving and the new one joining keep the list (owner, 2026-10-20).
fn hold_path() -> Option<PathBuf> {
    path().map(|p| p.with_extension("restart"))
}

/// The next leave and join keep the open chats (an update's restart). The
/// chats now stay through every leave and join anyway (owner, 2026-10-21),
/// so this only records that a restart was held.
pub fn hold_for_restart() {
    if let Some(p) = hold_path() {
        let _ = std::fs::write(p, b"restart\n");
    }
}

/// Signs this process in as the desktop; a fresh start keeps the chats
/// from the last run (owner, 2026-10-21).
pub fn join() {
    let me = App { pid: std::process::id() as i32, start: process_start(std::process::id() as i32), kind: "desktop".into(), dirs: Vec::new() };
    let _ = change(|file| {
        file.apps = live(std::mem::take(&mut file.apps));
        file.apps.retain(|a| a.pid != me.pid);
        file.apps.push(me.clone());
    });
}

/// Signs this process out; the chats stay for the next app (owner,
/// 2026-10-21).
pub fn leave() {
    let pid = std::process::id() as i32;
    let _ = change(|file| {
        file.apps = live(std::mem::take(&mut file.apps));
        file.apps.retain(|a| a.pid != pid);
    });
}

/// Lists chat at the end, or updates its folder and title when listed.
pub fn add(chat: Chat) -> Result<(), String> {
    if chat.id.is_empty() {
        return Ok(());
    }
    change(|file| {
        if let Some(listed) = file.chats.iter_mut().find(|c| c.id == chat.id) {
            if !chat.dir.is_empty() {
                listed.dir = chat.dir.clone();
            }
            if !chat.title.is_empty() {
                listed.title = chat.title.clone();
            }
            return;
        }
        let mut chat = chat.clone();
        if chat.opened == 0 {
            chat.opened = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0);
        }
        file.chats.push(chat);
    })
}

/// Records the folders this desktop hears (watch.rs).
pub fn set_dirs(dirs: &[String]) -> Result<(), String> {
    let pid = std::process::id() as i32;
    change(|file| {
        if let Some(me) = file.apps.iter_mut().find(|a| a.pid == pid) {
            me.dirs = dirs.to_vec();
        }
    })
}

pub fn remove(id: &str) -> Result<(), String> {
    change(|file| file.chats.retain(|c| c.id != id))
}

fn change(apply: impl FnOnce(&mut File)) -> Result<(), String> {
    let path = path().ok_or("no home folder")?;
    let dir = path.parent().ok_or("no folder for the open chats")?;
    std::fs::create_dir_all(dir).map_err(|e| e.to_string())?;
    let lock = std::fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(path.with_extension("json.lock"))
        .map_err(|e| e.to_string())?;
    use std::os::fd::AsRawFd;
    // SAFETY: flock on a descriptor this function owns; it is unlocked when `lock` closes.
    if unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX) } != 0 {
        return Err(std::io::Error::last_os_error().to_string());
    }
    let mut file = read(&path).inspect_err(|e| eprintln!("crush-desktop: open chats left as they are: {e}"))?;
    apply(&mut file);
    file.version = 1;
    let text = serde_json::to_vec_pretty(&file).map_err(|e| e.to_string())?;
    let tmp = dir.join(format!(".open-chats-{}", std::process::id()));
    std::fs::write(&tmp, text).map_err(|e| e.to_string())?;
    std::fs::rename(&tmp, &path).map_err(|e| e.to_string())
}

fn live(apps: Vec<App>) -> Vec<App> {
    apps.into_iter().filter(|a| a.pid > 0 && alive(a.pid, &a.start)).collect()
}

/// Field 22 of /proc/PID/stat, the start time after boot, or "".
fn process_start(pid: i32) -> String {
    let Ok(stat) = std::fs::read_to_string(format!("/proc/{pid}/stat")) else { return String::new() };
    let Some(end) = stat.rfind(')') else { return String::new() };
    stat[end + 1..].split_whitespace().nth(19).unwrap_or_default().to_string()
}

fn alive(pid: i32, start: &str) -> bool {
    // SAFETY: signal 0 only checks that the process exists.
    let found = unsafe { libc::kill(pid, 0) } == 0 || std::io::Error::last_os_error().raw_os_error() == Some(libc::EPERM);
    if !found {
        return false;
    }
    if start.is_empty() {
        return true;
    }
    let now = process_start(pid);
    now.is_empty() || now == start
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_chats_stay_through_apps_leaving_and_joining() {
        let dir = std::env::temp_dir().join(format!("openchats-test-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::env::set_var("XDG_STATE_HOME", &dir);
        join();
        add(Chat { id: "a".into(), dir: "/p".into(), ..Default::default() }).unwrap();
        add(Chat { id: "a".into(), dir: String::new(), title: "Named".into(), opened: 0 }).unwrap();
        assert_eq!(chats().iter().map(|c| (c.id.as_str(), c.dir.as_str(), c.title.as_str())).collect::<Vec<_>>(), vec![("a", "/p", "Named")]);
        // Another app (a running child process) has it open too.
        let mut other = std::process::Command::new("sleep").arg("30").spawn().unwrap();
        let pid = other.id() as i32;
        change(|f| f.apps.push(App { pid, start: process_start(pid), kind: "terminal".into(), dirs: Vec::new() })).unwrap();
        leave();
        assert_eq!(chats().len(), 1, "the terminal still runs: the chats stay");
        let _ = other.kill();
        let _ = other.wait();
        assert_eq!(chats().len(), 1, "no app running keeps the chats (owner, 2026-10-21)");
        join();
        assert_eq!(chats().len(), 1, "a new app joins the kept chats");
        remove("a").unwrap();
        assert!(chats().is_empty(), "only a manual close takes a chat off");

        // Go writes an empty list as null: it reads as empty, and the chats stay.
        let file = path().unwrap();
        std::fs::write(&file, r#"{"version":1,"apps":null,"chats":[{"id":"b","dir":"/q","opened":1}]}"#).unwrap();
        assert_eq!(chats().len(), 1, "a null list reads as empty");
        join();
        assert_eq!(chats().len(), 1, "joining keeps the chats");
        // A file it cannot read is never written over (owner, 2026-10-24).
        std::fs::write(&file, b"{not json").unwrap();
        assert!(add(Chat { id: "c".into(), dir: "/r".into(), ..Default::default() }).is_err());
        assert_eq!(std::fs::read(&file).unwrap(), b"{not json");
        let _ = std::fs::remove_dir_all(&dir);
    }
}
