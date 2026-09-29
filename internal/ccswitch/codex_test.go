package ccswitch

import (
	"strings"
	"testing"
)

func TestNormalizeCodexWireAPI(t *testing.T) {
	cases := map[string]string{
		"":                   "responses",
		"responses":          "responses",
		"response":           "responses",
		"  Responses  ":      "responses",
		"chat":               "responses",
		"Chat":               "responses",
		"completions":        "responses",
		"chat_completions":   "responses",
		"openai-completions": "responses",
	}
	for input, want := range cases {
		if got := normalizeCodexWireAPI(input); got != want {
			t.Errorf("normalizeCodexWireAPI(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPatchCodexConfigUpgradesLegacyChat(t *testing.T) {
	existing := `model = 'gpt-test'
model_provider = 'custom'

[model_providers.custom]
name = 'custom'
base_url = 'https://chat.example.com/v1'
wire_api = 'chat'
requires_openai_auth = true
`
	// 用户未显式选择 Wire API 时，历史遗留的 chat 也必须升级为 responses
	out, err := patchCodexConfig(existing, ProviderInput{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "wire_api = 'chat'") || strings.Contains(out, `wire_api = "chat"`) {
		t.Fatalf("legacy chat not upgraded:\n%s", out)
	}
	if !strings.Contains(out, "wire_api = 'responses'") && !strings.Contains(out, `wire_api = "responses"`) {
		t.Fatalf("missing responses wire_api:\n%s", out)
	}
}

func TestExtractCodexConfigInputNormalizesChat(t *testing.T) {
	config := `model_provider = 'custom'

[model_providers.custom]
base_url = 'https://chat.example.com/v1'
wire_api = 'chat'
`
	input := extractCodexConfigInput(config)
	if input.WireAPI != "responses" {
		t.Fatalf("WireAPI = %q, want responses", input.WireAPI)
	}
	if input.BaseURL != "https://chat.example.com/v1" {
		t.Fatalf("BaseURL = %q", input.BaseURL)
	}
}

func TestPatchCodexConfigDropsResponseStorageFlag(t *testing.T) {
	// 新建配置不应再写该字段
	fresh, err := patchCodexConfig("", ProviderInput{
		BaseURL: "https://gw.example.com/v1",
		Model:   "gpt-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fresh, "disable_response_storage") {
		t.Fatalf("fresh config should not contain disable_response_storage:\n%s", fresh)
	}

	// 存量配置里的历史字段应被清除
	existing := `disable_response_storage = true
model = 'gpt-test'
model_provider = 'custom'

[model_providers.custom]
base_url = 'https://gw.example.com/v1'
wire_api = 'responses'
`
	out, err := patchCodexConfig(existing, ProviderInput{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "disable_response_storage") {
		t.Fatalf("legacy field not purged:\n%s", out)
	}
	if !strings.Contains(out, "wire_api = 'responses'") {
		t.Fatalf("wire_api lost:\n%s", out)
	}
}
