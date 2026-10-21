//! The one error shape every command reports (desktop-design 1.2.2). The
//! screens branch on `code`, never on `message`, which is plain words shown as
//! is. The app's own codes are the table of 1.2.2 (for example `aborted`, a
//! call stopped with `api_abort`); engine errors keep the engine's code.

use serde::Serialize;

#[derive(Debug, Clone, Serialize)]
pub struct AppError {
    /// A stable code; the screens branch on it, never on message.
    pub code: String,
    /// Plain words, shown as is.
    pub message: String,
    /// proto.Error.field (secure_field_error), passed through.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub field: Option<String>,
    /// The HTTP status when the error came from the engine.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<u16>,
}

pub type AppResult<T> = Result<T, AppError>;

/// The codes phases 5 and 6 add (desktop-design-5-6 6.4).
#[allow(dead_code)] // each is used by the piece that owns its command (7.3)
pub mod codes {
    /// `api_secret` was asked for a route that takes no secret.
    pub const SECRET_ROUTE: &str = "secret_route";
    pub const PTY_FAILED: &str = "pty_failed";
    pub const NO_SUCH_PTY: &str = "no_such_pty";
    pub const NO_EDITOR: &str = "no_editor";
    pub const NOT_A_FOLDER: &str = "not_a_folder";
    pub const DIALOG_FAILED: &str = "dialog_failed";
    pub const NOTIFY_FAILED: &str = "notify_failed";
    pub const UPDATE_RUNNING: &str = "update_running";
    /// The app's own updater (update.sh) is missing or gave no answer.
    pub const UPDATE_UNAVAILABLE: &str = "update_unavailable";
}

impl AppError {
    pub fn new(code: &str, message: impl Into<String>) -> Self {
        AppError {
            code: code.to_string(),
            message: message.into(),
            field: None,
            status: None,
        }
    }

    /// The scaffold's placeholder: a body its build piece has not written yet.
    #[allow(dead_code)] // every phase 5-6 command is built now; kept for new ones
    pub fn not_built(what: &str) -> Self {
        AppError::new("not_built", format!("{what} is not built yet"))
    }

    /// Decodes the engine's error body, proto.Error {message, code, field}
    /// (internal/proto/proto.go:37-41). A body without a code gets the one the
    /// engine gives a plain error of that status (statusCode,
    /// internal/server/errors.go:26-41), and any other status `http_<status>`,
    /// so the code is never empty.
    #[allow(dead_code)] // called by uds_http.rs (P3.1)
    pub fn from_engine(status: u16, body: &[u8]) -> Self {
        #[derive(serde::Deserialize)]
        struct EngineError {
            #[serde(default)]
            message: String,
            #[serde(default)]
            code: String,
            #[serde(default)]
            field: Option<String>,
        }
        let (message, code, field) = match serde_json::from_slice::<EngineError>(body) {
            Ok(e) => (e.message, e.code, e.field.filter(|f| !f.is_empty())),
            Err(_) => (String::from_utf8_lossy(body).trim().to_string(), String::new(), None),
        };
        let code = if code.is_empty() { status_code(status) } else { code };
        let message = if message.is_empty() {
            format!("the engine answered HTTP {status}")
        } else {
            message
        };
        AppError {
            code,
            message,
            field,
            status: Some(status),
        }
    }
}

#[allow(dead_code)] // used by from_engine
fn status_code(status: u16) -> String {
    match status {
        400 => "bad_request".to_string(),
        404 => "not_found".to_string(),
        409 => "conflict".to_string(),
        500 => "internal".to_string(),
        503 => "shutting_down".to_string(),
        _ => format!("http_{status}"),
    }
}

impl std::fmt::Display for AppError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{} ({})", self.message, self.code)
    }
}

impl std::error::Error for AppError {}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn from_engine_keeps_the_engines_code_and_field() {
        let body = json!({"message": "wrong key", "code": "secure_field_error", "field": "token"});
        let e = AppError::from_engine(400, body.to_string().as_bytes());
        assert_eq!(e.code, "secure_field_error");
        assert_eq!(e.message, "wrong key");
        assert_eq!(e.field.as_deref(), Some("token"));
        assert_eq!(e.status, Some(400));
    }

    #[test]
    fn from_engine_fills_the_code_by_status() {
        for (status, code) in [
            (400, "bad_request"),
            (404, "not_found"),
            (409, "conflict"),
            (500, "internal"),
            (503, "shutting_down"),
        ] {
            let e = AppError::from_engine(status, br#"{"message":"no"}"#);
            assert_eq!(e.code, code, "status {status}");
            assert_eq!(e.message, "no");
        }
        let e = AppError::from_engine(502, b"");
        assert_eq!(e.message, "the engine answered HTTP 502");
        let e = AppError::from_engine(500, b"plain text\n");
        assert_eq!((e.code.as_str(), e.message.as_str()), ("internal", "plain text"));
    }

    #[test]
    fn from_engine_names_any_other_status_by_number() {
        for status in [401, 418, 502] {
            let e = AppError::from_engine(status, br#"{"message":"no"}"#);
            assert_eq!(e.code, format!("http_{status}"));
            assert_eq!(e.status, Some(status));
        }
        assert_eq!(AppError::from_engine(502, b"").code, "http_502");
        let kept = AppError::from_engine(418, br#"{"message":"no","code":"teapot"}"#);
        assert_eq!(kept.code, "teapot");
    }

    #[test]
    fn serializes_without_empty_options() {
        let v = serde_json::to_value(AppError::not_built("x")).unwrap();
        assert_eq!(v, json!({"code": "not_built", "message": "x is not built yet"}));
    }
}
