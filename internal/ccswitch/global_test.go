package ccswitch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlobalProviderFanOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CC_SWITCH_TEST_HOME", home)
	// store uses homeDir() which reads HOME / user.Current; ensure dirs exist
	if err := os.MkdirAll(filepath.Join(home, ".cc-switch"), 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	input := ProviderInput{
		Name:            "统一中转",
		BaseURL:         "https://gateway.example.com/v1",
		APIKey:          "sk-test-key",
		Model:           "gpt-test",
		ReasoningEffort: "high",
		Website:         "https://example.com",
		Notes:           "global fanout",
	}

	global, err := store.AddGlobalProvider(input)
	if err != nil {
		t.Fatalf("add global: %v", err)
	}
	if global.ID == "" {
		t.Fatal("empty global id")
	}

	for _, app := range AllAppTypes {
		providers, err := store.ListProviders(app)
		if err != nil {
			t.Fatalf("list %s: %v", app, err)
		}
		if len(providers) != 1 {
			t.Fatalf("%s providers=%d want 1", app, len(providers))
		}
		child := providers[0]
		if stringValue(child.Meta["global_id"]) != global.ID {
			t.Fatalf("%s missing global_id meta: %#v", app, child.Meta)
		}
		got := store.ExtractInput(app, child)
		if got.Name != input.Name {
			t.Fatalf("%s name=%q", app, got.Name)
		}
		if got.APIKey != input.APIKey {
			t.Fatalf("%s apiKey mismatch", app)
		}
		if got.BaseURL == "" {
			t.Fatalf("%s empty base url", app)
		}
		// 全局 fan-out 不应自动切换
		current, err := store.GetEffectiveCurrentProvider(app)
		if err != nil {
			t.Fatal(err)
		}
		if current != "" {
			t.Fatalf("%s auto switched to %s", app, current)
		}
	}

	// update global should sync children
	input.Model = "gpt-test-2"
	input.BaseURL = "https://gateway.example.com/v2"
	updated, err := store.UpdateGlobalProvider(*global, input)
	if err != nil {
		t.Fatalf("update global: %v", err)
	}
	if updated.Name != input.Name {
		t.Fatal("update name")
	}
	for _, app := range AllAppTypes {
		child, err := store.findLinkedProvider(app, global.ID)
		if err != nil || child == nil {
			t.Fatalf("linked %s missing: %v", app, err)
		}
		got := store.ExtractInput(app, *child)
		if got.Model != "gpt-test-2" && app != AppCodex {
			// codex model extracted from config text
		}
		if got.Model != "gpt-test-2" {
			t.Fatalf("%s model=%q want gpt-test-2", app, got.Model)
		}
	}
}
