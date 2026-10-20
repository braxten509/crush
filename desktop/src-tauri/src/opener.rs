//! Opening links after a click (desktop-design 1.2.8, CHAT-102).
//!
//! `classify` is a port of the terminal's linkTarget (internal/ui/model/
//! links.go:19-66), together with the parts of Go's url.Parse it relies on, so
//! the same links open and every refusal reads the same. `open` then starts the
//! desktop's opener the way the terminal's openDesktopLink does
//! (links_linux.go:13-26, links_other.go:7-9), or in probe mode only records the
//! request, so a test never shows a browser.

use crate::error::{AppError, AppResult};
use crate::files::go_error_text;
use crate::state::{append_json_line, AppState};
use std::ffi::{OsStr, OsString};
use std::os::unix::ffi::{OsStrExt, OsStringExt};
use std::os::unix::fs::PermissionsExt;
use std::os::unix::process::{CommandExt, ExitStatusExt};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

#[derive(Debug, PartialEq)]
pub enum LinkKind {
    Web,
    File(std::path::PathBuf),
}

/// Same rules and texts as the terminal (CHECKLIST.md:211): http, https and mailto are web links;
/// file links are relative to `cwd`, may start with `~`, and a trailing `:line:col` is dropped
/// when no file has that exact name. Refusals: "cannot open links of type <scheme>",
/// "cannot open a file on another computer", "link does not name a local file".
pub fn classify(url: &str, cwd: &str) -> AppResult<LinkKind> {
    let home = std::env::var_os("HOME");
    link_target(url, Path::new(cwd), home.as_deref()).map_err(|why| AppError::new("link_refused", why))
}

/// Linux: the first of xdg-open, x-www-browser, www-browser that exists, detached; Mac: `open`.
/// File links use the same opener, which opens the file in the program the desktop assigns to
/// it (inferred reading of "your editor"; a terminal-tab editor is Ctrl+O, phase 6).
/// When the app runs with --probe, nothing is started: the request is appended as one JSON line
/// to "<probe file>.opened.jsonl" (so a test never shows a browser).
pub fn open(app: &crate::host::AppHandle, url: &str, cwd: &str) -> AppResult<()> {
    let probe_log = app
        .try_state::<AppState>()
        .and_then(|state| state.launch.probe_side_file("opened.jsonl"));
    open_with(url, cwd, probe_log.as_deref(), launch)
}

/// `open` with its two outside effects passed in: the probe's side file, and
/// the launcher (tests give a fake one).
fn open_with(
    url: &str,
    cwd: &str,
    probe_log: Option<&Path>,
    launch: impl FnOnce(&str) -> Result<(), String>,
) -> AppResult<()> {
    let kind = classify(url, cwd)?;
    // What the opener gets: a web link as written, a file as its file URL.
    let (kind_name, target) = match &kind {
        LinkKind::Web => ("web", url.to_string()),
        LinkKind::File(path) => ("file", file_url(path)),
    };
    if let Some(log) = probe_log {
        append_json_line(
            log,
            &serde_json::json!({"url": url, "kind": kind_name, "target": target}),
        );
        return Ok(());
    }
    launch(&target).map_err(|why| AppError::new("open_failed", why))
}

/// The terminal's linkTarget. Err holds the terminal's error text.
fn link_target(destination: &str, working_dir: &Path, home: Option<&OsStr>) -> Result<LinkKind, String> {
    let u = go::parse_url(destination).map_err(|e| format!("cannot open link: {e}"))?;
    match u.scheme.as_str() {
        "https" | "http" | "mailto" => return Ok(LinkKind::Web),
        "file" => {
            if !u.host.is_empty() && u.host != b"localhost" {
                return Err("cannot open a file on another computer".to_string());
            }
        }
        "" => {
            if !u.host.is_empty() || u.path.is_empty() {
                return Err("link does not name a local file".to_string());
            }
        }
        other => return Err(format!("cannot open links of type {}", go::quote(other.as_bytes()))),
    }
    let mut path = u.path;
    if path.starts_with(b"~/") {
        // os.UserHomeDir on Unix: $HOME, or an error when it is empty.
        let home = home
            .filter(|h| !h.is_empty())
            .ok_or_else(|| "$HOME is not defined".to_string())?;
        path = go::join(&[home.as_bytes(), &path[2..]]);
    }
    if !path.starts_with(b"/") {
        path = go::join(&[working_dir.as_os_str().as_bytes(), &path]);
    }
    // filepath.Abs.
    path = if path.starts_with(b"/") {
        go::clean(&path)
    } else {
        let here = std::env::current_dir().map_err(|e| format!("getwd: {}", go_error_text(&e)))?;
        go::join(&[here.as_os_str().as_bytes(), &path])
    };
    // Keep actual file names containing colons. Only remove an editor
    // line/column suffix when that exact path does not exist.
    if matches!(std::fs::metadata(OsStr::from_bytes(&path)), Err(e) if e.kind() == std::io::ErrorKind::NotFound) {
        path = strip_line_suffix(&path);
    }
    if let Err(e) = std::fs::metadata(OsStr::from_bytes(&path)) {
        let shown = String::from_utf8_lossy(&path);
        return Err(format!("cannot open {shown}: stat {shown}: {}", go_error_text(&e)));
    }
    Ok(LinkKind::File(PathBuf::from(OsString::from_vec(path))))
}

/// Removes the terminal's linkLineSuffix, `:[0-9]+(?::[0-9]+)?$`, at its
/// leftmost match, as Go's regexp does.
fn strip_line_suffix(path: &[u8]) -> Vec<u8> {
    let digits = |s: &[u8]| !s.is_empty() && s.iter().all(u8::is_ascii_digit);
    for (i, &b) in path.iter().enumerate() {
        if b != b':' {
            continue;
        }
        let rest = &path[i + 1..];
        let matched = match rest.iter().position(|&c| c == b':') {
            None => digits(rest),
            Some(j) => digits(&rest[..j]) && digits(&rest[j + 1..]),
        };
        if matched {
            return path[..i].to_vec();
        }
    }
    path.to_vec()
}

/// `(&url.URL{Scheme: "file", Path: path}).String()`.
fn file_url(path: &Path) -> String {
    format!("file://{}", go::escape_path(path.as_os_str().as_bytes()))
}

/// How long a click waits for the opener's answer. The terminal waits for the
/// opener to end; a desktop opener hands the link on and ends at once, and
/// one that keeps running past this has taken the link.
const OPENER_WAIT: Duration = Duration::from_secs(3);

fn launch(target: &str) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    const PROGRAMS: &[&str] = &["open"];
    #[cfg(not(target_os = "macos"))]
    const PROGRAMS: &[&str] = &["xdg-open", "x-www-browser", "www-browser"];
    launch_with(target, PROGRAMS, std::env::var_os("PATH").as_deref(), OPENER_WAIT)
}

/// Starts the first of `programs` found on `path_env` with the target, its
/// streams on /dev/null and in its own process group, so nothing it prints
/// reaches the app's output and nothing sent to the app's group reaches it.
fn launch_with(target: &str, programs: &[&str], path_env: Option<&OsStr>, wait: Duration) -> Result<(), String> {
    let Some(program) = programs.iter().find_map(|name| look_path(name, path_env)) else {
        return Err(format!(
            "exec: {}: executable file not found in $PATH",
            go::quote(programs.join(",").as_bytes())
        ));
    };
    let mut child = Command::new(&program)
        .arg(target)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .process_group(0)
        .spawn()
        .map_err(|e| format!("could not open link: fork/exec {}: {}", program.display(), go_error_text(&e)))?;
    let deadline = Instant::now() + wait;
    loop {
        match child.try_wait() {
            Ok(Some(status)) if status.success() => return Ok(()),
            Ok(Some(status)) => return Err(format!("could not open link: {}", exit_text(status))),
            Ok(None) if Instant::now() < deadline => std::thread::sleep(Duration::from_millis(10)),
            Ok(None) => {
                // Still running: it has the link. Wait for it elsewhere, so it
                // never stays behind as a zombie.
                std::thread::spawn(move || {
                    let _ = child.wait();
                });
                return Ok(());
            }
            Err(e) => return Err(format!("could not open link: {}", go_error_text(&e))),
        }
    }
}

/// exec.LookPath: an executable regular file in one of PATH's folders.
/// Relative folders are skipped (Go refuses what they find, exec.ErrDot).
fn look_path(name: &str, path_env: Option<&OsStr>) -> Option<PathBuf> {
    std::env::split_paths(path_env?)
        .filter(|dir| dir.is_absolute())
        .map(|dir| dir.join(name))
        .find(|candidate| {
            std::fs::metadata(candidate).is_ok_and(|m| m.is_file() && m.permissions().mode() & 0o111 != 0)
        })
}

/// Go's text for a process that ended badly ("exit status 7", "signal: killed").
fn exit_text(status: std::process::ExitStatus) -> String {
    if let Some(code) = status.code() {
        return format!("exit status {code}");
    }
    const NAMES: [&str; 15] = [
        "hangup",
        "interrupt",
        "quit",
        "illegal instruction",
        "trace/breakpoint trap",
        "aborted",
        "bus error",
        "floating point exception",
        "killed",
        "user defined signal 1",
        "segmentation fault",
        "user defined signal 2",
        "broken pipe",
        "alarm clock",
        "terminated",
    ];
    match status.signal() {
        Some(sig) if (1..=15).contains(&sig) => format!("signal: {}", NAMES[sig as usize - 1]),
        Some(sig) => format!("signal: signal {sig}"),
        None => "exit status unknown".to_string(),
    }
}

/// The parts of Go's net/url, net/netip and path/filepath (Go 1.27.1) that
/// linkTarget uses, ported so the same input gives the same result and the
/// same error text.
mod go {
    /// What url.Parse gives linkTarget: the scheme (lower case), the host and
    /// the path, both unescaped (they may hold any bytes).
    pub struct Url {
        pub scheme: String,
        pub host: Vec<u8>,
        pub path: Vec<u8>,
    }

    #[derive(Clone, Copy, PartialEq)]
    enum Mode {
        Path,
        Host,
        Zone,
        UserPassword,
        Fragment,
    }

    /// url.Parse. Err holds the *url.Error text: `parse "<url>": <why>`.
    pub fn parse_url(raw: &str) -> Result<Url, String> {
        let (u, frag) = match raw.find('#') {
            Some(i) => (&raw[..i], &raw[i + 1..]),
            None => (raw, ""),
        };
        let url = parse(u).map_err(|e| format!("parse {}: {e}", quote(u.as_bytes())))?;
        if !frag.is_empty() {
            unescape(frag.as_bytes(), Mode::Fragment).map_err(|e| format!("parse {}: {e}", quote(raw.as_bytes())))?;
        }
        Ok(url)
    }

    fn parse(raw: &str) -> Result<Url, String> {
        if raw.bytes().any(|b| b < b' ' || b == 0x7f) {
            return Err("net/url: invalid control character in URL".to_string());
        }
        if raw == "*" {
            return Ok(Url {
                scheme: String::new(),
                host: Vec::new(),
                path: b"*".to_vec(),
            });
        }
        let (scheme, mut rest) = scheme(raw)?;
        let scheme = scheme.to_ascii_lowercase();
        if rest.ends_with('?') && rest.matches('?').count() == 1 {
            rest = &rest[..rest.len() - 1];
        } else if let Some(i) = rest.find('?') {
            rest = &rest[..i];
        }
        if !rest.starts_with('/') {
            if !scheme.is_empty() {
                // A rootless path after a scheme is opaque (mailto:a@b): no host, no path.
                return Ok(Url {
                    scheme,
                    host: Vec::new(),
                    path: Vec::new(),
                });
            }
            if rest.split('/').next().unwrap_or("").contains(':') {
                return Err("first path segment in URL cannot contain colon".to_string());
            }
        }
        let mut host = Vec::new();
        if (!scheme.is_empty() || !rest.starts_with("///")) && rest.starts_with("//") {
            let after = &rest[2..];
            let (authority, tail) = match after.find('/') {
                Some(i) => (&after[..i], &after[i..]),
                None => (after, ""),
            };
            host = authority_host(&scheme, authority)?;
            rest = tail;
        }
        let path = unescape(rest.as_bytes(), Mode::Path)?;
        Ok(Url { scheme, host, path })
    }

    /// getScheme.
    fn scheme(raw: &str) -> Result<(&str, &str), String> {
        for (i, c) in raw.bytes().enumerate() {
            match c {
                b'a'..=b'z' | b'A'..=b'Z' => {}
                b'0'..=b'9' | b'+' | b'-' | b'.' => {
                    if i == 0 {
                        return Ok(("", raw));
                    }
                }
                b':' => {
                    if i == 0 {
                        return Err("missing protocol scheme".to_string());
                    }
                    return Ok((&raw[..i], &raw[i + 1..]));
                }
                _ => return Ok(("", raw)),
            }
        }
        Ok(("", raw))
    }

    /// parseAuthority: the host, after checking the user part.
    fn authority_host(scheme: &str, authority: &str) -> Result<Vec<u8>, String> {
        let at = authority.rfind('@');
        let host = host(
            scheme,
            match at {
                Some(i) => &authority[i + 1..],
                None => authority,
            },
        )?;
        if let Some(i) = at {
            let userinfo = &authority[..i];
            let allowed = |c: char| {
                c.is_ascii_alphanumeric()
                    || matches!(
                        c,
                        '-' | '.' | '_' | ':' | '~' | '!' | '$' | '&' | '\'' | '(' | ')' | '*' | '+' | ',' | ';' | '=' | '%' | '@'
                    )
            };
            if !userinfo.chars().all(allowed) {
                return Err("net/url: invalid userinfo".to_string());
            }
            match userinfo.split_once(':') {
                None => {
                    unescape(userinfo.as_bytes(), Mode::UserPassword)?;
                }
                Some((user, password)) => {
                    unescape(user.as_bytes(), Mode::UserPassword)?;
                    unescape(password.as_bytes(), Mode::UserPassword)?;
                }
            }
        }
        Ok(host)
    }

    /// parseHost (Go 1.27.1, net/url/url.go:549-640): host[:port], or an IP
    /// literal in brackets, which must be an IPv6 address.
    fn host(scheme: &str, host: &str) -> Result<Vec<u8>, String> {
        let port_ok = |port: &str| port.is_empty() || (port.starts_with(':') && port[1..].bytes().all(|b| b.is_ascii_digit()));
        let bad_port = |port: &str| format!("invalid port {} after host", quote(port.as_bytes()));
        match host.rfind('[') {
            Some(0) => {
                let Some(close) = host.rfind(']') else {
                    return Err("missing ']' in host".to_string());
                };
                let colon_port = &host[close + 1..];
                if !port_ok(colon_port) {
                    return Err(bad_port(colon_port));
                }
                // "%25" starts an IPv6 zone, which may use more escapes than a host.
                let name = &host.as_bytes()[1..close];
                let mut unescaped = match name.windows(3).position(|w| w == b"%25") {
                    Some(zone) => {
                        let mut part = unescape(&name[..zone], Mode::Host)?;
                        part.extend(unescape(&name[zone..], Mode::Zone)?);
                        part
                    }
                    None => unescape(name, Mode::Host)?,
                };
                if parse_addr(&unescaped).map_err(|e| format!("invalid host: {e}"))? {
                    // Only an IPv6 address may stand in brackets.
                    return Err("invalid IP-literal".to_string());
                }
                let mut out = b"[".to_vec();
                out.append(&mut unescaped);
                out.push(b']');
                out.extend_from_slice(colon_port.as_bytes());
                return Ok(out);
            }
            Some(_) => return Err("invalid IP-literal".to_string()),
            None => {}
        }
        if let (Some(first), Some(last)) = (host.find(':'), host.rfind(':')) {
            // Since Go 1.26 a second colon is an error for http and https
            // (GODEBUG urlstrictcolons, on for Crush's "go 1.27.0"); other
            // schemes take the port after the last colon.
            let i = if scheme == "http" || scheme == "https" { first } else { last };
            if !port_ok(&host[i..]) {
                return Err(bad_port(&host[i..]));
            }
        }
        unescape(host.as_bytes(), Mode::Host)
    }

    /// netip.ParseAddr (Go 1.27.1, net/netip/netip.go:114-343): Ok(true) for
    /// an IPv4 address, Ok(false) for IPv6; Err holds Go's text.
    fn parse_addr(s: &[u8]) -> Result<bool, String> {
        for &c in s {
            match c {
                b'.' => return parse_ipv4_fields(s, 0, s.len()).map(|()| true),
                b':' => return parse_ipv6(s).map(|()| false),
                b'%' => return Err(addr_error(s, "missing IPv6 address", b"")),
                _ => {}
            }
        }
        Err(addr_error(s, "unable to parse IP", b""))
    }

    /// parseAddrError's text.
    fn addr_error(input: &[u8], msg: &str, at: &[u8]) -> String {
        if at.is_empty() {
            format!("ParseAddr({}): {msg}", quote(input))
        } else {
            format!("ParseAddr({}): {msg} (at {})", quote(input), quote(at))
        }
    }

    /// parseIPv4Fields: input[off..end] as four decimal fields.
    fn parse_ipv4_fields(input: &[u8], off: usize, end: usize) -> Result<(), String> {
        let s = &input[off..end];
        let (mut val, mut pos, mut digits) = (0u32, 0, 0);
        for (i, &c) in s.iter().enumerate() {
            if c.is_ascii_digit() {
                if digits == 1 && val == 0 {
                    return Err(addr_error(input, "IPv4 field has octet with leading zero", b""));
                }
                val = val * 10 + u32::from(c - b'0');
                digits += 1;
                if val > 255 {
                    return Err(addr_error(input, "IPv4 field has value >255", b""));
                }
            } else if c == b'.' {
                if i == 0 || i == s.len() - 1 || s[i - 1] == b'.' {
                    return Err(addr_error(input, "IPv4 field must have at least one digit", &s[i..]));
                }
                if pos == 3 {
                    return Err(addr_error(input, "IPv4 address too long", b""));
                }
                pos += 1;
                val = 0;
                digits = 0;
            } else {
                return Err(addr_error(input, "unexpected character", &s[i..]));
            }
        }
        if pos < 3 {
            return Err(addr_error(input, "IPv4 address too short", b""));
        }
        Ok(())
    }

    /// parseIPv6: groups of up to four hex digits, at most one "::", an
    /// optional IPv4 tail and an optional non-empty "%zone".
    fn parse_ipv6(input: &[u8]) -> Result<(), String> {
        let mut s = input;
        let mut zone: &[u8] = b"";
        if let Some(i) = s.iter().position(|&c| c == b'%') {
            (s, zone) = (&s[..i], &s[i + 1..]);
            if zone.is_empty() {
                return Err(addr_error(input, "zone must be a non-empty string", b""));
            }
        }
        let mut ellipsis = None;
        if s.starts_with(b"::") {
            ellipsis = Some(0);
            s = &s[2..];
            if s.is_empty() {
                return Ok(());
            }
        }
        let mut i = 0;
        while i < 16 {
            let (mut off, mut acc) = (0, 0u32);
            while off < s.len() {
                let Some(digit) = char::from(s[off]).to_digit(16) else {
                    break;
                };
                acc = (acc << 4) + digit;
                if off > 3 {
                    return Err(addr_error(input, "each group must have 4 or less digits", s));
                }
                if acc > 0xFFFF {
                    return Err(addr_error(input, "IPv6 field has value >=2^16", s));
                }
                off += 1;
            }
            if off == 0 {
                return Err(addr_error(input, "each colon-separated field must have at least one digit", s));
            }
            if off < s.len() && s[off] == b'.' {
                if ellipsis.is_none() && i != 12 {
                    return Err(addr_error(
                        input,
                        "embedded IPv4 address must replace the final 2 fields of the address",
                        s,
                    ));
                }
                if i + 4 > 16 {
                    return Err(addr_error(
                        input,
                        "too many hex fields to fit an embedded IPv4 at the end of the address",
                        s,
                    ));
                }
                let end = if zone.is_empty() { input.len() } else { input.len() - zone.len() - 1 };
                parse_ipv4_fields(input, end - s.len(), end)?;
                s = b"";
                i += 4;
                break;
            }
            i += 2;
            s = &s[off..];
            if s.is_empty() {
                break;
            }
            if s[0] != b':' {
                return Err(addr_error(input, "unexpected character, want colon", s));
            } else if s.len() == 1 {
                return Err(addr_error(input, "colon must be followed by more characters", s));
            }
            s = &s[1..];
            if s[0] == b':' {
                if ellipsis.is_some() {
                    return Err(addr_error(input, "multiple :: in address", s));
                }
                ellipsis = Some(i);
                s = &s[1..];
                if s.is_empty() {
                    break;
                }
            }
        }
        if !s.is_empty() {
            return Err(addr_error(input, "trailing garbage after address", s));
        }
        if i < 16 {
            if ellipsis.is_none() {
                return Err(addr_error(input, "address string too short", b""));
            }
        } else if ellipsis.is_some() {
            return Err(addr_error(input, "the :: must expand to at least one field of zeros", b""));
        }
        Ok(())
    }

    fn unhex(c: u8) -> u8 {
        match c {
            b'0'..=b'9' => c - b'0',
            b'a'..=b'f' => c - b'a' + 10,
            b'A'..=b'F' => c - b'A' + 10,
            _ => 0,
        }
    }

    /// unescape: checks every escape (and, in a host, every character), then decodes.
    fn unescape(s: &[u8], mode: Mode) -> Result<Vec<u8>, String> {
        let mut i = 0;
        while i < s.len() {
            if s[i] == b'%' {
                if i + 2 >= s.len() || !s[i + 1].is_ascii_hexdigit() || !s[i + 2].is_ascii_hexdigit() {
                    let end = (i + 3).min(s.len());
                    return Err(format!("invalid URL escape {}", quote(&s[i..end])));
                }
                let escape = &s[i..i + 3];
                // In a host, %-escapes may only stand for non-ASCII bytes (or %25 in a zone).
                if mode == Mode::Host && unhex(s[i + 1]) < 8 && escape != b"%25" {
                    return Err(format!("invalid URL escape {}", quote(escape)));
                }
                if mode == Mode::Zone {
                    let v = (unhex(s[i + 1]) << 4) | unhex(s[i + 2]);
                    if escape != b"%25" && v != b' ' && should_escape(v, Mode::Host) {
                        return Err(format!("invalid URL escape {}", quote(escape)));
                    }
                }
                i += 3;
            } else {
                if matches!(mode, Mode::Host | Mode::Zone) && s[i] < 0x80 && should_escape(s[i], mode) {
                    return Err(format!("invalid character {} in host name", quote(&s[i..i + 1])));
                }
                i += 1;
            }
        }
        let mut out = Vec::with_capacity(s.len());
        let mut i = 0;
        while i < s.len() {
            if s[i] == b'%' {
                out.push((unhex(s[i + 1]) << 4) | unhex(s[i + 2]));
                i += 3;
            } else {
                out.push(s[i]);
                i += 1;
            }
        }
        Ok(out)
    }

    /// shouldEscape, for the modes linkTarget meets.
    fn should_escape(c: u8, mode: Mode) -> bool {
        if c.is_ascii_alphanumeric() {
            return false;
        }
        if matches!(mode, Mode::Host | Mode::Zone)
            && matches!(
                c,
                b'!' | b'$' | b'&' | b'\'' | b'(' | b')' | b'*' | b'+' | b',' | b';' | b'=' | b':' | b'[' | b']' | b'<' | b'>' | b'"'
            )
        {
            return false;
        }
        match c {
            b'-' | b'_' | b'.' | b'~' => false,
            b'$' | b'&' | b'+' | b',' | b'/' | b':' | b';' | b'=' | b'?' | b'@' => match mode {
                Mode::Path => c == b'?',
                Mode::UserPassword => matches!(c, b'@' | b'/' | b'?' | b':'),
                Mode::Fragment => false,
                Mode::Host | Mode::Zone => true,
            },
            b'!' | b'(' | b')' | b'*' => mode != Mode::Fragment,
            _ => true,
        }
    }

    /// The escaping of URL.EscapedPath (mode encodePath), upper-case hex.
    pub fn escape_path(path: &[u8]) -> String {
        const HEX: &[u8; 16] = b"0123456789ABCDEF";
        let mut out = String::with_capacity(path.len());
        for &c in path {
            if should_escape(c, Mode::Path) {
                out.push('%');
                out.push(HEX[(c >> 4) as usize] as char);
                out.push(HEX[(c & 15) as usize] as char);
            } else {
                out.push(c as char);
            }
        }
        out
    }

    /// strconv.Quote, for the texts in Go's errors.
    pub fn quote(s: &[u8]) -> String {
        let mut out = String::from("\"");
        for chunk in s.utf8_chunks() {
            for c in chunk.valid().chars() {
                match c {
                    '"' => out.push_str("\\\""),
                    '\\' => out.push_str("\\\\"),
                    '\x07' => out.push_str("\\a"),
                    '\x08' => out.push_str("\\b"),
                    '\x0c' => out.push_str("\\f"),
                    '\n' => out.push_str("\\n"),
                    '\r' => out.push_str("\\r"),
                    '\t' => out.push_str("\\t"),
                    '\x0b' => out.push_str("\\v"),
                    c if (c as u32) < 0x20 || c == '\x7f' => out.push_str(&format!("\\x{:02x}", c as u32)),
                    c if c.is_control() => out.push_str(&format!("\\u{:04x}", c as u32)),
                    c => out.push(c),
                }
            }
            for b in chunk.invalid() {
                out.push_str(&format!("\\x{b:02x}"));
            }
        }
        out.push('"');
        out
    }

    /// filepath.Join: the non-empty parts joined by "/", then cleaned.
    pub fn join(parts: &[&[u8]]) -> Vec<u8> {
        let kept: Vec<&[u8]> = parts.iter().copied().filter(|p| !p.is_empty()).collect();
        if kept.is_empty() {
            return Vec::new();
        }
        clean(&kept.join(&b'/'))
    }

    /// filepath.Clean (Unix): one slash at a time, no "." elements, ".."
    /// removes the element before it, and a rooted path never climbs above "/".
    pub fn clean(path: &[u8]) -> Vec<u8> {
        if path.is_empty() {
            return b".".to_vec();
        }
        let rooted = path[0] == b'/';
        let n = path.len();
        let mut out: Vec<u8> = Vec::with_capacity(n);
        let (mut r, mut dotdot) = (0, 0);
        if rooted {
            out.push(b'/');
            r = 1;
            dotdot = 1;
        }
        while r < n {
            // An empty element, or a "." element.
            if path[r] == b'/' || (path[r] == b'.' && (r + 1 == n || path[r + 1] == b'/')) {
                r += 1;
            } else if path[r] == b'.' && path[r + 1] == b'.' && (r + 2 == n || path[r + 2] == b'/') {
                r += 2;
                if out.len() > dotdot {
                    out.pop();
                    while out.len() > dotdot && out[out.len() - 1] != b'/' {
                        out.pop();
                    }
                    // Drop the separator before the removed element, as Go's
                    // buffer index does.
                    if out.len() > dotdot && out[out.len() - 1] == b'/' {
                        out.pop();
                    }
                } else if !rooted {
                    if !out.is_empty() {
                        out.push(b'/');
                    }
                    out.extend_from_slice(b"..");
                    dotdot = out.len();
                }
            } else {
                if (rooted && out.len() != 1) || (!rooted && !out.is_empty()) {
                    out.push(b'/');
                }
                while r < n && path[r] != b'/' {
                    out.push(path[r]);
                    r += 1;
                }
            }
        }
        if out.is_empty() {
            out.push(b'.');
        }
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::sync::atomic::{AtomicU64, Ordering};

    /// A throwaway folder for one test, removed at the end.
    struct Dir(PathBuf);

    impl Dir {
        fn new(name: &str) -> Dir {
            static N: AtomicU64 = AtomicU64::new(0);
            let dir = std::env::temp_dir().join(format!(
                "crush-desktop-opener-{name}-{}-{}",
                std::process::id(),
                N.fetch_add(1, Ordering::Relaxed)
            ));
            let _ = fs::remove_dir_all(&dir);
            fs::create_dir_all(&dir).unwrap();
            Dir(dir)
        }
    }

    impl Drop for Dir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    /// launch_with on a fake opener the test has just written. Another test
    /// thread that forks while the script is still open for writing makes its
    /// start fail with "text file busy" for a moment; only that is tried again.
    fn launch_fake(target: &str, programs: &[&str], bin: &Path, wait: Duration) -> Result<(), String> {
        for _ in 0..50 {
            match launch_with(target, programs, Some(bin.as_os_str()), wait) {
                Err(e) if e.ends_with("text file busy") => std::thread::sleep(Duration::from_millis(20)),
                other => return other,
            }
        }
        panic!("the fake opener stayed busy");
    }

    fn target(destination: &str, dir: &Path) -> Result<String, String> {
        link_target(destination, dir, Some(OsStr::new("/nonexistent-home"))).map(|kind| match kind {
            LinkKind::Web => destination.to_string(),
            LinkKind::File(path) => file_url(&path),
        })
    }

    /// TestLinkTarget (internal/ui/model/links_test.go:18-48), case by case.
    #[test]
    fn classify_follows_the_terminals_link_target() {
        let dir = Dir::new("target");
        let dir = &dir.0;
        let path = dir.join("A file #1%.txt");
        fs::write(&path, "test").unwrap();
        let file_url_of_path = file_url(&path);
        assert_eq!(
            file_url_of_path,
            format!("file://{}/A%20file%20%231%25.txt", dir.display()),
            "file URLs escape as Go's URL.String does"
        );
        for destination in [
            file_url_of_path.clone(),
            format!("{file_url_of_path}:12:3"),
            format!("{file_url_of_path}#L12"),
            "A%20file%20%231%25.txt".to_string(),
        ] {
            assert_eq!(target(&destination, dir), Ok(file_url_of_path.clone()), "{destination}");
        }
        let dir_url = file_url(dir);
        for destination in [dir.display().to_string(), "./".to_string(), dir_url.clone()] {
            assert_eq!(target(&destination, dir), Ok(dir_url.clone()), "{destination}");
        }
        for destination in ["https://example.com/a?q=b#c", "mailto:a@example.com", "HTTP://EXAMPLE.COM/"] {
            assert_eq!(link_target(destination, dir, None), Ok(LinkKind::Web), "{destination}");
        }
        for destination in ["javascript:alert(1)", "file://another-computer/etc/passwd", "#heading", "./missing.txt"] {
            assert!(target(destination, dir).is_err(), "{destination} must be refused");
        }
        // Existing colon-containing names win over editor line suffixes.
        let colon = dir.join("report:12");
        fs::write(&colon, "").unwrap();
        assert_eq!(target(&colon.display().to_string(), dir), Ok(file_url(&colon)));
        // The regexp's leftmost match: "report:12:3:4" loses ":3:4", not ":12:3:4".
        assert_eq!(target("report:12:3:4", dir).unwrap_err(), "cannot open links of type \"report\"");
        assert_eq!(target("./report:12:3:4", dir), Ok(file_url(&colon)));
        // ...and "report:12:3" loses ":12:3" (leftmost), so it names "report", which is missing.
        let report = dir.join("report");
        assert_eq!(
            target("./report:12:3", dir),
            Err(format!("cannot open {0}: stat {0}: no such file or directory", report.display()))
        );
    }

    #[test]
    fn refusals_read_as_the_terminals() {
        let dir = Dir::new("refusals");
        let dir = &dir.0;
        let missing = dir.join("missing.txt");
        let cases: Vec<(&str, String)> = vec![
            ("javascript:alert(1)", "cannot open links of type \"javascript\"".into()),
            ("JAVASCRIPT:alert(1)", "cannot open links of type \"javascript\"".into()),
            ("vbscript:x", "cannot open links of type \"vbscript\"".into()),
            ("data:text/html,x", "cannot open links of type \"data\"".into()),
            ("ftp://example.com/x", "cannot open links of type \"ftp\"".into()),
            ("file://another-computer/etc/passwd", "cannot open a file on another computer".into()),
            ("#heading", "link does not name a local file".into()),
            ("", "link does not name a local file".into()),
            ("//server/share/x", "link does not name a local file".into()),
            (
                "./missing.txt",
                format!("cannot open {0}: stat {0}: no such file or directory", missing.display()),
            ),
            (
                "./missing.txt:3:4",
                format!("cannot open {0}: stat {0}: no such file or directory", missing.display()),
            ),
            ("%zz", "cannot open link: parse \"%zz\": invalid URL escape \"%zz\"".into()),
            ("a%2", "cannot open link: parse \"a%2\": invalid URL escape \"%2\"".into()),
            ("x.txt#a%zz", "cannot open link: parse \"x.txt#a%zz\": invalid URL escape \"%zz\"".into()),
            ("http://[::1/x", "cannot open link: parse \"http://[::1/x\": missing ']' in host".into()),
            ("http://example.com:8o/", "cannot open link: parse \"http://example.com:8o/\": invalid port \":8o\" after host".into()),
            ("http://exa mple.com/", "cannot open link: parse \"http://exa mple.com/\": invalid character \" \" in host name".into()),
            ("http://a b@example.com/", "cannot open link: parse \"http://a b@example.com/\": net/url: invalid userinfo".into()),
            (":x", "cannot open link: parse \":x\": missing protocol scheme".into()),
            ("java\nscript:alert(1)", "cannot open link: parse \"java\\nscript:alert(1)\": net/url: invalid control character in URL".into()),
            ("a\tb.txt", "cannot open link: parse \"a\\tb.txt\": net/url: invalid control character in URL".into()),
            // IP literals (Go 1.27.1 url.go:549-606): only IPv6 in brackets,
            // checked with netip.ParseAddr, whose words Go passes on.
            ("http://[zz]/", "cannot open link: parse \"http://[zz]/\": invalid host: ParseAddr(\"zz\"): unable to parse IP".into()),
            ("http://a[b]/", "cannot open link: parse \"http://a[b]/\": invalid IP-literal".into()),
            ("http://[1.2.3.4]/", "cannot open link: parse \"http://[1.2.3.4]/\": invalid IP-literal".into()),
            (
                "http://[1::2::3]/",
                "cannot open link: parse \"http://[1::2::3]/\": invalid host: ParseAddr(\"1::2::3\"): multiple :: in address (at \":3\")".into(),
            ),
            (
                "http://[fe80::1%25]/",
                "cannot open link: parse \"http://[fe80::1%25]/\": invalid host: ParseAddr(\"fe80::1%\"): zone must be a non-empty string".into(),
            ),
            // A second colon is an error for http and https only (urlstrictcolons).
            ("http://a:1:2/", "cannot open link: parse \"http://a:1:2/\": invalid port \":1:2\" after host".into()),
            ("file://a:1:2/x", "cannot open a file on another computer".into()),
        ];
        for (destination, want) in cases {
            assert_eq!(target(destination, dir).unwrap_err(), want, "{destination:?}");
        }
        // Valid IPv6 literals, with a port or a zone, are web links.
        for destination in ["http://[::1]:8080/x", "http://[fe80::1%25en0]/", "http://[::ffff:1.2.3.4]/"] {
            assert_eq!(link_target(destination, dir, None), Ok(LinkKind::Web), "{destination}");
        }
        // A first path segment with a colon is a scheme, as in Go.
        assert_eq!(target("notes.md:12", dir).unwrap_err(), "cannot open links of type \"notes.md\"");
        assert_eq!(target("a:b/c", dir).unwrap_err(), "cannot open links of type \"a\"");
        // The command's error carries the code the screens branch on.
        let err = classify("javascript:alert(1)", &dir.display().to_string()).unwrap_err();
        assert_eq!((err.code.as_str(), err.message.as_str()), ("link_refused", "cannot open links of type \"javascript\""));
    }

    #[test]
    fn home_and_localhost_links_name_local_files() {
        let dir = Dir::new("home");
        let dir = &dir.0;
        fs::create_dir_all(dir.join("notes")).unwrap();
        fs::write(dir.join("notes/plan.md"), "x").unwrap();
        let home = Some(dir.as_os_str());
        let elsewhere = Path::new("/");
        assert_eq!(
            link_target("~/notes/plan.md:7", elsewhere, home),
            Ok(LinkKind::File(dir.join("notes/plan.md")))
        );
        assert_eq!(
            link_target("~/notes/plan.md", elsewhere, None).unwrap_err(),
            "$HOME is not defined"
        );
        let url = format!("file://localhost{}", dir.join("notes/plan.md").display());
        assert_eq!(link_target(&url, elsewhere, home), Ok(LinkKind::File(dir.join("notes/plan.md"))));
        assert_eq!(
            link_target("notes/../notes/./plan.md", dir, home),
            Ok(LinkKind::File(dir.join("notes/plan.md"))),
            "paths are cleaned as filepath.Abs does"
        );
    }

    #[test]
    fn clean_matches_go() {
        for (input, want) in [
            ("", "."),
            ("/", "/"),
            ("//a//b/", "/a/b"),
            ("a/./b/../c", "a/c"),
            ("/../a", "/a"),
            ("../../a", "../../a"),
            ("a/../..", ".."),
            ("/a/b/..", "/a"),
            ("/a/b/../..", "/"),
            ("abc/def/../../..", ".."),
            ("./", "."),
        ] {
            assert_eq!(String::from_utf8(go::clean(input.as_bytes())).unwrap(), want, "{input:?}");
        }
        assert_eq!(go::quote(b"a\"b\\c\x01\x7f\xff"), "\"a\\\"b\\\\c\\x01\\x7f\\xff\"");
    }

    #[test]
    fn probe_mode_writes_opened_jsonl() {
        let dir = Dir::new("probe");
        let log = dir.0.join("probe.json.opened.jsonl");
        fs::write(dir.0.join("notes.md"), "x").unwrap();
        let cwd = dir.0.display().to_string();
        let never = |_: &str| -> Result<(), String> { panic!("probe mode started an opener") };
        open_with("https://example.com/a", &cwd, Some(&log), never).unwrap();
        open_with("./notes.md:3", &cwd, Some(&log), never).unwrap();
        let err = open_with("javascript:alert(1)", &cwd, Some(&log), never).unwrap_err();
        assert_eq!(err.code, "link_refused");
        let err = open_with("./missing.md", &cwd, Some(&log), never).unwrap_err();
        assert_eq!(err.code, "link_refused");
        let lines: Vec<serde_json::Value> = fs::read_to_string(&log)
            .unwrap()
            .lines()
            .map(|l| serde_json::from_str(l).unwrap())
            .collect();
        assert_eq!(
            lines,
            vec![
                serde_json::json!({"url": "https://example.com/a", "kind": "web", "target": "https://example.com/a"}),
                serde_json::json!({"url": "./notes.md:3", "kind": "file", "target": file_url(&dir.0.join("notes.md"))}),
            ],
            "only the two allowed links are recorded, once each"
        );
        // Outside probe mode the opener gets the target, and its failure is open_failed.
        let mut got = String::new();
        open_with("notes.md", &cwd, None, |t| {
            got = t.to_string();
            Ok(())
        })
        .unwrap();
        assert_eq!(got, file_url(&dir.0.join("notes.md")));
        let err = open_with("https://example.com/", &cwd, None, |_| Err("could not open link: exit status 3".into())).unwrap_err();
        assert_eq!((err.code.as_str(), err.message.as_str()), ("open_failed", "could not open link: exit status 3"));
        assert_eq!(fs::read_to_string(&log).unwrap().lines().count(), 2);
    }

    /// TestOpenChatLinkDesktopDispatch (links_test.go:133-150), with fake openers.
    /// Linux only: the fake opener checks its streams through /proc.
    #[cfg(target_os = "linux")]
    #[test]
    fn opener_runs_the_first_desktop_opener_with_quiet_streams() {
        let dir = Dir::new("dispatch");
        let bin = dir.0.join("bin");
        fs::create_dir_all(bin.join("xdg-open")).unwrap(); // a folder is not a program
        let output = dir.0.join("opened");
        let script = format!(
            "#!/bin/sh\n[ /dev/null -ef /proc/self/fd/0 ] || exit 40\n[ /dev/null -ef /proc/self/fd/1 ] || exit 41\n\
             [ /dev/null -ef /proc/self/fd/2 ] || exit 42\nprintf 'noisy app output\\n'\nprintf 'warning\\n' >&2\n\
             printf '%s' \"$1\" > '{}'\n",
            output.display()
        );
        let fake = bin.join("x-www-browser");
        fs::write(&fake, script).unwrap();
        fs::set_permissions(&fake, fs::Permissions::from_mode(0o700)).unwrap();
        let programs = &["xdg-open", "x-www-browser", "www-browser"];
        let target = "file:///tmp/folder%20with%20spaces";
        launch_fake(target, programs, &bin, Duration::from_secs(5)).unwrap();
        assert_eq!(fs::read_to_string(&output).unwrap(), target);
    }

    /// TestOpenChatLinkDesktopFailure (links_test.go:152-163).
    #[test]
    fn opener_failure_reports_exit_status() {
        let dir = Dir::new("failure");
        let fake = dir.0.join("xdg-open");
        fs::write(&fake, "#!/bin/sh\necho 'app failure' >&2\nexit 7\n").unwrap();
        fs::set_permissions(&fake, fs::Permissions::from_mode(0o700)).unwrap();
        let programs = &["xdg-open", "x-www-browser", "www-browser"];
        assert_eq!(
            launch_fake("https://example.com/", programs, &dir.0, Duration::from_secs(5)),
            Err("could not open link: exit status 7".to_string())
        );
        let empty = dir.0.join("empty");
        fs::create_dir_all(&empty).unwrap();
        assert_eq!(
            launch_with("https://example.com/", programs, Some(empty.as_os_str()), Duration::from_secs(5)),
            Err("exec: \"xdg-open,x-www-browser,www-browser\": executable file not found in $PATH".to_string())
        );
        // An opener that keeps running has the link: the click returns.
        let slow = dir.0.join("slow");
        fs::create_dir_all(&slow).unwrap();
        fs::write(slow.join("xdg-open"), "#!/bin/sh\nsleep 3\n").unwrap();
        fs::set_permissions(slow.join("xdg-open"), fs::Permissions::from_mode(0o700)).unwrap();
        let started = Instant::now();
        assert_eq!(launch_fake("https://example.com/", programs, &slow, Duration::from_millis(200)), Ok(()));
        assert!(started.elapsed() < Duration::from_secs(2));
    }
}
