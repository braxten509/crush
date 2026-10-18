// What the engine sends, as JSDoc types (desktop-design 1.5.5). JSON names are
// exactly the engine's. No code: other modules refer to these types with
// import("./wire.js").Session and the like.

/** @typedef {{id:string, parent_session_id:string, title:string, message_count:number,
 *   prompt_tokens:number, completion_tokens:number, summary_message_id:string, cost:number,
 *   todos?:Todo[], channel?:string, kept_skills?:KeptSkill[], created_at:number, updated_at:number,
 *   is_busy:boolean, attached_clients:number,
 *   context_budget?:{provider:string, model:string, tokens:number, estimated:boolean},
 *   estimated_usage:boolean, has_unfinished_work:boolean, pending_prompt:""|"permission"|"question"}} Session
 *   (internal/proto/session.go:16-32; the last four lines are engine design 5.1(a)) */
/** @typedef {{content:string, status:string, active_form:string}} Todo        (session.go:52-56) */
/** @typedef {{id:string, name:string, by_agent?:boolean}} KeptSkill           (session.go:35-40) */
/** @typedef {{id:string, role:"assistant"|"user"|"system"|"tool", session_id:string, parts:Part[],
 *   model:string, provider:string, created_at:number, updated_at:number,
 *   activity_at?:number, activity?:string, activity_detail?:string,
 *   prism_model_id?:string, prism_model_name?:string, prism_hypercredit_savings?:number,
 *   prism_dollar_savings?:number, is_summary_message?:boolean, is_compacting?:boolean}} Message
 *   (internal/proto/message.go:24-42; times are Unix seconds) */
/** @typedef {{type:"reasoning", data:{thinking:string, signature:string, started_at?:number, finished_at?:number}}
 *  | {type:"text", data:{text:string, submission_id?:string, hidden?:boolean}}
 *  | {type:"image_url", data:{url:string, detail?:string}}
 *  | {type:"binary", data:{Path:string, MIMEType:string, Data:string}}
 *  | {type:"tool_call", data:{id:string, name:string, input:string, type?:string, finished?:boolean}}
 *  | {type:"tool_result", data:{tool_call_id:string, name:string, content:string, data?:string,
 *        mime_type?:string, metadata:string, is_error:boolean, review?:Review}}
 *  | {type:"finish", data:{reason:"end_turn"|"max_tokens"|"tool_use"|"canceled"|"error"|"content_filter"|"unknown",
 *        time:number, message?:string, details?:string}}
 *  | {type:"shell_command", data:{command:string, output:string, exit_code:number}}} Part
 *   (message.go:95-196 and 519-571). Note: binary parts have no JSON tags, so their keys are
 *   capitalised and Data is base64 (message.go:137-141). */
/** @typedef {{root:string, deletions?:boolean, id?:string, session_id?:string, changes?:Change[],
 *   summary?:{files:number, deletions?:boolean, paths?:string[], copied?:number, checkouts?:number, moved?:number,
 *   generated?:number, removed?:number, no_lines?:boolean, adds?:number, dels?:number, counted?:boolean}}} Review
 *   (internal/filechange/snapshot.go:65-93) */
/** @typedef {{path:string, order?:number, before?:FileState, after?:FileState, transfer?:object}} Change
 *  @typedef {{content?:string, digest?:string, size:number, mode:number, omitted?:string}} FileState (snapshot.go:39-60) */
/** @typedef {{id:string, path:string, yolo?:boolean, debug?:boolean, data_dir?:string, version?:string,
 *   client_id?:string, channels?:string[], skills?:object[], protocol?:number}} Workspace
 *   (internal/proto/proto.go:15-34, as the window gets it: Rust removes config and env first, 1.2.5) */
/** @typedef {{is_busy:boolean, is_ready:boolean, model:{id:string, name:string, context_window:number,
 *   can_reason:boolean, reasoning_levels?:string[], default_reasoning_effort?:string, supports_attachments:boolean},
 *   model_cfg:{model:string, provider:string, reasoning_effort?:string, service_tier?:string, ultracode?:boolean, think?:boolean}}} AgentInfo
 *   (proto.go:123-128; catwalk Model fields: catwalk@v0.52.49/pkg/catwalk/provider.go:91-104, where
 *   SupportsImages has the JSON name supports_attachments; SelectedModel: internal/config/config.go:50-69) */
/** @typedef {{session_id:string, prompt:string, submission_id?:string, hidden_user_message?:boolean,
 *   run_id?:string, channel?:string, attachments?:{file_path:string, file_name:string, mime_type:string, content:string}[]}} AgentMessage
 *   (proto.go:149-157; attachment content is base64, message.go:651-656) */
/** @typedef {{submission_id?:string, session_id:string, run_id?:string, message_id:string,
 *   text?:string, error?:string, cancelled?:boolean}} RunComplete (proto.go:82-90) */
/** @typedef {{type:"error"|"response"|"summarize", message:Message, error?:string, run_id?:string,
 *   session_id?:string, session_title?:string, progress?:string, done?:boolean}} AgentEvent (internal/proto/agent.go:29-55) */
/** @typedef {{id:string, session_id:string, tool_call_id:string, tool_name:string, description:string,
 *   action:string, params:any, path:string}} PermissionRequest (internal/proto/permission.go:16-25) */
/** @typedef {{session_id:string, tool_call_id:string, granted:boolean, denied:boolean}} PermissionNotification (permission.go:8-13) */
/** @typedef {{request_id:string, session_id:string, seq:number, chunk:string}} ShellOutput (engine design 5.9) */
/** @typedef {{kind:string, state:"checking"|"ready"|"error", read_at?:string, error?:string,
 *   windows:{name:string, used:number, resets_at?:string, model?:string}[]}} CLILimits (engine design 5.9, engine-design.md:1841-1856) */
/** @typedef {{session_id:string, kind:"permission"|"question", pending:boolean}} PromptPending (engine design 5.1(e)) */
/** @typedef {{message_id:string, highlight_text:string, highlight_start:number, highlight_end:number, title:string}} BranchRequest
 *  @typedef {{id:string, session_id:string, parent_session_id:string, source_message_id:string, highlight_text:string,
 *   highlight_start:number, highlight_end:number, origin:boolean, created_at:number}} ChatLink
 *  @typedef {{session:Session, link:ChatLink, copied_through:string, copied:number}} Branch
 *   (internal/proto/tree.go:116-143; engine design 5.11) */
/** @typedef {{message_id:string, parent_id:string, label?:string, active:boolean, message:Message}} TreeEntry
 *  @typedef {{revision:number, entries:TreeEntry[]}} Tree
 *  @typedef {{expected_revision:number, target:string, label:string}} TreeLabelRequest        ("" removes the label)
 *  @typedef {{expected_revision:number, target:string, summarize?:boolean}} TreeMoveRequest    (summarize: navigate only)
 *  @typedef {{expected_revision:number, target:string, fork:boolean, undo_id?:string}} TreeCopyRequest
 *           (undo_id: the FileRestoreResult.undo_id of a restore applied just before; the undo goes with the new chat)
 *  @typedef {{revision:number}} TreeResult                                    (label, switch, navigate)
 *  @typedef {{session_id:string, prompt:string, revision:number}} TreeCopyResult (revision: the source chat's, afterwards)
 *  @typedef {{session_id:string, revision:number}} TreeChanged
 *  @typedef {{target:string, fork:boolean}} FilePreviewRequest
 *  @typedef {{plan_id:string, session_id:string, target:string, source:string, revision:number, undo_id?:string,
 *   entries:FilePlanEntry[]}} FilePlan
 *  @typedef {{path:string, before:object, after:object, action:string, adds:number, dels:number,
 *   conflict?:string, unavailable?:string, git_head?:string, git_moved?:boolean}} FilePlanEntry
 *  @typedef {{plan_id:string}} FileUndoRequest
 *  @typedef {{restored:number, skipped?:string[], undo_id?:string, notice:string}} FileRestoreResult
 *   (internal/proto/tree.go:13-111, merged from piece C; the undo_id on TreeCopyRequest is :47,
 *   engine design item 13. Routes: internal/rpc/routes_tree.go:7-21) */
/** @typedef {{message:string, code?:string, field?:string}} EngineError (proto.go:37-41) */
