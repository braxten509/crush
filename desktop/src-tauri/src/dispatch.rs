//! A page's invoke(cmd, args), as Electron passes it on (host.rs): the
//! command's arguments by their page names (camelCase, as Tauri named them),
//! its answer or its error as JSON. Every command runs on a worker: the async
//! ones as tasks, the others on the blocking pool, so a slow one never holds
//! up the line to Electron.

use crate::host::{AppHandle, Channel};
use crate::{applog, commands, refresh};
use serde::de::DeserializeOwned;
use serde::Serialize;
use serde_json::{json, Value};

type Reply = Result<Value, Value>;

/// The argument `name`; a missing one reads as null (so an Option is None).
fn arg<T: DeserializeOwned>(args: &Value, name: &str) -> Result<T, Value> {
    let value = args.get(name).cloned().unwrap_or(Value::Null);
    serde_json::from_value(value).map_err(|e| json!(format!("invalid args `{name}`: {e}")))
}

/// A page's `new Channel()`, which the page sent as {"__channel": id}.
fn channel(app: &AppHandle, label: &str, args: &Value, name: &str) -> Result<Channel<Value>, Value> {
    let id = args
        .get(name)
        .and_then(|c| c.get("__channel"))
        .and_then(Value::as_u64)
        .ok_or_else(|| json!(format!("invalid args `{name}`: not a channel")))?;
    Ok(Channel::new(app, label, id))
}

fn ok<T: Serialize>(value: T) -> Reply {
    serde_json::to_value(value).map_err(|e| json!(e.to_string()))
}

fn res<T: Serialize, E: Serialize>(result: Result<T, E>) -> Reply {
    match result {
        Ok(value) => ok(value),
        Err(e) => Err(serde_json::to_value(e).unwrap_or_else(|e| json!(e.to_string()))),
    }
}

/// Runs `work` on the blocking pool.
async fn blocking(work: impl FnOnce() -> Reply + Send + 'static) -> Reply {
    tokio::task::spawn_blocking(work).await.unwrap_or_else(|e| Err(json!(format!("the command failed: {e}"))))
}

pub async fn call(app: AppHandle, label: String, cmd: String, args: Value) -> Reply {
    let w = app.window(&label);
    match cmd.as_str() {
        // Async commands.
        "engine_open" => res(commands::engine_open(app.clone(), w, app.state(), arg(&args, "cwd")?).await),
        "engine_open_folder" => res(commands::engine_open_folder(app.clone(), w, arg(&args, "cwd")?).await),
        "engine_show_folder" => res(commands::engine_show_folder(app.clone(), w, arg(&args, "cwd")?).await),
        "engine_leave_folder" => res(commands::engine_leave_folder(app.clone(), w, arg(&args, "cwd")?).await),
        "engine_retry" => res(commands::engine_retry(app.clone(), w).await),
        "engine_set_current_session" => {
            res(commands::engine_set_current_session(app.clone(), w, app.state(), arg(&args, "sessionId")?).await)
        }
        "api" => res(commands::api(
            app.clone(),
            w,
            app.state(),
            arg(&args, "method")?,
            arg(&args, "path")?,
            arg(&args, "query")?,
            arg(&args, "body")?,
            arg(&args, "callId")?,
            arg(&args, "dir")?,
        )
        .await),
        "api_secret" => res(commands::api_secret(app.clone(), w, app.state(), arg(&args, "path")?, arg(&args, "body")?).await),
        "window_close" => res(commands::window_close(app.clone(), w, arg(&args, "stop")?).await),
        "project_switch" => res(commands::project_switch(app.clone(), w, arg(&args, "projectDir")?).await),
        "project_enter" => res(commands::project_enter(app.clone(), w, arg(&args, "projectDir")?).await),
        // No engines in fixture runs: a list with nothing in it.
        "folder_chat_states" => match app.try_state::<crate::fixture::Fixture>() {
            Some(_) => ok(json!({})),
            None => ok(crate::watch::chat_states(arg(&args, "chats")?).await),
        },
        // Everything else, on the blocking pool.
        _ => blocking(move || sync_call(&app, w, &cmd, &args)).await,
    }
}

fn sync_call(app: &AppHandle, w: crate::host::Window, cmd: &str, args: &Value) -> Reply {
    let a = app.clone();
    match cmd {
        "app_info" => ok(commands::app_info(w, app.state(), app.state())),
        "window_close_hold" => ok(commands::window_close_hold(
            app,
            w,
            arg::<Option<bool>>(&args, "asking")?.unwrap_or(true),
            arg::<Option<bool>>(&args, "closing")?.unwrap_or(false),
        )),
        "probe_write" => res(commands::probe_write(w, app.state(), arg(args, "json")?)),
        "probe_request" => res(commands::probe_request(app.state())),
        "probe_eval" => res(commands::probe_eval(w, app.state())),
        "updates_claim" => ok(commands::updates_claim()),
        "frame_rate_report" => ok(refresh::frame_rate_report(w, arg(args, "rate")?)),
        "scroll_test_mode" => ok(refresh::scroll_test_mode()),
        "scroll_test_report" => ok(refresh::scroll_test_report(w, arg(args, "text")?)),
        "app_log" => ok(applog::app_log(w, arg(args, "level")?, arg(args, "message")?)),
        "events_take" => ok(commands::events_take(a, w)),
        "api_abort" => ok(commands::api_abort(a, w, app.state(), arg(args, "callId")?)),
        "settings_get" => ok(commands::settings_get(app.state())),
        "open_chats_get" => ok(crate::openchats::chats()),
        "open_chats_add" => res(crate::openchats::add(crate::openchats::Chat {
            id: arg(args, "id")?,
            dir: arg(args, "dir")?,
            title: arg::<Option<String>>(args, "title")?.unwrap_or_default(),
            opened: 0,
        })),
        "open_chats_remove" => res(crate::openchats::remove(&arg::<String>(args, "id")?)),
        "settings_set" => res(commands::settings_set(a, arg(args, "key")?, arg(args, "value")?)),
        "closed_set" => res(commands::closed_set(a, arg(args, "project")?, arg(args, "sessionId")?, arg(args, "closed")?)),
        "clipboard_write" => res(commands::clipboard_write(a, arg(args, "text")?)),
        "clipboard_read" => res(commands::clipboard_read(a)),
        "open_link" => res(commands::open_link(a, arg(args, "url")?, arg(args, "cwd")?)),
        "file_info" => res(commands::file_info(arg(args, "path")?, arg(args, "cwd")?)),
        "file_read" => res(commands::file_read(arg(args, "path")?, arg(args, "cwd")?, arg(args, "maxBytes")?)),
        "files_list" => res(commands::files_list(w, arg(args, "cwd")?, arg(args, "maxDepth")?, arg(args, "maxItems")?)),
        "window_open" => res(commands::window_open(
            a,
            arg(args, "projectDir")?,
            arg(args, "sessionId")?,
            arg(args, "newChat")?,
            arg(args, "launchId")?,
        )),
        "pty_spawn" => {
            let on_data = channel(app, w.label(), args, "onData")?;
            res(commands::pty_spawn(a, w, arg(args, "cwd")?, arg(args, "argv")?, arg(args, "cols")?, arg(args, "rows")?, on_data))
        }
        "pty_write" => res(commands::pty_write(a, arg(args, "id")?, arg(args, "data")?)),
        "pty_resize" => res(commands::pty_resize(a, arg(args, "id")?, arg(args, "cols")?, arg(args, "rows")?)),
        "pty_cwd" => res(commands::pty_cwd(a, arg(args, "id")?)),
        "pty_close" => res(commands::pty_close(a, arg(args, "id")?)),
        "pty_close_all" => res(commands::pty_close_all(a, w)),
        "editor_prepare" => res(commands::editor_prepare(app.state(), arg(args, "text")?, arg(args, "line")?, arg(args, "column")?)),
        "editor_finish" => res(commands::editor_finish(app.state(), arg(args, "path")?)),
        "alert_notify" => res(commands::alert_notify(a, arg(args, "title")?, arg(args, "body")?)),
        "alert_sound" => res(commands::alert_sound(a, arg(args, "id")?)),
        "alert_window_state" => res(commands::alert_window_state(
            a,
            w,
            arg(args, "state")?,
            arg(args, "projectDir")?,
            arg(args, "sessionId")?,
            arg(args, "focused")?,
            arg::<Option<Vec<String>>>(args, "waiting")?.unwrap_or_default(),
        )),
        "alert_handler" => res(commands::alert_handler(a, arg(args, "projectDir")?, arg(args, "sessionId")?)),
        "crash_pending" => res(commands::crash_pending(a, w)),
        "crash_restart" => res(commands::crash_restart(a, w)),
        "logs_tail" => res(commands::logs_tail(a, w, arg(args, "lines")?)),
        "logs_follow" => {
            let on_lines = channel(app, w.label(), args, "onLines")?;
            res(commands::logs_follow(a, w, arg(args, "from")?, on_lines))
        }
        "logs_unfollow" => res(commands::logs_unfollow(a, arg(args, "followId")?)),
        "selfupdate_run" => res(commands::selfupdate_run(a, arg(args, "tag")?)),
        "selfupdate_install" => res(commands::selfupdate_install(a)),
        "app_update_check" => res(commands::app_update_check(a)),
        "app_update_run" => res(commands::app_update_run(a)),
        "app_restart_hold" => res(Ok::<_, crate::error::AppError>(commands::app_restart_hold())),
        "crush_dirs" => res(commands::crush_dirs(a, w)),
        "crush_stats" => res(commands::crush_stats(a, w)),
        "docker_mcp_version" => res(commands::docker_mcp_version()),
        "sign_in_run" => res(commands::sign_in_run(arg(args, "argv")?)),
        "projects_list" => res(commands::projects_list()),
        "project_save" => res(commands::project_save(arg(args, "path")?)),
        "project_remove" => res(commands::project_remove(arg(args, "path")?)),
        "recent_folders" => res(commands::recent_folders(arg(args, "limit")?)),
        "subagent_instructions_read" => res(commands::subagent_instructions_read()),
        "subagent_instructions_write" => res(commands::subagent_instructions_write(arg(args, "text")?)),
        "remote_password_exists" => res(commands::remote_password_exists()),
        "remote_password_set" => res(commands::remote_password_set(a, arg(args, "first")?, arg(args, "second")?)),
        "dir_list" => res(commands::dir_list(arg(args, "path")?)),
        "folder_pick" => res(commands::folder_pick(a, arg(args, "start")?)),
        _ => Err(json!(format!("Command {cmd} not found"))),
    }
}
