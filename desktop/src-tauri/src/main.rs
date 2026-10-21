// Crush Desktop's helper (owner 2026-10-20: the app moved from Tauri to
// Electron). Electron (electron/main.cjs) starts this program with the app's
// command line and owns the windows; this program keeps the engine
// connections, files, terminals, settings and alerts, and the two talk in
// JSON lines over stdin and stdout (host.rs). Test runs start the app with
// `--probe <file>` (the page reports what is on screen) and may add
// `--fixture <file>` (a recorded engine, 6.3).

mod alerts;
mod openchats;
mod applog;
mod batch;
mod clipboard;
mod commands;
mod configfiles;
mod connection;
mod crash;
mod crush_bin;
mod crushinfo;
mod dirlist;
mod dispatch;
mod docker;
mod editor;
mod engine_cli;
mod error;
mod files;
mod fixture;
mod folderdialog;
mod host;
mod login_env;
mod logs;
mod opener;
mod projects;
mod pty;
mod refresh;
mod remote;
mod selfupdate;
mod settings;
mod sse;
mod state;
mod taskbar;
mod uds_http;
mod windows;

use host::{async_runtime, AppHandle};
use serde_json::{json, Value};
use state::{AppState, Launch};
use std::io::BufRead;
use std::path::{Path, PathBuf};
use std::sync::Mutex;

fn main() {
    crash::install();
    // REM-017: a phone launch names itself in CRUSH_REMOTE_LAUNCH. The process
    // starts again with `--launch-id` in its arguments and the variable gone,
    // before any thread starts, so the single-instance hand-off (which carries
    // only arguments) keeps the launch.
    #[cfg(unix)]
    if let Some(id) = std::env::var_os("CRUSH_REMOTE_LAUNCH") {
        relaunch_with_launch_id(id);
    }
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.iter().any(|a| a == "--version") {
        println!("crush-desktop {}", env!("CARGO_PKG_VERSION"));
        return;
    }
    let (input, output) = take_lines();
    applog::write("info", "app", &format!("start crush-desktop {} (Electron)", env!("CARGO_PKG_VERSION")));
    let here = std::env::current_dir().unwrap_or_default();
    let ui_debug = std::env::var("CRUSH_UI_DEBUG").is_ok_and(|v| v == "true");
    let launch = match parse_launch(&args, &here, ui_debug) {
        Ok(launch) => launch,
        Err(message) => {
            eprintln!("{message}");
            std::process::exit(1);
        }
    };
    let fixture = match launch.fixture.as_deref().map(fixture::Fixture::load).transpose() {
        Ok(fixture) => fixture,
        Err(message) => {
            eprintln!("{message}");
            std::process::exit(1);
        }
    };
    let opened_log = launch.probe_side_file("opened.jsonl");
    let first_window = windows::WindowLaunch::first(&launch);
    let app = AppHandle::new(Box::new(output));
    app.manage(AppState {
        windows: Mutex::default(),
        settings: Mutex::new(settings::load()),
        launch,
    });
    app.manage(windows::Windows::new(first_window));
    app.manage(pty::Ptys::default());
    app.manage(editor::Edits::default());
    app.manage(alerts::Alerts::default());
    app.manage(logs::Follows::default());
    app.manage(selfupdate::Runner::default());
    openchats::join();
    if let Some(fixture) = fixture {
        // A fixture run starts with its chats open (the recorded ones unless it names its own).
        for chat in fixture.open_chats() {
            let _ = openchats::add(chat);
        }
        app.manage(fixture);
    }
    settings::watch(app.clone());
    app.send(&json!({"t": "ready"}));
    for line in input.lines() {
        let Ok(line) = line else { break };
        match serde_json::from_str::<Value>(&line) {
            Ok(message) => handle(&app, message, opened_log.as_deref()),
            Err(e) => eprintln!("crush-desktop: a message from Electron could not be read: {e}"),
        }
    }
    // Electron is gone: so is the app, which signs out of the open chats.
    openchats::leave();
    std::process::exit(0);
}

/// Keeps stdin and stdout for the line to Electron and points the program's
/// own (which every child process inherits: the engine, sound players,
/// terminals) at /dev/null and stderr, so nothing else ever writes into the
/// line or reads from it.
fn take_lines() -> (std::io::BufReader<std::fs::File>, std::fs::File) {
    use std::os::fd::FromRawFd;
    // SAFETY: plain descriptor calls on 0, 1 and 2; each new descriptor is owned by one File.
    unsafe {
        let input = libc::fcntl(0, libc::F_DUPFD_CLOEXEC, 3);
        let output = libc::fcntl(1, libc::F_DUPFD_CLOEXEC, 3);
        if input < 0 || output < 0 {
            eprintln!("crush-desktop: could not keep the line to Electron");
            std::process::exit(1);
        }
        let null = libc::open(c"/dev/null".as_ptr(), libc::O_RDONLY);
        if null >= 0 {
            libc::dup2(null, 0);
            libc::close(null);
        }
        libc::dup2(2, 1);
        (std::io::BufReader::new(std::fs::File::from_raw_fd(input)), std::fs::File::from_raw_fd(output))
    }
}

/// One message from Electron (host.rs lists them).
fn handle(app: &AppHandle, message: Value, opened_log: Option<&Path>) {
    let text = |key: &str| message.get(key).and_then(Value::as_str).unwrap_or_default().to_string();
    match message.get("t").and_then(Value::as_str).unwrap_or_default() {
        "call" => {
            let Some(id) = message.get("id").and_then(Value::as_u64) else { return };
            let (app, label, cmd) = (app.clone(), text("label"), text("cmd"));
            let args = message.get("args").cloned().unwrap_or(Value::Null);
            async_runtime::spawn(async move {
                let answer = match dispatch::call(app.clone(), label, cmd, args).await {
                    Ok(value) => json!({"t": "result", "id": id, "ok": value}),
                    Err(error) => json!({"t": "result", "id": id, "err": error}),
                };
                app.send(&answer);
            });
        }
        "reply" => {
            let Some(id) = message.get("id").and_then(Value::as_u64) else { return };
            let answer = match message.get("err") {
                Some(error) => Err(error.as_str().map(String::from).unwrap_or_else(|| error.to_string())),
                None => Ok(message.get("ok").cloned().unwrap_or(Value::Null)),
            };
            app.answer(id, answer);
        }
        "window" => {
            let label = text("label");
            match text("event").as_str() {
                "created" => app.window_created(&label),
                // The system close button: the page runs its close steps (6.4).
                "close-requested" => windows::close_requested(app, &label),
                "destroyed" => {
                    app.window_gone(&label);
                    alerts::window_gone(app, &label);
                    logs::stop_window(app, &label);
                }
                "focus" => app.set_focused(&label, true),
                "blur" => app.set_focused(&label, false),
                "crashed" => crash::webview_crashed(app, &label, &text("reason")),
                other => eprintln!("crush-desktop: unknown window event {other}"),
            }
        }
        "handoff" => {
            let argv = message.get("argv").and_then(Value::as_array).map(|a| a.iter().filter_map(Value::as_str).map(String::from).collect()).unwrap_or_default();
            let (app, cwd) = (app.clone(), text("cwd"));
            // Opening a window waits for Electron's answer, which this thread reads.
            async_runtime::spawn_blocking(move || windows::handoff(&app, argv, cwd));
        }
        // The navigation guard (1.2.9) lives in Electron; a refusal is logged
        // with the address's scheme only, and in probe mode recorded in
        // `<probe>.opened.jsonl`.
        "refused" => {
            let scheme = text("scheme");
            eprintln!("crush-desktop: refused a navigation to a {scheme} address");
            if let Some(log) = opened_log {
                state::append_json_line(log, &json!({"refused_navigation": scheme}));
            }
        }
        other => eprintln!("crush-desktop: unknown message {other}"),
    }
}

/// Starts this program again with `--launch-id <id>` added and
/// CRUSH_REMOTE_LAUNCH removed. Returns only when that fails; the process then
/// goes on without the launch ID.
#[cfg(unix)]
fn relaunch_with_launch_id(id: std::ffi::OsString) {
    use std::os::unix::process::CommandExt;
    let exe = match std::env::current_exe() {
        Ok(exe) => exe,
        Err(e) => {
            eprintln!("crush-desktop: cannot find this program to take the phone launch: {e}");
            return;
        }
    };
    let mut args: Vec<std::ffi::OsString> = std::env::args_os().skip(1).collect();
    if !id.is_empty() {
        args.push("--launch-id".into());
        args.push(id);
    }
    let e = std::process::Command::new(exe).args(args).env_remove("CRUSH_REMOTE_LAUNCH").exec();
    eprintln!("crush-desktop: could not start again for the phone launch: {e}");
}

/// Reads the command line: the terminal's flags (internal/cmd/root.go:37-48),
/// the desktop's --project, --launch-id and --new-chat (desktop-design-5-6
/// 7.1; a second process hands these over, windows::handoff), plus the
/// test-only --probe and --fixture. Unknown arguments are ignored.
fn parse_launch(args: &[String], here: &Path, ui_debug: bool) -> Result<Launch, String> {
    let mut launch = Launch {
        cwd: here.to_path_buf(),
        ui_debug,
        ..Default::default()
    };
    let mut i = 0;
    while i < args.len() {
        let arg = &args[i];
        i += 1;
        let (name, inline) = match arg.split_once('=') {
            Some((name, value)) if arg.starts_with('-') => (name, Some(value.to_string())),
            _ => (arg.as_str(), None),
        };
        // A short value flag may also carry its value glued on: -cDIR.
        let glued = name.len() > 2
            && name.starts_with('-')
            && !name.starts_with("--")
            && b"csDH".contains(&name.as_bytes()[1]);
        let (name, inline) = match inline {
            None if glued => (&name[..2], Some(name[2..].to_string())),
            inline => (name, inline),
        };
        let mut value = |flag: &str| -> Result<String, String> {
            if let Some(v) = &inline {
                return Ok(v.clone());
            }
            let v = args
                .get(i)
                .cloned()
                .ok_or_else(|| format!("flag needs an argument: {flag}"))?;
            i += 1;
            Ok(v)
        };
        let switch = |flag: &str| -> Result<bool, String> {
            match inline.as_deref() {
                None | Some("true") => Ok(true),
                Some("false") => Ok(false),
                Some(other) => Err(format!("invalid argument \"{other}\" for \"{flag}\" flag")),
            }
        };
        match name {
            "--probe" => launch.probe = Some(PathBuf::from(value("--probe")?)),
            "--fixture" => launch.fixture = Some(PathBuf::from(value("--fixture")?)),
            "-c" | "--cwd" => launch.cwd = here.join(value("--cwd")?),
            // The first window's folder, the same as --cwd (PLAN.md:143).
            "--project" => launch.cwd = here.join(value("--project")?),
            "--launch-id" => launch.launch_id = Some(value("--launch-id")?),
            "--new-chat" => launch.new_chat = switch("--new-chat")?,
            "-s" | "--session" => launch.session = Some(value("--session")?),
            "-C" | "--continue" => launch.continue_last = switch("--continue")?,
            "-D" | "--data-dir" => launch.data_dir = Some(value("--data-dir")?),
            "-d" | "--debug" => launch.debug = switch("--debug")?,
            "-H" | "--host" => launch.host = Some(value("--host")?),
            "-y" | "--yolo" => launch.yolo = Some(switch("--yolo")?),
            // pflag's StringSlice: repeatable, and each value may hold several, comma-separated.
            "--channels" => launch.channels.extend(
                value("--channels")?
                    .split(',')
                    .map(str::trim)
                    .filter(|s| !s.is_empty())
                    .map(String::from),
            ),
            _ => eprintln!("crush-desktop: ignoring the argument {arg}"),
        }
    }
    if launch.session.is_some() && launch.continue_last {
        // cobra's text for MarkFlagsMutuallyExclusive (internal/cmd/root.go:47).
        return Err("if any flags in the group [session continue] are set none of the others can be; [continue session] were all set".to_string());
    }
    Ok(launch)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(args: &[&str]) -> Result<Launch, String> {
        let args: Vec<String> = args.iter().map(|s| s.to_string()).collect();
        parse_launch(&args, Path::new("/work"), false)
    }

    #[test]
    fn flags_take_the_terminals_spellings() {
        let launch = parse(&[
            "--probe", "/tmp/p.json", "--fixture=/f.json", "-c", "proj", "-s", "abc", "-D", "/data",
            "-d", "-H", "unix:///s", "--yolo=false", "--channels", "a:b,c:d", "--channels=e:f",
        ])
        .unwrap();
        assert_eq!(
            launch,
            Launch {
                probe: Some(PathBuf::from("/tmp/p.json")),
                fixture: Some(PathBuf::from("/f.json")),
                cwd: PathBuf::from("/work/proj"),
                session: Some("abc".into()),
                continue_last: false,
                data_dir: Some("/data".into()),
                debug: true,
                host: Some("unix:///s".into()),
                yolo: Some(false),
                channels: vec!["a:b".into(), "c:d".into(), "e:f".into()],
                ui_debug: false,
                launch_id: None,
                new_chat: false,
            }
        );
        let launch = parse(&["--cwd=/abs", "-C", "-y", "--session-less"]).unwrap();
        assert_eq!(launch.cwd, PathBuf::from("/abs"));
        assert!(launch.continue_last);
        assert_eq!(launch.yolo, Some(true));
        assert_eq!(parse(&["-c/glued"]).unwrap().cwd, PathBuf::from("/glued"));
        let launch = parse(&[]).unwrap();
        assert_eq!((launch.cwd, launch.yolo), (PathBuf::from("/work"), None));
    }

    #[test]
    fn session_and_continue_exclude_each_other() {
        let err = parse(&["-s", "abc", "--continue"]).unwrap_err();
        assert!(err.starts_with("if any flags in the group [session continue] are set none of the others can be"));
        assert_eq!(parse(&["--cwd"]).unwrap_err(), "flag needs an argument: --cwd");
        assert!(parse(&["--yolo=maybe"]).is_err());
    }
}
