package ccswitch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeReasoningAndHaikuModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CC_SWITCH_TEST_HOME", home)

	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	created, _, err := store.AddProvider(AppClaude, ProviderInput{
		Name:           "Gateway",
		BaseURL:        "https://gw.example.com",
		APIKey:         "sk-claude",
		Model:          "deepseek-v4",
		ReasoningModel: "gpt-5.4",
		HaikuModel:     "deepseek-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SwitchProvider(AppClaude, created.ID); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	env := doc["env"].(map[string]any)
	if env["ANTHROPIC_MODEL"] != "deepseek-v4" {
		t.Fatalf("model=%v", env["ANTHROPIC_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "deepseek-v4" {
		t.Fatalf("sonnet=%v", env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "deepseek-flash" {
		t.Fatalf("haiku=%v", env["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	}
	if env["ANTHROPIC_SMALL_FAST_MODEL"] != "deepseek-flash" {
		t.Fatalf("small=%v", env["ANTHROPIC_SMALL_FAST_MODEL"])
	}
	if env["ANTHROPIC_REASONING_MODEL"] != "gpt-5.4" {
		t.Fatalf("reasoning=%v", env["ANTHROPIC_REASONING_MODEL"])
	}

	got := store.ExtractInput(AppClaude, *created)
	if got.Model != "deepseek-v4" || got.HaikuModel != "deepseek-flash" || got.ReasoningModel != "gpt-5.4" {
		t.Fatalf("extract=%+v", got)
	}
}

func TestCodexExtendedFieldsAndWireAPIChat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CC_SWITCH_TEST_HOME", home)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	created, _, err := store.AddProvider(AppCodex, ProviderInput{
		Name:             "Chat Gateway",
		BaseURL:          "https://chat.example.com/v1",
		APIKey:           "sk-codex",
		Model:            "gpt-test",
		ReasoningEffort:  "high",
		ReasoningSummary: "concise",
		ModelVerbosity:   "medium",
		ServiceTier:      "flex",
		WireAPI:          "chat",
		ContextWindow:    200000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SwitchProvider(AppCodex, created.ID); err != nil {
		t.Fatal(err)
	}

	cfg, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(cfg)
	for _, want := range []string{
		`model = 'gpt-test'`,
		`model_reasoning_effort = 'high'`,
		`model_reasoning_summary = 'concise'`,
		`model_verbosity = 'medium'`,
		`service_tier = 'flex'`,
		`model_context_window = 200000`,
		`wire_api = 'chat'`,
		`base_url = 'https://chat.example.com/v1'`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}

	got := store.ExtractInput(AppCodex, *created)
	if got.WireAPI != "chat" {
		t.Fatalf("wire=%q", got.WireAPI)
	}
	if got.ContextWindow != 200000 || got.ModelVerbosity != "medium" || got.ServiceTier != "flex" {
		t.Fatalf("extract=%+v", got)
	}
	if got.ReasoningSummary != "concise" || got.ReasoningEffort != "high" {
		t.Fatalf("reasoning fields=%+v", got)
	}
}

func TestPiMaxTokensReasoningAndCompat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CC_SWITCH_TEST_HOME", home)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	created, _, err := store.AddProvider(AppPi, ProviderInput{
		Name:                     "Compat GW",
		BaseURL:                  "https://pi.example.com/v1",
		APIKey:                   "sk-pi",
		Model:                    "m1",
		APIType:                  "openai-completions",
		ContextWindow:            256000,
		MaxTokens:                8192,
		Reasoning:                "false",
		MaxTokensField:           "max_tokens",
		SupportsDeveloperRole:    "false",
		SupportsReasoningEffort:  "false",
		SupportsUsageInStreaming: "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SwitchProvider(AppPi, created.ID); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	entry := doc["providers"].(map[string]any)["compat-gw"].(map[string]any)
	model := entry["models"].([]any)[0].(map[string]any)
	if int(model["contextWindow"].(float64)) != 256000 {
		t.Fatalf("ctx=%v", model["contextWindow"])
	}
	if int(model["maxTokens"].(float64)) != 8192 {
		t.Fatalf("maxTokens=%v", model["maxTokens"])
	}
	if model["reasoning"] != false {
		t.Fatalf("reasoning=%v", model["reasoning"])
	}
	compat := entry["compat"].(map[string]any)
	if compat["maxTokensField"] != "max_tokens" {
		t.Fatalf("maxTokensField=%v", compat["maxTokensField"])
	}
	if compat["supportsDeveloperRole"] != false || compat["supportsReasoningEffort"] != false {
		t.Fatalf("compat=%v", compat)
	}
	if compat["supportsUsageInStreaming"] != true {
		t.Fatalf("usage stream=%v", compat["supportsUsageInStreaming"])
	}

	got := store.ExtractInput(AppPi, *created)
	if got.MaxTokens != 8192 || got.Reasoning != "false" || got.MaxTokensField != "max_tokens" {
		t.Fatalf("extract=%+v", got)
	}
	if got.SupportsDeveloperRole != "false" || got.SupportsUsageInStreaming != "true" {
		t.Fatalf("compat extract=%+v", got)
	}
}

func TestNormalizeCodexWireAPI(t *testing.T) {
	if normalizeCodexWireAPI("") != "responses" {
		t.Fatal("default")
	}
	if normalizeCodexWireAPI("chat") != "chat" {
		t.Fatal("chat")
	}
	if normalizeCodexWireAPI("openai-completions") != "chat" {
		t.Fatal("alias")
	}
}
