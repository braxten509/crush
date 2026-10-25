package app

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
)

// Every chat owns its model (owner, 2026-10-24: no shared model anywhere).
// wireChatModels makes new chats start on the last pick and gives each old
// chat that has no model one of its own.
func (app *App) wireChatModels(ctx context.Context) {
	store := app.config
	app.Sessions.SetNewChatModel(func(context.Context) session.ModelChoice {
		m, ok := config.NewChatModel(store.Config())
		if !ok {
			return nil
		}
		return choiceJSON(m)
	})
	adopted, err := app.Sessions.AdoptModels(ctx, func(ctx context.Context, chat session.Session) session.ModelChoice {
		var provider, model string
		if last, err := app.Messages.GetLastAssistantMessage(ctx, chat.ID); err == nil {
			provider, model = last.Provider, last.Model
		}
		m, ok := adoptedModel(store.Config(), provider, model, func() (config.SelectedModel, bool) {
			return config.NewChatModel(store.Config())
		})
		if !ok {
			return nil
		}
		return choiceJSON(m)
	})
	if err != nil {
		slog.Warn("Could not give old chats their own model", "error", err)
	} else if adopted > 0 {
		slog.Info("Gave old chats their own model", "chats", adopted)
	}
}

// adoptedModel is the model an old chat without one adopts: the model it
// last replied with (as the apps' restore did), with the effort last used
// with it; else the old shared model it has been running on; else the
// last pick.
func adoptedModel(cfg *config.Config, provider, model string, lastPick func() (config.SelectedModel, bool)) (config.SelectedModel, bool) {
	shared, hasShared := cfg.Models[config.SelectedModelTypeLarge]
	hasShared = hasShared && shared.Provider != "" && shared.Model != ""
	if provider != "" && model != "" && cfg.IsModelAvailable(provider, model) {
		m := cfg.Remembered(provider, model)
		// It ran on the shared model until now, so on the same tool it
		// keeps that model's account, not the tool's own login.
		if hasShared && shared.Provider == provider {
			m.Account = shared.Account
		}
		return m, true
	}
	if hasShared {
		return shared, true
	}
	return lastPick()
}

// choiceJSON is a model as a chat stores it.
func choiceJSON(m config.SelectedModel) session.ModelChoice {
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return data
}
