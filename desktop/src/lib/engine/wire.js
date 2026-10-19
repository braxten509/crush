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

// Phases 5 and 6 (desktop-design-5-6 6.7; scaffold S5). JSON names exactly as
// the engine sends them (internal/proto/share.go, live.go, proto.go,
// requests.go, mcp.go, agent.go, version.go, skill_management.go); the G-n
// shapes are that design's contracts (its 9.1).

/** @typedef {{id:string, session_id:string, child_session_id:string, name:string, cli:string, model:string,
 *   effort?:string, fast?:boolean, read_only?:boolean, status:"running"|"done"|"failed"|"stopped",
 *   started:string, ended?:string, delivered?:boolean, started_by?:string}} Helper          (live.go:13-28) */
/** @typedef {{pid?:number, job_id?:string, command:string, name?:string, started:string, session_id:string,
 *   started_by?:string}} Job                                                                (live.go:51-59)
 *  @typedef {{job_id?:string, pid?:number}} JobRef                                         (live.go:62-65)
 *  @typedef {{moved:boolean}} BackgroundResult                                              (live.go:69-71) */
/** @typedef {{kind:string, can_background:boolean, supports_fast?:boolean, supports_ultracode?:boolean,
 *   supports_thinking_toggle?:boolean}} AgentCapabilities                       (live.go:74-77; the last three G-8) */
/** @typedef {{supported:boolean, percent:number, high:boolean, episode:number, top?:{name:string, bytes:number}[],
 *   message?:string, limit_bytes:number, box_used_bytes:number, total_ram_bytes:number}} MemoryStatus (live.go:98-114)
 *  @typedef {{claimed:boolean}} ClaimResult                                                 (live.go:117-119)
 *  @typedef {{value:string}} MemoryLimitRequest
 *  @typedef {{limit_bytes:number, applied:boolean, notice:string}} MemoryLimitResult     (live.go:122-131) */
/** @typedef {{name:string, bin:string, path:string, current:string, latest:string}} CLIUpdate
 *  @typedef {{skipped:boolean, updates:CLIUpdate[], errors?:string[]}} CLIUpdateCheck
 *  @typedef {{bins:string[]}} CLIUpdateBins
 *  @typedef {{name:string, latest:string, error?:string}} CLIUpdateResult
 *  @typedef {{results:CLIUpdateResult[]}} CLIUpdatesInstalled
 *  @typedef {{tag:string, current:string, dir:string}} CrushRelease
 *  @typedef {{has_checkout:boolean, release?:CrushRelease, staged?:string, declined:boolean, error?:string}} CrushUpdateCheck
 *  @typedef {{tag:string}} CrushTag                                                         (live.go:134-185) */
/** @typedef {{current_version:string, latest_version:string, is_development:boolean}} UpdateAvailable (proto.go:54-58) */
/** @typedef {{launch_id?:string}} RemoteStart
 *  @typedef {{state:string, title:string, detail:string, action?:string, button?:string}} RemoteSetup
 *  @typedef {{state:string, host?:string, port?:number, phones?:string[], setup?:RemoteSetup, error?:string}} RemoteStatus
 *  @typedef {{session_id:string, focused:boolean, plan:boolean}} RemotePresence
 *  @typedef {{action_id:string, kind:string, session_id:string, text?:string, uploads?:RemoteUpload[]}} RemoteAction
 *  @typedef {{upload_id:string, name:string, mime:string, size:number}} RemoteUpload
 *  @typedef {{error?:string, session_id?:string}} RemoteActionDone                         (share.go:7-79) */
/** @typedef {{question_id:string, label:string, status:string}} SecureField
 *  @typedef {{form_id:string, session_id:string, request:QuestionRequest, fields:SecureField[]}} SecureForm
 *  @typedef {{form_id:string, reason:"submitted"|"cancelled"|"answered_elsewhere"|"closed"}} SecureFormClosed
 *  @typedef {{answers:QuestionAnswer[], values:Object<string,string>}} SecureSubmit       (share.go:82-113) */
/** @typedef {{id:string, session_id:string, tool_call_id:string, questions:QuestionItem[], confirm_title:string,
 *   confirm_description:string}} QuestionRequest
 *  @typedef {{id:string, type:"yes_no"|"single_choice"|"multi_choice"|"free_text"|"secure_entry", label:string,
 *   question:string, description:string, choices:QuestionChoice[], selected?:string[]}} QuestionItem  (selected: G-13)
 *  @typedef {{id:string, label:string, description:string, image:string}} QuestionChoice
 *  @typedef {{batch_request_id:string, responses:QuestionResponse[]}} QuestionAnswer
 *  @typedef {{request_id:string, selected_ids:string[], fill_in_text:string, yes?:boolean, notes?:Object<string,string>}} QuestionResponse
 *  @typedef {{resolved:boolean}} QuestionAnswerResponse
 *  @typedef {{batch_id:string, session_id:string}} QuestionNotification                    (proto.go:224-281) */
/** @typedef {{id:string, title:string, description:string, prompt_id:string, client_id:string,
 *   arguments:{id:string, title:string, description:string, required:boolean}[]}} MCPPrompt   (mcp.go:150-164)
 *  @typedef {{client_id:string, prompt_id:string, args:Object<string,string>}} MCPGetPromptRequest
 *  @typedef {{prompt:string}} MCPGetPromptResponse
 *  @typedef {{name:string, url:string}} MCPPendingAuthServer
 *  @typedef {{auth_url:string}} MCPAuthResponse
 *  @typedef {{name:string, disabled:boolean}} MCPSetServerDisabledRequest                 (requests.go:135-176) */
/** @typedef {{scope:number, provider_id:string, kind:string, api_key:any}} ConfigProviderKeyRequest
 *  @typedef {{scope:number, provider_id:string}} ConfigRefreshOAuthRequest
 *  @typedef {{token?:object, success:boolean}} ImportCopilotResponse                      (requests.go:53-93)
 *  @typedef {{action:"toggle"|"install", name?:string, enabled?:boolean,
 *   skill?:{id:string, name:string, source:string, skillId:string, installs:number}}} ManageSkillRequest
 *                                                     (skill_management.go:14-19; internal/skills/directory.go:23-28) */
/** @typedef {{version:string, commit:string, build_id:string, go_version:string, platform:string, protocol:number}} VersionInfo
 *                                                                                           (version.go:4-11)
 *  AgentEvent (above) gains the fields the engine already sends: aws_sso_command?:string, aws_sso_url?:string
 *  (agent.go:29-55), and provider_id?:string (G-10). Its `type` also takes "agent_finished", "re_authenticate",
 *  "aws_sso_auth" and "aws_sso_auth_result" (internal/agent/notify/notify.go:11-27). */
/** G-n contracts (desktop-design-5-6 9.1):
 *  @typedef {{name:string, source:"builtin"|"user", overridden:boolean, dark:boolean}} ThemeListItem
 *  @typedef {{name:string, root:string, base:string, source:string, overridden:boolean, pure:boolean, dark:boolean,
 *   revision:string, palette:Object<string,string>, resolved:Object<string,string>, syntax:Object<string,string>,
 *   markdown:Object<string,string>}} ThemeView                                             (G-2, 3.2)
 *  @typedef {{flow_id:string, url:string, user_code:string, code_entry:boolean}} AuthFlow
 *  @typedef {{flow_id:string, state:"waiting"|"saving"|"done"|"failed", error?:string}} AuthFlowEvent (G-5, 2.4)
 *  @typedef {{id:string, name:string, content:string, arguments:{name:string, title:string, required:boolean}[],
 *   skill:{name:string, description:string, skill_file_path:string}|null}} CustomCommand     (G-7, 2.2)
 *  @typedef {{port:number, program:string, name:string, command:string, job_id?:string, pid?:number,
 *   session_id:string}} ServerPort                                                          (G-18, 2.22)
 *  @typedef {{results:{source:string, ok:boolean, error?:string}[]}} ProvidersRefresh       (G-9)
 *  @typedef {{cpus:string}} CPUAffinity                                                     (G-16)
 *  AgentInfo (above) gains agent?:"coder"|"plan"|"", the main agent's name                 (G-26, section 4) */
