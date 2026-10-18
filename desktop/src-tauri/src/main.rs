// Crush Desktop: the window, its flags and its commands (desktop-design 1.2).
// Test runs start it with `--probe <file>` (the page reports what is on
// screen) and may add `--fixture <file>` (a recorded engine, 6.3).
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod batch;
mod clipboard;
mod commands;
mod connection;
mod crush_bin;
mod engine_cli;
mod error;
mod files;
mod fixture;
mod login_env;
mod opener;
mod settings;
mod sse;
mod state;
mod uds_http;

use state::{AppState, Launch};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use tauri::Manager;

fn main() {
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
    let mut builder = tauri::Builder::default()
        .plugin(nav_guard(opened_log))
        .manage(AppState {
            windows: Mutex::default(),
            settings: Mutex::new(settings::load()),
            launch,
        });
    if let Some(fixture) = fixture {
        builder = builder.manage(fixture);
    }
    builder
        .setup(|app| {
            settings::watch(app.handle().clone());
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                // Leave the engine first (DELETE /v1/clients/{id}, 1.2.6), then close.
                api.prevent_close();
                let window = window.clone();
                tauri::async_runtime::spawn(async move {
                    window_closing(&window).await;
                    let _ = window.destroy();
                });
            }
        })
        .invoke_handler(tauri::generate_handler![
            commands::app_info,
            commands::probe_write,
            commands::probe_request,
            commands::engine_open,
            commands::engine_retry,
            commands::engine_set_current_session,
            commands::events_take,
            commands::api,
            commands::settings_get,
            commands::settings_set,
            commands::closed_set,
            commands::clipboard_write,
            commands::clipboard_read,
            commands::open_link,
            commands::file_info,
            commands::file_read,
            commands::files_list,
        ])
        .run(tauri::generate_context!())
        .expect("Crush Desktop could not start");
}

/// The window's engine connection (or fixture player) ends with the window.
/// Q2 (what keeps running) is the engine's job.
async fn window_closing(window: &tauri::Window) {
    let app = window.app_handle().clone();
    let label = window.label().to_string();
    if let Some(fixture) = app.try_state::<fixture::Fixture>() {
        fixture.close(&label);
        return;
    }
    connection::close(&app, &label).await;
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

/// Reads the command line: the terminal's flags (internal/cmd/root.go:37-48)
/// plus the test-only --probe and --fixture. Unknown arguments are ignored.
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
