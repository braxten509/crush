package backend

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/sessioncatalog"
	"github.com/charmbracelet/crush/internal/skills"
)

// ErrReviewNotInSession is returned when a review is requested through a
// session the message does not belong to.
var ErrReviewNotInSession = errors.New("review does not belong to this session")

// Ops is the single implementation of the operations a frontend performs
// against one in-process workspace. The server [Backend] resolves a
// workspace ID and calls into Ops; the in-process workspace
// (workspace.AppWorkspace) calls Ops directly. Sharing the bodies keeps
// the two modes from drifting apart.
//
// Ops is a small value type; build one per call with the services it
// needs. Methods tolerate the partially populated [app.App] values tests
// use only as far as the operation itself requires.
type Ops struct {
	App *app.App
	Cfg *config.ConfigStore
	// Skills is the workspace's skill manager. When nil, App.Skills is
	// used.
	Skills *skills.Manager
	// ConfigChanged, when set, runs after every successful config
	// mutation. The server uses it to tell remote clients to refresh
	// their config snapshot; in-process frontends read the store
	// directly and leave it nil.
	ConfigChanged func()
	// ConfigEvent, when set, only tells the apps the config changed,
	// without the MCP re-init ConfigChanged runs. The MCP toggles use it.
	ConfigEvent func()
}

// configChanged runs the ConfigChanged hook, if any.
func (o Ops) configChanged() {
	if o.ConfigChanged != nil {
		o.ConfigChanged()
	}
}

// configEvent runs the ConfigEvent hook, if any.
func (o Ops) configEvent() {
	if o.ConfigEvent != nil {
		o.ConfigEvent()
	}
}

// skills returns the workspace's skill manager.
func (o Ops) skillManager() *skills.Manager {
	if o.Skills != nil {
		return o.Skills
	}
	if o.App != nil {
		return o.App.Skills
	}
	return nil
}

// -- Sessions --

// CreateSession creates a new session.
func (o Ops) CreateSession(ctx context.Context, title string) (session.Session, error) {
	return o.App.Sessions.Create(ctx, title)
}

// GetSession returns a session by ID.
func (o Ops) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	return o.App.Sessions.Get(ctx, sessionID)
}

// ListSessions returns every session.
func (o Ops) ListSessions(ctx context.Context) ([]session.Session, error) {
	return o.App.Sessions.List(ctx)
}

// SaveSession updates a session.
// An app sends back the whole chat it read; the usage Crush counted since
// (a turn that ran meanwhile) is kept, not undone by the older copy.
func (o Ops) SaveSession(ctx context.Context, sess session.Session) (session.Session, error) {
	return o.App.Sessions.Edit(ctx, sess.ID, func(current *session.Session) bool {
		counted := *current
		*current = sess
		current.MessageCount = counted.MessageCount
		current.PromptTokens = counted.PromptTokens
		current.CompletionTokens = counted.CompletionTokens
		current.Cost = counted.Cost
		return true
	})
}

// DeleteSession deletes a session.
func (o Ops) DeleteSession(ctx context.Context, sessionID string) error {
	return o.App.Sessions.Delete(ctx, sessionID)
}

// SetKeptSkill turns a skill on or off for a whole session.
func (o Ops) SetKeptSkill(ctx context.Context, sessionID string, skill session.KeptSkill, on bool) (session.Session, error) {
	return o.App.Sessions.SetKeptSkill(ctx, sessionID, skill, on)
}

// SetSessionModel saves the model one chat runs on.
func (o Ops) SetSessionModel(ctx context.Context, sessionID string, model session.ModelChoice) (session.Session, error) {
	return o.App.Sessions.SetModel(ctx, sessionID, model)
}

// savedSessions returns the all-project catalog rooted at this
// workspace.
func (o Ops) savedSessions() sessioncatalog.Catalog {
	return sessioncatalog.Catalog{
		Directory:     o.Cfg.WorkingDir(),
		DataDirectory: o.Cfg.Config().Options.DataDirectory,
	}
}

// ListSavedSessions lists saved chats across every project.
func (o Ops) ListSavedSessions(ctx context.Context, selected string) ([]session.Session, error) {
	return o.savedSessions().List(ctx, selected)
}

// ListSavedSessionsShown is ListSavedSessions that also keeps the drafts
// an app shows right now and, with folders above 0, also lists that many
// recently used projects' chats.
func (o Ops) ListSavedSessionsShown(ctx context.Context, selected string, shown []string, folders int) ([]session.Session, error) {
	catalog := o.savedSessions()
	catalog.Shown = shown
	catalog.Folders = folders
	return catalog.List(ctx, selected)
}

// ChangeSavedSession renames or deletes a saved chat. Chats stored in
// this workspace's own data directory go through the session service so
// its subscribers see the change; chats of other projects are edited in
// their own databases.
func (o Ops) ChangeSavedSession(ctx context.Context, target session.Session, remove bool) error {
	if target.DataDirectory == o.Cfg.Config().Options.DataDirectory {
		if remove {
			return o.DeleteSession(ctx, target.ID)
		}
		return o.App.Sessions.Rename(ctx, target.ID, target.Title)
	}
	return o.savedSessions().Change(ctx, target, remove)
}

// DiscardEmptySession deletes the session when it has no messages.
func (o Ops) DiscardEmptySession(ctx context.Context, id string) error {
	return o.savedSessions().DiscardEmpty(ctx, id)
}

// -- Messages and history --

// ListMessages returns every message of a session. It first drains the
// debounced message updates so callers observe the latest in-memory
// state; a cold List would otherwise miss streaming deltas at
// session-switch time.
func (o Ops) ListMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	if err := o.App.Messages.FlushAll(ctx); err != nil {
		return nil, err
	}
	return o.App.Messages.List(ctx, sessionID)
}

// ListUserMessages returns the user-role messages of a session.
func (o Ops) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return o.App.Messages.ListUserMessages(ctx, sessionID)
}

// ListAllUserMessages returns the user-role messages of every session.
func (o Ops) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	return o.App.Messages.ListAllUserMessages(ctx)
}

// LoadMessageReview loads the retained file review of a message, which
// must belong to sessionID.
func (o Ops) LoadMessageReview(ctx context.Context, sessionID, messageID string) (message.Message, error) {
	msg, err := o.App.Messages.Get(ctx, messageID)
	if err != nil {
		return message.Message{}, err
	}
	if msg.SessionID != sessionID {
		return message.Message{}, ErrReviewNotInSession
	}
	return o.App.Messages.LoadReview(ctx, messageID)
}

// ListSessionHistory returns the file versions recorded for a session.
func (o Ops) ListSessionHistory(ctx context.Context, sessionID string) ([]history.File, error) {
	return o.App.History.ListBySession(ctx, sessionID)
}

// -- File tracker --

// FileTrackerRecordRead records that a session read a file.
func (o Ops) FileTrackerRecordRead(ctx context.Context, sessionID, path string) {
	o.App.FileTracker.RecordRead(ctx, sessionID, path)
}

// FileTrackerLastReadTime returns when a session last read a file.
func (o Ops) FileTrackerLastReadTime(ctx context.Context, sessionID, path string) time.Time {
	return o.App.FileTracker.LastReadTime(ctx, sessionID, path)
}

// FileTrackerListReadFiles returns the files a session has read.
func (o Ops) FileTrackerListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	return o.App.FileTracker.ListReadFiles(ctx, sessionID)
}

// -- Permissions and questions --

// ResolvePermission grants, persistently grants, or denies a pending
// permission request. The bool reports whether this call resolved it
// (false when another caller already had).
func (o Ops) ResolvePermission(req permission.PermissionRequest, action proto.PermissionAction) (bool, error) {
	switch action {
	case proto.PermissionAllow:
		return o.App.Permissions.Grant(req), nil
	case proto.PermissionAllowForSession:
		return o.App.Permissions.GrantPersistent(req), nil
	case proto.PermissionDeny:
		return o.App.Permissions.Deny(req), nil
	default:
		return false, ErrInvalidPermissionAction
	}
}

// LSPStart starts the language servers for a path.
func (o Ops) LSPStart(ctx context.Context, path string) {
	o.App.LSPManager.Start(ctx, path)
}

// LSPStopAll stops every language server.
func (o Ops) LSPStopAll(ctx context.Context) {
	o.App.LSPManager.StopAll(ctx)
}

// -- Skills --

// ListSkills refreshes discovery and returns the visible skills.
func (o Ops) ListSkills() []skills.CatalogEntry {
	mgr := o.skillManager()
	mgr.Refresh(skills.ConfigDiscovery(o.Cfg))
	return skills.Catalog(mgr.ActiveSkills(), mgr.ResolvedPaths(), mgr.WorkingDir())
}

// ReadSkill reads a skill's content by ID.
func (o Ops) ReadSkill(skillID string) ([]byte, skills.SkillReadResult, error) {
	mgr := o.skillManager()
	return skills.ReadContent(mgr.ActiveSkills(), mgr.ResolvedPaths(), mgr.WorkingDir(), skillID)
}

// InstalledSkills lists installed skills, including disabled ones.
func (o Ops) InstalledSkills() []skills.InstalledSkill {
	return skills.Installed(o.Cfg, o.skillManager())
}

// SetSkillEnabled enables or disables an installed skill.
func (o Ops) SetSkillEnabled(name string, enabled bool) error {
	if err := skills.SetEnabled(o.Cfg, o.skillManager(), name, enabled); err != nil {
		return err
	}
	o.configChanged()
	return nil
}

// InstallSkill installs a skill from a directory listing.
func (o Ops) InstallSkill(ctx context.Context, selected skills.DirectorySkill) error {
	if err := skills.Install(ctx, o.Cfg, o.skillManager(), selected); err != nil {
		return err
	}
	o.configChanged()
	return nil
}

// ops returns the shared operations bound to this workspace. Config
// mutations publish a ConfigChanged event so remote clients refresh
// their cached snapshot.
func (w *Workspace) ops() Ops {
	return Ops{
		App:           w.App,
		Cfg:           w.Cfg,
		Skills:        w.Skills,
		ConfigChanged: func() { publishConfigChanged(w) },
		ConfigEvent:   func() { sendConfigChanged(w) },
	}
}

// ops resolves a workspace ID to its shared operations.
func (b *Backend) ops(workspaceID string) (Ops, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return Ops{}, err
	}
	return ws.ops(), nil
}
