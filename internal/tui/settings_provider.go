package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

// ProviderSection handles provider and model selection.
type ProviderSection struct {
	providers       []string
	models          []string
	providerIdx     int
	modelIdx        int
	focusField      int // 0 = providers, 1 = models
	currentProvider string
	currentModel    string
	loaded          bool
	modelsLoading   bool
}

func NewProviderSection(provider, model string) ProviderSection {
	return ProviderSection{
		currentProvider: provider,
		currentModel:    model,
	}
}

func (ps *ProviderSection) SetProvider(p string) { ps.currentProvider = p }
func (ps *ProviderSection) SetModel(m string)    { ps.currentModel = m }

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

func (ps *ProviderSection) Update(msg tea.KeyMsg, apiClient *client.APIClient, currentProvider string) tea.Cmd {
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
			}
		} else {
			if ps.modelIdx < len(ps.models)-1 {
				ps.modelIdx++
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

	// Model list
	modelHeader := "Models"
	if ps.modelsLoading {
		modelHeader += " (載入中...)"
	}
	if ps.focusField == 1 {
		modelHeader = "› " + modelHeader
	}
	lines = append(lines, theme.SectionStyle.Render(modelHeader))

	for i, m := range ps.models {
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

	lines = append(lines, "")
	lines = append(lines, theme.HintStyle.Render("↑↓ 選擇  ·  Enter 確認  ·  Esc 返回"))

	return strings.Join(lines, "\n")
}
