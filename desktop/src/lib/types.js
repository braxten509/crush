// What the app builds and hands from one piece to another, as JSDoc types
// (desktop-design 1.5.6). No code. wire.js holds what the engine sends. Every
// item component takes exactly one prop, `item`, of its kind's type; ItemFrame
// (P3.5) draws the selection bar, the key letter and the focus around it, so no
// item component draws those.

/** Fields every timeline item has (built by lib/timeline.js, P3.5).
 *   key      stable for the item's life: "<mid>" (user, reply), "notice:<mid>", "banner:<mid>", "footer:<mid>",
 *            "status:<mid>", "group:<first step's mid>", "tool:<tool call id>", "shell:<mid>:<n>" (saved),
 *            "shell:<request id>" (live), "pending:<submission id>"
 *   version  bumped by lib/timeline.js whenever anything the item draws changes; the height cache and the
 *            rendered-item cache key on key + version (1.6)
 *   letter   "" or the vermilion key's letters (4.4: B, C, … then AA)
 * @typedef {{key:string, version:number, letter:string, sessionId:string, messageId:string}} ItemBase
 *
 * @typedef {ItemBase & {kind:"user", text:string, chips:ChipView[]}} UserItem          CHAT-002 to CHAT-007
 * @typedef {ItemBase & {kind:"reply", text:string, finished:boolean, plan:"none"|"open"|"closed",
 *   author:string, time:number}} ReplyItem                                              CHAT-014 to CHAT-018, CHAT-031, CHAT-032
 *   (text is the stored Markdown, M.Content().Text; render/plan.js removes the plan markers when it draws)
 * @typedef {ItemBase & {kind:"notice", notice:"skill"|"task"|"answers"|"system", state:string, name:string,
 *   text:string}} NoticeItem                                                            CHAT-008 to CHAT-011
 * @typedef {ItemBase & {kind:"banner", banner:"canceled"|"error"|"refused"|"compacting"|"compacted"|"compaction_canceled",
 *   title:string, details:string}} BannerItem                                           CHAT-025, CHAT-027 to CHAT-029
 * @typedef {ItemBase & {kind:"footer", text:string, saved:string}} FooterItem          CHAT-035 to CHAT-037
 * @typedef {ItemBase & {kind:"pending", submissionId:string, text:string, chips:ChipView[],
 *   state:"working"|"uncertain"}} PendingItem                                           TYPE-042, TYPE-048
 * @typedef {ItemBase & {kind:"status", words:string, startedAt:number, thought:string}} StatusItem   CHAT-019 to CHAT-022
 * @typedef {ItemBase & {kind:"shell", command:string, output:string, exitCode:number|null, running:boolean,
 *   requestId:string}} ShellItem                                                        CHAT-013, CHAT-038 to CHAT-043
 *   (from a saved shell_command part, requestId ""; or from composer.shells while it runs)
 * @typedef {ItemBase & {kind:"tools", groupId:string, calls:ToolCallView[], working:boolean, lastFailed:boolean,
 *   backgrounded:number, failed:number, summary:string, live:StatusItem|null,
 *   changes:{sessionId:string, messageId:string, toolCallIds:string[], title:string}|null}} ToolGroupItem   CHAT-080 to CHAT-086
 * @typedef {ItemBase & {kind:"tool", call:ToolCallView}} ToolItem    a tool that never folds (Question, Agent, Agentic Fetch)
 * @typedef {UserItem|ReplyItem|NoticeItem|BannerItem|FooterItem|PendingItem|StatusItem|ShellItem|ToolGroupItem|ToolItem} TimelineItem
 *
 * @typedef {{id:string, name:string, input:string, messageId:string,
 *   status:"pending"|"running"|"permission"|"done"|"error"|"cancelled"|"background",
 *   result:{content:string, data?:string, mime_type?:string, metadata:string, is_error:boolean, review?:Review}|null,
 *   hooks:object[], children:ToolCallView[]}} ToolCallView    one call and its result; children: an agent's nested tools (CHAT-078)
 * @typedef {{kind:"picture"|"file"|"text"|"skill", name:string, mime:string, viewable:boolean, src?:string}} ChipView
 * @typedef {{path:string, before:string, after:string}} DiffFile                       DiffView's input, one per file (CHAT-107 to CHAT-112)
 *
 * @typedef {{requestId:string, sessionId:string, command:string, output:string, lastSeq:number,
 *   running:boolean, exitCode:number|null, startedAt:number}} ShellState
 *   one live `!` command in composer.shells; output is the shell_output chunks joined in seq order (CHAT-039)
 *
 * @typedef {{id:string, sessionId:string, messageId:string, index:number,
 *   kind:"diagram"|"math"|"table"|"picture", title:string, source:string, src?:string, host?:string}} Figure
 *   id is "<message id>:<index>" (desktop-design 1.5.6, "Figure ids and who registers them")
 *
 * @typedef {{kind:"file"|"picture"|"text", name:string, mime:string, path?:string, base64:string}} Attachment
 * @typedef {{kind:"permission"|"plan"|"customize"|"question"|"secure"|"update", component:any, props:object}} SlotEntry
 *   ("update": SYS's yes/no offers through ASK's askYesNo, desktop-design-5-6 2.19)
 * @typedef {{kind:"helper"|"terminal", component:any, props:object}} MainViewEntry
 *   what lib/mainview.js shows in the middle column instead of the chat (desktop-design-5-6 6.4)
 * @typedef {import("./engine/wire.js").Review} Review
 */
