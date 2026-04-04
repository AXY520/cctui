package ccswitch

import (
	"encoding/json"
	"strings"
)

type AppType string

const (
	AppClaude AppType = "claude"
	AppCodex  AppType = "codex"
	AppGemini AppType = "gemini"
)

var AllAppTypes = []AppType{AppClaude, AppCodex, AppGemini}

func (a AppType) String() string {
	return string(a)
}

func (a AppType) DisplayName() string {
	switch a {
	case AppClaude:
		return "Claude"
	case AppCodex:
		return "Codex"
	case AppGemini:
		return "Gemini"
	default:
		return strings.Title(string(a))
	}
}

type Provider struct {
	ID              string
	Name            string
	SettingsConfig  map[string]any
	WebsiteURL      *string
	Category        *string
	CreatedAt       *int64
	SortIndex       *int64
	Notes           *string
	Meta            map[string]any
	Icon            *string
	IconColor       *string
	InFailoverQueue bool
}

func (p Provider) Clone() Provider {
	clone := p
	clone.SettingsConfig = CloneMap(p.SettingsConfig)
	clone.Meta = CloneMap(p.Meta)
	return clone
}

type ProviderInput struct {
	Name            string
	BaseURL         string
	APIKey          string
	Model           string
	ReasoningEffort string
	Website         string
	Notes           string
}

type Snapshot struct {
	Providers map[AppType][]Provider
	Current   map[AppType]string
}

func CloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}

	buf, err := json.Marshal(input)
	if err != nil {
		return map[string]any{}
	}

	var out map[string]any
	if err := json.Unmarshal(buf, &out); err != nil {
		return map[string]any{}
	}

	return out
}
