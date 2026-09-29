package ccswitch

import (
	"encoding/json"
)

type AppType string

const (
	AppGlobal AppType = "global"
	AppClaude AppType = "claude"
	AppCodex  AppType = "codex"
	AppPi     AppType = "pi"
)

// AllAppTypes 是会读写 live 配置的应用。
var AllAppTypes = []AppType{AppClaude, AppCodex, AppPi}

// UIAppTypes 是 TUI 列表展示顺序，全局供应商置顶。
var UIAppTypes = []AppType{AppGlobal, AppClaude, AppCodex, AppPi}

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
	case AppPi:
		return "Pi"
	default:
		return string(a)
	}
}

// IsLiveApp 表示该类型会读写本地 CLI live 配置。
func (a AppType) IsLiveApp() bool {
	switch a {
	case AppClaude, AppCodex, AppPi:
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

// ProviderInput 是表单/同步用的供应商字段并集。
// 各 CLI 只会消费自己认识的字段；全局 fan-out 时按目标应用各取所需。
type ProviderInput struct {
	Name    string
	BaseURL string
	APIKey  string
	Model   string

	// Claude
	ReasoningModel string // ANTHROPIC_REASONING_MODEL
	HaikuModel     string // ANTHROPIC_DEFAULT_HAIKU_MODEL（SMALL_FAST 已废弃仅作读取兜底）；空则跟 Model

	// Codex
	ReasoningEffort  string // model_reasoning_effort
	ReasoningSummary string // model_reasoning_summary
	ModelVerbosity   string // model_verbosity
	ServiceTier      string // service_tier
	WireAPI          string // model_providers.*.wire_api；Codex 已移除 chat，仅支持 responses

	// Pi
	APIType                  string // openai-completions / openai-responses / anthropic-messages / google-generative-ai
	MaxTokens                int    // models[].maxTokens；0 表示不写
	Reasoning                string // models[].reasoning: true|false；空默认 true
	MaxTokensField           string // compat.maxTokensField
	SupportsDeveloperRole    string // compat.supportsDeveloperRole: true|false|""
	SupportsReasoningEffort  string // compat.supportsReasoningEffort
	SupportsUsageInStreaming string // compat.supportsUsageInStreaming

	// Codex + Pi
	ContextWindow int // Codex: model_context_window；Pi: models[].contextWindow；0 对 Pi 表示默认 128000，对 Codex 表示不写

	Website string
	Notes   string
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
