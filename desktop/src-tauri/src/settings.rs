//! The desktop settings file and its watcher (desktop-design 1.2.8).
//!
//! The file is `dirs::config_dir()/crush-desktop/settings.json`. Every change
//! runs under one in-process mutex and an exclusive lock on `settings.lock`
//! beside the file, reads the file again, applies its one change and writes it
//! atomically (a temp file in the same folder, then rename), so two app
//! processes never lose each other's change. Each change is published to this
//! process's windows as `settings-changed` (1.4.1); other app processes learn
//! of it through their watcher, which looks at the file's inode, modification
//! time and length every 500 ms.
//!
//! The inner `SettingsFile` takes its path, so the tests never touch the
//! owner's real settings file and need no AppHandle.

use crate::error::{AppError, AppResult};
use crate::state::AppState;
use serde_json::Value;
use std::fs::{self, File, OpenOptions};
use std::io::{self, Read, Write};
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Mutex, OnceLock};
use std::time::Duration;
use tauri::{Emitter, Manager};

/// `dirs::config_dir()/crush-desktop/settings.json`. Phases 5 and 6 add keys;
/// unknown keys are kept on write (`other`).
#[derive(serde::Serialize, serde::Deserialize, Clone, Debug, PartialEq)]
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

/// The window event every change sends to this process's windows (1.4.1).
const EVENT: &str = "settings-changed";

/// How often the watcher looks at the file (B16's bar is 1 s).
const POLL: Duration = Duration::from_millis(500);

/// Defaults when missing or broken.
pub fn load() -> Settings {
    global().map(SettingsFile::load).unwrap_or_default()
}

/// Known keys only: crush_path, yolo.
pub fn set_key(app: &tauri::AppHandle, key: &str, value: serde_json::Value) -> AppResult<Settings> {
    let change = KeyChange::parse(key, value)?;
    let file = global().ok_or_else(no_folder)?;
    file.change(|s| change.apply(s), |s| publish(app, s))
}

/// Phase 4's closed chats, kept per engine data folder (`project` is the
/// engine's `data_dir`, the project's one stable name).
pub fn set_closed(
    app: &tauri::AppHandle,
    project: &str,
    session_id: &str,
    closed: bool,
) -> AppResult<Settings> {
    check_closed_args(project, session_id)?;
    let file = global().ok_or_else(no_folder)?;
    file.change(|s| apply_closed(s, project, session_id, closed), |s| publish(app, s))
}

/// Started once by main.rs setup (S3 wires the call, P3.2 writes the body). One thread for the
/// app's life: every 500 ms it reads the settings file's inode, modification time and length
/// (std::os::unix::fs::MetadataExt). When any of the three differs from the last seen value, it
/// re-reads and parses the file; when the parsed Settings differ from AppState.settings, it
/// stores them and emits `settings-changed` to every window of this process.
pub fn watch(app: tauri::AppHandle) {
    let Some(file) = global() else {
        eprintln!("crush-desktop: no settings folder, so changes from other app windows are not followed");
        return;
    };
    // One watcher for the app's life, however often this is called.
    static RUNNING: AtomicBool = AtomicBool::new(false);
    static NEVER: AtomicBool = AtomicBool::new(false);
    if RUNNING.swap(true, Ordering::SeqCst) {
        return;
    }
    let started = std::thread::Builder::new()
        .name("settings-watch".to_string())
        .spawn(move || watch_loop(file, POLL, &NEVER, |s| publish(&app, s)));
    if let Err(e) = started {
        eprintln!("crush-desktop: could not start the settings watcher: {e}");
    }
}

/// The settings file of this user, or None when the system names no config folder.
fn global() -> Option<&'static SettingsFile> {
    static FILE: OnceLock<Option<SettingsFile>> = OnceLock::new();
    FILE.get_or_init(|| {
        dirs::config_dir().map(|dir| SettingsFile::new(dir.join("crush-desktop").join("settings.json")))
    })
    .as_ref()
}

/// Hands new settings to this process: AppState.settings (what `settings_get`
/// answers) and the `settings-changed` event to every window. Called while the
/// file's mutex is held, so two publishes never cross.
fn publish(app: &tauri::AppHandle, settings: &Settings) {
    if let Some(state) = app.try_state::<AppState>() {
        *state.settings.lock().unwrap() = settings.clone();
    }
    if let Err(e) = app.emit(EVENT, settings) {
        eprintln!("crush-desktop: could not tell the windows about new settings: {e}");
    }
}

fn no_folder() -> AppError {
    AppError::new(
        "settings_failed",
        "Crush Desktop could not save its settings: the system names no config folder.",
    )
}

fn bad_setting(message: impl Into<String>) -> AppError {
    AppError::new("bad_setting", message)
}

/// `settings_set`'s change, checked before the file is locked.
#[derive(Debug, PartialEq)]
enum KeyChange {
    CrushPath(Option<String>),
    Yolo(bool),
    /// Phases 5 and 6 (desktop-design-5-6 6.1, 7.1), kept beside the others in
    /// the file: `theme` (a theme name; "" is the default, 3.3) and
    /// `list_collapsed` (2.20).
    Other(&'static str, Value),
}

impl KeyChange {
    fn parse(key: &str, value: Value) -> AppResult<KeyChange> {
        match key {
            // A blank path means "use crush from PATH" (1.2.3), the same as null.
            "crush_path" => match value {
                Value::Null => Ok(KeyChange::CrushPath(None)),
                Value::String(path) if path.trim().is_empty() => Ok(KeyChange::CrushPath(None)),
                Value::String(path) => Ok(KeyChange::CrushPath(Some(path.trim().to_string()))),
                _ => Err(bad_setting("crush_path takes the path of a crush program, or null.")),
            },
            "yolo" => match value {
                Value::Bool(on) => Ok(KeyChange::Yolo(on)),
                _ => Err(bad_setting("yolo takes true or false.")),
            },
            "theme" => match value {
                Value::String(name) => Ok(KeyChange::Other("theme", Value::String(name.trim().to_string()))),
                _ => Err(bad_setting("theme takes the name of a theme, or \"\" for the default.")),
            },
            "list_collapsed" => match value {
                Value::Bool(on) => Ok(KeyChange::Other("list_collapsed", Value::Bool(on))),
                _ => Err(bad_setting("list_collapsed takes true or false.")),
            },
            // Light or dark paper (owner, 2026-10-20), or the system's choice.
            "appearance" => match value {
                Value::String(mode) if matches!(mode.as_str(), "light" | "dark" | "system") => Ok(KeyChange::Other("appearance", Value::String(mode))),
                _ => Err(bad_setting("appearance takes \"light\", \"dark\" or \"system\".")),
            },
            other => Err(bad_setting(format!("{other} is not a setting Crush Desktop can change."))),
        }
    }

    fn apply(&self, settings: &mut Settings) {
        match self {
            KeyChange::CrushPath(path) => settings.crush_path = path.clone(),
            KeyChange::Yolo(on) => settings.yolo = *on,
            KeyChange::Other(key, value) => {
                settings.other.insert(key.to_string(), value.clone());
            }
        }
    }
}

fn check_closed_args(project: &str, session_id: &str) -> AppResult<()> {
    if project.is_empty() {
        return Err(bad_setting("A closed chat needs the project's data folder."));
    }
    if session_id.is_empty() {
        return Err(bad_setting("A closed chat needs its session ID."));
    }
    Ok(())
}

/// Closing adds the chat once, at the end of the project's list; reopening
/// removes it, and a project whose list becomes empty leaves the file.
fn apply_closed(settings: &mut Settings, project: &str, session_id: &str, closed: bool) {
    if closed {
        let list = settings.closed_chats.entry(project.to_string()).or_default();
        if !list.iter().any(|id| id == session_id) {
            list.push(session_id.to_string());
        }
    } else if let Some(list) = settings.closed_chats.get_mut(project) {
        list.retain(|id| id != session_id);
        if list.is_empty() {
            settings.closed_chats.remove(project);
        }
    }
}

/// One version of the file on disk: its inode, modification time and length.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Stamp {
    ino: u64,
    mtime: i64,
    mtime_ns: i64,
    len: u64,
}

impl Stamp {
    fn of(meta: &fs::Metadata) -> Stamp {
        Stamp {
            ino: meta.ino(),
            mtime: meta.mtime(),
            mtime_ns: meta.mtime_nsec(),
            len: meta.len(),
        }
    }
}

/// What this process last saw of the file, kept under the in-process mutex.
struct Seen {
    /// The file as last read or written here; None while it is missing.
    stamp: Option<Stamp>,
    /// The settings this process holds (AppState.settings mirrors them).
    current: Settings,
}

/// One settings file. The real app has one (`global`); each test makes its own
/// on a throwaway path, and two of them on one path stand in for two processes.
struct SettingsFile {
    path: PathBuf,
    lock_path: PathBuf,
    seen: Mutex<Seen>,
}

impl SettingsFile {
    fn new(path: PathBuf) -> SettingsFile {
        SettingsFile {
            lock_path: path.with_file_name("settings.lock"),
            path,
            seen: Mutex::new(Seen {
                stamp: None,
                current: Settings::default(),
            }),
        }
    }

    /// Reads the file. The stamp comes from the open handle, so it always
    /// belongs to the bytes read. A missing file is (None, the defaults); a file
    /// that does not parse is (its stamp, Err(why)).
    fn read(&self) -> io::Result<(Option<Stamp>, Result<Settings, String>)> {
        let mut file = match File::open(&self.path) {
            Ok(file) => file,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok((None, Ok(Settings::default()))),
            Err(e) => return Err(e),
        };
        let stamp = Stamp::of(&file.metadata()?);
        let mut text = String::new();
        let parsed = match file.read_to_string(&mut text) {
            Ok(_) => serde_json::from_str::<Settings>(&text).map_err(|e| e.to_string()),
            Err(e) if e.kind() == io::ErrorKind::InvalidData => Err("it is not UTF-8 text".to_string()),
            Err(e) => return Err(e),
        };
        Ok((Some(stamp), parsed))
    }

    /// At start: the file's settings (the defaults when it is missing, broken or
    /// unreadable), remembered as what this process has seen.
    fn load(&self) -> Settings {
        let mut seen = self.seen.lock().unwrap();
        match self.read() {
            Ok((stamp, parsed)) => {
                seen.stamp = stamp;
                seen.current = parsed.unwrap_or_else(|why| {
                    eprintln!(
                        "crush-desktop: ignoring the settings file {} until it changes: {why}",
                        self.path.display()
                    );
                    Settings::default()
                });
            }
            Err(e) => eprintln!("crush-desktop: could not read the settings file {}: {e}", self.path.display()),
        }
        seen.current.clone()
    }

    /// One change: under the in-process mutex and the cross-process lock, read
    /// the file again, apply the change, write it atomically, remember the new
    /// file as seen (so the watcher never reports this write) and publish.
    /// A file that does not parse is never overwritten: the change fails.
    fn change(&self, apply: impl FnOnce(&mut Settings), publish: impl FnOnce(&Settings)) -> AppResult<Settings> {
        let mut seen = self.seen.lock().unwrap();
        let failed = |e: io::Error| {
            AppError::new(
                "settings_failed",
                format!("Crush Desktop could not save its settings in {}: {e}", self.path.display()),
            )
        };
        if let Some(dir) = self.path.parent() {
            fs::create_dir_all(dir).map_err(failed)?;
        }
        let lock = OpenOptions::new()
            .create(true)
            .truncate(false)
            .write(true)
            .open(&self.lock_path)
            .map_err(failed)?;
        lock.lock().map_err(failed)?;
        let (_, parsed) = self.read().map_err(failed)?;
        let mut settings = parsed.map_err(|why| {
            AppError::new(
                "settings_failed",
                format!(
                    "Crush Desktop did not save its settings: the settings file {} could not be read ({why}). Fix or remove it, then try again.",
                    self.path.display()
                ),
            )
        })?;
        apply(&mut settings);
        let stamp = write_atomic(&self.path, &settings).map_err(failed)?;
        drop(lock);
        seen.stamp = Some(stamp);
        seen.current = settings.clone();
        publish(&settings);
        Ok(settings)
    }

    /// One look by the watcher. When the file's stamp differs from the last
    /// seen one, it is read again; settings that differ from this process's are
    /// stored and published. A file that does not parse is ignored until it
    /// changes again. Returns whether anything was published.
    fn poll(&self, publish: impl FnOnce(&Settings)) -> bool {
        let mut seen = self.seen.lock().unwrap();
        let now = match fs::metadata(&self.path) {
            Ok(meta) => Some(Stamp::of(&meta)),
            Err(e) if e.kind() == io::ErrorKind::NotFound => None,
            Err(_) => return false,
        };
        if now == seen.stamp {
            return false;
        }
        let Ok((stamp, parsed)) = self.read() else {
            return false;
        };
        seen.stamp = stamp;
        let Ok(settings) = parsed else {
            return false;
        };
        if settings == seen.current {
            return false;
        }
        seen.current = settings.clone();
        publish(&settings);
        true
    }
}

/// The watcher's loop, until `stop` is set (never, in the app).
fn watch_loop(file: &SettingsFile, every: Duration, stop: &AtomicBool, publish: impl Fn(&Settings)) {
    while !stop.load(Ordering::SeqCst) {
        std::thread::sleep(every);
        file.poll(&publish);
    }
}

/// Writes the settings to a new file in the same folder and renames it over
/// the old one, so a reader sees the old file or the new one, never half of
/// either. Returns the new file's stamp.
fn write_atomic(path: &Path, settings: &Settings) -> io::Result<Stamp> {
    static COUNT: AtomicU64 = AtomicU64::new(0);
    let mut text = serde_json::to_string_pretty(settings).map_err(io::Error::other)?;
    text.push('\n');
    let name = path.file_name().and_then(|n| n.to_str()).unwrap_or("settings.json");
    let tmp = path.with_file_name(format!(
        ".{name}.{}.{}.tmp",
        std::process::id(),
        COUNT.fetch_add(1, Ordering::Relaxed)
    ));
    let written = (|| {
        let mut file = OpenOptions::new().write(true).create_new(true).open(&tmp)?;
        file.write_all(text.as_bytes())?;
        file.sync_all()?;
        let stamp = Stamp::of(&file.metadata()?);
        fs::rename(&tmp, path)?;
        Ok(stamp)
    })();
    if written.is_err() {
        let _ = fs::remove_file(&tmp);
    }
    written
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::collections::BTreeMap;
    use std::sync::Arc;
    use std::time::Instant;

    /// A throwaway folder for one test, removed at the end.
    struct Dir(PathBuf);

    impl Dir {
        fn new(name: &str) -> Dir {
            static N: AtomicU64 = AtomicU64::new(0);
            let dir = std::env::temp_dir().join(format!(
                "crush-desktop-settings-{name}-{}-{}",
                std::process::id(),
                N.fetch_add(1, Ordering::Relaxed)
            ));
            let _ = fs::remove_dir_all(&dir);
            fs::create_dir_all(&dir).unwrap();
            Dir(dir)
        }

        fn settings(&self) -> PathBuf {
            self.0.join("crush-desktop").join("settings.json")
        }
    }

    impl Drop for Dir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    /// What another app process does: writes a whole new file and renames it
    /// into place, without this process's mutex or knowledge.
    fn write_from_outside(path: &Path, value: &Value) {
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        let tmp = path.with_file_name("outside.tmp");
        fs::write(&tmp, serde_json::to_string_pretty(value).unwrap()).unwrap();
        fs::rename(&tmp, path).unwrap();
    }

    fn raw(path: &Path) -> Value {
        serde_json::from_str(&fs::read_to_string(path).unwrap()).unwrap()
    }

    /// How long a test waits for the watcher: ten of its polls, so a machine
    /// busy with other test suites cannot fail a test by being slow.
    const PATIENCE: Duration = Duration::from_secs(5);

    /// The real watcher loop on its own thread, recording what it publishes.
    struct Watcher {
        reports: Arc<Mutex<Vec<Settings>>>,
        stop: Arc<AtomicBool>,
        thread: Option<std::thread::JoinHandle<()>>,
    }

    impl Watcher {
        fn start(file: Arc<SettingsFile>) -> Watcher {
            let reports: Arc<Mutex<Vec<Settings>>> = Arc::default();
            let stop = Arc::new(AtomicBool::new(false));
            let thread = {
                let (reports, stop) = (reports.clone(), stop.clone());
                std::thread::spawn(move || watch_loop(&file, POLL, &stop, |s| reports.lock().unwrap().push(s.clone())))
            };
            Watcher {
                reports,
                stop,
                thread: Some(thread),
            }
        }

        fn reports(&self) -> Vec<Settings> {
            self.reports.lock().unwrap().clone()
        }

        /// The reports, once there are `count` of them or PATIENCE has passed.
        fn wait_for(&self, count: usize) -> Vec<Settings> {
            let end = Instant::now() + PATIENCE;
            while self.reports().len() < count && Instant::now() < end {
                std::thread::sleep(Duration::from_millis(10));
            }
            self.reports()
        }
    }

    impl Drop for Watcher {
        fn drop(&mut self) {
            self.stop.store(true, Ordering::SeqCst);
            if let Some(thread) = self.thread.take() {
                let _ = thread.join();
            }
        }
    }

    fn close(file: &SettingsFile, project: &str, id: &str) -> Settings {
        file.change(|s| apply_closed(s, project, id, true), |_| {}).unwrap()
    }

    #[test]
    fn atomic_write_keeps_unknown_keys() {
        let dir = Dir::new("unknown");
        let path = dir.settings();
        write_from_outside(
            &path,
            &json!({"version": 1, "yolo": false, "theme": "edition", "window": {"w": 900, "h": 600},
                    "closed_chats": {"/data/a": ["s1"]}}),
        );
        let file = SettingsFile::new(path.clone());
        assert!(!file.load().yolo);
        let inode_before = fs::metadata(&path).unwrap().ino();
        let saved = close(&file, "/data/a", "s2");
        assert_eq!(saved.other.get("theme"), Some(&json!("edition")));
        let on_disk = raw(&path);
        assert_eq!(on_disk["theme"], json!("edition"), "an unknown key was dropped: {on_disk}");
        assert_eq!(on_disk["window"], json!({"w": 900, "h": 600}), "an unknown key was dropped: {on_disk}");
        assert_eq!(on_disk["yolo"], json!(false));
        assert_eq!(on_disk["closed_chats"], json!({"/data/a": ["s1", "s2"]}));
        assert_eq!(on_disk["crush_path"], Value::Null);
        // Atomic: the old file was replaced by a rename (a new inode), not
        // written in place, and no temp file is left beside it.
        assert_ne!(fs::metadata(&path).unwrap().ino(), inode_before, "the file was written in place");
        let left: Vec<String> = fs::read_dir(path.parent().unwrap())
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
            .filter(|n| n != "settings.json" && n != "settings.lock")
            .collect();
        assert!(left.is_empty(), "files left beside the settings: {left:?}");
    }

    #[test]
    fn closed_set_is_kept_per_data_dir() {
        let dir = Dir::new("closed");
        let file = SettingsFile::new(dir.settings());
        file.load();
        close(&file, "/data/one", "a");
        close(&file, "/data/two", "b");
        close(&file, "/data/one", "c");
        let s = close(&file, "/data/one", "a"); // closing twice keeps one entry
        let mut want = BTreeMap::new();
        want.insert("/data/one".to_string(), vec!["a".to_string(), "c".to_string()]);
        want.insert("/data/two".to_string(), vec!["b".to_string()]);
        assert_eq!(s.closed_chats, want);
        let s = file.change(|s| apply_closed(s, "/data/one", "a", false), |_| {}).unwrap();
        assert_eq!(s.closed_chats["/data/one"], vec!["c".to_string()]);
        let s = file.change(|s| apply_closed(s, "/data/one", "c", false), |_| {}).unwrap();
        assert!(!s.closed_chats.contains_key("/data/one"), "an empty project stays: {:?}", s.closed_chats);
        assert_eq!(s.closed_chats["/data/two"], vec!["b".to_string()]);
        // It survives a restart: a fresh handle reads the same set.
        assert_eq!(SettingsFile::new(dir.settings()).load().closed_chats, s.closed_chats);
        assert!(check_closed_args("", "a").is_err());
        assert!(check_closed_args("/data/one", "").is_err());
    }

    #[test]
    fn watch_sees_other_process_write() {
        let dir = Dir::new("watch");
        let path = dir.settings();
        write_from_outside(&path, &json!({"version": 1, "yolo": true}));
        let file = Arc::new(SettingsFile::new(path.clone()));
        file.load();
        let watcher = Watcher::start(file.clone());
        std::thread::sleep(POLL + POLL / 2); // the watcher is mid-cycle

        write_from_outside(&path, &json!({"version": 1, "yolo": true, "closed_chats": {"/data/p": ["x"]}}));
        let reports = watcher.wait_for(1);
        assert!(!reports.is_empty(), "the other process's write was not reported within {PATIENCE:?}");
        assert_eq!(reports[0].closed_chats["/data/p"], vec!["x".to_string()]);
        // It is reported once, not again on the next four looks.
        std::thread::sleep(POLL * 4);
        assert_eq!(watcher.reports().len(), 1, "reported more than once");
        assert_eq!(file.seen.lock().unwrap().current.closed_chats["/data/p"], vec!["x".to_string()]);
    }

    #[test]
    fn own_write_does_not_echo() {
        let dir = Dir::new("echo");
        let file = Arc::new(SettingsFile::new(dir.settings()));
        file.load();
        let watcher = Watcher::start(file.clone());
        let mut published = 0;
        file.change(|s| apply_closed(s, "/data/p", "mine", true), |_| published += 1)
            .unwrap();
        assert_eq!(published, 1, "the change itself publishes once");
        // A fixed window of four looks (the design's bar is 1.5 s); an echo
        // would come on the first.
        std::thread::sleep(POLL * 4);
        assert!(
            watcher.reports().is_empty(),
            "the watcher reported this process's own write: {:?}",
            watcher.reports().iter().map(|r| &r.closed_chats).collect::<Vec<_>>()
        );
        // A later change from outside is still seen after the own write.
        write_from_outside(&dir.settings(), &json!({"closed_chats": {"/data/p": ["mine", "theirs"]}}));
        let reports = watcher.wait_for(1);
        assert_eq!(reports.len(), 1, "a change from outside after an own write was missed");
        assert_eq!(reports[0].closed_chats["/data/p"], vec!["mine".to_string(), "theirs".to_string()]);
    }

    #[test]
    fn concurrent_changes_both_kept() {
        let dir = Dir::new("concurrent");
        let path = dir.settings();
        // Two handles that open the file separately, each with its own mutex,
        // stand in for two app processes; only the file lock keeps them apart.
        let one = Arc::new(SettingsFile::new(path.clone()));
        let two = Arc::new(SettingsFile::new(path.clone()));
        let run = |file: Arc<SettingsFile>, prefix: &'static str| {
            std::thread::spawn(move || {
                for i in 0..60 {
                    file.change(|s| apply_closed(s, "/data/p", &format!("{prefix}{i}"), true), |_| {})
                        .unwrap();
                }
            })
        };
        let (a, b) = (run(one, "a"), run(two, "b"));
        a.join().unwrap();
        b.join().unwrap();
        let kept = SettingsFile::new(path).load().closed_chats["/data/p"].clone();
        let lost: Vec<String> = (0..60)
            .flat_map(|i| [format!("a{i}"), format!("b{i}")])
            .filter(|id| !kept.contains(id))
            .collect();
        assert!(lost.is_empty(), "{} changes were lost: {lost:?}", lost.len());
        assert_eq!(kept.len(), 120);
    }

    #[test]
    fn missing_or_broken_files_load_defaults_and_broken_ones_are_never_overwritten() {
        let dir = Dir::new("broken");
        let path = dir.settings();
        let file = SettingsFile::new(path.clone());
        assert_eq!(file.load(), Settings::default());
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(&path, "{\"yolo\": fals").unwrap();
        let file = SettingsFile::new(path.clone());
        assert_eq!(file.load(), Settings::default());
        let err = file.change(|s| s.yolo = false, |_| panic!("published")).unwrap_err();
        assert_eq!(err.code, "settings_failed");
        assert!(err.message.contains("could not be read"), "{}", err.message);
        assert_eq!(fs::read_to_string(&path).unwrap(), "{\"yolo\": fals", "a broken file was overwritten");
        // The watcher ignores the broken file until it changes again.
        assert!(!file.poll(|_| panic!("published a broken file")));
        write_from_outside(&path, &json!({"yolo": false}));
        let mut got = None;
        assert!(file.poll(|s| got = Some(s.clone())));
        assert!(!got.unwrap().yolo);
        // A file deleted by hand counts as the defaults again.
        fs::remove_file(&path).unwrap();
        let mut got = None;
        assert!(file.poll(|s| got = Some(s.clone())));
        assert_eq!(got.unwrap(), Settings::default());
    }

    #[test]
    fn set_key_takes_only_known_keys() {
        assert_eq!(
            KeyChange::parse("crush_path", json!(" /opt/crush/bin/crush ")).unwrap(),
            KeyChange::CrushPath(Some("/opt/crush/bin/crush".into()))
        );
        assert_eq!(KeyChange::parse("crush_path", json!(null)).unwrap(), KeyChange::CrushPath(None));
        assert_eq!(KeyChange::parse("crush_path", json!("  ")).unwrap(), KeyChange::CrushPath(None));
        assert_eq!(KeyChange::parse("yolo", json!(false)).unwrap(), KeyChange::Yolo(false));
        assert_eq!(KeyChange::parse("appearance", json!("dark")).unwrap(), KeyChange::Other("appearance", json!("dark")));
        for (key, value) in [("yolo", json!("no")), ("crush_path", json!(3)), ("closed_chats", json!({})), ("theme", json!(3)), ("list_collapsed", json!("yes")), ("appearance", json!("dim")), ("appearance", json!(true))] {
            let err = KeyChange::parse(key, value).unwrap_err();
            assert_eq!(err.code, "bad_setting", "{key}");
        }
        let dir = Dir::new("keys");
        let file = SettingsFile::new(dir.settings());
        let change = KeyChange::parse("crush_path", json!("/usr/local/bin/crush")).unwrap();
        file.change(|s| change.apply(s), |_| {}).unwrap();
        let change = KeyChange::parse("yolo", json!(false)).unwrap();
        let s = file.change(|s| change.apply(s), |_| {}).unwrap();
        assert_eq!((s.crush_path.as_deref(), s.yolo), (Some("/usr/local/bin/crush"), false));
        assert_eq!(raw(&dir.settings())["crush_path"], json!("/usr/local/bin/crush"));
    }
}
