package ccswitch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func (s *Store) importPiLiveProviders() (bool, error) {
	providersDoc, err := s.readPiModelsFile()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	providersMap := getOrCreateMap(providersDoc, "providers")
	if len(providersMap) == 0 {
		return false, nil
	}

	authDoc, _ := s.readPiAuthFile()
	settingsDoc, _ := s.readPiSettingsFile()
	defaultProvider := stringValue(settingsDoc["defaultProvider"])
	defaultModel := stringValue(settingsDoc["defaultModel"])

	var (
		importedAny bool
		currentID   string
		index       int64
	)
	now := time.Now().UnixMilli()
	existing, err := s.ListProviders(AppPi)
	if err != nil {
		return false, err
	}

	names := make([]string, 0, len(providersMap))
	for name := range providersMap {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, providerID := range names {
		raw := providersMap[providerID]
		entry, ok := raw.(map[string]any)
		if !ok || entry == nil {
			continue
		}
		cfg := buildPiSettingsConfig(providerID, entry, authDoc, defaultModel)
		id := uniqueProviderID(providerID, existing, AppPi)
		sortIndex := index
		provider := Provider{
			ID:             id,
			Name:           providerID,
			SettingsConfig: cfg,
			CreatedAt:      &now,
			SortIndex:      &sortIndex,
			Meta:           map[string]any{"piProviderId": providerID},
		}
		if err := s.saveProviderRow(AppPi, provider); err != nil {
			return importedAny, err
		}
		existing = append(existing, provider)
		importedAny = true
		if currentID == "" || providerID == defaultProvider {
			currentID = id
		}
		index++
	}

	if currentID != "" {
		if err := s.setCurrentProvider(AppPi, currentID); err != nil {
			return importedAny, err
		}
	}
	return importedAny, nil
}

func (s *Store) piAgentDir() string {
	return s.configDirFor(AppPi)
}

func (s *Store) piModelsPath() string {
	return filepath.Join(s.piAgentDir(), "models.json")
}

func (s *Store) piAuthPath() string {
	return filepath.Join(s.piAgentDir(), "auth.json")
}

func (s *Store) piSettingsPath() string {
	return filepath.Join(s.piAgentDir(), "settings.json")
}

func (s *Store) writePiLive(provider Provider) error {
	cfg := provider.SettingsConfig
	providerID := strings.TrimSpace(stringValue(cfg["providerId"]))
	if providerID == "" {
		providerID = slugify(provider.Name)
	}
	if providerID == "" {
		return fmt.Errorf("Pi providerId 不能为空")
	}

	baseURL := strings.TrimSpace(stringValue(cfg["baseUrl"]))
	apiType := firstNonEmpty(strings.TrimSpace(stringValue(cfg["api"])), "openai-completions")
	apiKey := strings.TrimSpace(stringValue(cfg["apiKey"]))
	modelID := strings.TrimSpace(stringValue(cfg["model"]))

	modelsDoc, err := s.readPiModelsFile()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if modelsDoc == nil {
		modelsDoc = map[string]any{}
	}
	providersMap := getOrCreateMap(modelsDoc, "providers")

	entry := map[string]any{
		"baseUrl": baseURL,
		"api":     apiType,
	}
	if apiKey != "" {
		entry["apiKey"] = apiKey
	}
	if rawModels, ok := cfg["models"].([]any); ok && len(rawModels) > 0 {
		entry["models"] = rawModels
	} else if modelID != "" {
		modelMap := map[string]any{
			"id":    modelID,
			"name":  modelID,
			"input": []any{"text"},
		}
		applyPiModelFields(
			modelMap,
			modelID,
			normalizePiContextWindow(intValue(cfg["contextWindow"])),
			intValue(cfg["maxTokens"]),
			parseBoolDefault(stringValue(cfg["reasoning"]), true),
		)
		entry["models"] = []any{modelMap}
	} else {
		entry["models"] = []any{}
	}
	if compat, ok := cfg["compat"]; ok {
		entry["compat"] = compat
	}
	if headers, ok := cfg["headers"]; ok {
		entry["headers"] = headers
	}
	providersMap[providerID] = entry
	modelsDoc["providers"] = providersMap
	if err := writeJSONAtomic(s.piModelsPath(), modelsDoc); err != nil {
		return fmt.Errorf("写入 Pi models.json 失败: %w", err)
	}

	authDoc, err := s.readPiAuthFile()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if authDoc == nil {
		authDoc = map[string]any{}
	}
	if apiKey != "" {
		authDoc[providerID] = map[string]any{
			"type": "api_key",
			"key":  apiKey,
		}
	}
	if err := writeJSONFileMode(s.piAuthPath(), authDoc, 0o600); err != nil {
		return fmt.Errorf("写入 Pi auth.json 失败: %w", err)
	}

	settingsDoc, err := s.readPiSettingsFile()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if settingsDoc == nil {
		settingsDoc = map[string]any{}
	}
	settingsDoc["defaultProvider"] = providerID
	if modelID != "" {
		settingsDoc["defaultModel"] = modelID
	}
	if err := writeJSONAtomic(s.piSettingsPath(), settingsDoc); err != nil {
		return fmt.Errorf("写入 Pi settings.json 失败: %w", err)
	}
	return nil
}

func (s *Store) readPiProviderLive(existing Provider) (map[string]any, error) {
	providerID := strings.TrimSpace(stringValue(existing.SettingsConfig["providerId"]))
	if providerID == "" {
		providerID = slugify(existing.Name)
	}
	modelsDoc, err := s.readPiModelsFile()
	if err != nil {
		return nil, err
	}
	providersMap := getOrCreateMap(modelsDoc, "providers")
	entry, _ := providersMap[providerID].(map[string]any)
	if entry == nil {
		return nil, os.ErrNotExist
	}
	authDoc, _ := s.readPiAuthFile()
	settingsDoc, _ := s.readPiSettingsFile()
	modelID := stringValue(existing.SettingsConfig["model"])
	if settingsDoc != nil && stringValue(settingsDoc["defaultProvider"]) == providerID {
		if def := stringValue(settingsDoc["defaultModel"]); def != "" {
			modelID = def
		}
	}
	return buildPiSettingsConfig(providerID, entry, authDoc, modelID), nil
}

func (s *Store) readPiModelsFile() (map[string]any, error) {
	return readJSONFileMap(s.piModelsPath())
}

func (s *Store) readPiAuthFile() (map[string]any, error) {
	return readJSONFileMap(s.piAuthPath())
}

func (s *Store) readPiSettingsFile() (map[string]any, error) {
	return readJSONFileMap(s.piSettingsPath())
}

func (s *Store) readPiLive() (map[string]any, error) {
	settingsDoc, err := s.readPiSettingsFile()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	modelsDoc, err := s.readPiModelsFile()
	if err != nil {
		return nil, err
	}
	providersMap := getOrCreateMap(modelsDoc, "providers")
	if len(providersMap) == 0 {
		return nil, os.ErrNotExist
	}
	providerID := ""
	if settingsDoc != nil {
		providerID = stringValue(settingsDoc["defaultProvider"])
	}
	if providerID == "" || providersMap[providerID] == nil {
		names := make([]string, 0, len(providersMap))
		for name := range providersMap {
			names = append(names, name)
		}
		slices.Sort(names)
		if len(names) == 0 {
			return nil, os.ErrNotExist
		}
		providerID = names[0]
	}
	entry, _ := providersMap[providerID].(map[string]any)
	if entry == nil {
		return nil, os.ErrNotExist
	}
	authDoc, _ := s.readPiAuthFile()
	defaultModel := ""
	if settingsDoc != nil {
		defaultModel = stringValue(settingsDoc["defaultModel"])
	}
	return buildPiSettingsConfig(providerID, entry, authDoc, defaultModel), nil
}

func buildPiSettingsConfig(providerID string, entry map[string]any, authDoc map[string]any, defaultModel string) map[string]any {
	cfg := map[string]any{
		"providerId": providerID,
		"baseUrl":    stringValue(entry["baseUrl"]),
		"api":        firstNonEmpty(stringValue(entry["api"]), "openai-completions"),
		"apiKey":     stringValue(entry["apiKey"]),
		"models":     entry["models"],
	}
	if compat, ok := entry["compat"]; ok {
		cfg["compat"] = compat
	}
	if headers, ok := entry["headers"]; ok {
		cfg["headers"] = headers
	}
	if authDoc != nil {
		if raw, ok := authDoc[providerID]; ok {
			if authMap, ok := raw.(map[string]any); ok {
				if key := stringValue(authMap["key"]); key != "" {
					cfg["apiKey"] = key
				}
			}
		}
	}
	modelID := strings.TrimSpace(defaultModel)
	if modelID == "" {
		modelID = firstPiModelID(cfg)
	}
	cfg["model"] = modelID
	return cfg
}

func piAuthKeyFromSettings(settings map[string]any) string {
	raw, ok := settings["auth"]
	if !ok {
		return ""
	}
	authMap, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return stringValue(authMap["key"])
}

func extractPiInput(provider Provider) ProviderInput {
	modelID := firstNonEmpty(stringValue(provider.SettingsConfig["model"]), firstPiModelID(provider.SettingsConfig))
	input := ProviderInput{
		Name:          provider.Name,
		BaseURL:       stringValue(provider.SettingsConfig["baseUrl"]),
		APIKey:        firstNonEmpty(stringValue(provider.SettingsConfig["apiKey"]), piAuthKeyFromSettings(provider.SettingsConfig)),
		Model:         modelID,
		APIType:       firstNonEmpty(stringValue(provider.SettingsConfig["api"]), "openai-completions"),
		ContextWindow: piContextWindowFromSettings(provider.SettingsConfig, modelID),
		MaxTokens:     piMaxTokensFromSettings(provider.SettingsConfig, modelID),
		Reasoning:     piReasoningFromSettings(provider.SettingsConfig, modelID),
		Website:       deref(provider.WebsiteURL),
		Notes:         deref(provider.Notes),
	}
	compat := map[string]any{}
	if raw, ok := provider.SettingsConfig["compat"].(map[string]any); ok && raw != nil {
		compat = raw
	}
	input.MaxTokensField = stringValue(compat["maxTokensField"])
	input.SupportsDeveloperRole = triStateFromAny(compat["supportsDeveloperRole"])
	input.SupportsReasoningEffort = triStateFromAny(compat["supportsReasoningEffort"])
	input.SupportsUsageInStreaming = triStateFromAny(compat["supportsUsageInStreaming"])
	return input
}

func applyPiModelFields(modelMap map[string]any, modelID string, contextWindow, maxTokens int, reasoning bool) {
	if modelMap == nil {
		return
	}
	modelMap["id"] = modelID
	if stringValue(modelMap["name"]) == "" {
		modelMap["name"] = modelID
	}
	modelMap["contextWindow"] = contextWindow
	modelMap["reasoning"] = reasoning
	if maxTokens > 0 {
		modelMap["maxTokens"] = maxTokens
	} else {
		delete(modelMap, "maxTokens")
	}
	if _, ok := modelMap["input"]; !ok {
		modelMap["input"] = []any{"text"}
	}
}

func buildPiCompat(existing map[string]any, input ProviderInput) map[string]any {
	compat := map[string]any{}
	if raw, ok := existing["compat"].(map[string]any); ok && raw != nil {
		compat = CloneMap(raw)
	}
	setTriStateCompat(compat, "maxTokensField", strings.TrimSpace(input.MaxTokensField), false)
	setTriStateCompat(compat, "supportsDeveloperRole", strings.TrimSpace(input.SupportsDeveloperRole), true)
	setTriStateCompat(compat, "supportsReasoningEffort", strings.TrimSpace(input.SupportsReasoningEffort), true)
	setTriStateCompat(compat, "supportsUsageInStreaming", strings.TrimSpace(input.SupportsUsageInStreaming), true)
	if len(compat) == 0 {
		return nil
	}
	return compat
}

func setTriStateCompat(compat map[string]any, key, value string, asBool bool) {
	if compat == nil {
		return
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		delete(compat, key)
		return
	}
	if !asBool {
		compat[key] = trimmed
		return
	}
	switch strings.ToLower(trimmed) {
	case "true", "1", "yes", "on":
		compat[key] = true
	case "false", "0", "no", "off":
		compat[key] = false
	default:
		compat[key] = trimmed
	}
}

func piMaxTokensFromSettings(settings map[string]any, modelID string) int {
	if value := intValue(settings["maxTokens"]); value > 0 {
		return value
	}
	if model := piModelMap(settings, modelID); model != nil {
		return intValue(model["maxTokens"])
	}
	return 0
}

func piReasoningFromSettings(settings map[string]any, modelID string) string {
	if value := strings.TrimSpace(stringValue(settings["reasoning"])); value != "" {
		return boolString(parseBoolDefault(value, true))
	}
	if model := piModelMap(settings, modelID); model != nil {
		if raw, ok := model["reasoning"]; ok {
			return boolString(parseBoolDefault(fmt.Sprint(raw), true))
		}
	}
	return "true"
}

func piModelMap(settings map[string]any, modelID string) map[string]any {
	raw, ok := settings["models"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	modelID = strings.TrimSpace(modelID)
	for _, item := range list {
		modelMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if modelID == "" || stringValue(modelMap["id"]) == modelID {
			return modelMap
		}
	}
	return nil
}

func firstPiModelID(settings map[string]any) string {
	raw, ok := settings["models"]
	if !ok {
		return ""
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	if modelMap, ok := list[0].(map[string]any); ok {
		return stringValue(modelMap["id"])
	}
	return ""
}

const defaultPiContextWindow = 128000

func normalizePiContextWindow(value int) int {
	if value <= 0 {
		return defaultPiContextWindow
	}
	return value
}

func piContextWindowFromSettings(settings map[string]any, modelID string) int {
	if value := intValue(settings["contextWindow"]); value > 0 {
		return value
	}
	raw, ok := settings["models"]
	if !ok {
		return defaultPiContextWindow
	}
	list, ok := raw.([]any)
	if !ok {
		return defaultPiContextWindow
	}
	modelID = strings.TrimSpace(modelID)
	for _, item := range list {
		modelMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if modelID == "" || stringValue(modelMap["id"]) == modelID {
			if value := intValue(modelMap["contextWindow"]); value > 0 {
				return value
			}
			if modelID != "" {
				break
			}
		}
	}
	return defaultPiContextWindow
}
