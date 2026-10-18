//! The login-shell environment (desktop-design 1.2.7). The first app's
//! environment wins in a workspace, and an app started from the desktop menu
//! may lack the PATH a terminal has, so the app sends its user's login-shell
//! environment instead of its own.
//! Never logged, recorded, probed or shown: only the variable count may be.

use crate::engine_cli::{run_limited, Ran};
use std::ffi::OsStr;
use std::process::Command;
use std::sync::OnceLock;
use std::time::Duration;

/// What the shell runs: a marker, then the environment as NUL-separated pairs.
const SCRIPT: &str = r#"printf "\0__CRUSH_ENV__\0"; env -0"#;
const MARKER: &[u8] = b"\0__CRUSH_ENV__\0";
const LIMIT: Duration = Duration::from_secs(5);
/// The app's own launch details, never meant for the AI's tools.
const DROPPED: [&str; 4] = ["CRUSH_DESKTOP_CRUSH", "DESKTOP_STARTUP_ID", "XDG_ACTIVATION_TOKEN", "INVOCATION_ID"];

static CACHE: OnceLock<Vec<(String, String)>> = OnceLock::new();

/// Runs `$SHELL -l -i -c 'printf "\0__CRUSH_ENV__\0"; env -0'` (stdin /dev/null, 5 s limit) and
/// parses the NUL-separated pairs after the marker, so anything the shell's startup prints first
/// is skipped. On any failure (no SHELL, timeout, no marker) it returns the app's own environment.
/// Always removes: CRUSH_DESKTOP_CRUSH, DESKTOP_STARTUP_ID, XDG_ACTIVATION_TOKEN, INVOCATION_ID.
/// Cached for the app's life; computed on a blocking thread at the first `engine_open`.
pub fn login_env() -> Vec<(String, String)> {
    CACHE
        .get_or_init(|| capture(std::env::var_os("SHELL").as_deref(), LIMIT))
        .clone()
}

fn capture(shell: Option<&OsStr>, limit: Duration) -> Vec<(String, String)> {
    let from_shell = shell
        .filter(|s| !s.is_empty())
        .and_then(|shell| from_login_shell(shell, limit));
    let pairs = from_shell.unwrap_or_else(|| {
        let own = own_env();
        eprintln!(
            "crush-desktop: the login shell gave no environment; using the app's own ({} variables)",
            own.len()
        );
        own
    });
    without_app_variables(pairs)
}

fn from_login_shell(shell: &OsStr, limit: Duration) -> Option<Vec<(String, String)>> {
    let mut cmd = Command::new(shell);
    cmd.args(["-l", "-i", "-c", SCRIPT]);
    match run_limited(&mut cmd, limit, true) {
        Ok(Ran::Exited { stdout, .. }) => parse(&stdout),
        Ok(Ran::TimedOut) | Err(_) => None,
    }
}

/// The pairs after the marker. The piece after the last NUL is dropped: it is
/// empty, or text a logout script printed after `env` ended.
fn parse(out: &[u8]) -> Option<Vec<(String, String)>> {
    let start = out.windows(MARKER.len()).position(|w| w == MARKER)? + MARKER.len();
    let mut pieces: Vec<&[u8]> = out[start..].split(|&b| b == 0).collect();
    pieces.pop();
    let pairs: Vec<(String, String)> = pieces
        .into_iter()
        .filter_map(|piece| {
            let eq = piece.iter().position(|&b| b == b'=').filter(|&i| i > 0)?;
            Some((lossy(&piece[..eq]), lossy(&piece[eq + 1..])))
        })
        .collect();
    (!pairs.is_empty()).then_some(pairs)
}

fn own_env() -> Vec<(String, String)> {
    std::env::vars_os()
        .map(|(k, v)| (k.to_string_lossy().into_owned(), v.to_string_lossy().into_owned()))
        .collect()
}

fn without_app_variables(mut pairs: Vec<(String, String)>) -> Vec<(String, String)> {
    pairs.retain(|(k, _)| !DROPPED.contains(&k.as_str()));
    pairs
}

fn lossy(bytes: &[u8]) -> String {
    String::from_utf8_lossy(bytes).into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::engine_cli::tests::{scratch, script};

    fn pairs(list: &[(&str, &str)]) -> Vec<(String, String)> {
        list.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect()
    }

    #[test]
    fn parse_takes_the_pairs_after_the_marker() {
        let out = b"Welcome back!\nA=startup noise\0B=2\0\0__CRUSH_ENV__\0PATH=/x/bin:/usr/bin\0EQ=a=b\0EMPTY=\0MULTI=line 1\nline 2\0=no key\0bye from .zlogout\n";
        assert_eq!(
            parse(out),
            Some(pairs(&[("PATH", "/x/bin:/usr/bin"), ("EQ", "a=b"), ("EMPTY", ""), ("MULTI", "line 1\nline 2")]))
        );
        assert_eq!(parse(b"PATH=/usr/bin\0HOME=/h\0"), None, "no marker, no environment");
        assert_eq!(parse(b"\0__CRUSH_ENV__\0"), None, "a marker alone is no environment");
    }

    #[test]
    fn the_apps_own_variables_are_removed() {
        let all = pairs(&[("PATH", "/bin"), ("CRUSH_DESKTOP_CRUSH", "/x/crush"), ("DESKTOP_STARTUP_ID", "1"),
            ("XDG_ACTIVATION_TOKEN", "t"), ("INVOCATION_ID", "i"), ("CRUSH_DESKTOP_ENV_MARK", "kept")]);
        assert_eq!(without_app_variables(all), pairs(&[("PATH", "/bin"), ("CRUSH_DESKTOP_ENV_MARK", "kept")]));
    }

    #[test]
    fn a_login_shell_is_read_and_a_slow_or_missing_one_falls_back() {
        let dir = scratch("loginenv");
        let args_file = dir.join("args");
        // A shell whose startup prints noise (even NULs and pairs) before running the script.
        let shell = script(&dir.join("shell"), &format!(
            r#"for a in "$@"; do printf '%s\n' "$a"; done > '{}'
echo 'Last login: today'; printf 'NOISE=1\0'
export P31_LOGIN_ONLY=from-login CRUSH_DESKTOP_CRUSH=/hidden/crush
exec /bin/sh -c "$4""#, args_file.display()));
        let env = capture(Some(shell.as_os_str()), LIMIT);
        let get = |k: &str| env.iter().find(|(key, _)| key == k).map(|(_, v)| v.clone());
        assert_eq!(get("P31_LOGIN_ONLY").as_deref(), Some("from-login"), "the login shell's variables come back");
        assert_eq!(get("NOISE"), None, "what the startup printed is skipped");
        assert_eq!(get("CRUSH_DESKTOP_CRUSH"), None, "the app's own variables are removed");
        let args = std::fs::read_to_string(&args_file).unwrap();
        assert_eq!(args.lines().collect::<Vec<_>>(), ["-l", "-i", "-c", SCRIPT]);

        // A shell that hangs: the app's own environment, after the limit.
        let slow = script(&dir.join("slow"), "sleep 5");
        let started = std::time::Instant::now();
        let env = capture(Some(slow.as_os_str()), Duration::from_millis(300));
        assert!(started.elapsed() < Duration::from_secs(3), "the slow shell is cut off");
        assert_eq!(env.iter().find(|(k, _)| k == "PATH").map(|(_, v)| v.clone()), std::env::var("PATH").ok());
        // No SHELL, or one that prints no marker: the app's own environment too.
        let plain = script(&dir.join("plain"), "echo hi");
        for shell in [None, Some(OsStr::new("")), Some(plain.as_os_str())] {
            let env = capture(shell, LIMIT);
            assert_eq!(env.len(), without_app_variables(own_env()).len());
        }
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
