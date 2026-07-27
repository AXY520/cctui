package ccswitch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPiProviderWriteAndSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CC_SWITCH_TEST_HOME", home)

	store, err := OpenStore()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	created, auto, err := store.AddProvider(AppPi, ProviderInput{
		Name:    "Local Gateway",
		BaseURL: "https://gateway.example.com/v1",
		APIKey:  "sk-test",
		Model:   "demo-model",
		APIType: "openai-completions",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if auto {
		// first provider may auto switch; either way fine
	}
	if created == nil || created.ID == "" {
		t.Fatal("empty created")
	}

	// force switch to ensure writePiLive path
	if err := store.SwitchProvider(AppPi, created.ID); err != nil {
		t.Fatalf("switch: %v", err)
	}

	modelsPath := filepath.Join(home, ".pi", "agent", "models.json")
	authPath := filepath.Join(home, ".pi", "agent", "auth.json")
	settingsPath := filepath.Join(home, ".pi", "agent", "settings.json")
	for _, p := range []string{modelsPath, authPath, settingsPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}

	modelsRaw, _ := os.ReadFile(modelsPath)
	var modelsDoc map[string]any
	if err := json.Unmarshal(modelsRaw, &modelsDoc); err != nil {
		t.Fatal(err)
	}
	providers := modelsDoc["providers"].(map[string]any)
	entry := providers["local-gateway"].(map[string]any)
	if entry["baseUrl"] != "https://gateway.example.com/v1" {
		t.Fatalf("baseUrl=%v", entry["baseUrl"])
	}
	if entry["api"] != "openai-completions" {
		t.Fatalf("api=%v", entry["api"])
	}

	authRaw, _ := os.ReadFile(authPath)
	var authDoc map[string]any
	_ = json.Unmarshal(authRaw, &authDoc)
	cred := authDoc["local-gateway"].(map[string]any)
	if cred["type"] != "api_key" || cred["key"] != "sk-test" {
		t.Fatalf("auth=%v", cred)
	}

	settingsRaw, _ := os.ReadFile(settingsPath)
	var settingsDoc map[string]any
	_ = json.Unmarshal(settingsRaw, &settingsDoc)
	if settingsDoc["defaultProvider"] != "local-gateway" {
		t.Fatalf("defaultProvider=%v", settingsDoc["defaultProvider"])
	}
	if settingsDoc["defaultModel"] != "demo-model" {
		t.Fatalf("defaultModel=%v", settingsDoc["defaultModel"])
	}
}

func TestGlobalFanOutIncludesPi(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CC_SWITCH_TEST_HOME", home)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.AddGlobalProvider(ProviderInput{
		Name:    "统一网关",
		BaseURL: "https://gw.example.com/v1",
		APIKey:  "sk-g",
		Model:   "m1",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range AllAppTypes {
		list, err := store.ListProviders(app)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 {
			t.Fatalf("%s want 1 got %d", app, len(list))
		}
	}
}
