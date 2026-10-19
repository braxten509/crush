// Crush Desktop: the window, its flags and its commands (desktop-design 1.2).
// Test runs start it with `--probe <file>` (the page reports what is on
// screen) and may add `--fixture <file>` (a recorded engine, 6.3).
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod alerts;
mod batch;
mod clipboard;
mod commands;
mod configfiles;
mod connection;
mod crash;
mod crush_bin;
mod crushinfo;
mod dirlist;
mod docker;
mod editor;
mod engine_cli;
mod error;
mod files;
mod fixture;
mod folderdialog;
mod login_env;
mod logs;
mod opener;
mod projects;
mod pty;
mod remote;
mod selfupdate;
mod settings;
mod sse;
mod state;
mod taskbar;
mod uds_http;
mod windows;

use state::{AppState, Launch};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use tauri::Manager;

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
    let mut builder = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(windows::handoff))
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(nav_guard(opened_log))
        .manage(AppState {
            windows: Mutex::default(),
            settings: Mutex::new(settings::load()),
            launch,
        })
        .manage(windows::Windows::new(first_window))
        .manage(pty::Ptys::default())
        .manage(editor::Edits::default())
        .manage(alerts::Alerts::default())
        .manage(logs::Follows::default())
        .manage(selfupdate::Runner::default());
    if let Some(fixture) = fixture {
        builder = builder.manage(fixture);
    }
    builder
        .setup(|app| {
            settings::watch(app.handle().clone());
            if let Some(window) = app.get_webview_window(windows::FIRST) {
                crash::watch_webview(&window);
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                // The page runs its close steps and calls window_close, which
                // leaves the engine (DELETE /v1/clients/{id}, 1.2.6) before the
                // window goes; after 10 s Rust closes it anyway (6.4).
                api.prevent_close();
                windows::close_requested(window.app_handle(), window.label());
            }
            if let tauri::WindowEvent::Destroyed = event {
                alerts::window_gone(window.app_handle(), window.label());
                logs::stop_window(window.app_handle(), window.label());
            }
        })
        .invoke_handler(tauri::generate_handler![
            commands::app_info,
            commands::probe_write,
            commands::probe_request,
            commands::probe_eval,
            commands::updates_claim,
            commands::engine_open,
            commands::engine_retry,
            commands::engine_set_current_session,
            commands::events_take,
            commands::api,
            commands::api_abort,
            commands::settings_get,
            commands::settings_set,
            commands::closed_set,
            commands::clipboard_write,
            commands::clipboard_read,
            commands::open_link,
            commands::file_info,
            commands::file_read,
            commands::files_list,
            commands::api_secret,
            commands::window_open,
            commands::window_close,
            commands::project_switch,
            commands::pty_spawn,
            commands::pty_write,
            commands::pty_resize,
            commands::pty_cwd,
            commands::pty_close,
            commands::pty_close_all,
            commands::editor_prepare,
            commands::editor_finish,
            commands::alert_notify,
            commands::alert_sound,
            commands::alert_window_state,
            commands::alert_handler,
            commands::crash_pending,
            commands::crash_restart,
            commands::logs_tail,
            commands::logs_follow,
            commands::logs_unfollow,
            commands::selfupdate_run,
            commands::selfupdate_install,
            commands::crush_dirs,
            commands::crush_stats,
            commands::docker_mcp_version,
            commands::projects_list,
            commands::project_save,
            commands::project_remove,
            commands::recent_folders,
            commands::subagent_instructions_read,
            commands::subagent_instructions_write,
            commands::remote_password_exists,
            commands::remote_password_set,
            commands::dir_list,
            commands::folder_pick,
        ])
        .run(tauri::generate_context!())
        .expect("Crush Desktop could not start");
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

/// The navigation guard (1.2.9): every webview the app opens, now and in later
/// phases, may show only the app's own pages. A refusal is logged with the
/// address's scheme only, and in probe mode recorded in `<probe>.opened.jsonl`.
fn nav_guard<R: tauri::Runtime>(opened_log: Option<PathBuf>) -> tauri::plugin::TauriPlugin<R> {
    tauri::plugin::Builder::new("nav-guard")
        .on_navigation(move |_webview, url| {
            if nav_allowed(url) {
                return true;
            }
            eprintln!("crush-desktop: refused a navigation to a {}: address", url.scheme());
            if let Some(log) = &opened_log {
                let line = serde_json::json!({"refused_navigation": format!("{}:", url.scheme())});
                state::append_json_line(log, &line);
            }
            false
        })
        .build()
}

/// Only the app's own pages: there is no dev server (tauri.conf.json has only frontendDist).
fn nav_allowed(url: &tauri::Url) -> bool {
    url.scheme() == "tauri" && url.host_str() == Some("localhost")
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
    fn nav_guard_allows_only_app_pages() {
        let url = |s: &str| tauri::Url::parse(s).unwrap();
        assert!(nav_allowed(&url("tauri://localhost/")));
        assert!(nav_allowed(&url("tauri://localhost/index.html?zoom=1#x")));
        for refused in [
            "http://localhost/",
            "https://example.com/",
            "http://tauri.localhost/",
            "file:///etc/passwd",
            "data:text/html,<p>x</p>",
            "tauri://example.com/",
            "tauri://localhost.example.com/",
            "javascript:alert(1)",
            "about:blank",
        ] {
            assert!(!nav_allowed(&url(refused)), "{refused} must be refused");
        }
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
