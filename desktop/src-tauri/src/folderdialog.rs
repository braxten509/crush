//! The system folder dialog (desktop-design-5-6 2.21, 6.4 `folder_pick`):
//! the folder picker's "Browse…". The dialog (tauri-plugin-dialog) opens at
//! `start` and blocks the command's worker thread, never the main one (the
//! command is async), until a folder is picked or the dialog is cancelled.
//!
//! Test runs (`--probe`) never open the dialog (8.4), so nothing appears on
//! the screen and no command waits on an answer nobody gives: the answer is
//! `CRUSH_DESKTOP_TEST_PICK_FOLDER`'s path, or a cancel when it is unset.

use crate::error::{AppError, AppResult};
use crate::state::AppState;
use std::path::Path;
use tauri::{AppHandle, Manager};
use tauri_plugin_dialog::DialogExt;

/// The stand-in for the dialog's answer, honored only with `--probe`.
const TEST_PICK: &str = "CRUSH_DESKTOP_TEST_PICK_FOLDER";

/// `folder_pick`: a path, or None when cancelled.
pub fn pick(app: &AppHandle, start: &str) -> AppResult<Option<String>> {
    let probe = app
        .try_state::<AppState>()
        .is_some_and(|state| state.launch.probe.is_some());
    if probe {
        return Ok(test_answer(std::env::var(TEST_PICK).ok()));
    }
    let mut dialog = app.dialog().file().set_title("Choose a folder");
    if Path::new(start).is_dir() {
        dialog = dialog.set_directory(start);
    }
    match dialog.blocking_pick_folder() {
        None => Ok(None),
        Some(picked) => picked
            .into_path()
            .map(|path| Some(path.to_string_lossy().into_owned()))
            .map_err(|e| {
                AppError::new(
                    "folder_pick_failed",
                    format!("The folder dialog gave no folder that can be used: {e}"),
                )
            }),
    }
}

/// A probe run's answer: the variable's value, or a cancel when it is unset.
fn test_answer(value: Option<String>) -> Option<String> {
    value.filter(|path| !path.is_empty())
}
