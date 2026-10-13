// Package models holds the model selection dialog and its grouped,
// filterable list.
package models

import (
	"cmp"
	"fmt"
	"log/slog"
	"slices"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
)

// ModelType represents the type of model to select.
type ModelType int

const (
	ModelTypeLarge ModelType = iota
	ModelTypeSmall
)

// String returns the string representation of the [ModelType].
func (mt ModelType) String() string {
	switch mt {
	case ModelTypeLarge:
		return "Large Task"
	case ModelTypeSmall:
		return "Small Task"
	default:
		return "Unknown"
	}
}

// Config returns the corresponding config model type.
func (mt ModelType) Config() config.SelectedModelType {
	switch mt {
	case ModelTypeLarge:
		return config.SelectedModelTypeLarge
	case ModelTypeSmall:
		return config.SelectedModelTypeSmall
	default:
		return ""
	}
}

// Placeholder returns the input placeholder for the model type.
func (mt ModelType) Placeholder() string {
	switch mt {
	case ModelTypeLarge:
		return largeModelInputPlaceholder
	case ModelTypeSmall:
		return smallModelInputPlaceholder
	default:
		return ""
	}
}

const (
	onboardingModelInputPlaceholder = "Find your fave"
	largeModelInputPlaceholder      = "Choose a model for large, complex tasks"
	smallModelInputPlaceholder      = "Choose a model for small, simple tasks"
)

const defaultModelsDialogMaxWidth = 73

// Models represents a model selection dialog.
type Models struct {
	com          *common.Common
	isOnboarding bool

	modelType ModelType
	providers []catwalk.Provider

	keyMap struct {
		Tab      key.Binding
		UpDown   key.Binding
		Select   key.Binding
		Edit     key.Binding
		Next     key.Binding
		Previous key.Binding
		Close    key.Binding
	}
	list  *ModelsList
	input textinput.Model
	help  help.Model
}

var _ dialog.Dialog = (*Models)(nil)

// NewModels creates a new Models dialog.
func NewModels(com *common.Common, isOnboarding bool) (*Models, error) {
	t := com.Styles
	m := &Models{}
	m.com = com
	m.isOnboarding = isOnboarding

	help := help.New()
	help.Styles = t.DialogHelpStyles()

	m.help = help
	m.list = NewModelsList(t)
	m.list.Focus()
	m.list.SetSelected(0)

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = onboardingModelInputPlaceholder
	m.input.SetStyles(com.Styles.TextInput)
	m.input.Focus()

	m.keyMap.Tab = key.NewBinding(
		key.WithKeys("tab", "shift+tab"),
		key.WithHelp("tab", "toggle type"),
	)
	m.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "confirm"),
	)
	m.keyMap.Edit = key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl+e", "edit"),
	)
	m.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	m.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	m.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	m.keyMap.Close = dialog.CloseKey

	// A stale catalog must not keep this dialog from opening: it is the
	// only way for the user to choose a model.
	var err error
	m.providers, err = config.Providers(m.com.Config())
	if err != nil {
		if len(m.providers) == 0 {
			return nil, fmt.Errorf("failed to get providers: %w", err)
		}
		slog.Warn("Listing the previously known providers", "error", err)
	}

	if err := m.setProviderItems(); err != nil {
		return nil, fmt.Errorf("failed to set provider items: %w", err)
	}

	return m, nil
}

// ID implements Dialog.
func (m *Models) ID() string {
	return dialog.ModelsID
}

// HandleMsg implements Dialog.
func (m *Models) HandleMsg(msg tea.Msg) dialog.Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keyMap.Close):
			return dialog.ActionClose{}
		case key.Matches(msg, m.keyMap.Previous):
			m.list.Focus()
			if m.list.IsSelectedFirst() {
				m.list.SelectLast()
			} else {
				m.list.SelectPrev()
			}
			m.list.ScrollToSelected()
		case key.Matches(msg, m.keyMap.Next):
			m.list.Focus()
			if m.list.IsSelectedLast() {
				m.list.SelectFirst()
			} else {
				m.list.SelectNext()
			}
			m.list.ScrollToSelected()
		case key.Matches(msg, m.keyMap.Select, m.keyMap.Edit):
			selectedItem := m.list.SelectedItem()
			if selectedItem == nil {
				break
			}

			modelItem, ok := selectedItem.(*ModelItem)
			if !ok {
				break
			}

			isEdit := key.Matches(msg, m.keyMap.Edit)

			return dialog.ActionSelectModel{
				Provider:       modelItem.prov,
				Model:          modelItem.SelectedModel(),
				ModelType:      modelItem.SelectedModelType(),
				ReAuthenticate: isEdit,
			}
		case key.Matches(msg, m.keyMap.Tab):
			if m.isOnboarding {
				break
			}
			if m.modelType == ModelTypeLarge {
				m.modelType = ModelTypeSmall
			} else {
				m.modelType = ModelTypeLarge
			}
			if err := m.setProviderItems(); err != nil {
				return util.ReportError(err)
			}
		default:
			prevValue := m.input.Value()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			value := m.input.Value()
			if value != prevValue {
				m.list.Focus()
				m.list.SetFilter(value)
				m.list.SelectFirst()
				m.list.ScrollToTop()
			}
			return dialog.ActionCmd{Cmd: cmd}
		}
	}
	return nil
}

// Cursor returns the cursor for the dialog.
func (m *Models) Cursor() *tea.Cursor {
	return dialog.InputCursor(m.com.Styles, m.input.Cursor())
}

// modelTypeRadioView returns the radio view for model type selection.
func (m *Models) modelTypeRadioView() string {
	t := m.com.Styles
	textStyle := t.Radio.Label
	largeRadioStyle := t.Radio.Off
	smallRadioStyle := t.Radio.Off
	if m.modelType == ModelTypeLarge {
		largeRadioStyle = t.Radio.On
	} else {
		smallRadioStyle = t.Radio.On
	}

	largeRadio := largeRadioStyle.Padding(0, 1).Render()
	smallRadio := smallRadioStyle.Padding(0, 1).Render()

	return fmt.Sprintf("%s%s  %s%s",
		largeRadio, textStyle.Render(ModelTypeLarge.String()),
		smallRadio, textStyle.Render(ModelTypeSmall.String()))
}

// Draw implements [Dialog].
func (m *Models) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := m.com.Styles
	width := max(0, min(defaultModelsDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(kit.DefaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	m.input.SetWidth(kit.DialogInputTextWidth(t, m.input, innerWidth))

	listHeight, listTotalHeight, _ := kit.SizeDialogList(t, m.list, innerWidth, height)

	rc := dialog.NewRenderContext(t, width)
	rc.Title = "Switch Model"
	rc.TitleInfo = m.modelTypeRadioView()

	if m.isOnboarding {
		titleText := t.Dialog.PrimaryText.Render("To start, let's choose a provider and model.")
		rc.AddPart(titleText)
	}

	inputView := t.Dialog.InputPrompt.Render(m.input.View())
	rc.AddPart(inputView)

	listView := t.Dialog.List.Height(m.list.Height()).Render(m.list.Render())
	listView = kit.JoinScrollbar(t, listView, listHeight, listTotalHeight, listHeight, m.list.Offset())
	rc.AddPart(listView)

	rc.Help = kit.RenderDialogHelp(t, &m.help, m, innerWidth)

	cur := m.Cursor()

	if m.isOnboarding {
		rc.Title = ""
		rc.TitleInfo = ""
		rc.IsOnboarding = true
		view := rc.Render()
		cur = kit.AdjustOnboardingInputCursor(t, cur)
		dialog.DrawOnboardingCursor(scr, area, view, cur)
	} else {
		view := rc.Render()
		dialog.DrawCenterCursor(scr, area, view, cur)
	}
	return cur
}

// ShortHelp returns the short help view.
func (m *Models) ShortHelp() []key.Binding {
	if m.isOnboarding {
		return []key.Binding{
			m.keyMap.UpDown,
			m.keyMap.Select,
		}
	}
	h := []key.Binding{
		m.keyMap.UpDown,
		m.keyMap.Tab,
		m.keyMap.Select,
	}
	if m.isSelectedConfigured() {
		h = append(h, m.keyMap.Edit)
	}
	h = append(h, m.keyMap.Close)
	return h
}

// FullHelp returns the full help view.
func (m *Models) FullHelp() [][]key.Binding {
	return [][]key.Binding{m.ShortHelp()}
}

func (m *Models) isSelectedConfigured() bool {
	selectedItem := m.list.SelectedItem()
	if selectedItem == nil {
		return false
	}
	modelItem, ok := selectedItem.(*ModelItem)
	if !ok {
		return false
	}
	providerID := string(modelItem.prov.ID)
	_, isConfigured := m.com.Config().Providers.Get(providerID)
	return isConfigured
}

// setProviderItems sets the provider items in the list.
func (m *Models) setProviderItems() error {
	cfg := m.com.Config()

	selectedType := m.modelType.Config()
	recentItems := cfg.RecentModels[selectedType]

	// Get a list of known providers to compare against
	knownProviders, err := config.Providers(cfg)
	if err != nil && len(knownProviders) == 0 {
		return fmt.Errorf("failed to get providers: %w", err)
	}

	b := &modelGroupsBuilder{
		t:            m.com.Styles,
		modelType:    m.modelType,
		currentModel: cfg.Models[selectedType],
		itemsMap:     make(map[string]*ModelItem),
		groups:       []ModelGroup{},
	}
	addedProviders := m.addCustomProviderGroups(b, cfg, knownProviders)
	m.addKnownProviderGroups(b, cfg, addedProviders)

	if len(recentItems) > 0 {
		if err := m.addRecentGroup(b, recentItems, selectedType); err != nil {
			return err
		}
	}

	// Set model groups in the list.
	m.list.SetGroups(b.groups...)
	m.list.SetSelectedItem(b.selectedItemID)
	if b.selectedItemID != "" {
		m.list.ScrollToSelected()
	} else {
		m.list.ScrollToTop()
	}

	// Update placeholder based on model type
	if !m.isOnboarding {
		m.input.Placeholder = m.modelType.Placeholder()
	}

	return nil
}

// modelGroupsBuilder collects the model groups shown by the dialog and
// tracks the item matching the current model.
type modelGroupsBuilder struct {
	t            *styles.Styles
	modelType    ModelType
	currentModel config.SelectedModel
	// itemsMap contains the keys of added model items.
	itemsMap       map[string]*ModelItem
	selectedItemID string
	groups         []ModelGroup
}

// newGroup builds a group titled name with an item for each model of
// provider, recording the items and the one for the current model.
func (b *modelGroupsBuilder) newGroup(name string, configured bool, provider catwalk.Provider, models []catwalk.Model) ModelGroup {
	group := NewModelGroup(b.t, name, configured)
	for _, model := range models {
		item := NewModelItem(b.t, provider, model, b.modelType, false)
		group.AppendItems(item)
		b.itemsMap[item.ID()] = item
		if model.ID == b.currentModel.Model && string(provider.ID) == b.currentModel.Provider {
			b.selectedItemID = item.ID()
		}
	}
	return group
}

// appendNonEmpty appends group unless it has no items.
func (b *modelGroupsBuilder) appendNonEmpty(group ModelGroup) {
	if len(group.Items) > 0 {
		b.groups = append(b.groups, group)
	}
}

// addCustomProviderGroups adds a group for each enabled configured
// provider missing from the known or offered providers, and returns the
// IDs it added.
func (m *Models) addCustomProviderGroups(b *modelGroupsBuilder, cfg *config.Config, knownProviders []catwalk.Provider) map[string]bool {
	// Track providers already added to avoid duplicates
	addedProviders := make(map[string]bool)

	containsProviderFunc := func(id string) func(p catwalk.Provider) bool {
		return func(p catwalk.Provider) bool {
			return p.ID == catwalk.InferenceProvider(id)
		}
	}

	for id, p := range cfg.Providers.Seq2() {
		if p.Disable {
			continue
		}

		// Check if this provider is not in the known providers list
		if !slices.ContainsFunc(knownProviders, containsProviderFunc(id)) ||
			!slices.ContainsFunc(m.providers, containsProviderFunc(id)) {
			provider := p.ToProvider()

			// Add this unknown provider to the list
			name := cmp.Or(p.Name, id)

			addedProviders[id] = true

			b.appendNonEmpty(b.newGroup(name, true, provider, p.Models))
		}
	}
	return addedProviders
}

// addKnownProviderGroups adds a group for each offered provider that is
// neither disabled nor already added.
func (m *Models) addKnownProviderGroups(b *modelGroupsBuilder, cfg *config.Config, addedProviders map[string]bool) {
	// Now add known providers from the predefined list.
	// Providers already has Hyper at the front of the list.
	for _, provider := range m.providers {
		providerID := string(provider.ID)
		if addedProviders[providerID] {
			continue
		}

		providerConfig, providerConfigured := cfg.Providers.Get(providerID)
		if providerConfigured && providerConfig.Disable {
			continue
		}

		displayProvider := provider
		if providerConfigured {
			displayProvider = withConfiguredModels(displayProvider, providerConfig)
		}

		name := cmp.Or(displayProvider.Name, providerID)

		// The OpenAI and xAI providers each hold exactly one credential.
		// Signed in with ChatGPT or Grok, only the models the
		// subscription grants are usable, so they are all the section
		// lists; the API catalog would only 404. Without a login the
		// section is the API catalog.
		if subscriptionModels := subscriptionModels(provider.ID, providerConfig); subscriptionModels != nil {
			b.appendNonEmpty(b.newGroup(name, true, provider, subscriptionModels))
			continue
		}

		b.groups = append(b.groups, b.newGroup(name, providerConfigured, provider, displayProvider.Models))
	}
}

// withConfiguredModels applies a provider's configured name and models to
// its catalog entry: configured names override catalog ones, and models
// missing from the catalog are appended.
func withConfiguredModels(displayProvider catwalk.Provider, providerConfig config.ProviderConfig) catwalk.Provider {
	displayProvider.Name = cmp.Or(providerConfig.Name, displayProvider.Name)
	modelIndex := make(map[string]int, len(displayProvider.Models))
	for i, model := range displayProvider.Models {
		modelIndex[model.ID] = i
	}
	for _, model := range providerConfig.Models {
		if model.ID == "" {
			continue
		}
		if idx, ok := modelIndex[model.ID]; ok {
			if model.Name != "" {
				displayProvider.Models[idx].Name = model.Name
			}
			continue
		}
		model.Name = cmp.Or(model.Name, model.ID)
		displayProvider.Models = append(displayProvider.Models, model)
		modelIndex[model.ID] = len(displayProvider.Models) - 1
	}
	return displayProvider
}

// subscriptionModels returns the models a signed-in ChatGPT or Grok
// subscription grants, or nil when the provider is not signed in with
// one.
func subscriptionModels(id catwalk.InferenceProvider, providerConfig config.ProviderConfig) []catwalk.Model {
	if providerConfig.OAuthToken == nil {
		return nil
	}
	switch id {
	case catwalk.InferenceProviderOpenAI:
		return providerConfig.ChatGPTModels
	case catwalk.InferenceProviderXAI:
		return providerConfig.GrokModels
	}
	return nil
}

// addRecentGroup puts the recently used models that are still offered in
// a group at the top, and drops the stale ones from the config.
func (m *Models) addRecentGroup(b *modelGroupsBuilder, recentItems []config.SelectedModel, selectedType config.SelectedModelType) error {
	recentGroup := NewModelGroup(b.t, "Recently used", false)

	var validRecentItems []config.SelectedModel
	for _, recent := range recentItems {
		key := modelKey(recent.Provider, recent.Model)
		item, ok := b.itemsMap[key]
		if !ok {
			continue
		}

		// Show provider for recent items
		item = NewModelItem(b.t, item.prov, item.model, m.modelType, true)
		item.showProvider = true

		validRecentItems = append(validRecentItems, recent)
		recentGroup.AppendItems(item)
		if recent.Model == b.currentModel.Model && recent.Provider == b.currentModel.Provider {
			b.selectedItemID = item.ID()
		}
	}

	if len(validRecentItems) != len(recentItems) {
		// FIXME: Does this need to be here? Is it mutating the config during a read?
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, fmt.Sprintf("recent_models.%s", selectedType), validRecentItems); err != nil {
			return fmt.Errorf("failed to update recent models: %w", err)
		}
	}

	if len(recentGroup.Items) > 0 {
		b.groups = append([]ModelGroup{recentGroup}, b.groups...)
	}
	return nil
}

func modelKey(providerID, modelID string) string {
	if providerID == "" || modelID == "" {
		return ""
	}
	return providerID + ":" + modelID
}
