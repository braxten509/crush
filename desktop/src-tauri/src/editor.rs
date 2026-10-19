//! The external editor (desktop-design-5-6 2.26, 6.4): the typing box's
//! text goes to a temporary file, and the user's editor opens it in a
//! terminal tab, as the terminal's Ctrl+O does (internal/ui/model/editor.go:144-193)
//! with github.com/charmbracelet/x/editor@v0.2.0. The editor's name comes
//! from the login-shell environment and is never logged.

use crate::error::{codes, AppError, AppResult};
use serde::Serialize;
use std::collections::HashSet;
use std::io::Write;
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::Mutex;

/// The editor when neither $VISUAL nor $EDITOR names one (x/editor editor.go:15-17).
const DEFAULT_EDITOR: &str = "nano";

/// The temporary files `editor_prepare` made (managed by main.rs).
#[derive(Default)]
pub struct Edits {
    made: Mutex<HashSet<PathBuf>>,
}

#[derive(Serialize)]
pub struct Prepared {
    pub path: String,
    pub argv: Vec<String>,
}

#[derive(Serialize)]
pub struct Finished {
    pub text: String,
}

/// `editor_prepare`: writes `text` to a new 0600 file and builds the editor command.
pub fn prepare(edits: &Edits, text: &str, line: u32, column: u32) -> AppResult<Prepared> {
    let env = crate::login_env::login_env();
    let get = |name: &str| env.iter().find(|(k, _)| k == name).map(|(_, v)| v.clone());
    let value = editor_value(get("VISUAL").as_deref(), get("EDITOR").as_deref());
    let program = value.split_whitespace().next().unwrap_or(DEFAULT_EDITOR).to_string();
    if !findable(&program, get("PATH").as_deref()) {
        // What Go's os/exec reports for a missing program.
        let why = if program.contains('/') {
            format!("fork/exec {program}: no such file or directory")
        } else {
            format!("exec: \"{program}\": executable file not found in $PATH")
        };
        return Err(AppError::new(codes::NO_EDITOR, why));
    }
    let path = new_file(text).map_err(|e| AppError::new("bad_path", format!("open temporary file: {e}")))?;
    let argv = editor_args(&value, &path.to_string_lossy(), line, column);
    edits.made.lock().unwrap().insert(path.clone());
    Ok(Prepared { path: path.to_string_lossy().into_owned(), argv })
}

/// `editor_finish`: reads and deletes a file `prepare` made; the text comes
/// back without the white space around it (editorFileMsg, editor.go:185-193).
pub fn finish(edits: &Edits, path: &str) -> AppResult<Finished> {
    let path = PathBuf::from(path);
    if !edits.made.lock().unwrap().remove(&path) {
        return Err(AppError::new("bad_path", "That file was not made for the editor."));
    }
    let read = std::fs::read(&path);
    let _ = std::fs::remove_file(&path);
    let bytes = read.map_err(|e| AppError::new("bad_path", format!("open {}: {}", path.display(), os_words(&e))))?;
    Ok(Finished { text: String::from_utf8_lossy(&bytes).trim().to_string() })
}

/// $VISUAL, else $EDITOR (PLAN section 7; the terminal's helper reads only
/// $EDITOR, editor.go:126-140), each ignored when blank; "" means nano.
fn editor_value(visual: Option<&str>, editor: Option<&str>) -> String {
    [visual, editor]
        .into_iter()
        .flatten()
        .find(|v| !v.trim().is_empty())
        .unwrap_or("")
        .to_string()
}

/// The command for `value` (split on white space, editor.go:126-140; nano
/// when empty) editing `path` at line and column, as x/editor's AtPosition
/// does (editor.go:61-86, :103-124): the position argument by the program's
/// base name, then the path unless the argument holds it.
fn editor_args(value: &str, path: &str, line: u32, column: u32) -> Vec<String> {
    let mut words: Vec<String> = value.split_whitespace().map(str::to_string).collect();
    if words.is_empty() {
        words.push(DEFAULT_EDITOR.to_string());
    }
    let (line, column) = (line.max(1), column.max(1));
    let name = Path::new(&words[0]).file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
    let (position, path_in_args): (Vec<String>, bool) = match name.as_str() {
        "vi" | "vim" | "nvim" => (vec![format!("+call cursor({line},{column})")], false),
        "nano" => (vec![format!("+{line},{column}")], false),
        "emacs" | "kak" => (vec![format!("+{line}:{column}")], false),
        "gedit" => (vec![format!("+{line}")], false),
        "code" => (vec!["--goto".to_string(), format!("{path}:{line}:{column}")], true),
        "hx" => (vec![format!("{path}:{line}:{column}")], true),
        _ => (Vec::new(), false),
    };
    words.extend(position);
    if !path_in_args {
        words.push(path.to_string());
    }
    words
}

/// Whether the program can be started: a path that is an executable file,
/// or a name found on the login shell's PATH (absolute folders only).
fn findable(program: &str, path: Option<&str>) -> bool {
    if program.contains('/') {
        return executable(Path::new(program));
    }
    path.into_iter()
        .flat_map(std::env::split_paths)
        .filter(|dir| dir.is_absolute())
        .any(|dir| executable(&dir.join(program)))
}

fn executable(path: &Path) -> bool {
    std::fs::metadata(path).map(|m| m.is_file() && m.permissions().mode() & 0o111 != 0).unwrap_or(false)
}

/// A new msg_<number>.md in the temporary folder, readable by its owner
/// only (os.CreateTemp("", "msg_*.md"), editor.go:145).
fn new_file(text: &str) -> std::io::Result<PathBuf> {
    let dir = std::env::temp_dir();
    let mut last = None;
    for _ in 0..100 {
        let number = uuid::Uuid::new_v4().as_u128() as u32;
        let path = dir.join(format!("msg_{number}.md"));
        match std::fs::OpenOptions::new().write(true).create_new(true).mode(0o600).open(&path) {
            Ok(mut file) => {
                if let Err(e) = file.write_all(text.as_bytes()) {
                    let _ = std::fs::remove_file(&path);
                    return Err(e);
                }
                return Ok(path);
            }
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => last = Some(e),
            Err(e) => return Err(e),
        }
    }
    Err(last.unwrap_or_else(|| std::io::Error::other("no free name")))
}

/// An OS error in Go's words ("no such file or directory").
fn os_words(e: &std::io::Error) -> String {
    let text = e.to_string();
    match text.find(" (os error") {
        Some(at) => text[..at].to_lowercase(),
        None => text,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn args(value: &str, line: u32, column: u32) -> Vec<String> {
        super::editor_args(value, "/tmp/msg_1.md", line, column)
    }

    #[test]
    fn editor_args() {
        // Each editor of 2.26 step 3, by base name, at line 3, column 7.
        for vim in ["vi", "vim", "nvim"] {
            assert_eq!(args(vim, 3, 7), [vim, "+call cursor(3,7)", "/tmp/msg_1.md"]);
        }
        assert_eq!(args("nano", 3, 7), ["nano", "+3,7", "/tmp/msg_1.md"]);
        assert_eq!(args("emacs", 3, 7), ["emacs", "+3:7", "/tmp/msg_1.md"]);
        assert_eq!(args("kak", 3, 7), ["kak", "+3:7", "/tmp/msg_1.md"]);
        assert_eq!(args("gedit", 3, 7), ["gedit", "+3", "/tmp/msg_1.md"]);
        assert_eq!(args("code", 3, 7), ["code", "--goto", "/tmp/msg_1.md:3:7"]);
        assert_eq!(args("hx", 3, 7), ["hx", "/tmp/msg_1.md:3:7"]);
        // Any other editor gets the path alone.
        assert_eq!(args("micro", 3, 7), ["micro", "/tmp/msg_1.md"]);
        // The value is split on white space; its other words come first; the
        // base name decides, so a full path works too.
        assert_eq!(args("code  --wait", 1, 1), ["code", "--wait", "--goto", "/tmp/msg_1.md:1:1"]);
        assert_eq!(args("/usr/bin/nvim -u NONE", 2, 1), ["/usr/bin/nvim", "-u", "NONE", "+call cursor(2,1)", "/tmp/msg_1.md"]);
        assert_eq!(args("emacs -nw", 1, 5), ["emacs", "-nw", "+1:5", "/tmp/msg_1.md"]);
        // Line and column below 1 become 1.
        assert_eq!(args("nano", 0, 0), ["nano", "+1,1", "/tmp/msg_1.md"]);
        // No editor named: nano.
        assert_eq!(args("", 4, 2), ["nano", "+4,2", "/tmp/msg_1.md"]);
        assert_eq!(args("   ", 4, 2), ["nano", "+4,2", "/tmp/msg_1.md"]);
        // $VISUAL, else $EDITOR, a blank one skipped.
        assert_eq!(editor_value(Some("hx"), Some("vim")), "hx");
        assert_eq!(editor_value(None, Some("vim")), "vim");
        assert_eq!(editor_value(Some("  "), Some("vim")), "vim");
        assert_eq!(editor_value(None, None), "");
    }
}
