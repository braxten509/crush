//! Event batching (engine-design.md 5.12).
//!
//! The engine sends every change as its own Server-Sent Events line. The
//! window takes everything that arrived since its last frame with
//! [`Batcher::take`], once per frame, so a burst of streaming updates costs
//! the screens one apply per frame. Updates of the same item inside one
//! segment merge into one entry; every other event is a barrier, kept whole
//! and in order. When too much waits (a hidden window runs no frames), the
//! mergeable entries give way to one [`Entry::Resync`].
#![allow(dead_code)]

use serde_json::Value;
use std::collections::HashMap;

/// The inner event type: what happened to the payload.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    Created,
    Updated,
    Deleted,
}

impl Kind {
    fn parse(s: &str) -> Option<Kind> {
        match s {
            "created" => Some(Kind::Created),
            "updated" => Some(Kind::Updated),
            "deleted" => Some(Kind::Deleted),
            _ => None,
        }
    }
}

/// One engine event: the envelope's `type`, the inner `type` and the inner
/// `payload`.
#[derive(Debug, Clone, PartialEq)]
pub struct Event {
    pub ty: String,
    pub kind: Kind,
    pub payload: Value,
}

/// What [`Batcher::take`] hands over.
#[derive(Debug, Clone, PartialEq)]
pub enum Entry {
    Event(Event),
    /// Events were dropped: read everything again through the routes.
    Resync,
}

/// One waiting entry. `first` is the kind of the first event merged into
/// it, which decides the merged kind. A created entry that was then deleted
/// leaves an empty slot, so the other entries keep their places.
struct Slot {
    entry: Option<Entry>,
    first: Kind,
    mergeable: bool,
}

/// Collects events between two frames.
pub struct Batcher {
    max_entries: usize,
    slots: Vec<Slot>,
    /// How many slots hold an entry.
    waiting: usize,
    /// The merge key of every mergeable entry of the current segment (the
    /// entries after the last barrier), with its slot.
    segment: HashMap<String, usize>,
    /// Set from an overflow until the next take: a Resync leads the batch,
    /// and mergeable events are covered by it.
    resync: bool,
}

impl Batcher {
    /// A batcher that holds at most `max_entries` entries before it falls
    /// back to a Resync. The app uses 5_000.
    pub fn new(max_entries: usize) -> Self {
        Batcher {
            max_entries,
            slots: Vec::new(),
            waiting: 0,
            segment: HashMap::new(),
            resync: false,
        }
    }

    /// Adds one event.
    pub fn push(&mut self, ev: Event) {
        match merge_key(&ev.ty, &ev.payload) {
            None => {
                // A barrier ends the segment: nothing after it merges into
                // an entry before it.
                self.segment.clear();
                self.append(ev, false);
            }
            Some(_) if self.resync => {
                // The pending Resync reads this item again anyway.
            }
            Some(key) => match self.segment.get(&key).copied() {
                Some(i) => self.merge(i, &key, ev),
                None => {
                    self.segment.insert(key, self.slots.len());
                    self.append(ev, true);
                }
            },
        }
        if self.waiting > self.max_entries {
            self.overflow();
        }
    }

    /// Hands over everything since the last take and leaves the batcher
    /// empty.
    pub fn take(&mut self) -> Vec<Entry> {
        self.segment.clear();
        self.resync = false;
        self.waiting = 0;
        self.slots.drain(..).filter_map(|s| s.entry).collect()
    }

    fn append(&mut self, ev: Event, mergeable: bool) {
        self.slots.push(Slot {
            first: ev.kind,
            entry: Some(Entry::Event(ev)),
            mergeable,
        });
        self.waiting += 1;
    }

    /// Merges ev into the entry in slot i, where it stands.
    fn merge(&mut self, i: usize, key: &str, ev: Event) {
        let slot = &mut self.slots[i];
        if slot.first == Kind::Created && ev.kind == Kind::Deleted {
            // Born and gone in one batch: the screens never see it.
            slot.entry = None;
            self.waiting -= 1;
            self.segment.remove(key);
            return;
        }
        let kind = if slot.first == Kind::Created {
            Kind::Created
        } else if ev.kind == Kind::Deleted {
            Kind::Deleted
        } else {
            Kind::Updated
        };
        let Some(Entry::Event(old)) = slot.entry.as_mut() else {
            return;
        };
        let mut payload = ev.payload;
        if ev.ty == "shell_output" {
            // Output accumulates: the chunks join in order, the rest (the
            // newest seq) comes from the newest event.
            let mut chunk = chunk_of(&old.payload);
            chunk.push_str(&chunk_of(&payload));
            if let Value::Object(map) = &mut payload {
                map.insert("chunk".to_string(), Value::String(chunk));
            }
        }
        old.kind = kind;
        old.payload = payload;
    }

    /// Drops every mergeable entry and puts one Resync first. Barriers stay,
    /// in order.
    fn overflow(&mut self) {
        if self.resync {
            // Only barriers wait, and they are never dropped.
            return;
        }
        let kept: Vec<Slot> = self
            .slots
            .drain(..)
            .filter(|s| !s.mergeable && s.entry.is_some())
            .collect();
        self.slots.push(Slot {
            entry: Some(Entry::Resync),
            first: Kind::Updated,
            mergeable: false,
        });
        self.slots.extend(kept);
        self.waiting = self.slots.len();
        self.segment.clear();
        self.resync = true;
    }
}

fn chunk_of(payload: &Value) -> String {
    payload
        .get("chunk")
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string()
}

/// The merge key of an event, or None for a barrier. Unknown types are
/// barriers, and so is a mergeable type whose key fields are missing: it is
/// never merged and never dropped.
fn merge_key(ty: &str, payload: &Value) -> Option<String> {
    let fields: &[&str] = match ty {
        "message" | "session" | "file" | "helper" => &["id"],
        "lsp_event" | "mcp_event" => &["name", "type"],
        "limits" => &["kind"],
        "memory_status" => &[],
        "tree_changed" => &["session_id"],
        "prompt_pending" => &["session_id", "kind"],
        "shell_output" => &["request_id"],
        _ => return None,
    };
    // JSON text never holds a raw NUL, so it separates the parts safely.
    let mut key = ty.to_string();
    for field in fields {
        match payload.get(field) {
            None | Some(Value::Null) => return None,
            Some(v) => {
                key.push('\0');
                key.push_str(&v.to_string());
            }
        }
    }
    Some(key)
}

/// Parses one SSE line. Lines without "data:" and bad JSON give None.
pub fn parse_sse_line(line: &str) -> Option<Event> {
    let line = line.trim_end_matches(['\r', '\n']);
    let data = line.strip_prefix("data:")?;
    // SSE drops one leading space after the colon.
    let data = data.strip_prefix(' ').unwrap_or(data);
    let mut envelope: Value = serde_json::from_str(data).ok()?;
    let ty = envelope.get("type")?.as_str()?.to_string();
    let inner = envelope.get_mut("payload")?;
    let kind = Kind::parse(inner.get("type")?.as_str()?)?;
    let payload = inner.get_mut("payload").map(Value::take).unwrap_or(Value::Null);
    Some(Event { ty, kind, payload })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::collections::BTreeMap;

    fn ev(ty: &str, kind: Kind, payload: Value) -> Event {
        Event {
            ty: ty.to_string(),
            kind,
            payload,
        }
    }

    fn events(entries: &[Entry]) -> Vec<&Event> {
        entries
            .iter()
            .filter_map(|e| match e {
                Entry::Event(ev) => Some(ev),
                Entry::Resync => None,
            })
            .collect()
    }

    #[test]
    fn merges_message_updates_keeping_latest() {
        let mut b = Batcher::new(5_000);
        for i in 0..50 {
            b.push(ev("message", Kind::Updated, json!({"id": "m1", "text": i})));
        }
        let out = b.take();
        assert_eq!(
            out,
            vec![Entry::Event(ev(
                "message",
                Kind::Updated,
                json!({"id": "m1", "text": 49})
            ))]
        );
        assert!(b.take().is_empty(), "take leaves the batcher empty");
    }

    #[test]
    fn created_then_updated_stays_created() {
        let mut b = Batcher::new(5_000);
        b.push(ev("session", Kind::Created, json!({"id": "s1", "title": "New"})));
        b.push(ev("session", Kind::Updated, json!({"id": "s1", "title": "Plans"})));
        b.push(ev("session", Kind::Updated, json!({"id": "s1", "title": "Plans 2"})));
        assert_eq!(
            b.take(),
            vec![Entry::Event(ev(
                "session",
                Kind::Created,
                json!({"id": "s1", "title": "Plans 2"})
            ))]
        );

        // The other kind rules: the latest delete wins over an update, and
        // anything else is an update.
        b.push(ev("file", Kind::Updated, json!({"id": "f1", "v": 1})));
        b.push(ev("file", Kind::Deleted, json!({"id": "f1", "v": 2})));
        b.push(ev("file", Kind::Deleted, json!({"id": "f2", "v": 1})));
        b.push(ev("file", Kind::Created, json!({"id": "f2", "v": 2})));
        assert_eq!(
            b.take(),
            vec![
                Entry::Event(ev("file", Kind::Deleted, json!({"id": "f1", "v": 2}))),
                Entry::Event(ev("file", Kind::Updated, json!({"id": "f2", "v": 2}))),
            ]
        );
    }

    #[test]
    fn created_then_deleted_vanishes() {
        let mut b = Batcher::new(5_000);
        b.push(ev("helper", Kind::Updated, json!({"id": "a"})));
        b.push(ev("helper", Kind::Created, json!({"id": "t1"})));
        b.push(ev("helper", Kind::Updated, json!({"id": "t1", "status": "running"})));
        b.push(ev("helper", Kind::Deleted, json!({"id": "t1"})));
        b.push(ev("helper", Kind::Updated, json!({"id": "b"})));
        let out = b.take();
        assert_eq!(
            out,
            vec![
                Entry::Event(ev("helper", Kind::Updated, json!({"id": "a"}))),
                Entry::Event(ev("helper", Kind::Updated, json!({"id": "b"}))),
            ]
        );

        // Created again after vanishing: a fresh entry.
        b.push(ev("message", Kind::Created, json!({"id": "m"})));
        b.push(ev("message", Kind::Deleted, json!({"id": "m"})));
        b.push(ev("message", Kind::Created, json!({"id": "m", "v": 2})));
        assert_eq!(
            b.take(),
            vec![Entry::Event(ev("message", Kind::Created, json!({"id": "m", "v": 2})))]
        );
    }

    #[test]
    fn barrier_stops_merging_across() {
        let mut b = Batcher::new(5_000);
        b.push(ev("message", Kind::Updated, json!({"id": "a", "v": 1})));
        b.push(ev("permission_request", Kind::Created, json!({"id": "p1"})));
        b.push(ev("message", Kind::Updated, json!({"id": "a", "v": 2})));
        let out = b.take();
        assert_eq!(
            out,
            vec![
                Entry::Event(ev("message", Kind::Updated, json!({"id": "a", "v": 1}))),
                Entry::Event(ev("permission_request", Kind::Created, json!({"id": "p1"}))),
                Entry::Event(ev("message", Kind::Updated, json!({"id": "a", "v": 2}))),
            ]
        );
    }

    #[test]
    fn keeps_first_appearance_order() {
        let mut b = Batcher::new(5_000);
        b.push(ev("message", Kind::Updated, json!({"id": "A", "v": 1})));
        b.push(ev("message", Kind::Updated, json!({"id": "B", "v": 1})));
        b.push(ev("message", Kind::Updated, json!({"id": "A", "v": 2})));
        let out = b.take();
        let ids: Vec<&Value> = events(&out).iter().map(|e| &e.payload["id"]).collect();
        assert_eq!(ids, vec![&json!("A"), &json!("B")]);
        assert_eq!(events(&out)[0].payload["v"], json!(2));
    }

    #[test]
    fn shell_output_appends_chunks_in_order() {
        let raw = ["$ make\n", "building…", " 50%", "\n✓ done\n"];
        let mut b = Batcher::new(5_000);
        for (seq, chunk) in raw.iter().enumerate() {
            b.push(ev(
                "shell_output",
                Kind::Created,
                json!({"request_id": "r1", "session_id": "s", "seq": seq, "chunk": chunk}),
            ));
            // Another request's output stays apart.
            b.push(ev(
                "shell_output",
                Kind::Created,
                json!({"request_id": "r2", "session_id": "s", "seq": seq, "chunk": "x"}),
            ));
        }
        let out = b.take();
        let evs = events(&out);
        assert_eq!(evs.len(), 2);
        assert_eq!(evs[0].payload["chunk"], json!(raw.concat()));
        assert_eq!(evs[0].payload["seq"], json!(raw.len() - 1), "the newest seq");
        assert_eq!(evs[1].payload["chunk"], json!("xxxx"));
    }

    #[test]
    fn unknown_type_is_barrier() {
        let mut b = Batcher::new(5_000);
        let future = json!({"id": "same", "n": 1});
        b.push(ev("message", Kind::Updated, json!({"id": "a", "v": 1})));
        b.push(ev("future_event", Kind::Updated, future.clone()));
        b.push(ev("future_event", Kind::Updated, future.clone()));
        b.push(ev("message", Kind::Updated, json!({"id": "a", "v": 2})));
        // A mergeable type without its key cannot merge either.
        b.push(ev("message", Kind::Updated, json!({"text": "no id"})));
        b.push(ev("message", Kind::Updated, json!({"text": "no id"})));
        let out = b.take();
        let types: Vec<&str> = events(&out).iter().map(|e| e.ty.as_str()).collect();
        assert_eq!(
            types,
            vec!["message", "future_event", "future_event", "message", "message", "message"]
        );
        assert_eq!(events(&out)[1].payload, future);
    }

    #[test]
    fn overflow_becomes_resync_keeping_barriers() {
        let mut b = Batcher::new(10);
        b.push(ev("permission_request", Kind::Created, json!({"id": "p1"})));
        for i in 0..20 {
            b.push(ev("message", Kind::Created, json!({"id": i})));
            if i == 12 {
                b.push(ev("agent_event", Kind::Created, json!({"n": 1})));
            }
        }
        b.push(ev("question_batch_request", Kind::Created, json!({"id": "q1"})));
        let out = b.take();
        assert_eq!(out[0], Entry::Resync);
        let types: Vec<&str> = events(&out).iter().map(|e| e.ty.as_str()).collect();
        assert_eq!(
            types,
            vec!["permission_request", "agent_event", "question_batch_request"],
            "every barrier stays, in order; the mergeable entries are dropped"
        );
        assert_eq!(out.iter().filter(|e| **e == Entry::Resync).count(), 1);

        // The next batch starts clean.
        b.push(ev("message", Kind::Updated, json!({"id": 1})));
        assert_eq!(b.take(), vec![Entry::Event(ev("message", Kind::Updated, json!({"id": 1})))]);
    }

    /// A small deterministic random source (xorshift64*), so the test needs
    /// no extra crate and fails the same way every run.
    struct Rng(u64);

    impl Rng {
        fn next(&mut self) -> u64 {
            self.0 ^= self.0 >> 12;
            self.0 ^= self.0 << 25;
            self.0 ^= self.0 >> 27;
            self.0.wrapping_mul(0x2545_F491_4F6C_DD1D)
        }
        fn below(&mut self, n: u64) -> u64 {
            self.next() % n
        }
    }

    /// The keyed state the screens keep. Barriers are recorded in order.
    #[derive(Default, Debug, PartialEq)]
    struct State {
        items: BTreeMap<String, Value>,
        shell: BTreeMap<String, String>,
        barriers: Vec<Value>,
    }

    impl State {
        fn apply(&mut self, e: &Entry) {
            let Entry::Event(e) = e else {
                panic!("no overflow expected in this test");
            };
            if e.ty == "shell_output" {
                let id = e.payload["request_id"].to_string();
                let out = self.shell.entry(id).or_default();
                out.push_str(e.payload["chunk"].as_str().unwrap());
                return;
            }
            let Some(key) = merge_key(&e.ty, &e.payload) else {
                self.barriers.push(json!([e.ty, e.payload]));
                return;
            };
            match e.kind {
                Kind::Deleted => {
                    self.items.remove(&key);
                }
                _ => {
                    self.items.insert(key, e.payload.clone());
                }
            }
        }
    }

    #[test]
    fn same_final_state_as_unbatched() {
        let mergeable = ["message", "session", "helper", "tree_changed", "prompt_pending"];
        let barrier_kinds = [
            "permission_request",
            "agent_event",
            "run_complete",
            "chat_link",
            "remote_action",
        ];
        for seed in 1..=20u64 {
            let mut rng = Rng(seed.wrapping_mul(0x9E37_79B9_7F4A_7C15));
            let mut b = Batcher::new(5_000);
            let mut raw = State::default();
            let mut batched = State::default();
            // Which keys exist, as the engine knows it, so the stream is a
            // real one: created only for a missing item.
            let mut exists = [false; 20];
            for step in 0..1_000 {
                let pick = rng.below(10);
                let e = if pick == 0 {
                    let ty = barrier_kinds[rng.below(5) as usize];
                    ev(ty, Kind::Created, json!({"step": step}))
                } else if pick == 1 {
                    let req = rng.below(3);
                    ev(
                        "shell_output",
                        Kind::Created,
                        json!({"request_id": req, "seq": step, "chunk": format!("<{step}>")}),
                    )
                } else {
                    let k = rng.below(20) as usize;
                    let ty = mergeable[k % mergeable.len()];
                    let payload = match ty {
                        "tree_changed" => json!({"session_id": k, "rev": step}),
                        "prompt_pending" => {
                            json!({"session_id": k, "kind": "permission", "pending": step % 2 == 0})
                        }
                        _ => json!({"id": k, "v": step}),
                    };
                    let kind = if !exists[k] {
                        exists[k] = true;
                        Kind::Created
                    } else if rng.below(4) == 0 {
                        exists[k] = false;
                        Kind::Deleted
                    } else {
                        Kind::Updated
                    };
                    ev(ty, kind, payload)
                };
                raw.apply(&Entry::Event(e.clone()));
                b.push(e);
                // Frames come at random moments.
                if rng.below(25) == 0 {
                    for entry in b.take() {
                        batched.apply(&entry);
                    }
                }
            }
            for entry in b.take() {
                batched.apply(&entry);
            }
            assert_eq!(raw, batched, "seed {seed}");
        }
    }

    #[test]
    fn parse_sse_line_accepts_only_data_lines() {
        let line = r#"data: {"type":"message","payload":{"type":"updated","payload":{"id":"m1"}}}"#;
        assert_eq!(
            parse_sse_line(line),
            Some(ev("message", Kind::Updated, json!({"id": "m1"})))
        );
        // No space after the colon, and a line ending, are fine too.
        let tight = "data:{\"type\":\"x\",\"payload\":{\"type\":\"created\",\"payload\":1}}\r\n";
        assert_eq!(parse_sse_line(tight), Some(ev("x", Kind::Created, json!(1))));
        for bad in [
            "",
            "\n",
            ": ping",
            "event: message",
            "id: 7",
            "data:",
            "data: ",
            "data: {not json",
            "data: []",
            "data: {\"type\":7,\"payload\":{\"type\":\"created\"}}",
            "data: {\"type\":\"message\"}",
            "data: {\"type\":\"message\",\"payload\":{\"type\":\"exploded\"}}",
            " data: {\"type\":\"message\",\"payload\":{\"type\":\"created\"}}",
        ] {
            assert_eq!(parse_sse_line(bad), None, "{bad:?}");
        }
        // An event without an inner payload still parses.
        assert_eq!(
            parse_sse_line(r#"data: {"type":"t","payload":{"type":"deleted"}}"#),
            Some(ev("t", Kind::Deleted, Value::Null))
        );
    }

    #[test]
    fn prompt_pending_merges_per_chat_and_kind() {
        let mut b = Batcher::new(5_000);
        let mark = |kind: &str, pending: bool| {
            ev(
                "prompt_pending",
                Kind::Updated,
                json!({"session_id": "c1", "kind": kind, "pending": pending}),
            )
        };
        b.push(mark("permission", true));
        b.push(mark("question", true));
        b.push(mark("permission", false));
        let out = b.take();
        assert_eq!(
            out,
            vec![
                Entry::Event(mark("permission", false)),
                Entry::Event(mark("question", true)),
            ]
        );
    }

    #[test]
    fn hundred_thousand_updates_fifty_keys() {
        let mut b = Batcher::new(5_000);
        for i in 0..100_000u64 {
            b.push(ev("message", Kind::Updated, json!({"id": i % 50, "v": i})));
        }
        let out = b.take();
        assert_eq!(out.len(), 50);
        assert!(!out.contains(&Entry::Resync));
        assert_eq!(events(&out)[0].payload, json!({"id": 0, "v": 99_950}));
    }
}
