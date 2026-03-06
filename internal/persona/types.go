package persona

// PersonaMeta holds identifying metadata for a persona.
type PersonaMeta struct {
	ID          string `yaml:"id"          json:"id"`
	Name        string `yaml:"name"        json:"name"`
	Description string `yaml:"description" json:"description"`
	Version     string `yaml:"version"     json:"version"`
	Author      string `yaml:"author"      json:"author"`
}

// GenerationParams holds optional sampling parameters that override provider defaults.
// Pointer fields allow distinguishing "not set" from zero values.
type GenerationParams struct {
	Temperature      *float64 `yaml:"temperature,omitempty"       json:"temperature,omitempty"`
	TopP             *float64 `yaml:"top_p,omitempty"             json:"top_p,omitempty"`
	FrequencyPenalty *float64 `yaml:"frequency_penalty,omitempty" json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `yaml:"presence_penalty,omitempty"  json:"presence_penalty,omitempty"`
}

// IsZero returns true when no generation parameter is set.
func (g GenerationParams) IsZero() bool {
	return g.Temperature == nil && g.TopP == nil &&
		g.FrequencyPenalty == nil && g.PresencePenalty == nil
}

// PersonaConfig represents the contents of a persona's config.yaml.
type PersonaConfig struct {
	Meta            PersonaMeta       `yaml:"meta"`
	SystemTemplate  string            `yaml:"system_template"`
	Generation      GenerationParams  `yaml:"generation"`
	Variables       map[string]any    `yaml:"variables,omitempty"`
	ProviderPatches map[string]string `yaml:"provider_patches,omitempty"`
}

// Anchor is a single few-shot example (user input → bot response).
type Anchor struct {
	ID          string   `yaml:"id"`
	Tags        []string `yaml:"tags"`
	UserInput   string   `yaml:"user_input"`
	BotResponse string   `yaml:"bot_response"`
}

// AnchorsFile represents the contents of a persona's anchors.yaml.
type AnchorsFile struct {
	Anchors []Anchor `yaml:"anchors"`
}

// Persona is a fully loaded persona with all resources.
type Persona struct {
	Config  PersonaConfig
	Anchors []Anchor
	Lore    string
	DirName string // folder name, used as a stable key
}

// PersonaInfo is a lightweight summary for API and TUI display.
type PersonaInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DirName     string `json:"dir_name"`
}

// Info converts a full Persona to a lightweight PersonaInfo.
func (p *Persona) Info() PersonaInfo {
	return PersonaInfo{
		ID:          p.Config.Meta.ID,
		Name:        p.Config.Meta.Name,
		Description: p.Config.Meta.Description,
		DirName:     p.DirName,
	}
}
