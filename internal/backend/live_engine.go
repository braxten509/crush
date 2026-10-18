package backend

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/memwatch"
)

// liveEngine is piece B's engine-wide state: the memory checks, the limits
// refresh at start and the last AI tool update check. It is kept per
// Backend here, because backend.go belongs to piece A.
type liveEngine struct {
	// limitsOnce runs the limits refresh at the first workspace.
	limitsOnce sync.Once

	// memMu guards memStop, the running memory checks' stop.
	memMu   sync.Mutex
	memStop context.CancelFunc
	// memCheck runs one memory check at a time; memLast is the last
	// check's status, once memSeen.
	memCheck sync.Mutex
	memLast  memwatch.Status
	memSeen  bool

	// updMu guards the last AI tool update check, kept for install and
	// decline.
	updMu     sync.Mutex
	lastCheck []cliupdate.Update
	checkedAt time.Time
	checked   bool
}

// liveEngines maps each Backend to its liveEngine.
var liveEngines sync.Map // *Backend → *liveEngine

// liveEngine returns b's engine-wide state, made on first use. It is
// forgotten when b's context ends.
func (b *Backend) liveEngine() *liveEngine {
	if e, ok := liveEngines.Load(b); ok {
		return e.(*liveEngine)
	}
	e, loaded := liveEngines.LoadOrStore(b, &liveEngine{})
	if !loaded {
		if done := b.engineContext().Done(); done != nil {
			go func() {
				<-done
				liveEngines.Delete(b)
				removeLimitsHook(b)
			}()
		}
	}
	return e.(*liveEngine)
}

// engineContext is the engine's lifetime context.
func (b *Backend) engineContext() context.Context {
	if b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}
