package ccswitch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type settingsStore struct {
	path string
	raw  map[string]any
}

func loadSettingsStore(path string) (*settingsStore, error) {
	store := &settingsStore{
		path: path,
		raw:  map[string]any{},
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, fmt.Errorf("读取 settings.json 失败: %w", err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(content, &store.raw); err != nil {
		return nil, fmt.Errorf("解析 settings.json 失败: %w", err)
	}
	return store, nil
}

func (s *settingsStore) getString(key string) string {
	if s == nil {
		return ""
	}
	if value, ok := s.raw[key]; ok {
		return stringValue(value)
	}
	return ""
}

func (s *settingsStore) setString(key, value string) {
	if s.raw == nil {
		s.raw = map[string]any{}
	}
	if strings.TrimSpace(value) == "" {
		delete(s.raw, key)
		return
	}
	s.raw[key] = value
}

func (s *settingsStore) save() error {
	return writeJSONAtomic(s.path, s.raw)
}

func currentProviderKey(app AppType) string {
	switch app {
	case AppClaude:
		return "currentProviderClaude"
	case AppCodex:
		return "currentProviderCodex"
	case AppPi:
		return "currentProviderPi"
	default:
		return ""
	}
}

func configDirKey(app AppType) string {
	switch app {
	case AppClaude:
		return "claudeConfigDir"
	case AppCodex:
		return "codexConfigDir"
	case AppPi:
		return "piConfigDir"
	default:
		return ""
	}
}
