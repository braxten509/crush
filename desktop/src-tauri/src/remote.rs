//! The launcher password (desktop-design-5-6 2.27, G-20): `crush remote
//! password --stdin` with the two entries written on the child's standard
//! input, never in its arguments or environment; neither value is logged and
//! the buffers are zeroed after the call. SHARE owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use std::io::{Read, Write};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};
use crate::host::AppHandle;

#[derive(Serialize)]
pub struct PasswordSet {
    pub ok: bool,
    pub message: String,
}

const LIMIT: Duration = Duration::from_secs(30);

/// `remote_password_set`: `crush remote password --stdin` with the two lines;
/// neither value is logged and the buffers are zeroed.
pub fn password_set(app: &AppHandle, mut first: String, mut second: String) -> AppResult<PasswordSet> {
    let answer = run(app, &first, &second);
    wipe(&mut first);
    wipe(&mut second);
    answer
}

fn wipe(text: &mut String) {
    // SAFETY: zero bytes are valid UTF-8.
    unsafe { text.as_bytes_mut().fill(0) };
    text.clear();
}

fn run(app: &AppHandle, first: &str, second: &str) -> AppResult<PasswordSet> {
    let env = crate::login_env::login_env();
    let setting = app.state::<crate::state::AppState>().settings.lock().unwrap().crush_path.clone();
    let path = env.iter().find(|(name, _)| name == "PATH").map(|(_, value)| value.as_str());
    let bin = crate::crush_bin::resolve(setting.as_deref(), path)?;
    let mut child = Command::new(&bin.path)
        .args(["remote", "password", "--stdin"])
        .envs(env.iter().map(|(k, v)| (k.as_str(), v.as_str())))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| AppError::new("crush_failed", format!("could not run crush: {e}")))?;
    {
        let mut input = child.stdin.take().ok_or_else(|| AppError::new("crush_failed", "could not talk to crush"))?;
        let mut lines = Vec::with_capacity(first.len() + second.len() + 2);
        lines.extend_from_slice(first.as_bytes());
        lines.push(b'\n');
        lines.extend_from_slice(second.as_bytes());
        lines.push(b'\n');
        let written = input.write_all(&lines);
        lines.fill(0);
        written.map_err(|e| AppError::new("crush_failed", format!("could not talk to crush: {e}")))?;
    }
    // Read both pipes while it runs, so a full pipe never stalls crush.
    let reader = |pipe: Option<Box<dyn Read + Send>>| {
        std::thread::spawn(move || {
            let mut text = String::new();
            if let Some(mut pipe) = pipe {
                let _ = pipe.read_to_string(&mut text);
            }
            text
        })
    };
    let out = reader(child.stdout.take().map(|p| Box::new(p) as Box<dyn Read + Send>));
    let err = reader(child.stderr.take().map(|p| Box::new(p) as Box<dyn Read + Send>));
    let started = Instant::now();
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if started.elapsed() >= LIMIT => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(AppError::new("crush_failed", "crush remote password took too long"));
            }
            Ok(None) => std::thread::sleep(Duration::from_millis(50)),
            Err(e) => return Err(AppError::new("crush_failed", e.to_string())),
        }
    };
    let out = out.join().unwrap_or_default();
    let err = err.join().unwrap_or_default();
    let last = |text: &str| text.lines().map(str::trim).filter(|l| !l.is_empty()).next_back().unwrap_or("").to_string();
    if status.success() {
        let message = last(&out);
        Ok(PasswordSet { ok: true, message: if message.is_empty() { "Password set. The phone asks for it before it opens a new Crush window here.".into() } else { message } })
    } else {
        // cobra prints "Error: <text>"; the text alone is shown.
        let message = last(&err);
        let message = message.strip_prefix("Error: ").unwrap_or(&message).to_string();
        Ok(PasswordSet { ok: false, message: if message.is_empty() { format!("crush remote password failed ({status})") } else { message } })
    }
}
