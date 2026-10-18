//! File reads for attachments and the `@` popup (desktop-design 1.2.8;
//! TYPE-011 to TYPE-014, TYPE-026, TYPE-029).
//!
//! Relative paths are read against the window's project folder (`cwd`). The
//! type of a read file comes from its first 512 bytes with a port of Go's
//! http.DetectContentType, the function the terminal names every attachment
//! with (mimeOf, internal/ui/model/paste.go:27-31), so the engine gets the same
//! type from both apps. Error texts follow Go's ("open <path>: permission
//! denied"), because the terminal shows them as they are ("Unable to read
//! file: …", TYPE-014).

use crate::error::{AppError, AppResult};
use base64::Engine as _;
use std::collections::{HashSet, VecDeque};
use std::fs::{self, File};
use std::io::{self, Read};
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant, UNIX_EPOCH};

#[derive(serde::Serialize, Debug, PartialEq)]
pub struct FileInfo {
    pub exists: bool,
    pub is_dir: bool,
    pub size: u64,
    pub mime: String,
    /// Added by P3.2: the modification time in Unix milliseconds (0 when
    /// unknown), for TYPE-029's "unchanged since this chat read it", which the
    /// terminal decides with the file's ModTime (editor.go:316-322).
    pub modified: u64,
}

/// Mime from the content, then the extension: the content sniffer (Go's, as
/// for reads) decides, and only a file it cannot name (octet-stream) is named
/// by its extension. Missing paths are `exists: false`, not an error.
pub fn info(path: &str, cwd: &str) -> AppResult<FileInfo> {
    let full = resolve(path, cwd)?;
    let meta = match fs::metadata(&full) {
        Ok(meta) => meta,
        Err(e) if matches!(e.kind(), io::ErrorKind::NotFound | io::ErrorKind::NotADirectory) => {
            return Ok(FileInfo {
                exists: false,
                is_dir: false,
                size: 0,
                mime: String::new(),
                modified: 0,
            });
        }
        Err(e) => return Err(bad_path(format!("stat {}: {}", full.display(), go_error_text(&e)))),
    };
    let mime = if meta.is_file() { info_type(&full) } else { String::new() };
    Ok(FileInfo {
        exists: true,
        is_dir: meta.is_dir(),
        size: meta.len(),
        mime,
        modified: meta
            .modified()
            .ok()
            .and_then(|t| t.duration_since(UNIX_EPOCH).ok())
            .map_or(0, |d| d.as_millis() as u64),
    })
}

/// (mime, base64); too_big over max_bytes.
pub fn read(path: &str, cwd: &str, max_bytes: u64) -> AppResult<(String, String)> {
    let full = resolve(path, cwd)?;
    let shown = full.display();
    let meta = fs::metadata(&full).map_err(|e| bad_path(format!("stat {shown}: {}", go_error_text(&e))))?;
    if meta.is_dir() {
        return Err(AppError::new("is_a_directory", format!("read {shown}: is a directory")));
    }
    if !meta.is_file() {
        return Err(AppError::new("not_a_file", format!("{shown} is not a regular file")));
    }
    if meta.len() > max_bytes {
        return Err(too_big(&full, meta.len(), max_bytes));
    }
    let file = File::open(&full).map_err(|e| bad_path(format!("open {shown}: {}", go_error_text(&e))))?;
    // The file may have grown since the look above: read one byte past the
    // limit to know.
    let mut data = Vec::with_capacity(meta.len().min(16 << 20) as usize);
    file.take(max_bytes.saturating_add(1))
        .read_to_end(&mut data)
        .map_err(|e| bad_path(format!("read {shown}: {}", go_error_text(&e))))?;
    if data.len() as u64 > max_bytes {
        return Err(too_big(&full, data.len() as u64, max_bytes));
    }
    let mime = detect_content_type(&data).to_string();
    Ok((mime, base64::engine::general_purpose::STANDARD.encode(&data)))
}

/// Path relative to cwd, "/" separators. Clone lets the command keep a window's list for 5 s.
#[derive(serde::Serialize, Clone, Debug, PartialEq)]
pub struct FileEntry {
    pub path: String,
    pub is_dir: bool,
}

/// The `@` popup's file list (TYPE-026). Inside a Git work tree: `git ls-files --cached --others
/// --exclude-standard` (2 s limit), which applies the ignore files; elsewhere a walk that skips
/// hidden folders. Both stop at max_depth folders deep and max_items entries. Inferred to match
/// the terminal's walker closely enough; the builder compares with `internal/fsext` and notes
/// any difference (for example `.crushignore`) in the piece's report.
///
/// As in the terminal (fsext.ListDirectory, internal/fsext/ls.go:270-316):
/// folders are listed too, a limit of 0 means no limit, and the names the
/// terminal never lists (ls.go:20-83) are left out in both ways. A Git list
/// that fails or takes longer than 2 s falls back to the walk. Entries come
/// sorted as the terminal sorts them (by path, a folder's path ending in "/").
pub fn list(cwd: &str, max_depth: u32, max_items: u32) -> AppResult<Vec<FileEntry>> {
    let root = absolute(cwd)?;
    match fs::metadata(&root) {
        Ok(meta) if meta.is_dir() => {}
        Ok(_) => return Err(bad_path(format!("{} is not a folder", root.display()))),
        Err(e) => return Err(bad_path(format!("open {}: {}", root.display(), go_error_text(&e)))),
    }
    let (depth, items) = (max_depth as usize, max_items as usize);
    let mut entries = match git_files(&root) {
        Some(files) => from_git(&root, files, depth, items),
        None => walk(&root, depth, items),
    };
    entries.sort_by_key(sort_key);
    Ok(entries)
}

/// The terminal's text for an OS error, as Go prints it: the C library's words
/// with a small first letter ("no such file or directory", "permission denied").
pub(crate) fn go_error_text(e: &io::Error) -> String {
    let Some(code) = e.raw_os_error() else {
        return e.to_string();
    };
    let text = io::Error::from_raw_os_error(code).to_string();
    let text = text.strip_suffix(&format!(" (os error {code})")).unwrap_or(&text);
    let mut chars = text.chars();
    match chars.next() {
        Some(first) => first.to_lowercase().chain(chars).collect(),
        None => String::new(),
    }
}

fn bad_path(message: impl Into<String>) -> AppError {
    AppError::new("bad_path", message)
}

fn too_big(path: &Path, size: u64, max_bytes: u64) -> AppError {
    AppError::new(
        "too_big",
        format!("{} is {size} bytes, more than the {max_bytes} bytes allowed", path.display()),
    )
}

/// A folder made absolute against the app's own folder, as Go's filepath.Abs.
fn absolute(dir: &str) -> AppResult<PathBuf> {
    if dir.contains('\0') {
        return Err(bad_path(format!("{dir:?} is not a usable folder")));
    }
    let dir = Path::new(dir);
    if dir.is_absolute() {
        return Ok(dir.to_path_buf());
    }
    std::env::current_dir()
        .map(|here| here.join(dir))
        .map_err(|e| bad_path(format!("getwd: {}", go_error_text(&e))))
}

/// A path as given, or relative to cwd. Never expands `~` (the terminal's
/// paste does not either; a link's `~` is the opener's, opener.rs).
fn resolve(path: &str, cwd: &str) -> AppResult<PathBuf> {
    if path.is_empty() {
        return Err(bad_path("no file path was given"));
    }
    if path.contains('\0') {
        return Err(bad_path(format!("{path:?} is not a usable file path")));
    }
    let path = Path::new(path);
    if path.is_absolute() {
        return Ok(path.to_path_buf());
    }
    Ok(absolute(cwd)?.join(path))
}

const OCTET_STREAM: &str = "application/octet-stream";

/// `info`'s type: the content's, else (for content the sniffer cannot name)
/// the extension's.
fn info_type(path: &Path) -> String {
    let mut head = Vec::with_capacity(512);
    let sniffed = File::open(path)
        .and_then(|f| f.take(512).read_to_end(&mut head))
        .ok()
        .map(|_| detect_content_type(&head));
    match sniffed {
        Some(found) if found != OCTET_STREAM => found.to_string(),
        _ => extension_type(path).or(sniffed).unwrap_or_default().to_string(),
    }
}

/// Types for files whose content Go's sniffer does not know.
fn extension_type(path: &Path) -> Option<&'static str> {
    let ext = path.extension()?.to_str()?.to_ascii_lowercase();
    Some(match ext.as_str() {
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "gif" => "image/gif",
        "webp" => "image/webp",
        "bmp" => "image/bmp",
        "ico" => "image/x-icon",
        "svg" => "image/svg+xml",
        "avif" => "image/avif",
        "heic" => "image/heic",
        "heif" => "image/heif",
        "tif" | "tiff" => "image/tiff",
        "pdf" => "application/pdf",
        "zip" => "application/zip",
        "gz" => "application/gzip",
        "tar" => "application/x-tar",
        "7z" => "application/x-7z-compressed",
        "xz" => "application/x-xz",
        "bz2" => "application/x-bzip2",
        "zst" => "application/zstd",
        "mp3" => "audio/mpeg",
        "wav" => "audio/wav",
        "ogg" => "audio/ogg",
        "flac" => "audio/flac",
        "m4a" => "audio/mp4",
        "mp4" => "video/mp4",
        "webm" => "video/webm",
        "mov" => "video/quicktime",
        "mkv" => "video/x-matroska",
        _ => return None,
    })
}

/// One of the patterns of Go's sniffSignatures (net/http/sniff.go).
enum Sig {
    /// An HTML tag, any case, after leading white space, then a space or ">".
    Html(&'static [u8]),
    Masked {
        mask: &'static [u8],
        pat: &'static [u8],
        skip_ws: bool,
        ct: &'static str,
    },
    Exact(&'static [u8], &'static str),
    Mp4,
    Text,
}

const EOT_MASK: [u8; 36] = {
    let mut mask = [0u8; 36];
    mask[34] = 0xFF;
    mask[35] = 0xFF;
    mask
};
const EOT_PAT: [u8; 36] = {
    let mut pat = [0u8; 36];
    pat[34] = b'L';
    pat[35] = b'P';
    pat
};

/// Go's table, in Go's order (the first match wins).
const SIGNATURES: &[Sig] = &[
    Sig::Html(b"<!DOCTYPE HTML"),
    Sig::Html(b"<HTML"),
    Sig::Html(b"<HEAD"),
    Sig::Html(b"<SCRIPT"),
    Sig::Html(b"<IFRAME"),
    Sig::Html(b"<H1"),
    Sig::Html(b"<DIV"),
    Sig::Html(b"<FONT"),
    Sig::Html(b"<TABLE"),
    Sig::Html(b"<A"),
    Sig::Html(b"<STYLE"),
    Sig::Html(b"<TITLE"),
    Sig::Html(b"<B"),
    Sig::Html(b"<BODY"),
    Sig::Html(b"<BR"),
    Sig::Html(b"<P"),
    Sig::Html(b"<!--"),
    Sig::Masked { mask: b"\xFF\xFF\xFF\xFF\xFF", pat: b"<?xml", skip_ws: true, ct: "text/xml; charset=utf-8" },
    Sig::Exact(b"%PDF-", "application/pdf"),
    Sig::Exact(b"%!PS-Adobe-", "application/postscript"),
    // UTF byte order marks.
    Sig::Masked { mask: b"\xFF\xFF\x00\x00", pat: b"\xFE\xFF\x00\x00", skip_ws: false, ct: "text/plain; charset=utf-16be" },
    Sig::Masked { mask: b"\xFF\xFF\x00\x00", pat: b"\xFF\xFE\x00\x00", skip_ws: false, ct: "text/plain; charset=utf-16le" },
    Sig::Masked { mask: b"\xFF\xFF\xFF\x00", pat: b"\xEF\xBB\xBF\x00", skip_ws: false, ct: "text/plain; charset=utf-8" },
    // Pictures.
    Sig::Exact(b"\x00\x00\x01\x00", "image/x-icon"),
    Sig::Exact(b"\x00\x00\x02\x00", "image/x-icon"),
    Sig::Exact(b"BM", "image/bmp"),
    Sig::Exact(b"GIF87a", "image/gif"),
    Sig::Exact(b"GIF89a", "image/gif"),
    Sig::Masked {
        mask: b"\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF\xFF\xFF",
        pat: b"RIFF\x00\x00\x00\x00WEBPVP",
        skip_ws: false,
        ct: "image/webp",
    },
    Sig::Exact(b"\x89PNG\x0D\x0A\x1A\x0A", "image/png"),
    Sig::Exact(b"\xFF\xD8\xFF", "image/jpeg"),
    // Sound and video.
    Sig::Masked { mask: b"\xFF\xFF\xFF\xFF", pat: b".snd", skip_ws: false, ct: "audio/basic" },
    Sig::Masked {
        mask: b"\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF",
        pat: b"FORM\x00\x00\x00\x00AIFF",
        skip_ws: false,
        ct: "audio/aiff",
    },
    Sig::Masked { mask: b"\xFF\xFF\xFF", pat: b"ID3", skip_ws: false, ct: "audio/mpeg" },
    Sig::Masked { mask: b"\xFF\xFF\xFF\xFF\xFF", pat: b"OggS\x00", skip_ws: false, ct: "application/ogg" },
    Sig::Masked {
        mask: b"\xFF\xFF\xFF\xFF\xFF\xFF\xFF\xFF",
        pat: b"MThd\x00\x00\x00\x06",
        skip_ws: false,
        ct: "audio/midi",
    },
    Sig::Masked {
        mask: b"\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF",
        pat: b"RIFF\x00\x00\x00\x00AVI ",
        skip_ws: false,
        ct: "video/avi",
    },
    Sig::Masked {
        mask: b"\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF",
        pat: b"RIFF\x00\x00\x00\x00WAVE",
        skip_ws: false,
        ct: "audio/wave",
    },
    Sig::Mp4,
    Sig::Exact(b"\x1A\x45\xDF\xA3", "video/webm"),
    // Fonts.
    Sig::Masked { mask: &EOT_MASK, pat: &EOT_PAT, skip_ws: false, ct: "application/vnd.ms-fontobject" },
    Sig::Exact(b"\x00\x01\x00\x00", "font/ttf"),
    Sig::Exact(b"OTTO", "font/otf"),
    Sig::Exact(b"ttcf", "font/collection"),
    Sig::Exact(b"wOFF", "font/woff"),
    Sig::Exact(b"wOF2", "font/woff2"),
    // Archives.
    Sig::Exact(b"\x1F\x8B\x08", "application/x-gzip"),
    Sig::Exact(b"PK\x03\x04", "application/zip"),
    Sig::Exact(b"Rar!\x1A\x07\x00", "application/x-rar-compressed"),
    Sig::Exact(b"Rar!\x1A\x07\x01\x00", "application/x-rar-compressed"),
    Sig::Exact(b"\x00\x61\x73\x6D", "application/wasm"),
    Sig::Text,
];

impl Sig {
    fn matches(&self, data: &[u8], first_non_ws: usize) -> Option<&'static str> {
        match self {
            Sig::Html(tag) => {
                let data = &data[first_non_ws..];
                if data.len() < tag.len() + 1 {
                    return None;
                }
                for (i, &b) in tag.iter().enumerate() {
                    let mut d = data[i];
                    if b.is_ascii_uppercase() {
                        d &= 0xDF;
                    }
                    if b != d {
                        return None;
                    }
                }
                // Then a tag-terminating byte.
                matches!(data[tag.len()], b' ' | b'>').then_some("text/html; charset=utf-8")
            }
            Sig::Masked { mask, pat, skip_ws, ct } => {
                let data = if *skip_ws { &data[first_non_ws..] } else { data };
                if pat.len() != mask.len() || data.len() < pat.len() {
                    return None;
                }
                pat.iter()
                    .zip(mask.iter())
                    .zip(data)
                    .all(|((p, m), d)| d & m == *p)
                    .then_some(*ct)
            }
            Sig::Exact(sig, ct) => data.starts_with(sig).then_some(*ct),
            Sig::Mp4 => {
                if data.len() < 12 {
                    return None;
                }
                let box_size = u32::from_be_bytes([data[0], data[1], data[2], data[3]]) as usize;
                if data.len() < box_size || !box_size.is_multiple_of(4) || &data[4..8] != b"ftyp" {
                    return None;
                }
                // Brands from byte 8 on, skipping the major brand's version (12).
                (8..box_size)
                    .step_by(4)
                    .any(|st| st != 12 && &data[st..st + 3] == b"mp4")
                    .then_some("video/mp4")
            }
            Sig::Text => data[first_non_ws..]
                .iter()
                .all(|&b| !(b <= 0x08 || b == 0x0B || (0x0E..=0x1A).contains(&b) || (0x1C..=0x1F).contains(&b)))
                .then_some("text/plain; charset=utf-8"),
        }
    }
}

/// Go's http.DetectContentType: looks at no more than the first 512 bytes and
/// always names a type ("application/octet-stream" when nothing matches).
pub(crate) fn detect_content_type(data: &[u8]) -> &'static str {
    let data = &data[..data.len().min(512)];
    let first_non_ws = data
        .iter()
        .position(|b| !matches!(b, b'\t' | b'\n' | b'\x0c' | b'\r' | b' '))
        .unwrap_or(data.len());
    SIGNATURES
        .iter()
        .find_map(|sig| sig.matches(data, first_non_ws))
        .unwrap_or(OCTET_STREAM)
}

/// fastIgnoreDirs (internal/fsext/ls.go:20-41): folders the terminal never lists.
const IGNORED_FOLDERS: &[&str] = &[
    ".git",
    ".svn",
    ".hg",
    ".bzr",
    ".vscode",
    ".idea",
    "node_modules",
    "__pycache__",
    ".pytest_cache",
    ".cache",
    ".tmp",
    ".Trash",
    ".Spotlight-V100",
    ".fseventsd",
    ".crush",
    "OrbStack",
    ".local",
    ".share",
];

/// commonIgnorePatterns (ls.go:43-83): files and folders with these names, at
/// any depth, are never listed ("*" stands for any start of the name).
const IGNORED_NAMES: &[&str] = &[
    "*.swp",
    "*.swo",
    "*~",
    ".DS_Store",
    "Thumbs.db",
    "target",
    "build",
    "dist",
    "out",
    "bin",
    "obj",
    "*.o",
    "*.so",
    "*.dylib",
    "*.dll",
    "*.exe",
    "*.log",
    "*.tmp",
    "*.temp",
    "*.pyc",
    "*.pyo",
    "vendor",
    "Cargo.lock",
    "package-lock.json",
    "yarn.lock",
    "pnpm-lock.yaml",
];

fn ignored_name(name: &str) -> bool {
    IGNORED_NAMES.iter().any(|pattern| match pattern.strip_prefix('*') {
        Some(ending) => name.ends_with(ending),
        None => name == *pattern,
    })
}

fn ignored_folder(name: &str) -> bool {
    IGNORED_FOLDERS.contains(&name) || ignored_name(name)
}

/// The terminal sorts its list as strings, with a "/" after each folder.
fn sort_key(entry: &FileEntry) -> String {
    if entry.is_dir {
        format!("{}/", entry.path)
    } else {
        entry.path.clone()
    }
}

/// How long the Git list may take before the walk is used instead.
const GIT_WAIT: Duration = Duration::from_secs(2);

/// `git ls-files -z --cached --others --exclude-standard` in `root`: every
/// tracked file and every untracked file the ignore files do not exclude, as
/// paths relative to `root`. None outside a Git work tree, without git, on any
/// failure, or after 2 s.
fn git_files(root: &Path) -> Option<Vec<String>> {
    let mut command = Command::new("git");
    command
        .arg("-C")
        .arg(root)
        .args(["ls-files", "-z", "--cached", "--others", "--exclude-standard"])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    // Only the folder decides which repository is listed.
    for var in [
        "GIT_DIR",
        "GIT_WORK_TREE",
        "GIT_INDEX_FILE",
        "GIT_OBJECT_DIRECTORY",
        "GIT_ALTERNATE_OBJECT_DIRECTORIES",
        "GIT_COMMON_DIR",
        "GIT_NAMESPACE",
        "GIT_PREFIX",
    ] {
        command.env_remove(var);
    }
    let mut child = command.spawn().ok()?;
    let mut stdout = child.stdout.take()?;
    let reader = std::thread::spawn(move || {
        let mut out = Vec::new();
        let _ = stdout.read_to_end(&mut out);
        out
    });
    let deadline = Instant::now() + GIT_WAIT;
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => std::thread::sleep(Duration::from_millis(5)),
            _ => {
                let _ = child.kill();
                let _ = child.wait();
                return None;
            }
        }
    };
    let out = reader.join().ok()?;
    if !status.success() {
        return None;
    }
    Some(
        out.split(|&b| b == 0)
            .filter(|path| !path.is_empty())
            .filter_map(|path| std::str::from_utf8(path).ok().map(str::to_string))
            .collect(),
    )
}

/// An entry with its depth (the number of path parts), before sorting.
struct Found {
    entry: FileEntry,
    depth: usize,
}

/// The Git list as entries: each file (still on disk and not left out by the
/// terminal's names) and the folders above it, down to max_depth. Over
/// max_items, the shallowest entries are kept, as a walk would reach them first.
fn from_git(root: &Path, files: Vec<String>, max_depth: usize, max_items: usize) -> Vec<FileEntry> {
    let within = |depth: usize| max_depth == 0 || depth <= max_depth;
    let mut seen: HashSet<String> = HashSet::new();
    let mut found: Vec<Found> = Vec::new();
    for file in files {
        let parts: Vec<&str> = file.split('/').collect();
        if parts.iter().any(|p| p.is_empty()) {
            continue;
        }
        let (folders, name) = parts.split_at(parts.len() - 1);
        if folders.iter().any(|f| ignored_folder(f)) {
            continue;
        }
        let full = root.join(&file);
        // A tracked file deleted since is still in the index.
        if fs::symlink_metadata(&full).is_err() {
            continue;
        }
        // A submodule, or a link to a folder, is a folder.
        let is_dir = fs::metadata(&full).is_ok_and(|m| m.is_dir());
        if ignored_name(name[0]) || (is_dir && ignored_folder(name[0])) {
            continue;
        }
        let depth = parts.len();
        for above in 1..depth {
            if !within(above) {
                break;
            }
            let folder = parts[..above].join("/");
            if seen.insert(folder.clone()) {
                found.push(Found {
                    entry: FileEntry { path: folder, is_dir: true },
                    depth: above,
                });
            }
        }
        if within(depth) && seen.insert(file.clone()) {
            found.push(Found {
                entry: FileEntry { path: file, is_dir },
                depth,
            });
        }
    }
    if max_items > 0 && found.len() > max_items {
        found.sort_by(|a, b| a.depth.cmp(&b.depth).then_with(|| sort_key(&a.entry).cmp(&sort_key(&b.entry))));
        found.truncate(max_items);
    }
    found.into_iter().map(|f| f.entry).collect()
}

/// Outside Git: a breadth-first walk from `root` (folders first in each
/// folder, then by name), skipping hidden folders and the names the terminal
/// never lists, following links to folders once each, until max_items.
fn walk(root: &Path, max_depth: usize, max_items: usize) -> Vec<FileEntry> {
    let mut out = Vec::new();
    let mut visited: HashSet<(u64, u64)> = HashSet::new();
    if let Ok(meta) = fs::metadata(root) {
        visited.insert((meta.dev(), meta.ino()));
    }
    let mut queue = VecDeque::from([(root.to_path_buf(), String::new(), 0usize)]);
    while let Some((dir, rel, depth)) = queue.pop_front() {
        let Ok(read) = fs::read_dir(&dir) else {
            continue; // a folder the user may not read is skipped, as in the terminal
        };
        let mut items: Vec<(String, bool, PathBuf)> = read
            .filter_map(|entry| {
                let entry = entry.ok()?;
                let name = entry.file_name().into_string().ok()?;
                let path = entry.path();
                let kind = entry.file_type().ok()?;
                let is_dir = if kind.is_symlink() {
                    fs::metadata(&path).is_ok_and(|m| m.is_dir())
                } else {
                    kind.is_dir()
                };
                Some((name, is_dir, path))
            })
            .collect();
        items.sort_by(|a, b| b.1.cmp(&a.1).then_with(|| a.0.cmp(&b.0)));
        for (name, is_dir, path) in items {
            if (is_dir && (name.starts_with('.') || ignored_folder(&name))) || ignored_name(&name) {
                continue;
            }
            let rel_path = if rel.is_empty() { name } else { format!("{rel}/{name}") };
            out.push(FileEntry {
                path: rel_path.clone(),
                is_dir,
            });
            if max_items > 0 && out.len() >= max_items {
                return out;
            }
            let entry_depth = depth + 1;
            if is_dir && (max_depth == 0 || entry_depth < max_depth) {
                if let Ok(meta) = fs::metadata(&path) {
                    if visited.insert((meta.dev(), meta.ino())) {
                        queue.push_back((path, rel_path, entry_depth));
                    }
                }
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::net::UnixListener;
    use std::sync::atomic::{AtomicU64, Ordering};

    /// A throwaway folder for one test, removed at the end.
    struct Dir(PathBuf);

    impl Dir {
        fn new(name: &str) -> Dir {
            static N: AtomicU64 = AtomicU64::new(0);
            let dir = std::env::temp_dir().join(format!(
                "crush-desktop-files-{name}-{}-{}",
                std::process::id(),
                N.fetch_add(1, Ordering::Relaxed)
            ));
            let _ = fs::remove_dir_all(&dir);
            fs::create_dir_all(&dir).unwrap();
            Dir(dir)
        }

        fn put(&self, rel: &str, content: &[u8]) {
            let path = self.0.join(rel);
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            fs::write(path, content).unwrap();
        }

        fn cwd(&self) -> String {
            self.0.display().to_string()
        }
    }

    impl Drop for Dir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    const PNG: &[u8] = b"\x89PNG\x0D\x0A\x1A\x0A\x00\x00\x00\x0DIHDR\x00\x00\x00\x01";

    fn git(dir: &Path, args: &[&str]) {
        let ok = Command::new("git")
            .arg("-C")
            .arg(dir)
            .args(args)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .unwrap()
            .success();
        assert!(ok, "git {args:?} failed");
    }

    fn paths(entries: &[FileEntry]) -> Vec<String> {
        entries.iter().map(sort_key).collect()
    }

    /// Go's http.DetectContentType answers for the same bytes.
    #[test]
    fn content_types_match_gos_sniffer() {
        let mut mp4 = b"\x00\x00\x00\x20ftypisom\x00\x00\x02\x00isomiso2avc1mp41".to_vec();
        mp4.extend_from_slice(&[0; 8]);
        // The brand "mp41" lies past this box's end (0x18 bytes), so it does not count.
        let mut not_mp4 = b"\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2mp41".to_vec();
        not_mp4.extend_from_slice(&[0; 8]);
        let long_text = "word ".repeat(200);
        let cases: Vec<(&[u8], &str)> = vec![
            (PNG, "image/png"),
            (b"\xFF\xD8\xFF\xE0\x00\x10JFIF", "image/jpeg"),
            (b"GIF89a\x01\x00", "image/gif"),
            (b"GIF87a\x01\x00", "image/gif"),
            (b"RIFF\x24\x00\x00\x00WEBPVP8 ", "image/webp"),
            (b"BM\x3a\x00", "image/bmp"),
            (b"\x00\x00\x01\x00\x01\x00", "image/x-icon"),
            (b"%PDF-1.7\n", "application/pdf"),
            (b"PK\x03\x04\x14\x00", "application/zip"),
            (b"\x1F\x8B\x08\x00", "application/x-gzip"),
            (b"\x00asm\x01\x00\x00\x00", "application/wasm"),
            (b"OggS\x00\x02", "application/ogg"),
            (b"ID3\x04\x00", "audio/mpeg"),
            (b"RIFF\x24\x00\x00\x00WAVEfmt ", "audio/wave"),
            (b"\x1A\x45\xDF\xA3\x9F", "video/webm"),
            (&mp4, "video/mp4"),
            (&not_mp4, "application/octet-stream"),
            (b"wOF2\x00\x01", "font/woff2"),
            (b"  <!DOCTYPE html>\n<html>", "text/html; charset=utf-8"),
            (b"<html><body>", "text/html; charset=utf-8"),
            (b"<p>x</p>", "text/html; charset=utf-8"),
            (b"<pre>x</pre>", "text/plain; charset=utf-8"),
            (b"\n<?xml version=\"1.0\"?>", "text/xml; charset=utf-8"),
            (b"\xEF\xBB\xBFhello", "text/plain; charset=utf-8"),
            (b"\xFF\xFEh\x00i\x00", "text/plain; charset=utf-16le"),
            (b"package main\n\nfunc main() {}\n", "text/plain; charset=utf-8"),
            (long_text.as_bytes(), "text/plain; charset=utf-8"),
            ("caf\u{e9} \u{1F600}\n".as_bytes(), "text/plain; charset=utf-8"),
            (b"", "text/plain; charset=utf-8"),
            (b"\x00\x01\x02\x03", "application/octet-stream"),
            (b"text then \x1a, a control byte", "application/octet-stream"),
            // ESC (0x1b) is allowed in text, so colored program output is text.
            (b"colored \x1b[31mred\x1b[0m\n", "text/plain; charset=utf-8"),
        ];
        for (data, want) in cases {
            assert_eq!(detect_content_type(data), want, "{:?}", String::from_utf8_lossy(data));
        }
        // Only the first 512 bytes count.
        let mut late_binary = vec![b'a'; 512];
        late_binary.push(0);
        assert_eq!(detect_content_type(&late_binary), "text/plain; charset=utf-8");
    }

    #[test]
    fn info_reports_files_folders_and_missing_paths() {
        let dir = Dir::new("info");
        dir.put("pics/dot.png", PNG);
        dir.put("notes.md", b"# Notes\n");
        dir.put("photo.heic", b"\x00\x00\x00\x18ftypheic\x00\x00\x00\x00");
        dir.put("blob.bin", b"\x00\x01\x02\x03");
        let cwd = dir.cwd();
        let png = info("pics/dot.png", &cwd).unwrap();
        assert_eq!((png.exists, png.is_dir, png.size, png.mime.as_str()), (true, false, PNG.len() as u64, "image/png"));
        let modified = fs::metadata(dir.0.join("pics/dot.png")).unwrap().modified().unwrap();
        assert_eq!(png.modified, modified.duration_since(UNIX_EPOCH).unwrap().as_millis() as u64);
        let absolute = dir.0.join("notes.md").display().to_string();
        assert_eq!(info(&absolute, "/").unwrap().mime, "text/plain; charset=utf-8");
        // Content the sniffer cannot name is named by its extension.
        assert_eq!(info("photo.heic", &cwd).unwrap().mime, "image/heic");
        assert_eq!(info("blob.bin", &cwd).unwrap().mime, "application/octet-stream");
        let folder = info("pics", &cwd).unwrap();
        assert_eq!((folder.exists, folder.is_dir, folder.mime.as_str()), (true, true, ""));
        let missing = FileInfo { exists: false, is_dir: false, size: 0, mime: String::new(), modified: 0 };
        assert_eq!(info("nope.png", &cwd).unwrap(), missing);
        assert_eq!(info("notes.md/inside", &cwd).unwrap(), missing, "a path through a file names nothing");
        assert_eq!(info("", &cwd).unwrap_err().code, "bad_path");
        assert_eq!(info("a\0b", &cwd).unwrap_err().code, "bad_path");
        let _socket = UnixListener::bind(dir.0.join("sock")).unwrap();
        let socket = info("sock", &cwd).unwrap();
        assert_eq!((socket.exists, socket.is_dir, socket.mime.as_str()), (true, false, ""));
    }

    #[test]
    fn read_gives_type_and_base64_within_the_limit() {
        let dir = Dir::new("read");
        dir.put("dot.png", PNG);
        dir.put("sub/a.txt", b"hello");
        let cwd = dir.cwd();
        let (mime, data) = read("dot.png", &cwd, 5 * 1024 * 1024).unwrap();
        assert_eq!(mime, "image/png");
        assert_eq!(base64::engine::general_purpose::STANDARD.decode(data).unwrap(), PNG);
        assert_eq!(read("sub/a.txt", &cwd, 5).unwrap(), ("text/plain; charset=utf-8".to_string(), "aGVsbG8=".to_string()));
        let err = read("sub/a.txt", &cwd, 4).unwrap_err();
        assert_eq!(err.code, "too_big", "{}", err.message);
        let err = read("sub", &cwd, 100).unwrap_err();
        assert_eq!((err.code.as_str(), err.message), ("is_a_directory", format!("read {}: is a directory", dir.0.join("sub").display())));
        let _socket = UnixListener::bind(dir.0.join("sock")).unwrap();
        assert_eq!(read("sock", &cwd, 100).unwrap_err().code, "not_a_file");
        let err = read("gone.png", &cwd, 100).unwrap_err();
        let gone = dir.0.join("gone.png");
        assert_eq!(
            (err.code.as_str(), err.message),
            ("bad_path", format!("stat {}: no such file or directory", gone.display()))
        );
        assert_eq!(read("", &cwd, 100).unwrap_err().code, "bad_path");
    }

    #[test]
    fn list_in_git_applies_the_ignore_files() {
        let dir = Dir::new("git");
        git(&dir.0, &["init", "-q"]);
        dir.put(".gitignore", b"secret.env\nlogs/\n");
        dir.put("README.md", b"# x\n");
        dir.put("src/main.go", b"package main\n");
        dir.put("src/deep/x/y.go", b"package x\n");
        dir.put("secret.env", b"KEY=1\n");
        dir.put("logs/today.txt", b"x\n");
        dir.put("notes.log", b"x\n");
        dir.put("node_modules/pkg/index.js", b"x\n");
        dir.put("build/out.txt", b"x\n");
        dir.put(".github/ci.yml", b"on: push\n");
        dir.put("gone.txt", b"x\n");
        git(&dir.0, &["add", "README.md", "gone.txt"]);
        fs::remove_file(dir.0.join("gone.txt")).unwrap(); // still in the index, gone from disk
        assert!(git_files(&dir.0).is_some(), "git ls-files did not run in a fresh repository");
        let all = list(&dir.cwd(), 0, 0).unwrap();
        assert_eq!(
            paths(&all),
            [".github/", ".github/ci.yml", ".gitignore", "README.md", "src/", "src/deep/", "src/deep/x/", "src/deep/x/y.go", "src/main.go"],
        );
        // A subfolder lists its own files, relative to itself.
        let sub = list(&dir.0.join("src").display().to_string(), 0, 0).unwrap();
        assert_eq!(paths(&sub), ["deep/", "deep/x/", "deep/x/y.go", "main.go"]);
    }

    #[test]
    fn list_in_git_keeps_depth_and_item_limits() {
        let dir = Dir::new("gitlimits");
        git(&dir.0, &["init", "-q"]);
        dir.put("a.txt", b"x");
        dir.put("src/main.go", b"x");
        dir.put("src/deep/x/y.go", b"x");
        dir.put("z/z1.txt", b"x");
        let cwd = dir.cwd();
        assert_eq!(paths(&list(&cwd, 1, 0).unwrap()), ["a.txt", "src/", "z/"]);
        assert_eq!(
            paths(&list(&cwd, 2, 0).unwrap()),
            ["a.txt", "src/", "src/deep/", "src/main.go", "z/", "z/z1.txt"]
        );
        // The shallowest entries are kept first.
        assert_eq!(paths(&list(&cwd, 0, 4).unwrap()), ["a.txt", "src/", "src/deep/", "z/"]);
        assert_eq!(list(&cwd, 0, 3).unwrap().len(), 3);
    }

    #[test]
    fn walk_outside_git_skips_hidden_folders_and_keeps_limits() {
        let dir = Dir::new("walk");
        dir.put("a.txt", b"x");
        dir.put(".hidden/x.txt", b"x");
        dir.put(".env", b"x");
        dir.put("node_modules/m/i.js", b"x");
        dir.put("sub/b.txt", b"x");
        dir.put("sub/c.tmp", b"x");
        dir.put("sub/deeper/d.txt", b"x");
        std::os::unix::fs::symlink(&dir.0, dir.0.join("sub/loop")).unwrap(); // a link back up
        assert!(git_files(&dir.0).is_none(), "the walk test folder must not be inside a Git work tree");
        let cwd = dir.cwd();
        let all = list(&cwd, 0, 0).unwrap();
        assert!(!paths(&all).iter().any(|p| p.starts_with(".hidden") || p.starts_with("node_modules")), "{all:?}");
        assert!(paths(&all).contains(&".env".to_string()), "hidden files stay: {all:?}");
        assert!(paths(&all).contains(&"sub/deeper/d.txt".to_string()), "{all:?}");
        assert!(!paths(&all).iter().any(|p| p.ends_with(".tmp")), "{all:?}");
        assert_eq!(
            paths(&list(&cwd, 1, 0).unwrap()),
            [".env", "a.txt", "sub/"],
            "max_depth 1 lists only the folder's own entries"
        );
        assert_eq!(
            paths(&list(&cwd, 2, 0).unwrap()),
            [".env", "a.txt", "sub/", "sub/b.txt", "sub/deeper/", "sub/loop/"]
        );
        // Breadth first, folders first: the first three entries met.
        assert_eq!(paths(&list(&cwd, 0, 3).unwrap()), [".env", "a.txt", "sub/"]);
        assert_eq!(list(&cwd, 0, 5).unwrap().len(), 5);
        assert_eq!(list("/nonexistent-folder-for-a-test", 0, 0).unwrap_err().code, "bad_path");
    }
}
