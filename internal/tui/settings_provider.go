package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// maxFallbackSlots is the maximum number of configurable fallback entries.
const maxFallbackSlots = 3

// fallbackSlot holds state for a single fallback configuration slot.
type fallbackSlot struct {
	provider    string   // selected provider (empty = not configured)
	model       string   // selected model
	providers   []string // available provider choices (shared from main list)
	models      []string // available model choices for selected provider
	providerIdx int
	modelIdx    int
}

// ProviderSection handles provider and model selection.
type ProviderSection struct {
	providers       []string
	models          []string
	providerIdx     int
	modelIdx        int
	focusField      int // 0=providers, 1=models, 2..4=fallback slots
	currentProvider string
	currentModel    string
	loaded          bool
	modelsLoading   bool

	// Fallback configuration (up to 3 slots)
	fallbacks     [maxFallbackSlots]fallbackSlot
	fallbackField int  // 0=provider, 1=model (within active fallback slot)
	fallbackSaved bool // briefly true after successful save
}

func NewProviderSection(provider, model string) ProviderSection {
	return ProviderSection{
		currentProvider: provider,
		currentModel:    model,
	}
}

func (ps *ProviderSection) SetProvider(p string) { ps.currentProvider = p }
func (ps *ProviderSection) SetModel(m string)    { ps.currentModel = m }

// HasActiveInput reports whether the section is in an editing mode that
// should capture Left/Right keys (preventing settings tab switching).
func (ps *ProviderSection) HasActiveInput() bool {
	return ps.focusField >= 2
}

// ---------------------------------------------------------------------------
// Message handlers
// ---------------------------------------------------------------------------

func (ps *ProviderSection) HandleProviders(msg ProvidersMsg, apiClient *client.APIClient) tea.Cmd {
	if msg.Err != nil {
		ps.providers = []string{"mock", "google-gemini-cli", "google-ai-studio", "anthropic", "openai", "openai-codex"}
	} else {
		ps.providers = msg.Providers
	}
	ps.loaded = true
	// Find current provider index
	for i, p := range ps.providers {
		if p == ps.currentProvider {
			ps.providerIdx = i
			break
		}
	}
	// Show hardcoded fallback immediately, then fetch dynamic list.
	ps.models = modelOptionsForProvider(ps.currentProvider, ps.currentModel)
	for i, m := range ps.models {
		if m == ps.currentModel {
			ps.modelIdx = i
			break
		}
	}

	// Sync fallback slots with the loaded providers list.
	for i := 0; i < maxFallbackSlots; i++ {
		ps.syncFallbackProviders(i)
	}

	// Trigger async model fetch for the current provider.
	if ps.currentProvider != "" && ps.currentProvider != "mock" {
		ps.modelsLoading = true
		return listModelsCmd(apiClient, ps.currentProvider, "")
	}
	return nil
}

func (ps *ProviderSection) HandleModels(msg AIStudioModelsMsg) tea.Cmd {
	if msg.Err != nil {
		ps.models = modelOptionsForProvider("google-ai-studio", ps.currentModel)
	} else {
		ps.models = []string{"default"}
		for _, m := range msg.Response.Models {
			ps.models = appendUnique(ps.models, strings.TrimSpace(m))
		}
		if ps.currentModel != "" {
			ps.models = appendUnique(ps.models, ps.currentModel)
		}
	}
	for i, m := range ps.models {
		if m == ps.currentModel {
			ps.modelIdx = i
			break
		}
	}
	return nil
}

// HandleModelsList processes the generic models list response.
func (ps *ProviderSection) HandleModelsList(msg ModelsListMsg) tea.Cmd {
	ps.modelsLoading = false
	// Only apply if the response matches the currently selected provider.
	if msg.Provider != ps.currentProvider {
		return nil
	}
	if msg.Err != nil {
		// Keep existing fallback list on error.
		return nil
	}
	ps.models = []string{"default"}
	for _, m := range msg.Response.Models {
		ps.models = appendUnique(ps.models, strings.TrimSpace(m))
	}
	if ps.currentModel != "" {
		ps.models = appendUnique(ps.models, ps.currentModel)
	}
	// Restore model cursor position.
	ps.modelIdx = 0
	for i, m := range ps.models {
		if m == ps.currentModel {
			ps.modelIdx = i
			break
		}
	}
	return nil
}

// HandleFallbacks processes the loaded fallback configuration.
func (ps *ProviderSection) HandleFallbacks(msg FallbacksMsg) {
	if msg.Err != nil {
		return
	}
	for i := 0; i < maxFallbackSlots; i++ {
		slot := &ps.fallbacks[i]
		if i < len(msg.Fallbacks) {
			slot.provider = msg.Fallbacks[i].Provider
			slot.model = msg.Fallbacks[i].Model
		} else {
			slot.provider = ""
			slot.model = ""
		}
		ps.syncFallbackProviders(i)
		if slot.provider != "" {
			slot.models = modelOptionsForProvider(slot.provider, slot.model)
		} else {
			slot.models = []string{"default"}
		}
		ps.syncFallbackModelIdx(i)
	}
}

// HandleFallbacksSaved processes the save result.
func (ps *ProviderSection) HandleFallbacksSaved(msg FallbacksSavedMsg) {
	ps.fallbackSaved = msg.Err == nil
}

// HandleFallbackModels processes model list for a specific fallback slot.
func (ps *ProviderSection) HandleFallbackModels(msg FallbackModelsMsg) {
	if msg.SlotIndex < 0 || msg.SlotIndex >= maxFallbackSlots {
		return
	}
	slot := &ps.fallbacks[msg.SlotIndex]
	if msg.Provider != slot.provider || msg.Err != nil {
		return
	}
	slot.models = []string{"default"}
	for _, m := range msg.Response.Models {
		slot.models = appendUnique(slot.models, strings.TrimSpace(m))
	}
	if slot.model != "" {
		slot.models = appendUnique(slot.models, slot.model)
	}
	ps.syncFallbackModelIdx(msg.SlotIndex)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (ps *ProviderSection) Update(msg tea.KeyMsg, apiClient *client.APIClient, currentProvider string) tea.Cmd {
	// Delegate to fallback handler when a fallback slot is focused.
	if ps.focusField >= 2 {
		return ps.updateFallback(msg, apiClient)
	}

	switch {
	case key.Matches(msg, settingsKeys.Up):
		if ps.focusField == 0 {
			if ps.providerIdx > 0 {
				ps.providerIdx--
			}
		} else {
			if ps.modelIdx > 0 {
				ps.modelIdx--
			} else if len(ps.providers) > 0 {
				// Jump to last provider
				ps.focusField = 0
				ps.providerIdx = len(ps.providers) - 1
			}
		}
	case key.Matches(msg, settingsKeys.Down):
		if ps.focusField == 0 {
			if ps.providerIdx < len(ps.providers)-1 {
				ps.providerIdx++
			} else if len(ps.models) > 0 {
				// Jump to first model
				ps.focusField = 1
				ps.modelIdx = 0
			} else {
				// Skip empty models list, jump to first fallback slot
				ps.focusField = 2
				ps.fallbackField = 0
			}
		} else {
			if ps.modelIdx < len(ps.models)-1 {
				ps.modelIdx++
			} else {
				// Jump to first fallback slot
				ps.focusField = 2
				ps.fallbackField = 0
			}
		}
	case key.Matches(msg, settingsKeys.Select):
		if ps.focusField == 0 && ps.providerIdx < len(ps.providers) {
			selected := ps.providers[ps.providerIdx]
			ps.currentProvider = selected
			// Show hardcoded fallback immediately.
			ps.models = modelOptionsForProvider(selected, "")
			ps.modelIdx = 0
			defaultModel := ps.models[0] // always "default"
			ps.currentModel = defaultModel
			cmds := []tea.Cmd{
				func() tea.Msg { return ProviderChangedMsg{Provider: selected} },
				func() tea.Msg { return ModelChangedMsg{ModelID: defaultModel} },
			}
			// Trigger async model fetch for the new provider.
			if selected != "mock" {
				ps.modelsLoading = true
				cmds = append(cmds, listModelsCmd(apiClient, selected, ""))
			}
			return tea.Batch(cmds...)
		}
		if ps.focusField == 1 && ps.modelIdx < len(ps.models) {
			selected := ps.models[ps.modelIdx]
			ps.currentModel = selected
			return func() tea.Msg { return ModelChangedMsg{ModelID: selected} }
		}
	}
	return nil
}

// updateFallback handles key events when a fallback slot is focused.
func (ps *ProviderSection) updateFallback(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	slotIdx := ps.focusField - 2

	switch {
	case key.Matches(msg, settingsKeys.Back):
		// Exit fallback editing mode, return to providers list.
		ps.focusField = 0
		ps.fallbackField = 0

	case key.Matches(msg, settingsKeys.Up):
		if ps.focusField == 2 {
			// Jump back to models list (last item) or providers list.
			if len(ps.models) > 0 {
				ps.focusField = 1
				ps.modelIdx = len(ps.models) - 1
			} else if len(ps.providers) > 0 {
				ps.focusField = 0
				ps.providerIdx = len(ps.providers) - 1
			}
		} else {
			ps.focusField--
		}

	case key.Matches(msg, settingsKeys.Down):
		if ps.focusField < 2+maxFallbackSlots-1 {
			ps.focusField++
		}

	case key.Matches(msg, settingsKeys.Left):
		if ps.fallbackField > 0 {
			ps.fallbackField--
		}

	case key.Matches(msg, settingsKeys.Right):
		if ps.fallbackField < 1 {
			ps.fallbackField++
		}

	case key.Matches(msg, settingsKeys.Select):
		return ps.cycleFallbackOption(slotIdx, apiClient)

	case key.Matches(msg, settingsKeys.Delete):
		return ps.clearFallbackSlot(slotIdx, apiClient)
	}

	return nil
}

// cycleFallbackOption cycles the focused sub-field to the next available option.
func (ps *ProviderSection) cycleFallbackOption(slotIdx int, apiClient *client.APIClient) tea.Cmd {
	slot := &ps.fallbacks[slotIdx]

	if ps.fallbackField == 0 {
		// Cycle provider.
		if len(slot.providers) == 0 {
			return nil
		}
		if slot.provider == "" {
			// First selection: pick the first provider.
			slot.providerIdx = 0
		} else {
			slot.providerIdx = (slot.providerIdx + 1) % len(slot.providers)
		}
		slot.provider = slot.providers[slot.providerIdx]
		// Reset model to "default" for the new provider.
		slot.model = "default"
		slot.models = modelOptionsForProvider(slot.provider, "")
		slot.modelIdx = 0
		return ps.saveFallbacks(apiClient)
	}

	// Cycle model (only when a provider is set).
	if slot.provider == "" || len(slot.models) == 0 {
		return nil
	}
	slot.modelIdx = (slot.modelIdx + 1) % len(slot.models)
	slot.model = slot.models[slot.modelIdx]
	return ps.saveFallbacks(apiClient)
}

// clearFallbackSlot clears a fallback slot and persists the change.
func (ps *ProviderSection) clearFallbackSlot(slotIdx int, apiClient *client.APIClient) tea.Cmd {
	slot := &ps.fallbacks[slotIdx]
	if slot.provider == "" {
		return nil // already empty
	}
	slot.provider = ""
	slot.model = ""
	slot.providerIdx = 0
	slot.modelIdx = 0
	slot.models = []string{"default"}
	return ps.saveFallbacks(apiClient)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (ps ProviderSection) View(width int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Provider / Model"))
	lines = append(lines, "")

	// Provider list
	providerHeader := "Providers"
	if ps.focusField == 0 {
		providerHeader = "› Providers"
	}
	lines = append(lines, theme.SectionStyle.Render(providerHeader))

	if !ps.loaded {
		lines = append(lines, theme.HintStyle.Render("  載入中..."))
	} else {
		for i, p := range ps.providers {
			prefix := "  "
			style := theme.NormalStyle
			if i == ps.providerIdx {
				if ps.focusField == 0 {
					prefix = "› "
					style = theme.SelectedStyle
				} else {
					prefix = "• "
				}
			}
			label := p
			if p == ps.currentProvider {
				label += " ✓"
			}
			lines = append(lines, style.Render(clampLine(prefix+label, textW)))
		}
	}

	lines = append(lines, "")

	// Model list (scroll-windowed to prevent layout overflow)
	modelHeader := "Models"
	if ps.modelsLoading {
		modelHeader += " (載入中...)"
	}
	if ps.focusField == 1 {
		modelHeader = "› " + modelHeader
	}
	lines = append(lines, theme.SectionStyle.Render(modelHeader))

	const maxVisibleModels = 6
	modelStart, modelEnd := scrollWindow(ps.modelIdx, len(ps.models), maxVisibleModels)

	if modelStart > 0 {
		lines = append(lines, theme.HintStyle.Render(fmt.Sprintf("  ↑ 還有 %d 個…", modelStart)))
	}
	for i := modelStart; i < modelEnd; i++ {
		m := ps.models[i]
		prefix := "  "
		style := theme.NormalStyle
		if i == ps.modelIdx {
			if ps.focusField == 1 {
				prefix = "› "
				style = theme.SelectedStyle
			} else {
				prefix = "• "
			}
		}
		label := m
		if m == ps.currentModel {
			label += " ✓"
		}
		lines = append(lines, style.Render(clampLine(prefix+label, textW)))
	}
	if modelEnd < len(ps.models) {
		lines = append(lines, theme.HintStyle.Render(fmt.Sprintf("  ↓ 還有 %d 個…", len(ps.models)-modelEnd)))
	}

	lines = append(lines, "")

	// Fallback section
	fbHeader := "Fallback 設定"
	if ps.fallbackSaved {
		fbHeader += " ✓"
	}
	lines = append(lines, theme.SectionStyle.Render(fbHeader))

	for i := 0; i < maxFallbackSlots; i++ {
		slot := &ps.fallbacks[i]
		isFocused := ps.focusField == i+2

		providerLabel := slot.provider
		if providerLabel == "" {
			providerLabel = "(未設定)"
		}
		modelLabel := slot.model
		if modelLabel == "" || slot.provider == "" {
			modelLabel = "—"
		}

		if isFocused {
			// Highlight the active sub-field with brackets.
			if ps.fallbackField == 0 {
				providerLabel = "[" + providerLabel + "]"
			} else {
				modelLabel = "[" + modelLabel + "]"
			}
			line := fmt.Sprintf("› #%d  %s / %s", i+1, providerLabel, modelLabel)
			lines = append(lines, theme.SelectedStyle.Render(clampLine(line, textW)))
		} else {
			line := fmt.Sprintf("  #%d  %s / %s", i+1, providerLabel, modelLabel)
			lines = append(lines, theme.NormalStyle.Render(clampLine(line, textW)))
		}
	}

	lines = append(lines, "")

	// Context-sensitive help text
	if ps.focusField >= 2 {
		lines = append(lines, theme.HintStyle.Render("←→ 切換欄位  ·  Enter 切換選項  ·  d 清除  ·  Esc 返回"))
	} else {
		lines = append(lines, theme.HintStyle.Render("↑↓ 選擇  ·  Enter 確認  ·  Esc 返回"))
	}

	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Fallback helpers
// ---------------------------------------------------------------------------

// syncFallbackProviders copies the main providers list into a fallback slot
// and updates the provider index to match the slot's current selection.
func (ps *ProviderSection) syncFallbackProviders(slotIdx int) {
	slot := &ps.fallbacks[slotIdx]
	slot.providers = ps.providers
	slot.providerIdx = 0
	for i, p := range slot.providers {
		if p == slot.provider {
			slot.providerIdx = i
			break
		}
	}
}

// syncFallbackModelIdx updates the model cursor to match the slot's current model.
func (ps *ProviderSection) syncFallbackModelIdx(slotIdx int) {
	slot := &ps.fallbacks[slotIdx]
	slot.modelIdx = 0
	for i, m := range slot.models {
		if m == slot.model {
			slot.modelIdx = i
			break
		}
	}
}

// collectFallbackEntries gathers non-empty fallback slots into a slice.
func (ps *ProviderSection) collectFallbackEntries() []core.FallbackEntry {
	var entries []core.FallbackEntry
	for i := 0; i < maxFallbackSlots; i++ {
		if ps.fallbacks[i].provider != "" {
			model := ps.fallbacks[i].model
			if model == "" {
				model = "default"
			}
			entries = append(entries, core.FallbackEntry{
				Provider: ps.fallbacks[i].provider,
				Model:    model,
			})
		}
	}
	return entries
}

// saveFallbacks triggers an async save of the current fallback configuration.
func (ps *ProviderSection) saveFallbacks(apiClient *client.APIClient) tea.Cmd {
	ps.fallbackSaved = false
	return saveFallbacksCmd(apiClient, ps.collectFallbackEntries())
}

// scrollWindow returns the visible [start, end) range for a list,
// keeping the cursor centered in a window of maxVisible items.
func scrollWindow(cursor, total, maxVisible int) (start, end int) {
	if total <= maxVisible {
		return 0, total
	}
	half := maxVisible / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
	}
	return start, end
}
