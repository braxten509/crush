//! HTTP/1.1 over the engine's unix socket (desktop-design 1.2.4). Scaffold S3
//! placeholder: piece P3.1 writes the bodies; the public signatures are
//! pinned.
#![allow(dead_code, unused_variables)]

use crate::error::{AppError, AppResult};

pub struct EngineHttp {
    socket: std::path::PathBuf,
}

/// body is Null when empty.
pub struct Reply {
    pub status: u16,
    pub body: serde_json::Value,
}

impl EngineHttp {
    pub fn new(socket: &str) -> Self {
        EngineHttp {
            socket: std::path::PathBuf::from(socket),
        }
    }

    /// `path_and_query` starts with "/v1/". Body is sent as JSON when Some.
    /// 2xx → Ok(Reply). Other statuses → Err(AppError::from_engine). Connect refused or a missing
    /// socket → Err(code "engine_unavailable"). A break after the request was written → Err("transport").
    pub async fn call(
        &self,
        method: &str,
        path_and_query: &str,
        body: Option<&serde_json::Value>,
        limit: std::time::Duration,
    ) -> AppResult<Reply> {
        Err(AppError::not_built("uds_http::EngineHttp::call"))
    }

    /// Opens a GET and returns the response body as a stream of raw byte chunks. Err on non-200.
    pub async fn stream(&self, path_and_query: &str) -> AppResult<ByteStream> {
        Err(AppError::not_built("uds_http::EngineHttp::stream"))
    }
}

pub struct ByteStream {
    body: hyper::body::Incoming,
}

impl ByteStream {
    pub async fn next_chunk(&mut self) -> Option<AppResult<bytes::Bytes>> {
        Some(Err(AppError::not_built("uds_http::ByteStream::next_chunk")))
    }
}
