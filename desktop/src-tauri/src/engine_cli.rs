//! The version check and `crush engine ensure` (desktop-design 1.2.3; engine
//! design 5.2, "CLI commands"). Both run `crush` as a child process and read
//! the one JSON value it prints on stdout; messages for people go to its
//! stderr, which is not read.

use crate::crush_bin::{self, CrushBin};
use crate::error::{AppError, AppResult};
use std::io::Read;
use std::process::{Command, Stdio};
use std::sync::{mpsc, Arc, Mutex};
use std::time::{Duration, Instant};

#[derive(serde::Deserialize, serde::Serialize, Clone)]
pub struct EngineVersion {
    pub version: String,
    pub protocol: i64,
}

#[derive(serde::Deserialize, serde::Serialize, Clone)]
pub struct Ensured {
    pub address: String,
    pub socket: String,
    pub protocol: i64,
    pub version: String,
    pub pid: i64,
    pub data_dir: String,
    pub started: bool,
}

pub const APP_PROTOCOL: i64 = 1; // proto.ProtocolVersion, internal/proto/protocol.go:6

const VERSION_LIMIT: Duration = Duration::from_secs(5);
const ENSURE_LIMIT: Duration = Duration::from_secs(20);

/// Runs `<bin> engine version` (5 s limit). Ok only when it printed {"version","protocol"} and
/// protocol == APP_PROTOCOL; any other outcome is Err(crush_too_old) with what crush said.
pub fn version(bin: &CrushBin) -> AppResult<EngineVersion> {
    version_within(bin, VERSION_LIMIT)
}

fn version_within(bin: &CrushBin, limit: Duration) -> AppResult<EngineVersion> {
    let at = bin.path.display();
    let too_old = |message: String| AppError::new("crush_too_old", message);
    let no_engine = || too_old(format!("The crush at {at} is too old for Crush Desktop: it has no engine command."));
    let mut cmd = Command::new(&bin.path);
    cmd.args(["engine", "version"]);
    match run_limited(&mut cmd, limit, false) {
        // Gone since it was found, or not runnable after all.
        Err(_) => Err(crush_bin::not_found(&bin.source, &bin.path)),
        Ok(Ran::TimedOut) => Err(too_old(format!("The crush at {at} did not answer in time."))),
        // A wrapper whose crush is missing: the shell's "command not found".
        Ok(Ran::Exited { code: Some(127), .. }) => Err(crush_bin::not_found(&bin.source, &bin.path)),
        Ok(Ran::Exited { code: Some(0), stdout }) => match parse_json::<EngineVersion>(&stdout) {
            Some(v) if v.protocol == APP_PROTOCOL => Ok(v),
            Some(v) => Err(too_old(format!(
                "The crush at {at} speaks engine protocol {}; Crush Desktop needs protocol {APP_PROTOCOL}.",
                v.protocol
            ))),
            None => Err(no_engine()),
        },
        // An older crush says "unknown command" and exits 1.
        Ok(Ran::Exited { .. }) => Err(no_engine()),
    }
}

/// Runs `<bin> engine ensure --cwd DIR [--data-dir D] --timeout 15s` (20 s limit) with `env`
/// as the child's whole environment. Parses the success JSON or the error JSON
/// {"error":{"code","message"}} (exit 3/4/5/6/1, engine design §5.2).
pub fn ensure(
    bin: &CrushBin,
    cwd: &str,
    data_dir: Option<&str>,
    env: &[(String, String)],
) -> AppResult<Ensured> {
    ensure_within(bin, cwd, data_dir, env, ENSURE_LIMIT)
}

fn ensure_within(
    bin: &CrushBin,
    cwd: &str,
    data_dir: Option<&str>,
    env: &[(String, String)],
    limit: Duration,
) -> AppResult<Ensured> {
    #[derive(serde::Deserialize)]
    struct Failure {
        error: FailureBody,
    }
    #[derive(serde::Deserialize)]
    struct FailureBody {
        #[serde(default)]
        code: String,
        #[serde(default)]
        message: String,
    }

    let mut cmd = Command::new(&bin.path);
    cmd.args(ensure_args(cwd, data_dir))
        .env_clear()
        .envs(env.iter().map(|(k, v)| (k, v)));
    // Errors name the step only: the environment never goes into a message (1.2.7).
    match run_limited(&mut cmd, limit, false) {
        Err(_) => Err(crush_bin::not_found(&bin.source, &bin.path)),
        Ok(Ran::TimedOut) => Err(AppError::new(
            "ensure_failed",
            format!("crush engine ensure did not answer within {} s.", limit.as_secs()),
        )),
        Ok(Ran::Exited { code, stdout }) => {
            if let Some(Failure { error }) = parse_json::<Failure>(&stdout) {
                let code = if error.code.is_empty() { "ensure_failed".to_string() } else { error.code };
                return Err(AppError::new(&code, error.message));
            }
            if code == Some(0) {
                if let Some(ensured) = parse_json::<Ensured>(&stdout) {
                    return Ok(ensured);
                }
            }
            let ended = match code {
                Some(n) => format!("exit status {n}"),
                None => "stopped by a signal".to_string(),
            };
            Err(AppError::new(
                "ensure_failed",
                format!("crush engine ensure gave no usable answer ({ended})."),
            ))
        }
    }
}

/// The arguments of `crush engine ensure` (START-009: `-D` goes to `--data-dir`).
pub(crate) fn ensure_args(cwd: &str, data_dir: Option<&str>) -> Vec<String> {
    let mut args: Vec<String> = vec!["engine".into(), "ensure".into(), "--cwd".into(), cwd.into()];
    if let Some(dir) = data_dir.filter(|d| !d.is_empty()) {
        args.extend(["--data-dir".into(), dir.into()]);
    }
    args.extend(["--timeout".into(), "15s".into()]);
    args
}

/// The one JSON value a command printed: the whole output, else its last
/// line that is not blank.
fn parse_json<T: serde::de::DeserializeOwned>(stdout: &[u8]) -> Option<T> {
    let text = String::from_utf8_lossy(stdout);
    serde_json::from_str(text.trim()).ok().or_else(|| {
        let last = text.lines().rev().find(|l| !l.trim().is_empty())?;
        serde_json::from_str(last.trim()).ok()
    })
}

/// How a child process ended.
pub(crate) enum Ran {
    /// It exited (code None: stopped by a signal) after printing `stdout`.
    Exited { code: Option<i32>, stdout: Vec<u8> },
    /// It did not end within its limit and was killed.
    TimedOut,
}

extern "C" {
    fn setsid() -> i32;
    fn kill(pid: i32, sig: i32) -> i32;
}
const SIGKILL: i32 = 9;

/// Runs `cmd` with stdin and stderr closed and stdout read, for at most
/// `limit`. With `own_session` the child leads a new session, so it has no
/// terminal to take over (an interactive login shell started from a terminal
/// would otherwise stop the app), and a timeout kills its whole group.
pub(crate) fn run_limited(cmd: &mut Command, limit: Duration, own_session: bool) -> std::io::Result<Ran> {
    cmd.stdin(Stdio::null()).stdout(Stdio::piped()).stderr(Stdio::null());
    if own_session {
        use std::os::unix::process::CommandExt;
        // SAFETY: setsid is async-signal-safe and touches no memory of ours.
        unsafe {
            cmd.pre_exec(|| {
                if setsid() < 0 {
                    return Err(std::io::Error::last_os_error());
                }
                Ok(())
            });
        }
    }
    let mut child = spawn(cmd)?;
    let mut pipe = child.stdout.take().expect("stdout is piped");
    let read: Arc<Mutex<Vec<u8>>> = Arc::default();
    let (ended_tx, ended_rx) = mpsc::channel::<()>();
    let sink = read.clone();
    std::thread::spawn(move || {
        let mut chunk = [0u8; 16 * 1024];
        while let Ok(n @ 1..) = pipe.read(&mut chunk) {
            sink.lock().unwrap().extend_from_slice(&chunk[..n]);
        }
        let _ = ended_tx.send(());
    });
    let deadline = Instant::now() + limit;
    let status = loop {
        if let Some(status) = child.try_wait()? {
            break Some(status);
        }
        if Instant::now() >= deadline {
            break None;
        }
        std::thread::sleep(Duration::from_millis(10));
    };
    let Some(status) = status else {
        if own_session {
            // SAFETY: a plain system call; the child leads its own group.
            unsafe { kill(-(child.id() as i32), SIGKILL) };
        }
        let _ = child.kill();
        let _ = child.wait();
        return Ok(Ran::TimedOut);
    };
    // Wait for the rest of the output, but not for a background process the
    // child left holding the pipe.
    let _ = ended_rx.recv_timeout(Duration::from_millis(500));
    let stdout = std::mem::take(&mut *read.lock().unwrap());
    Ok(Ran::Exited { code: status.code(), stdout })
}

/// Spawns, trying again while the program file is still open for writing
/// somewhere (ETXTBSY: a file written just before, while another thread forked).
fn spawn(cmd: &mut Command) -> std::io::Result<std::process::Child> {
    const ETXTBSY: i32 = 26;
    let mut tries = 0;
    loop {
        match cmd.spawn() {
            Err(e) if e.raw_os_error() == Some(ETXTBSY) && tries < 20 => {
                tries += 1;
                std::thread::sleep(Duration::from_millis(10));
            }
            other => return other,
        }
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::crush_bin::BinSource;
    use std::os::unix::fs::PermissionsExt;
    use std::path::{Path, PathBuf};

    /// A fresh folder for one test, removed by the test at its end.
    pub(crate) fn scratch(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("crush-desktop-p31-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    /// A shell script at `path` that runs `body`.
    pub(crate) fn script(path: &Path, body: &str) -> PathBuf {
        std::fs::write(path, format!("#!/bin/sh\n{body}\n")).unwrap();
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o755)).unwrap();
        path.to_path_buf()
    }

    fn bin(path: PathBuf) -> CrushBin {
        CrushBin { path, source: BinSource::Env }
    }

    #[test]
    fn version_texts() {
        let dir = scratch("version");
        let fake = |name: &str, body: &str| bin(script(&dir.join(name), body));
        let ok = fake("ok", r#"printf '{"version":"v0.99.0","protocol":1}\n'"#);
        let v = version_within(&ok, VERSION_LIMIT).ok().unwrap();
        assert_eq!((v.version.as_str(), v.protocol), ("v0.99.0", 1));

        let cases = [
            (r#"printf '{"version":"v2.0.0","protocol":2}\n'"#, "crush_too_old",
             "speaks engine protocol 2; Crush Desktop needs protocol 1."),
            (r#"echo 'Error: unknown command "engine" for "crush"' >&2; echo 'Run crush --help'; exit 1"#,
             "crush_too_old", "is too old for Crush Desktop: it has no engine command."),
            ("echo 'not json'", "crush_too_old", "is too old for Crush Desktop: it has no engine command."),
            ("sleep 5", "crush_too_old", "did not answer in time."),
        ];
        for (i, (body, code, tail)) in cases.iter().enumerate() {
            let b = fake(&format!("case{i}"), body);
            let started = Instant::now();
            let e = version_within(&b, Duration::from_millis(300)).err().unwrap();
            assert_eq!(e.code, *code, "{body}");
            assert_eq!(e.message, format!("The crush at {} {tail}", b.path.display()), "{body}");
            assert!(started.elapsed() < Duration::from_secs(3), "a slow crush is cut off at the limit");
        }
        // Exit 127 (a wrapper whose crush is gone) and a missing file: not found.
        let gone = fake("wrapper", "exit 127");
        let e = version_within(&gone, VERSION_LIMIT).err().unwrap();
        assert_eq!((e.code.as_str(), e.message.clone()), ("crush_not_found", format!("Crush Desktop could not find crush at {}.", gone.path.display())));
        let on_path = CrushBin { path: gone.path.clone(), source: BinSource::Path };
        let e = version_within(&on_path, VERSION_LIMIT).err().unwrap();
        assert_eq!(e.message, "Crush Desktop could not find crush on your PATH.");
        let e = version_within(&bin(dir.join("missing")), VERSION_LIMIT).err().unwrap();
        assert_eq!(e.code, "crush_not_found");
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn ensure_json_and_error_json() {
        let dir = scratch("ensure");
        let args_file = dir.join("args");
        let env_file = dir.join("env");
        let ok = bin(script(&dir.join("ok"), &format!(
            r#"for a in "$@"; do printf '%s\n' "$a"; done > '{}'
export -p > '{}'
printf '%s\n' '{{"address":"unix:///run/e.sock","socket":"/run/e.sock","protocol":1,"version":"v1","pid":4242,"data_dir":"/p/.crush","started":true}}'"#,
            args_file.display(), env_file.display()
        )));
        let env = vec![("P31_ENSURE_VAR".to_string(), "kept".to_string()), ("PATH".to_string(), "/usr/bin:/bin".to_string())];
        let e = ensure_within(&ok, "/home/u/proj", Some("/data/x"), &env, ENSURE_LIMIT).ok().unwrap();
        assert_eq!((e.socket.as_str(), e.pid, e.data_dir.as_str(), e.started), ("/run/e.sock", 4242, "/p/.crush", true));
        // START-009: the argument list, with -D as --data-dir.
        let args = std::fs::read_to_string(&args_file).unwrap();
        assert_eq!(args.lines().collect::<Vec<_>>(), ["engine", "ensure", "--cwd", "/home/u/proj", "--data-dir", "/data/x", "--timeout", "15s"]);
        // The given environment is the child's whole environment.
        let seen = std::fs::read_to_string(&env_file).unwrap();
        assert!(seen.contains("P31_ENSURE_VAR") && seen.contains("kept"), "the given variables reach crush");
        assert!(std::env::var_os("HOME").is_some() && !seen.contains("HOME="), "nothing of the app's own environment does");
        ensure_within(&ok, "/home/u/proj", None, &env, ENSURE_LIMIT).ok().unwrap();
        let args = std::fs::read_to_string(&args_file).unwrap();
        assert_eq!(args.lines().collect::<Vec<_>>(), ["engine", "ensure", "--cwd", "/home/u/proj", "--timeout", "15s"]);

        // Error JSON with each exit code: the code and message pass through as they are.
        for (exit, code) in [(3, "protocol_mismatch"), (4, "project_locked"), (5, "start_failed"), (6, "unsafe_socket_dir"), (1, "wrong_project")] {
            let message = format!("the {code} text");
            let failing = bin(script(&dir.join(format!("fail{exit}")), &format!(
                r#"echo 'crush engine ensure: {message}' >&2
printf '%s\n' '{{"error":{{"code":"{code}","message":"{message}"}}}}'
exit {exit}"#)));
            let e = ensure_within(&failing, "/p", None, &env, ENSURE_LIMIT).err().unwrap();
            assert_eq!((e.code.as_str(), e.message.as_str()), (code, message.as_str()), "exit {exit}");
        }
        // No usable JSON: ensure_failed, naming the step and how it ended only.
        for (name, body, ended) in [
            ("garbage", "echo oops; exit 1", "exit status 1"),
            ("partial", r#"printf '{"socket":"/s"}\n'"#, "exit status 0"),
        ] {
            let b = bin(script(&dir.join(name), body));
            let e = ensure_within(&b, "/p", None, &env, ENSURE_LIMIT).err().unwrap();
            assert_eq!((e.code.as_str(), e.message.clone()), ("ensure_failed", format!("crush engine ensure gave no usable answer ({ended}).")));
        }
        let slow = bin(script(&dir.join("slow"), "sleep 5"));
        let e = ensure_within(&slow, "/p", None, &env, Duration::from_millis(300)).err().unwrap();
        assert_eq!(e.code, "ensure_failed");
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
