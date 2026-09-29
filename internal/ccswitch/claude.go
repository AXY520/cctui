package ccswitch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) readClaudeLive() (map[string]any, error) {
	path := s.claudeSettingsPath()
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("读取 Claude live 配置失败: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil, fmt.Errorf("解析 Claude live 配置失败: %w", err)
	}
	return settings, nil
}

func (s *Store) claudeSettingsPath() string {
	dir := s.configDirFor(AppClaude)
	settingsPath := filepath.Join(dir, "settings.json")
	if fileExists(settingsPath) {
		return settingsPath
	}
	legacyPath := filepath.Join(dir, "claude.json")
	if fileExists(legacyPath) {
		return legacyPath
	}
	return settingsPath
}
