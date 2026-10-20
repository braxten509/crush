//! The helper's side of Electron (owner 2026-10-20: the app moved from Tauri's
//! WebKit to Electron, which scrolls at the screen's full rate). Electron's
//! main process (electron/main.cjs) owns the windows; this program keeps the
//! engine connections, files, terminals, settings and alerts. The two talk in
//! JSON lines, one message per line:
//!
//! Electron → helper (stdin)
//!   {"t":"call","id":N,"label":L,"cmd":C,"args":{…}}   a page's invoke(C, args)
//!   {"t":"reply","id":N,"ok":V} | {"t":"reply","id":N,"err":"…"}  answer to an ask
//!   {"t":"window","label":L,"event":"created"|"close-requested"|"destroyed"|"focus"|"blur"|"crashed"}
//!   {"t":"handoff","argv":[…],"cwd":"…"}               a second start (single instance)
//!   {"t":"refused","scheme":"…"}                       a navigation the window refused
//! helper → Electron (stdout)
//!   {"t":"ready"}
//!   {"t":"result","id":N,"ok":V} | {"t":"result","id":N,"err":V}
//!   {"t":"emit","label":L|null,"event":E,"payload":P}  null: every window
//!   {"t":"channel","label":L,"channel":C,"data":D}
//!   {"t":"ask","id":M,"op":O,…}                        Electron answers with a reply
//!   {"t":"do","op":O,…}                                no answer
//!
//! The names below follow Tauri's (AppHandle, Window, State, Channel,
//! async_runtime), so the modules kept their code.

use serde::Serialize;
use serde_json::{json, Map, Value};
use std::any::{Any, TypeId};
use std::collections::HashMap;
use std::io::Write;
use std::marker::PhantomData;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{mpsc, Arc, Mutex};
use std::time::Duration;

#[derive(Debug, Clone)]
pub struct Error(pub String);

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for Error {}

type Answer = Result<Value, String>;

struct Inner {
    out: Mutex<Box<dyn Write + Send>>,
    states: Mutex<HashMap<TypeId, Arc<dyn Any + Send + Sync>>>,
    /// The open windows and whether each has focus.
    windows: Mutex<HashMap<String, bool>>,
    asks: Mutex<HashMap<u64, mpsc::Sender<Answer>>>,
    next: AtomicU64,
}

/// The app: its kept states, its windows and the line to Electron.
#[derive(Clone)]
pub struct AppHandle(Arc<Inner>);

impl AppHandle {
    pub fn new(out: Box<dyn Write + Send>) -> Self {
        AppHandle(Arc::new(Inner {
            out: Mutex::new(out),
            states: Mutex::default(),
            windows: Mutex::default(),
            asks: Mutex::default(),
            next: AtomicU64::new(1),
        }))
    }

    /// Keeps `value` for `state`; false when one of its type is already kept.
    pub fn manage<T: Send + Sync + 'static>(&self, value: T) -> bool {
        let mut states = self.0.states.lock().unwrap();
        if states.contains_key(&TypeId::of::<T>()) {
            return false;
        }
        states.insert(TypeId::of::<T>(), Arc::new(value));
        true
    }

    pub fn try_state<T: Send + Sync + 'static>(&self) -> Option<State<'_, T>> {
        let kept = self.0.states.lock().unwrap().get(&TypeId::of::<T>()).cloned()?;
        kept.downcast::<T>().ok().map(|value| State(value, PhantomData))
    }

    /// A kept state; like Tauri, a missing one is a bug in the app.
    pub fn state<T: Send + Sync + 'static>(&self) -> State<'_, T> {
        self.try_state()
            .unwrap_or_else(|| panic!("no state of type {} is kept", std::any::type_name::<T>()))
    }

    /// One message to Electron. A closed line means Electron is gone; the
    /// reader sees that too and ends the program.
    pub fn send(&self, message: &Value) {
        let mut out = self.0.out.lock().unwrap_or_else(|e| e.into_inner());
        let _ = writeln!(out, "{message}");
        let _ = out.flush();
    }

    /// `event` to every window.
    pub fn emit<S: Serialize>(&self, event: &str, payload: S) -> Result<(), Error> {
        let payload = serde_json::to_value(payload).map_err(|e| Error(e.to_string()))?;
        self.send(&json!({"t": "emit", "label": null, "event": event, "payload": payload}));
        Ok(())
    }

    /// `event` to the window `label`.
    pub fn emit_to<S: Serialize>(&self, label: &str, event: &str, payload: S) -> Result<(), Error> {
        let payload = serde_json::to_value(payload).map_err(|e| Error(e.to_string()))?;
        if !self.0.windows.lock().unwrap().contains_key(label) {
            return Err(Error(format!("there is no window {label}")));
        }
        self.send(&json!({"t": "emit", "label": label, "event": event, "payload": payload}));
        Ok(())
    }

    /// The window `label`, whether or not it is still open (a page's call names it).
    pub fn window(&self, label: &str) -> Window {
        Window { label: label.to_string(), app: self.clone() }
    }

    pub fn get_webview_window(&self, label: &str) -> Option<WebviewWindow> {
        self.0.windows.lock().unwrap().contains_key(label).then(|| self.window(label))
    }

    pub fn webview_windows(&self) -> HashMap<String, WebviewWindow> {
        let labels: Vec<String> = self.0.windows.lock().unwrap().keys().cloned().collect();
        labels.into_iter().map(|label| (label.clone(), self.window(&label))).collect()
    }

    /// Asks Electron to open the window `label` and waits until it is there.
    pub fn create_window(&self, label: &str) -> Result<WebviewWindow, Error> {
        self.ask("window.create", json!({"label": label}), Some(Duration::from_secs(20)))
            .map_err(Error)?;
        self.window_created(label);
        Ok(self.window(label))
    }

    /// Asks Electron for something and waits for the answer: up to `wait`,
    /// or for as long as it takes (a dialog the user answers). Call it off
    /// the async workers: it blocks its thread.
    pub fn ask(&self, op: &str, data: Value, wait: Option<Duration>) -> Answer {
        let id = self.0.next.fetch_add(1, Ordering::SeqCst);
        let (tx, rx) = mpsc::channel();
        self.0.asks.lock().unwrap().insert(id, tx);
        let mut message = Map::new();
        message.insert("t".into(), "ask".into());
        message.insert("id".into(), id.into());
        message.insert("op".into(), op.into());
        if let Value::Object(fields) = data {
            message.extend(fields);
        }
        self.send(&Value::Object(message));
        let answer = match wait {
            Some(wait) => rx.recv_timeout(wait).map_err(|_| format!("no answer to {op} in time")),
            None => rx.recv().map_err(|_| format!("no answer to {op}")),
        };
        self.0.asks.lock().unwrap().remove(&id);
        answer?
    }

    /// Something for Electron to do, without an answer.
    pub fn tell(&self, op: &str, data: Value) {
        let mut message = Map::new();
        message.insert("t".into(), "do".into());
        message.insert("op".into(), op.into());
        if let Value::Object(fields) = data {
            message.extend(fields);
        }
        self.send(&Value::Object(message));
    }

    /// Electron's answer to an ask.
    pub fn answer(&self, id: u64, answer: Answer) {
        if let Some(tx) = self.0.asks.lock().unwrap().remove(&id) {
            let _ = tx.send(answer);
        }
    }

    pub fn window_created(&self, label: &str) {
        self.0.windows.lock().unwrap().entry(label.to_string()).or_insert(false);
    }

    pub fn window_gone(&self, label: &str) {
        self.0.windows.lock().unwrap().remove(label);
    }

    pub fn set_focused(&self, label: &str, focused: bool) {
        if let Some(entry) = self.0.windows.lock().unwrap().get_mut(label) {
            *entry = focused;
        }
    }

    fn is_focused(&self, label: &str) -> bool {
        self.0.windows.lock().unwrap().get(label).copied().unwrap_or(false)
    }
}

/// A kept state, borrowed for a call.
pub struct State<'a, T: 'static>(Arc<T>, PhantomData<&'a ()>);

impl<T> std::ops::Deref for State<'_, T> {
    type Target = T;
    fn deref(&self) -> &T {
        &self.0
    }
}

/// One window, by its label. Each action is passed to Electron.
#[derive(Clone)]
pub struct Window {
    label: String,
    app: AppHandle,
}

pub type WebviewWindow = Window;

impl Window {
    pub fn label(&self) -> &str {
        &self.label
    }

    fn act(&self, action: &str, extra: Value) -> Result<(), Error> {
        let mut data = json!({"label": self.label, "action": action});
        if let (Value::Object(fields), Value::Object(more)) = (&mut data, extra) {
            fields.extend(more);
        }
        self.app.tell("window", data);
        Ok(())
    }

    pub fn unminimize(&self) -> Result<(), Error> {
        self.act("unminimize", Value::Null)
    }

    pub fn show(&self) -> Result<(), Error> {
        self.act("show", Value::Null)
    }

    pub fn set_focus(&self) -> Result<(), Error> {
        self.act("focus", Value::Null)
    }

    /// Ends the window without the close steps.
    pub fn destroy(&self) -> Result<(), Error> {
        self.act("destroy", Value::Null)
    }

    pub fn eval(&self, script: impl Into<String>) -> Result<(), Error> {
        self.act("eval", json!({"script": script.into()}))
    }

    pub fn is_focused(&self) -> Result<bool, Error> {
        Ok(self.app.is_focused(&self.label))
    }

    /// The taskbar's attention mark for this window.
    pub fn request_user_attention(&self) -> Result<(), Error> {
        self.act("attention", Value::Null)
    }

    /// The app icon's count (None clears it) and busy mark.
    pub fn set_taskbar(&self, badge: Option<i64>, busy: bool) -> Result<(), Error> {
        self.act("taskbar", json!({"badge": badge, "busy": busy}))
    }
}

/// A page's stream for one call's later messages (a terminal's output, a
/// log's new lines): `new Channel()` in the page, `{"__channel": id}` here.
pub struct Channel<T> {
    app: AppHandle,
    label: String,
    id: u64,
    kind: PhantomData<fn(T)>,
}

impl<T> Clone for Channel<T> {
    fn clone(&self) -> Self {
        Channel { app: self.app.clone(), label: self.label.clone(), id: self.id, kind: PhantomData }
    }
}

impl<T: Serialize> Channel<T> {
    pub fn new(app: &AppHandle, label: &str, id: u64) -> Self {
        Channel { app: app.clone(), label: label.to_string(), id, kind: PhantomData }
    }

    pub fn send(&self, data: T) -> Result<(), Error> {
        let data = serde_json::to_value(data).map_err(|e| Error(e.to_string()))?;
        self.app.send(&json!({"t": "channel", "label": self.label, "channel": self.id, "data": data}));
        Ok(())
    }
}

/// Tauri's async_runtime: one tokio runtime for the program's life.
pub mod async_runtime {
    use std::future::Future;
    use std::sync::OnceLock;

    pub use tokio::task::JoinHandle;

    pub fn runtime() -> &'static tokio::runtime::Runtime {
        static RUNTIME: OnceLock<tokio::runtime::Runtime> = OnceLock::new();
        RUNTIME.get_or_init(|| {
            tokio::runtime::Builder::new_multi_thread()
                .enable_all()
                .thread_name("crush-desktop-worker")
                .build()
                .expect("the helper's async runtime could not start")
        })
    }

    pub fn spawn<F>(future: F) -> JoinHandle<F::Output>
    where
        F: Future + Send + 'static,
        F::Output: Send + 'static,
    {
        runtime().spawn(future)
    }

    pub fn spawn_blocking<F, R>(work: F) -> JoinHandle<R>
    where
        F: FnOnce() -> R + Send + 'static,
        R: Send + 'static,
    {
        runtime().spawn_blocking(work)
    }

    #[cfg_attr(not(test), allow(dead_code))] // the tests' helper
    pub fn block_on<F: Future>(future: F) -> F::Output {
        runtime().block_on(future)
    }
}
