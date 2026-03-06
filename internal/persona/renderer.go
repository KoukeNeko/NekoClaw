package persona

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"
)

// templateData holds all variables available inside a persona's system_template.
type templateData struct {
	Lore    string
	Anchors string
	Time    string
	Memory  string
}

// RenderSystemPrompt executes the persona's Go template with the provided
// memory content and returns the final system prompt string.
// When providerID is non-empty, a matching ProviderPatches entry (if any) is
// appended to reinforce provider-specific behaviour (e.g. stricter roleplay).
func RenderSystemPrompt(p *Persona, memoryContent string, providerID string) (string, error) {
	if strings.TrimSpace(p.Config.SystemTemplate) == "" {
		return "", nil
	}

	tmpl, err := template.New("system").Parse(p.Config.SystemTemplate)
	if err != nil {
		return "", fmt.Errorf("parse persona template %s: %w", p.DirName, err)
	}

	data := templateData{
		Lore:    p.Lore,
		Anchors: FormatAnchors(p.Anchors),
		Time:    time.Now().Format("2006-01-02 15:04 MST"),
		Memory:  memoryContent,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render persona template %s: %w", p.DirName, err)
	}

	rendered := buf.String()

	// Append provider-specific patch to reinforce roleplay compliance.
	if providerID != "" {
		if patch := strings.TrimSpace(p.Config.ProviderPatches[providerID]); patch != "" {
			rendered = rendered + "\n\n" + patch
		}
	}

	return rendered, nil
}

// FormatAnchors converts a slice of Anchors into readable few-shot text.
func FormatAnchors(anchors []Anchor) string {
	if len(anchors) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, a := range anchors {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("### Example %d: %s\n", i+1, a.ID))
		if len(a.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(a.Tags, ", ")))
		}
		sb.WriteString(fmt.Sprintf("**User:** %s\n", strings.TrimSpace(a.UserInput)))
		sb.WriteString(fmt.Sprintf("**Response:**\n%s", strings.TrimSpace(a.BotResponse)))
	}
	return sb.String()
}
