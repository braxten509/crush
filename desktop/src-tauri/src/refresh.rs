//! The display's full refresh rate (owner, 2026-10-19). WebKit draws a page
//! about 60 times a second while its PreferPageRenderingUpdatesNear60FPS
//! feature is on, which is the default. The menu entry's preload
//! (~/.local/lib/webkit-full-refresh) turns it off for apps that read their
//! settings; `full_rate` turns it off in every web view the app makes, and
//! `frame_rate_report` logs the rate the page measured (applog.rs), so a slow
//! rate can be traced without looking at the screen. The feature switch arrived in
//! WebKitGTK 2.42, newer than the webkit2gtk crate's bindings, so its
//! functions are looked up at run time.
use std::collections::HashMap;
use std::sync::Mutex;

static STATE: Mutex<Option<HashMap<String, &'static str>>> = Mutex::new(None);

/// Turns the 60-a-second preference off in the window's web view (Linux).
pub fn full_rate<R: tauri::Runtime>(window: &tauri::WebviewWindow<R>) {
    #[cfg(target_os = "linux")]
    {
        let label = window.label().to_string();
        let _ = window.with_webview(move |webview| {
            use gtk::glib::object::ObjectType;
            use webkit2gtk::WebViewExt;
            let state = match webview.inner().settings() {
                Some(settings) => unsafe { turn_off(settings.as_ptr() as *mut std::ffi::c_void) },
                None => "no settings",
            };
            STATE.lock().unwrap_or_else(|e| e.into_inner()).get_or_insert_with(HashMap::new).insert(label, state);
        });
    }
    #[cfg(not(target_os = "linux"))]
    let _ = window;
}

#[cfg(target_os = "linux")]
unsafe fn sym(name: &str) -> *mut std::ffi::c_void {
    let name = std::ffi::CString::new(name).unwrap_or_default();
    libc::dlsym(libc::RTLD_DEFAULT, name.as_ptr())
}

/// What happened: "turned off", "already off", or why nothing was done.
#[cfg(target_os = "linux")]
unsafe fn turn_off(settings: *mut std::ffi::c_void) -> &'static str {
    type Ptr = *mut std::ffi::c_void;
    let (all, length, get, ident, enabled, set) = (
        sym("webkit_settings_get_all_features"),
        sym("webkit_feature_list_get_length"),
        sym("webkit_feature_list_get"),
        sym("webkit_feature_get_identifier"),
        sym("webkit_settings_get_feature_enabled"),
        sym("webkit_settings_set_feature_enabled"),
    );
    if [all, length, get, ident, enabled, set].iter().any(|f| f.is_null()) {
        return "no feature switch in this WebKit";
    }
    let all: extern "C" fn() -> Ptr = std::mem::transmute(all);
    let length: extern "C" fn(Ptr) -> usize = std::mem::transmute(length);
    let get: extern "C" fn(Ptr, usize) -> Ptr = std::mem::transmute(get);
    let ident: extern "C" fn(Ptr) -> *const std::ffi::c_char = std::mem::transmute(ident);
    let enabled: extern "C" fn(Ptr, Ptr) -> i32 = std::mem::transmute(enabled);
    let set: extern "C" fn(Ptr, Ptr, i32) = std::mem::transmute(set);
    // The list lives as long as the process, so the feature it holds stays valid.
    let list = all();
    for i in 0..length(list) {
        let feature = get(list, i);
        let name = std::ffi::CStr::from_ptr(ident(feature)).to_string_lossy();
        if name == "PreferPageRenderingUpdatesNear60FPS" || name == "PreferPageRenderingUpdatesNear60FPSEnabled" {
            if enabled(settings, feature) == 0 {
                return "already off";
            }
            set(settings, feature, 0);
            return "turned off";
        }
    }
    "no such feature"
}

/// `frame_rate_report`: the page's measured frames per second, logged with
/// the window's feature state and whether the menu entry's preload is in.
#[tauri::command]
pub fn frame_rate_report(window: tauri::Window, rate: f64) {
    let state = STATE.lock().unwrap_or_else(|e| e.into_inner()).as_ref().and_then(|m| m.get(window.label()).copied()).unwrap_or("not set");
    let preload = std::fs::read_to_string("/proc/self/maps").map(|m| m.contains("libwebkit-full-refresh")).unwrap_or(false);
    crate::applog::write("info", window.label(), &format!("frame rate {rate:.1}/s, near-60 preference {state}, menu preload {preload}"));
}
