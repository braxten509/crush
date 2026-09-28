package remote

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/workspace"
)

// Source is everything the server reads from and asks of the running
// workspace, besides the TUI actions.
type Source interface {
	Events(ctx context.Context) <-chan pubsub.Event[tea.Msg]
	ListMessages(ctx context.Context, sessionID string) ([]message.Message, error)
	GetSession(ctx context.Context, sessionID string) (session.Session, error)
	SaveSession(ctx context.Context, sess session.Session) (session.Session, error)
	AgentIsSessionBusy(sessionID string) bool
	AgentQueuedPromptsList(sessionID string) []string

	PendingPermission() (permission.PermissionRequest, bool)
	PermissionGrant(perm permission.PermissionRequest) bool
	PermissionGrantPersistent(perm permission.PermissionRequest) bool
	PermissionDeny(perm permission.PermissionRequest) bool
	PermissionSkipRequests() bool

	PendingQuestion() (question.Request, bool)
	QuestionAnswer(answers []question.Answer) bool
	QuestionCancel() bool

	Config() *config.Config
	WorkingDir() string

	Tasks(sessionID string) []agent.Task
	Processes() []agent.Process
	StopTask(id string) error
	KillProcess(pid int) error
	Limits(kind catwalk.Type, model string) []cliagent.Limit
	CanBackground(kind catwalk.Type) bool
}

// NewSource wraps an in-process workspace. /remote works only in-process:
// sub-agents, questions from `crush ask` and Ctrl+B live there.
func NewSource(ws *workspace.AppWorkspace) Source {
	return appSource{ws}
}

type appSource struct {
	*workspace.AppWorkspace
}

func (a appSource) Events(ctx context.Context) <-chan pubsub.Event[tea.Msg] {
	return a.App().Events(ctx)
}

func (a appSource) PendingPermission() (permission.PermissionRequest, bool) {
	if p, ok := a.App().Permissions.(interface {
		Pending() (permission.PermissionRequest, bool)
	}); ok {
		return p.Pending()
	}
	return permission.PermissionRequest{}, false
}

func (a appSource) PendingQuestion() (question.Request, bool) {
	if q, ok := a.App().Questions.(interface {
		Pending() (question.Request, bool)
	}); ok {
		return q.Pending()
	}
	return question.Request{}, false
}

func (a appSource) Tasks(sessionID string) []agent.Task { return agent.SessionTasks(sessionID) }
func (a appSource) Processes() []agent.Process          { return agent.BackgroundProcesses() }
func (a appSource) StopTask(id string) error            { return agent.StopTask(id) }
func (a appSource) KillProcess(pid int) error           { return agent.KillProcess(pid) }

func (a appSource) Limits(kind catwalk.Type, model string) []cliagent.Limit {
	return cliagent.Limits(kind, model)
}

func (a appSource) CanBackground(kind catwalk.Type) bool { return cliagent.CanBackground(kind) }
