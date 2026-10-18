//! The login-shell environment (desktop-design 1.2.7). Scaffold S3
//! placeholder: piece P3.1 writes the body; the public signature is pinned.
//! Never logged, recorded, probed or shown.
#![allow(dead_code)]

/// Runs `$SHELL -l -i -c 'printf "\0__CRUSH_ENV__\0"; env -0'` (stdin /dev/null, 5 s limit) and
/// parses the NUL-separated pairs after the marker, so anything the shell's startup prints first
/// is skipped. On any failure (no SHELL, timeout, no marker) it returns the app's own environment.
/// Always removes: CRUSH_DESKTOP_CRUSH, DESKTOP_STARTUP_ID, XDG_ACTIVATION_TOKEN, INVOCATION_ID.
/// Cached for the app's life; computed on a blocking thread at the first `engine_open`.
pub fn login_env() -> Vec<(String, String)> {
    Vec::new()
}
