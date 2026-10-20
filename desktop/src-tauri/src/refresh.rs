//! The page's frame rate and scrolling, measured by the page itself
//! (applog.js) and logged here, so a slow screen can be traced without
//! looking at it. `frame_rate_report` logs the rate the page measured.
//! CRUSH_SCROLL_TEST (owner, 2026-10-20) measures scrolling the same way: any
//! value but "" or "0" logs how often a scrolled list really moves and what
//! keeps the page busy. (Under WebKit the list moved only about 45 times a
//! second on the owner's 170 Hz screen; that is why the app moved to
//! Electron, which needs none of the WebKit switches this file once set.)

/// CRUSH_SCROLL_TEST: on for any value but "" or "0".
fn scroll_test_on() -> bool {
    !matches!(std::env::var("CRUSH_SCROLL_TEST").as_deref(), Err(_) | Ok("") | Ok("0"))
}

/// `frame_rate_report`: the page's measured frames per second.
pub fn frame_rate_report(window: crate::host::Window, rate: f64) {
    crate::applog::write("info", window.label(), &format!("frame rate {rate:.1}/s"));
}

/// `scroll_test_mode`: whether CRUSH_SCROLL_TEST is on.
pub fn scroll_test_mode() -> bool {
    scroll_test_on()
}

/// `scroll_test_report`: one scroll's measurement from the page.
pub fn scroll_test_report(window: crate::host::Window, text: String) {
    crate::applog::write("info", window.label(), &format!("scroll test: {text}"));
}
