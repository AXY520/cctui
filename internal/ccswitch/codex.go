package ccswitch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

var (
	baseURLRe         = regexp.MustCompile(`(?m)base_url\s*=\s*["']([^"']+)["']`)
	modelRe           = regexp.MustCompile(`(?m)^model\s*=\s*["']([^"']+)["']`)
	reasoningEffortRe = regexp.MustCompile(`(?m)^model_reasoning_effort\s*=\s*["']([^"']+)["']`)
)

func (s *Store) readCodexLive() (map[string]any, error) {
	authExists := fileExists(s.codexAuthPath())
	configExists := fileExists(s.codexConfigPath())
	if !authExists && !configExists {
		return nil, os.ErrNotExist
	}

	auth := map[string]any{}
	if authExists {
		content, err := os.ReadFile(s.codexAuthPath())
		if err != nil {
			return nil, fmt.Errorf("读取 Codex auth.json 失败: %w", err)
		}
		if len(strings.TrimSpace(string(content))) > 0 {
			if err := json.Unmarshal(content, &auth); err != nil {
				return nil, fmt.Errorf("解析 Codex auth.json 失败: %w", err)
			}
		}
	}

	config := ""
	if configExists {
		content, err := os.ReadFile(s.codexConfigPath())
		if err != nil {
			return nil, fmt.Errorf("读取 Codex config.toml 失败: %w", err)
		}
		config = string(content)
	}

	return map[string]any{
		"auth":   auth,
		"config": config,
	}, nil
}

func (s *Store) codexAuthPath() string {
	return filepath.Join(s.configDirFor(AppCodex), "auth.json")
}

func (s *Store) codexConfigPath() string {
	return filepath.Join(s.configDirFor(AppCodex), "config.toml")
}

func patchCodexConfig(existing string, input ProviderInput) (string, error) {
	trimmed := strings.TrimSpace(existing)
	wireAPI := normalizeCodexWireAPI(input.WireAPI)

	if trimmed == "" {
		if strings.TrimSpace(input.BaseURL) == "" &&
			strings.TrimSpace(input.Model) == "" &&
			strings.TrimSpace(input.ReasoningEffort) == "" &&
			strings.TrimSpace(input.ReasoningSummary) == "" &&
			strings.TrimSpace(input.ModelVerbosity) == "" &&
			strings.TrimSpace(input.ServiceTier) == "" &&
			input.ContextWindow <= 0 {
			return "", nil
		}

		doc := map[string]any{}
		applyCodexTopLevelFields(doc, input)
		if strings.TrimSpace(input.BaseURL) != "" {
			doc["model_provider"] = "custom"
			doc["model_providers"] = map[string]any{
				"custom": map[string]any{
					"name":                 "custom",
					"base_url":             strings.TrimSpace(input.BaseURL),
					"wire_api":             wireAPI,
					"requires_openai_auth": true,
				},
			}
		}

		buf, err := toml.Marshal(doc)
		if err != nil {
			return "", fmt.Errorf("生成 Codex config.toml 失败: %w", err)
		}
		return strings.TrimSpace(string(buf)), nil
	}

	doc := map[string]any{}
	if err := toml.Unmarshal([]byte(existing), &doc); err != nil {
		return "", fmt.Errorf("解析 Codex config.toml 失败: %w", err)
	}

	// Codex 0.158+ 已移除该字段（启动警告 ignored），顺手清掉历史遗留
	delete(doc, "disable_response_storage")

	applyCodexTopLevelFields(doc, input)

	if providerKey, ok := doc["model_provider"].(string); ok && strings.TrimSpace(providerKey) != "" {
		modelProviders, ok := doc["model_providers"].(map[string]any)
		if !ok || modelProviders == nil {
			modelProviders = map[string]any{}
			doc["model_providers"] = modelProviders
		}
		providerTable, ok := modelProviders[providerKey].(map[string]any)
		if !ok || providerTable == nil {
			providerTable = map[string]any{}
			modelProviders[providerKey] = providerTable
		}
		patchGenericMapString(providerTable, "base_url", input.BaseURL)
		if strings.TrimSpace(input.BaseURL) != "" {
			if _, ok := providerTable["name"]; !ok {
				providerTable["name"] = providerKey
			}
			if _, ok := providerTable["requires_openai_auth"]; !ok {
				providerTable["requires_openai_auth"] = true
			}
		}
		// 用户显式选择时覆盖；否则保留原值，但 chat 已被 Codex 移除，统一升级为 responses
		if strings.TrimSpace(input.WireAPI) != "" {
			providerTable["wire_api"] = wireAPI
		} else {
			providerTable["wire_api"] = normalizeCodexWireAPI(stringValue(providerTable["wire_api"]))
		}
	} else if strings.TrimSpace(input.BaseURL) != "" {
		doc["model_provider"] = "custom"
		modelProviders, _ := doc["model_providers"].(map[string]any)
		if modelProviders == nil {
			modelProviders = map[string]any{}
			doc["model_providers"] = modelProviders
		}
		providerTable, _ := modelProviders["custom"].(map[string]any)
		if providerTable == nil {
			providerTable = map[string]any{}
			modelProviders["custom"] = providerTable
		}
		providerTable["name"] = "custom"
		providerTable["base_url"] = strings.TrimSpace(input.BaseURL)
		providerTable["wire_api"] = wireAPI
		providerTable["requires_openai_auth"] = true
	} else {
		patchGenericMapString(doc, "base_url", input.BaseURL)
	}

	buf, err := toml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("写回 Codex config.toml 失败: %w", err)
	}

	return strings.TrimSpace(string(buf)), nil
}

func applyCodexTopLevelFields(doc map[string]any, input ProviderInput) {
	patchGenericMapString(doc, "model", input.Model)
	patchGenericMapString(doc, "model_reasoning_effort", input.ReasoningEffort)
	patchGenericMapString(doc, "model_reasoning_summary", input.ReasoningSummary)
	patchGenericMapString(doc, "model_verbosity", input.ModelVerbosity)
	patchGenericMapString(doc, "service_tier", input.ServiceTier)
	if input.ContextWindow > 0 {
		doc["model_context_window"] = input.ContextWindow
	} else {
		delete(doc, "model_context_window")
	}
}

func normalizeCodexWireAPI(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "responses", "response":
		return "responses"
	case "chat", "completions", "chat_completions", "chat-completions", "openai-completions":
		// Codex 已彻底移除 Chat Completions 支持（2026-02 起硬错误），
		// 历史遗留的 chat 统一升级为 responses，否则 Codex 直接拒绝启动。
		// 见 https://github.com/openai/codex/discussions/7782
		return "responses"
	default:
		return strings.TrimSpace(value)
	}
}

func extractCodexConfigInput(configText string) ProviderInput {
	input := ProviderInput{}
	if strings.TrimSpace(configText) == "" {
		return input
	}
	doc := map[string]any{}
	if err := toml.Unmarshal([]byte(configText), &doc); err != nil {
		// 回退正则，兼容脏配置
		input.BaseURL = extractFirstMatch(baseURLRe, configText)
		input.Model = extractFirstMatch(modelRe, configText)
		input.ReasoningEffort = extractFirstMatch(reasoningEffortRe, configText)
		return input
	}
	input.Model = stringValue(doc["model"])
	input.ReasoningEffort = stringValue(doc["model_reasoning_effort"])
	input.ReasoningSummary = stringValue(doc["model_reasoning_summary"])
	input.ModelVerbosity = stringValue(doc["model_verbosity"])
	input.ServiceTier = stringValue(doc["service_tier"])
	input.ContextWindow = intValue(doc["model_context_window"])

	if providerKey := strings.TrimSpace(stringValue(doc["model_provider"])); providerKey != "" {
		if modelProviders, ok := doc["model_providers"].(map[string]any); ok {
			if providerTable, ok := modelProviders[providerKey].(map[string]any); ok && providerTable != nil {
				input.BaseURL = stringValue(providerTable["base_url"])
				input.WireAPI = normalizeCodexWireAPI(stringValue(providerTable["wire_api"]))
			}
		}
	}
	if input.BaseURL == "" {
		input.BaseURL = extractFirstMatch(baseURLRe, configText)
	}
	if input.WireAPI == "" {
		input.WireAPI = "responses"
	}
	return input
}

func writeCodexLiveAtomic(authPath, configPath string, auth map[string]any, config string) error {
	oldAuth, _ := os.ReadFile(authPath)
	authExisted := fileExists(authPath)

	if err := writeJSONAtomic(authPath, auth); err != nil {
		return fmt.Errorf("写入 Codex auth.json 失败: %w", err)
	}
	if err := writeTextAtomic(configPath, config); err != nil {
		if authExisted {
			_ = writeBytesAtomic(authPath, oldAuth)
		} else {
			_ = os.Remove(authPath)
		}
		return fmt.Errorf("写入 Codex config.toml 失败: %w", err)
	}
	return nil
}
