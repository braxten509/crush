//! HTTP/1.1 over the engine's unix socket (desktop-design 1.2.4). One new
//! connection per call: a unix connect costs microseconds, and no pool can go
//! stale across an engine restart. The engine answers any `Host`.

use crate::error::{AppError, AppResult};
use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::client::conn::http1::{self, SendRequest};
use hyper::header::{CONTENT_LENGTH, CONTENT_TYPE, HOST};
use hyper::{Method, Request};
use hyper_util::rt::TokioIo;
use std::time::Duration;
use tokio::net::UnixStream;

pub struct EngineHttp {
    socket: std::path::PathBuf,
}

/// body is Null when empty.
pub struct Reply {
    pub status: u16,
    pub body: serde_json::Value,
}

/// The task that drives one call's connection. Dropping it (the call ended,
/// or was itself stopped) aborts the task, which closes the socket: that is
/// how the engine learns a request was given up (1.2.4).
struct ConnTask(tokio::task::JoinHandle<()>);

impl Drop for ConnTask {
    fn drop(&mut self) {
        self.0.abort();
    }
}

impl EngineHttp {
    pub fn new(socket: &str) -> Self {
        EngineHttp {
            socket: std::path::PathBuf::from(socket),
        }
    }

    /// The engine's socket: one per engine, so it tells engines apart.
    pub fn socket(&self) -> &std::path::Path {
        &self.socket
    }

    /// `path_and_query` starts with "/v1/". Body is sent as JSON when Some, always with
    /// `Content-Length` (`http_body_util::Full<Bytes>`), never chunked (1.2.4).
    /// 2xx → Ok(Reply). Other statuses → Err(AppError::from_engine). Connect refused or a missing
    /// socket → Err(code "engine_unavailable"). A break after the request was written → Err("transport").
    /// `limit` None: no time limit (an `api` call with a `callId`, 1.3).
    pub async fn call(
        &self,
        method: &str,
        path_and_query: &str,
        body: Option<&serde_json::Value>,
        limit: Option<std::time::Duration>,
    ) -> AppResult<Reply> {
        let exchange = self.exchange(method, path_and_query, body);
        match limit {
            None => exchange.await,
            Some(limit) => match tokio::time::timeout(limit, exchange).await {
                Ok(result) => result,
                // The exchange is dropped here, which closes its socket.
                Err(_) => Err(AppError::new(
                    "timeout",
                    format!("The Crush engine did not answer within {}.", span(limit)),
                )),
            },
        }
    }

    async fn exchange(&self, method: &str, target: &str, body: Option<&serde_json::Value>) -> AppResult<Reply> {
        let request = request(method, target, body)?;
        let (mut sender, _conn) = self.connect().await?;
        let response = sender.send_request(request).await.map_err(transport)?;
        let status = response.status().as_u16();
        let bytes = response.into_body().collect().await.map_err(transport)?.to_bytes();
        if !(200..300).contains(&status) {
            return Err(AppError::from_engine(status, &bytes));
        }
        Ok(Reply {
            status,
            body: json_body(&bytes)?,
        })
    }

    /// Opens a GET and returns the response body as a stream of raw byte chunks. Err on non-200.
    pub async fn stream(&self, path_and_query: &str) -> AppResult<ByteStream> {
        let request = request("GET", path_and_query, None)?;
        let (mut sender, conn) = self.connect().await?;
        let response = sender.send_request(request).await.map_err(transport)?;
        let status = response.status().as_u16();
        if status != 200 {
            let body = response.into_body().collect();
            let bytes = match tokio::time::timeout(Duration::from_secs(5), body).await {
                Ok(Ok(collected)) => collected.to_bytes(),
                _ => Bytes::new(),
            };
            return Err(AppError::from_engine(status, &bytes));
        }
        Ok(ByteStream {
            body: response.into_body(),
            _sender: sender,
            _conn: conn,
        })
    }

    async fn connect(&self) -> AppResult<(SendRequest<Full<Bytes>>, ConnTask)> {
        let stream = UnixStream::connect(&self.socket).await.map_err(|e| {
            AppError::new(
                "engine_unavailable",
                format!("Crush Desktop cannot reach the Crush engine at {}: {e}.", self.socket.display()),
            )
        })?;
        let (sender, conn) = http1::handshake(TokioIo::new(stream)).await.map_err(transport)?;
        let task = tokio::spawn(async move {
            let _ = conn.await;
        });
        Ok((sender, ConnTask(task)))
    }
}

pub struct ByteStream {
    body: hyper::body::Incoming,
    /// Kept, so the connection is not wound down while the body streams.
    _sender: SendRequest<Full<Bytes>>,
    /// Dropping the stream closes its socket.
    _conn: ConnTask,
}

impl ByteStream {
    pub async fn next_chunk(&mut self) -> Option<AppResult<bytes::Bytes>> {
        loop {
            match self.body.frame().await? {
                Err(e) => return Some(Err(transport(e))),
                // Trailers and empty frames carry no event bytes.
                Ok(frame) => match frame.into_data() {
                    Ok(data) if !data.is_empty() => return Some(Ok(data)),
                    _ => continue,
                },
            }
        }
    }
}

/// The request: `host: crush`, a JSON content type, and every body with a
/// `Content-Length` (a POST without one says 0), so it is never chunked: Go's
/// server notices a closed connection only after it read the body to its end.
fn request(method: &str, target: &str, body: Option<&serde_json::Value>) -> AppResult<Request<Full<Bytes>>> {
    let method = Method::from_bytes(method.as_bytes())
        .map_err(|_| AppError::new("bad_request", format!("{method} is not an HTTP method")))?;
    let bytes = body
        .map(|v| Bytes::from(serde_json::to_vec(v).unwrap_or_default()))
        .unwrap_or_default();
    let mut builder = Request::builder()
        .uri(target)
        .header(HOST, "crush")
        .header(CONTENT_TYPE, "application/json");
    if body.is_some() || method != Method::GET {
        builder = builder.header(CONTENT_LENGTH, bytes.len());
    }
    builder
        .method(method)
        .body(Full::new(bytes))
        .map_err(|e| AppError::new("bad_request", format!("cannot send {target}: {e}")))
}

fn json_body(bytes: &[u8]) -> AppResult<serde_json::Value> {
    if bytes.iter().all(u8::is_ascii_whitespace) {
        return Ok(serde_json::Value::Null);
    }
    serde_json::from_slice(bytes)
        .map_err(|_| AppError::new("internal", "The Crush engine sent an answer that is not JSON."))
}

/// The socket broke after the request went out: the engine may or may not
/// have acted on it (the composer's "uncertain" case, TYPE-048).
fn transport(e: hyper::Error) -> AppError {
    AppError::new(
        "transport",
        format!("The connection to the Crush engine broke during the call ({e}); it may or may not have acted."),
    )
}

/// "30 s", or "100 ms" below a second.
fn span(d: Duration) -> String {
    if d >= Duration::from_secs(1) && d.subsec_millis() == 0 {
        format!("{} s", d.as_secs())
    } else {
        format!("{} ms", d.as_millis())
    }
}

/// A fake engine for tests: a unix socket server, on plain threads, that
/// records every request and answers as the test says.
#[cfg(test)]
pub(crate) mod fake {
    use std::io::{BufRead, BufReader, Read, Write};
    use std::os::unix::net::{UnixListener, UnixStream};
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicBool, Ordering};
    use std::sync::{Arc, Condvar, Mutex};
    use std::time::{Duration, Instant};

    /// One request as the fake read it.
    #[derive(Clone, Debug)]
    pub struct Seen {
        pub method: String,
        pub target: String,
        pub headers: Vec<(String, String)>,
        pub body: Vec<u8>,
        pub chunked: bool,
    }

    impl Seen {
        pub fn path(&self) -> &str {
            self.target.split('?').next().unwrap_or_default()
        }
        pub fn query(&self, key: &str) -> Option<String> {
            let query = self.target.split_once('?')?.1;
            query.split('&').find_map(|pair| {
                let (k, v) = pair.split_once('=')?;
                (k == key).then(|| v.to_string())
            })
        }
        pub fn header(&self, name: &str) -> Option<&str> {
            self.headers.iter().find(|(k, _)| k == name).map(|(_, v)| v.as_str())
        }
        pub fn json(&self) -> serde_json::Value {
            serde_json::from_slice(&self.body).unwrap_or_default()
        }
        /// "GET /v1/version", without the query.
        pub fn line(&self) -> String {
            format!("{} {}", self.method, self.path())
        }
    }

    /// How the fake answers one request.
    pub enum Answer {
        Json(u16, serde_json::Value),
        /// The status with no body.
        Status(u16),
        Raw(u16, &'static str),
        After(Duration, Box<Answer>),
        /// Never answers; waits for the client to close the connection.
        Hang,
        /// Closes the connection without answering.
        Drop,
        /// A 200 event stream that stays open until the test ends it.
        Events,
    }

    type Handler = Arc<dyn Fn(&Seen) -> Answer + Send + Sync>;

    struct Shared {
        seen: Mutex<Vec<Seen>>,
        seen_changed: Condvar,
        /// Connections the client closed while the fake waited on them.
        closed: Mutex<usize>,
        closed_changed: Condvar,
        streams: Mutex<Vec<UnixStream>>,
        handler: Mutex<Handler>,
        stop: AtomicBool,
    }

    pub struct FakeEngine {
        pub socket: PathBuf,
        dir: PathBuf,
        shared: Arc<Shared>,
    }

    impl FakeEngine {
        pub fn start(name: &str, handler: impl Fn(&Seen) -> Answer + Send + Sync + 'static) -> FakeEngine {
            let dir = std::env::temp_dir().join(format!("crush-desktop-p31-{name}-{}", std::process::id()));
            let _ = std::fs::remove_dir_all(&dir);
            std::fs::create_dir_all(&dir).unwrap();
            let socket = dir.join("engine.sock");
            let shared = Arc::new(Shared {
                seen: Mutex::default(),
                seen_changed: Condvar::new(),
                closed: Mutex::default(),
                closed_changed: Condvar::new(),
                streams: Mutex::default(),
                handler: Mutex::new(Arc::new(handler)),
                stop: AtomicBool::new(false),
            });
            let fake = FakeEngine { socket, dir, shared };
            fake.listen();
            fake
        }

        /// Listens (again) on the socket, as a new engine at the same address.
        pub fn listen(&self) {
            let _ = std::fs::remove_file(&self.socket);
            let listener = UnixListener::bind(&self.socket).unwrap();
            listener.set_nonblocking(true).unwrap();
            self.shared.stop.store(false, Ordering::SeqCst);
            let shared = self.shared.clone();
            std::thread::spawn(move || loop {
                if shared.stop.load(Ordering::SeqCst) {
                    return;
                }
                match listener.accept() {
                    Ok((stream, _)) => {
                        stream.set_nonblocking(false).unwrap();
                        let shared = shared.clone();
                        std::thread::spawn(move || serve(stream, &shared));
                    }
                    Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                        std::thread::sleep(Duration::from_millis(2));
                    }
                    Err(_) => return,
                }
            });
        }

        pub fn set_handler(&self, handler: impl Fn(&Seen) -> Answer + Send + Sync + 'static) {
            *self.shared.handler.lock().unwrap() = Arc::new(handler);
        }

        pub fn seen(&self) -> Vec<Seen> {
            self.shared.seen.lock().unwrap().clone()
        }

        pub fn forget(&self) {
            self.shared.seen.lock().unwrap().clear();
        }

        /// Waits up to `timeout` for `cond` over the requests seen so far.
        pub fn wait_seen(&self, timeout: Duration, cond: impl Fn(&[Seen]) -> bool) -> bool {
            let end = Instant::now() + timeout;
            let mut seen = self.shared.seen.lock().unwrap();
            loop {
                if cond(&seen) {
                    return true;
                }
                let left = end.saturating_duration_since(Instant::now());
                if left.is_zero() {
                    return false;
                }
                seen = self.shared.seen_changed.wait_timeout(seen, left).unwrap().0;
            }
        }

        pub fn wait_closed(&self, n: usize, timeout: Duration) -> bool {
            let end = Instant::now() + timeout;
            let mut closed = self.shared.closed.lock().unwrap();
            while *closed < n {
                let left = end.saturating_duration_since(Instant::now());
                if left.is_zero() {
                    return false;
                }
                closed = self.shared.closed_changed.wait_timeout(closed, left).unwrap().0;
            }
            true
        }

        /// Writes `data` as one chunk to every open event stream.
        pub fn send(&self, data: &str) {
            for s in self.shared.streams.lock().unwrap().iter_mut() {
                let _ = write!(s, "{:x}\r\n{data}\r\n", data.len());
                let _ = s.flush();
            }
        }

        /// Ends every open event stream, as an engine that stops sending does.
        pub fn end_streams(&self) {
            for mut s in self.shared.streams.lock().unwrap().drain(..) {
                let _ = s.write_all(b"0\r\n\r\n");
                let _ = s.shutdown(std::net::Shutdown::Both);
            }
        }

        /// Stops listening and removes the socket, as a killed engine: later
        /// connects fail, and its event streams end.
        pub fn kill(&self) {
            self.shared.stop.store(true, Ordering::SeqCst);
            std::thread::sleep(Duration::from_millis(10));
            let _ = std::fs::remove_file(&self.socket);
            for s in self.shared.streams.lock().unwrap().drain(..) {
                let _ = s.shutdown(std::net::Shutdown::Both);
            }
        }
    }

    impl Drop for FakeEngine {
        fn drop(&mut self) {
            self.kill();
            let _ = std::fs::remove_dir_all(&self.dir);
        }
    }

    fn serve(mut stream: UnixStream, shared: &Arc<Shared>) {
        let Some(seen) = read_request(&stream) else { return };
        let handler = shared.handler.lock().unwrap().clone();
        let answer = handler(&seen);
        shared.seen.lock().unwrap().push(seen);
        shared.seen_changed.notify_all();
        respond(&mut stream, answer, shared);
    }

    fn respond(stream: &mut UnixStream, answer: Answer, shared: &Arc<Shared>) {
        match answer {
            Answer::After(wait, answer) => {
                std::thread::sleep(wait);
                respond(stream, *answer, shared);
            }
            Answer::Json(status, value) => write_answer(stream, status, value.to_string().as_bytes()),
            Answer::Status(status) => write_answer(stream, status, b""),
            Answer::Raw(status, text) => write_answer(stream, status, text.as_bytes()),
            Answer::Drop => {
                let _ = stream.shutdown(std::net::Shutdown::Both);
            }
            Answer::Hang => {
                wait_for_close(stream);
                *shared.closed.lock().unwrap() += 1;
                shared.closed_changed.notify_all();
            }
            Answer::Events => {
                // Listed before the head goes out, so a test that saw the
                // stream open can always send on it.
                let Ok(copy) = stream.try_clone() else { return };
                let head = "HTTP/1.1 200 OK\r\ncontent-type: text/event-stream\r\ncache-control: no-cache\r\ntransfer-encoding: chunked\r\n\r\n";
                let mut streams = shared.streams.lock().unwrap();
                streams.push(copy);
                if stream.write_all(head.as_bytes()).is_err() {
                    return;
                }
                drop(streams);
                wait_for_close(stream);
            }
        }
    }

    /// Blocks until the other side closes the connection (or it is shut).
    fn wait_for_close(stream: &mut UnixStream) {
        let mut byte = [0u8; 1];
        while let Ok(1..) = stream.read(&mut byte) {}
    }

    fn write_answer(stream: &mut UnixStream, status: u16, body: &[u8]) {
        let head = format!(
            "HTTP/1.1 {status} X\r\ncontent-type: application/json\r\ncontent-length: {}\r\nconnection: close\r\n\r\n",
            body.len()
        );
        let _ = stream.write_all(head.as_bytes());
        let _ = stream.write_all(body);
        let _ = stream.flush();
    }

    fn read_request(stream: &UnixStream) -> Option<Seen> {
        let mut reader = BufReader::new(stream.try_clone().ok()?);
        let mut line = String::new();
        reader.read_line(&mut line).ok()?;
        let mut parts = line.split_whitespace();
        let (method, target) = (parts.next()?.to_string(), parts.next()?.to_string());
        let mut headers = Vec::new();
        loop {
            let mut h = String::new();
            reader.read_line(&mut h).ok()?;
            let h = h.trim_end();
            if h.is_empty() {
                break;
            }
            let (k, v) = h.split_once(':')?;
            headers.push((k.trim().to_ascii_lowercase(), v.trim().to_string()));
        }
        let get = |name: &str| headers.iter().find(|(k, _)| k == name).map(|(_, v)| v.clone());
        let chunked = get("transfer-encoding").is_some_and(|v| v.eq_ignore_ascii_case("chunked"));
        let mut body = Vec::new();
        if chunked {
            loop {
                let mut size = String::new();
                reader.read_line(&mut size).ok()?;
                let n = usize::from_str_radix(size.trim(), 16).ok()?;
                let mut chunk = vec![0u8; n + 2];
                reader.read_exact(&mut chunk).ok()?;
                if n == 0 {
                    break;
                }
                body.extend_from_slice(&chunk[..n]);
            }
        } else if let Some(n) = get("content-length").and_then(|v| v.parse::<usize>().ok()) {
            body.resize(n, 0);
            reader.read_exact(&mut body).ok()?;
        }
        Some(Seen { method, target, headers, body, chunked })
    }
}

#[cfg(test)]
mod tests {
    use super::fake::{Answer, FakeEngine};
    use super::*;
    use serde_json::json;

    fn run<T>(f: impl std::future::Future<Output = T>) -> T {
        crate::host::async_runtime::block_on(f)
    }

    #[test]
    fn status_to_code_mapping() {
        let fake = FakeEngine::start("http-status", |seen| match seen.path() {
            "/v1/missing" => Answer::Json(404, json!({"message": "no such chat"})),
            "/v1/busy" => Answer::Json(409, json!({"message": "busy", "code": "agent_busy"})),
            "/v1/closing" => Answer::Json(503, json!({"message": "server is shutting down"})),
            "/v1/gateway" => Answer::Status(502),
            "/v1/empty" => Answer::Status(200),
            "/v1/accepted" => Answer::Json(202, json!({"ok": 1})),
            "/v1/text" => Answer::Raw(200, "not json"),
            _ => Answer::Json(200, json!({"path": seen.target})),
        });
        let http = EngineHttp::new(fake.socket.to_str().unwrap());
        run(async {
            let limit = Some(Duration::from_secs(5));
            let call = |path: &'static str| http.call("GET", path, None, limit);
            let err = |r: AppResult<Reply>| r.err().map(|e| (e.code, e.status, e.message)).unwrap();
            assert_eq!(err(call("/v1/missing").await), ("not_found".into(), Some(404), "no such chat".into()));
            assert_eq!(err(call("/v1/busy").await), ("agent_busy".into(), Some(409), "busy".into()));
            assert_eq!(err(call("/v1/closing").await).0, "shutting_down");
            assert_eq!(err(call("/v1/gateway").await).0, "http_502");
            assert_eq!(err(call("/v1/text").await).0, "internal");
            let r = call("/v1/empty").await.ok().unwrap();
            assert_eq!((r.status, r.body), (200, serde_json::Value::Null));
            let r = call("/v1/accepted").await.ok().unwrap();
            assert_eq!((r.status, r.body), (202, json!({"ok": 1})));
            let r = call("/v1/version?x=1").await.ok().unwrap();
            assert_eq!(r.body, json!({"path": "/v1/version?x=1"}));
        });
        let seen = fake.seen();
        assert!(seen.iter().all(|s| s.header("host") == Some("crush")), "every request says host: crush");
    }

    #[test]
    fn no_engine_is_engine_unavailable_and_a_break_is_transport() {
        let dir = crate::engine_cli::tests::scratch("http-down");
        let missing = dir.join("none.sock");
        // A socket file nobody listens on: connection refused.
        let stale = dir.join("stale.sock");
        drop(std::os::unix::net::UnixListener::bind(&stale).unwrap());
        run(async {
            for socket in [&missing, &stale] {
                let http = EngineHttp::new(socket.to_str().unwrap());
                let e = http.call("GET", "/v1/version", None, None).await.err().unwrap();
                assert_eq!(e.code, "engine_unavailable", "{}", socket.display());
                let e = http.stream("/v1/workspaces/w/events?client_id=c").await.err().unwrap();
                assert_eq!(e.code, "engine_unavailable");
            }
        });
        let fake = FakeEngine::start("http-break", |_| Answer::Drop);
        let http = EngineHttp::new(fake.socket.to_str().unwrap());
        let e = run(http.call("POST", "/v1/workspaces/w/agent", Some(&json!({"prompt": "hi"})), None)).err().unwrap();
        assert_eq!(e.code, "transport");
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn a_slow_answer_meets_the_limit_and_closes_its_socket() {
        let fake = FakeEngine::start("http-slow", |seen| match seen.path() {
            "/v1/hang" => Answer::Hang,
            _ => Answer::After(Duration::from_millis(300), Box::new(Answer::Json(200, json!({"late": true})))),
        });
        let http = EngineHttp::new(fake.socket.to_str().unwrap());
        run(async {
            let e = http.call("GET", "/v1/slow", None, Some(Duration::from_millis(100))).await.err().unwrap();
            assert_eq!((e.code.as_str(), e.message.as_str()), ("timeout", "The Crush engine did not answer within 100 ms."));
            let r = http.call("GET", "/v1/slow", None, None).await.ok().unwrap();
            assert_eq!(r.body, json!({"late": true}));
            let e = http.call("GET", "/v1/hang", None, Some(Duration::from_millis(100))).await.err().unwrap();
            assert_eq!(e.code, "timeout");
        });
        assert!(fake.wait_closed(1, Duration::from_secs(2)), "a call given up closes its socket");
    }

    #[test]
    fn bodies_carry_content_length_never_chunked() {
        let fake = FakeEngine::start("http-length", |_| Answer::Status(200));
        let http = EngineHttp::new(fake.socket.to_str().unwrap());
        let body = json!({"session_id": "s1", "prompt": "héllo"});
        run(async {
            http.call("POST", "/v1/workspaces/w/agent", Some(&body), None).await.ok().unwrap();
            http.call("POST", "/v1/workspaces/w/agent/sessions/s1/cancel", None, None).await.ok().unwrap();
            http.call("GET", "/v1/version", None, None).await.ok().unwrap();
        });
        let seen = fake.seen();
        let sent = serde_json::to_vec(&body).unwrap();
        assert_eq!(seen[0].header("content-length"), Some(sent.len().to_string().as_str()));
        assert_eq!((seen[0].chunked, seen[0].json()), (false, body));
        assert_eq!((seen[1].header("content-length"), seen[1].chunked), (Some("0"), false));
        assert!(seen.iter().all(|s| s.header("transfer-encoding").is_none()));
        assert_eq!(seen[0].header("content-type"), Some("application/json"));
    }

    #[test]
    fn stream_hands_over_raw_chunks_until_the_end() {
        let fake = FakeEngine::start("http-stream", |seen| match seen.path() {
            "/v1/workspaces/gone/events" => Answer::Json(404, json!({"message": "workspace not found"})),
            _ => Answer::Events,
        });
        let http = EngineHttp::new(fake.socket.to_str().unwrap());
        run(async {
            let e = http.stream("/v1/workspaces/gone/events?client_id=c").await.err().unwrap();
            assert_eq!((e.code.as_str(), e.status), ("not_found", Some(404)));
            let mut stream = http.stream("/v1/workspaces/w/events?client_id=c").await.ok().unwrap();
            fake.send("data: {\"type\":\"message\",\"pay");
            let first = stream.next_chunk().await.unwrap().ok().unwrap();
            assert_eq!(&first[..], b"data: {\"type\":\"message\",\"pay");
            fake.send("load\":{}}\n\n");
            let second = stream.next_chunk().await.unwrap().ok().unwrap();
            assert_eq!(&second[..], b"load\":{}}\n\n");
            fake.end_streams();
            assert!(stream.next_chunk().await.is_none(), "the end of the stream");
        });
    }
}
