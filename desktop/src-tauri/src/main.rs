// Crush Desktop. Phase 0 skeleton: opens the window and, for test runs
// started with `--probe <file>`, lets the page report what is on screen.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use serde::Serialize;
use std::{fs, path::PathBuf};

struct Launch {
    // Test runs only: where the page writes its on-screen report.
    probe: Option<PathBuf>,
}

#[derive(Serialize)]
struct AppInfo {
    version: &'static str,
    probe: bool,
}

#[tauri::command]
fn app_info(launch: tauri::State<Launch>) -> AppInfo {
    AppInfo { version: env!("CARGO_PKG_VERSION"), probe: launch.probe.is_some() }
}

#[tauri::command]
fn probe_write(launch: tauri::State<Launch>, json: String) -> Result<(), String> {
    let path = launch.probe.as_ref().ok_or("not started with --probe")?;
    // Write then rename, so a reader never sees half a report.
    let tmp = path.with_extension("tmp");
    fs::write(&tmp, json).map_err(|e| e.to_string())?;
    fs::rename(&tmp, path).map_err(|e| e.to_string())
}

fn arg_value(args: &[String], name: &str) -> Option<String> {
    args.iter().position(|a| a == name).and_then(|i| args.get(i + 1)).cloned()
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.iter().any(|a| a == "--version") {
        println!("crush-desktop {}", env!("CARGO_PKG_VERSION"));
        return;
    }
    let probe = arg_value(&args, "--probe").map(PathBuf::from);
    tauri::Builder::default()
        .manage(Launch { probe })
        .invoke_handler(tauri::generate_handler![app_info, probe_write])
        .run(tauri::generate_context!())
        .expect("Crush Desktop could not start");
}
