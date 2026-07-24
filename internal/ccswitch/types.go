package ccswitch

import (
	"encoding/json"
	"strings"
)

type AppType string

const (
	AppGlobal AppType = "global"
	AppClaude AppType = "claude"
	AppCodex  AppType = "codex"
	AppGemini AppType = "gemini"
)

// AllAppTypes 是会读写 live 配置的应用。
var AllAppTypes = []AppType{AppClaude, AppCodex, AppGemini}

// UIAppTypes 是 TUI 列表展示顺序，全局供应商置顶。
var UIAppTypes = []AppType{AppGlobal, AppClaude, AppCodex, AppGemini}

func (a AppType) String() string {
	return string(a)
}

func (a AppType) DisplayName() string {
	switch a {
	case AppGlobal:
		return "全局"
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

// IsLiveApp 表示该类型会读写本地 CLI live 配置。
func (a AppType) IsLiveApp() bool {
	switch a {
	case AppClaude, AppCodex, AppGemini:
		return true
	default:
		return false
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
