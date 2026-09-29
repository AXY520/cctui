package ccswitch

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

type Store struct {
	db       *sql.DB
	settings *settingsStore
}

func OpenStore() (*Store, error) {
	appDir := filepath.Join(homeDir(), ".cc-switch")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}

	settings, err := loadSettingsStore(filepath.Join(appDir, "settings.json"))
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", filepath.Join(appDir, "cc-switch.db"))
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	store := &Store{db: db, settings: settings}
	if err := store.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Bootstrap() ([]string, error) {
	var warnings []string

	for _, app := range AllAppTypes {
		providers, err := s.ListProviders(app)
		if err != nil {
			return warnings, err
		}
		if len(providers) > 0 {
			continue
		}

		imported, err := s.importCurrentLive(app)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s 导入失败: %v", app.DisplayName(), err))
			continue
		}
		if imported {
			warnings = append(warnings, fmt.Sprintf("已导入 %s 当前 live 配置", app.DisplayName()))
		}
	}

	return warnings, nil
}

func (s *Store) Snapshot() (*Snapshot, error) {
	out := &Snapshot{
		Providers: make(map[AppType][]Provider, len(UIAppTypes)),
		Current:   make(map[AppType]string, len(AllAppTypes)),
	}

	for _, app := range UIAppTypes {
		providers, err := s.ListProviders(app)
		if err != nil {
			return nil, err
		}
		out.Providers[app] = providers

		if !app.IsLiveApp() {
			continue
		}
		current, err := s.GetEffectiveCurrentProvider(app)
		if err != nil {
			return nil, err
		}
		out.Current[app] = current
	}

	return out, nil
}

func (s *Store) ListProviders(app AppType) ([]Provider, error) {
	rows, err := s.db.Query(`
		SELECT id, name, settings_config, website_url, category, created_at, sort_index, notes, icon, icon_color, meta, in_failover_queue
		FROM providers
		WHERE app_type = ?
		ORDER BY COALESCE(sort_index, 999999), created_at ASC, id ASC
	`, app.String())
	if err != nil {
		return nil, fmt.Errorf("读取 %s 供应商失败: %w", app.DisplayName(), err)
	}
	defer rows.Close()

	var providers []Provider
	for rows.Next() {
		var (
			id, name               string
			settingsJSON, metaJSON string
			websiteURL, category   sql.NullString
			createdAt, sortIndex   sql.NullInt64
			notes, icon, iconColor sql.NullString
			inFailoverQueue        bool
			settingsConfig         map[string]any
			meta                   map[string]any
		)

		if err := rows.Scan(
			&id,
			&name,
			&settingsJSON,
			&websiteURL,
			&category,
			&createdAt,
			&sortIndex,
			&notes,
			&icon,
			&iconColor,
			&metaJSON,
			&inFailoverQueue,
		); err != nil {
			return nil, fmt.Errorf("扫描 %s 供应商失败: %w", app.DisplayName(), err)
		}

		if err := json.Unmarshal([]byte(settingsJSON), &settingsConfig); err != nil {
			settingsConfig = map[string]any{}
		}
		if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
			meta = map[string]any{}
		}

		provider := Provider{
			ID:              id,
			Name:            name,
			SettingsConfig:  settingsConfig,
			WebsiteURL:      nullStringPtr(websiteURL),
			Category:        nullStringPtr(category),
			CreatedAt:       nullInt64Ptr(createdAt),
			SortIndex:       nullInt64Ptr(sortIndex),
			Notes:           nullStringPtr(notes),
			Meta:            meta,
			Icon:            nullStringPtr(icon),
			IconColor:       nullStringPtr(iconColor),
			InFailoverQueue: inFailoverQueue,
		}
		providers = append(providers, provider)
	}

	return providers, rows.Err()
}

func (s *Store) GetProvider(app AppType, id string) (*Provider, error) {
	var (
		name, settingsJSON, metaJSON string
		websiteURL, category         sql.NullString
		createdAt, sortIndex         sql.NullInt64
		notes, icon, iconColor       sql.NullString
		inFailoverQueue              bool
		settingsConfig               map[string]any
		meta                         map[string]any
	)

	err := s.db.QueryRow(`
		SELECT name, settings_config, website_url, category, created_at, sort_index, notes, icon, icon_color, meta, in_failover_queue
		FROM providers
		WHERE id = ? AND app_type = ?
	`, id, app.String()).Scan(
		&name,
		&settingsJSON,
		&websiteURL,
		&category,
		&createdAt,
		&sortIndex,
		&notes,
		&icon,
		&iconColor,
		&metaJSON,
		&inFailoverQueue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取供应商失败: %w", err)
	}

	if err := json.Unmarshal([]byte(settingsJSON), &settingsConfig); err != nil {
		settingsConfig = map[string]any{}
	}
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		meta = map[string]any{}
	}

	return &Provider{
		ID:              id,
		Name:            name,
		SettingsConfig:  settingsConfig,
		WebsiteURL:      nullStringPtr(websiteURL),
		Category:        nullStringPtr(category),
		CreatedAt:       nullInt64Ptr(createdAt),
		SortIndex:       nullInt64Ptr(sortIndex),
		Notes:           nullStringPtr(notes),
		Meta:            meta,
		Icon:            nullStringPtr(icon),
		IconColor:       nullStringPtr(iconColor),
		InFailoverQueue: inFailoverQueue,
	}, nil
}

func (s *Store) AddProvider(app AppType, input ProviderInput) (*Provider, bool, error) {
	if app == AppGlobal {
		provider, err := s.AddGlobalProvider(input)
		return provider, false, err
	}
	return s.addProvider(app, input, true, nil)
}

// AddGlobalProvider 新增全局供应商，并同步复制到 Claude/Codex/Pi。
// 同步出来的 CLI 供应商不会自动切换，需用户在对应分组中手动启用。
func (s *Store) AddGlobalProvider(input ProviderInput) (*Provider, error) {
	global, _, err := s.addProvider(AppGlobal, input, false, nil)
	if err != nil {
		return nil, err
	}

	linked := map[string]string{}
	for _, app := range AllAppTypes {
		meta := map[string]any{
			"global_id":   global.ID,
			"from_global": true,
		}
		child, _, err := s.addProvider(app, input, false, meta)
		if err != nil {
			return nil, fmt.Errorf("同步到 %s 失败: %w", app.DisplayName(), err)
		}
		linked[app.String()] = child.ID
	}

	if global.Meta == nil {
		global.Meta = map[string]any{}
	}
	global.Meta["linked"] = linked
	global.Meta["from_global"] = true
	if err := s.saveProviderRow(AppGlobal, *global); err != nil {
		return nil, err
	}
	return global, nil
}

func (s *Store) addProvider(app AppType, input ProviderInput, autoSwitch bool, meta map[string]any) (*Provider, bool, error) {
	providers, err := s.ListProviders(app)
	if err != nil {
		return nil, false, err
	}

	id := uniqueProviderID(input.Name, providers, app)
	provider, err := s.buildProvider(app, nil, input, id)
	if err != nil {
		return nil, false, err
	}

	now := time.Now().UnixMilli()
	sortIndex := nextSortIndex(providers)
	provider.CreatedAt = &now
	provider.SortIndex = &sortIndex
	if meta != nil {
		if provider.Meta == nil {
			provider.Meta = map[string]any{}
		}
		for key, value := range meta {
			provider.Meta[key] = value
		}
	}

	if err := s.saveProviderRow(app, provider); err != nil {
		return nil, false, err
	}

	autoSwitched := false
	if autoSwitch && app.IsLiveApp() {
		current, err := s.GetEffectiveCurrentProvider(app)
		if err != nil {
			return nil, false, err
		}
		if current == "" {
			if err := s.writeLiveSettings(app, provider); err != nil {
				return nil, false, err
			}
			if err := s.setCurrentProvider(app, provider.ID); err != nil {
				return nil, false, err
			}
			autoSwitched = true
		}
	}

	return &provider, autoSwitched, nil
}

func (s *Store) UpdateProvider(app AppType, existing Provider, input ProviderInput) (*Provider, error) {
	if app == AppGlobal {
		return s.UpdateGlobalProvider(existing, input)
	}
	return s.updateProvider(app, existing, input)
}

// UpdateGlobalProvider 更新全局供应商，并同步到各 CLI 中由它生成的副本。
func (s *Store) UpdateGlobalProvider(existing Provider, input ProviderInput) (*Provider, error) {
	global, err := s.updateProvider(AppGlobal, existing, input)
	if err != nil {
		return nil, err
	}

	linked := map[string]string{}
	for _, app := range AllAppTypes {
		child, err := s.findLinkedProvider(app, existing.ID)
		if err != nil {
			return nil, err
		}
		if child == nil {
			meta := map[string]any{
				"global_id":   existing.ID,
				"from_global": true,
			}
			created, _, err := s.addProvider(app, input, false, meta)
			if err != nil {
				return nil, fmt.Errorf("补齐同步到 %s 失败: %w", app.DisplayName(), err)
			}
			linked[app.String()] = created.ID
			continue
		}

		// 保留关联元数据
		if child.Meta == nil {
			child.Meta = map[string]any{}
		}
		child.Meta["global_id"] = existing.ID
		child.Meta["from_global"] = true
		updated, err := s.updateProvider(app, *child, input)
		if err != nil {
			return nil, fmt.Errorf("同步更新 %s 失败: %w", app.DisplayName(), err)
		}
		linked[app.String()] = updated.ID
	}

	if global.Meta == nil {
		global.Meta = map[string]any{}
	}
	global.Meta["linked"] = linked
	global.Meta["from_global"] = true
	if err := s.saveProviderRow(AppGlobal, *global); err != nil {
		return nil, err
	}
	return global, nil
}

func (s *Store) updateProvider(app AppType, existing Provider, input ProviderInput) (*Provider, error) {
	provider, err := s.buildProvider(app, &existing, input, existing.ID)
	if err != nil {
		return nil, err
	}
	provider.CreatedAt = existing.CreatedAt
	provider.SortIndex = existing.SortIndex
	provider.InFailoverQueue = existing.InFailoverQueue
	provider.Icon = existing.Icon
	provider.IconColor = existing.IconColor
	provider.Category = existing.Category
	provider.Meta = CloneMap(existing.Meta)

	if err := s.saveProviderRow(app, provider); err != nil {
		return nil, err
	}

	if app.IsLiveApp() {
		current, err := s.GetEffectiveCurrentProvider(app)
		if err != nil {
			return nil, err
		}
		if current == existing.ID {
			if err := s.writeLiveSettings(app, provider); err != nil {
				return nil, err
			}
		}
	}

	return &provider, nil
}

func (s *Store) DeleteProvider(app AppType, id string) error {
	if app == AppGlobal {
		return s.DeleteGlobalProvider(id)
	}
	return s.deleteProvider(app, id)
}

// DeleteGlobalProvider 删除全局供应商，并尽量删除各 CLI 中的关联副本。
func (s *Store) DeleteGlobalProvider(id string) error {
	var blocked []string
	for _, app := range AllAppTypes {
		child, err := s.findLinkedProvider(app, id)
		if err != nil {
			return err
		}
		if child == nil {
			continue
		}
		if err := s.deleteProvider(app, child.ID); err != nil {
			blocked = append(blocked, fmt.Sprintf("%s(%s): %v", app.DisplayName(), child.Name, err))
		}
	}

	if err := s.deleteProvider(AppGlobal, id); err != nil {
		return err
	}
	if len(blocked) > 0 {
		return fmt.Errorf("全局供应商已删除，但部分 CLI 副本未删: %s", strings.Join(blocked, "; "))
	}
	return nil
}

func (s *Store) deleteProvider(app AppType, id string) error {
	providers, err := s.ListProviders(app)
	if err != nil {
		return err
	}

	current := ""
	if app.IsLiveApp() {
		current, err = s.GetEffectiveCurrentProvider(app)
		if err != nil {
			return err
		}
		if current == id {
			if len(providers) > 1 {
				return fmt.Errorf("不能删除当前正在使用的供应商，请先切换到其他供应商")
			}
		}
	}

	if _, err := s.db.Exec(`DELETE FROM providers WHERE id = ? AND app_type = ?`, id, app.String()); err != nil {
		return fmt.Errorf("删除供应商失败: %w", err)
	}

	if app.IsLiveApp() && current == id {
		s.settings.setString(currentProviderKey(app), "")
		if err := s.settings.save(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) findLinkedProvider(app AppType, globalID string) (*Provider, error) {
	providers, err := s.ListProviders(app)
	if err != nil {
		return nil, err
	}
	for _, provider := range providers {
		if stringValue(provider.Meta["global_id"]) == globalID {
			copyProvider := provider
			return &copyProvider, nil
		}
	}
	return nil, nil
}

func (s *Store) SwitchProvider(app AppType, id string) error {
	if !app.IsLiveApp() {
		return fmt.Errorf("全局供应商不能直接切换，请到 Claude/Codex/Pi 分组中选择")
	}
	target, err := s.GetProvider(app, id)
	if err != nil {
		return err
	}
	if target == nil {
		return fmt.Errorf("供应商不存在")
	}

	current, err := s.GetEffectiveCurrentProvider(app)
	if err != nil {
		return err
	}
	if current == id {
		return nil
	}

	if current != "" {
		if existing, err := s.GetProvider(app, current); err == nil && existing != nil {
			if app == AppPi {
				if liveSettings, err := s.readPiProviderLive(*existing); err == nil {
					existing.SettingsConfig = liveSettings
					_ = s.saveProviderRow(app, *existing)
				}
			} else if liveSettings, err := s.readLiveSettings(app); err == nil {
				existing.SettingsConfig = liveSettings
				_ = s.saveProviderRow(app, *existing)
			}
		}
	}

	if err := s.writeLiveSettings(app, *target); err != nil {
		return err
	}
	if err := s.setCurrentProvider(app, id); err != nil {
		return err
	}

	return nil
}

func (s *Store) GetEffectiveCurrentProvider(app AppType) (string, error) {
	localKey := currentProviderKey(app)
	if local := s.settings.getString(localKey); local != "" {
		exists, err := s.providerExists(app, local)
		if err != nil {
			return "", err
		}
		if exists {
			return local, nil
		}
		s.settings.setString(localKey, "")
		if err := s.settings.save(); err != nil {
			return "", err
		}
	}

	var current sql.NullString
	err := s.db.QueryRow(`
		SELECT id FROM providers WHERE app_type = ? AND is_current = 1 LIMIT 1
	`, app.String()).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取当前供应商失败: %w", err)
	}
	if current.Valid {
		return current.String, nil
	}
	return "", nil
}

func (s *Store) ExtractInput(app AppType, provider Provider) ProviderInput {
	switch app {
	case AppGlobal:
		settings := provider.SettingsConfig
		return ProviderInput{
			Name:                     provider.Name,
			BaseURL:                  stringValue(settings["base_url"]),
			APIKey:                   stringValue(settings["api_key"]),
			Model:                    stringValue(settings["model"]),
			ReasoningModel:           stringValue(settings["reasoning_model"]),
			HaikuModel:               stringValue(settings["haiku_model"]),
			ReasoningEffort:          stringValue(settings["reasoning_effort"]),
			ReasoningSummary:         stringValue(settings["reasoning_summary"]),
			ModelVerbosity:           stringValue(settings["model_verbosity"]),
			ServiceTier:              stringValue(settings["service_tier"]),
			WireAPI:                  normalizeCodexWireAPI(stringValue(settings["wire_api"])),
			APIType:                  stringValue(settings["api_type"]),
			ContextWindow:            intValue(settings["context_window"]),
			MaxTokens:                intValue(settings["max_tokens"]),
			Reasoning:                stringValue(settings["reasoning"]),
			MaxTokensField:           stringValue(settings["max_tokens_field"]),
			SupportsDeveloperRole:    stringValue(settings["supports_developer_role"]),
			SupportsReasoningEffort:  stringValue(settings["supports_reasoning_effort"]),
			SupportsUsageInStreaming: stringValue(settings["supports_usage_in_streaming"]),
			Website:                  deref(provider.WebsiteURL),
			Notes:                    deref(provider.Notes),
		}
	case AppClaude:
		env := getOrCreateMap(provider.SettingsConfig, "env")
		apiKey := stringValue(env["ANTHROPIC_AUTH_TOKEN"])
		if apiKey == "" {
			apiKey = stringValue(env["ANTHROPIC_API_KEY"])
		}
		model := firstNonEmpty(
			stringValue(env["ANTHROPIC_MODEL"]),
			stringValue(env["ANTHROPIC_DEFAULT_SONNET_MODEL"]),
			stringValue(env["ANTHROPIC_DEFAULT_OPUS_MODEL"]),
			stringValue(env["ANTHROPIC_DEFAULT_HAIKU_MODEL"]),
		)
		haiku := firstNonEmpty(stringValue(env["ANTHROPIC_DEFAULT_HAIKU_MODEL"]), stringValue(env["ANTHROPIC_SMALL_FAST_MODEL"]))
		if haiku == model {
			haiku = ""
		}
		return ProviderInput{
			Name:           provider.Name,
			BaseURL:        stringValue(env["ANTHROPIC_BASE_URL"]),
			APIKey:         apiKey,
			Model:          model,
			ReasoningModel: stringValue(env["ANTHROPIC_REASONING_MODEL"]),
			HaikuModel:     haiku,
			Website:        deref(provider.WebsiteURL),
			Notes:          deref(provider.Notes),
		}
	case AppCodex:
		auth := getOrCreateMap(provider.SettingsConfig, "auth")
		configText := stringValue(provider.SettingsConfig["config"])
		input := extractCodexConfigInput(configText)
		input.Name = provider.Name
		input.APIKey = stringValue(auth["OPENAI_API_KEY"])
		input.Website = deref(provider.WebsiteURL)
		input.Notes = deref(provider.Notes)
		return input
	case AppPi:
		return extractPiInput(provider)
	default:
		return ProviderInput{Name: provider.Name}
	}
}

func (s *Store) EndpointSummary(app AppType, provider Provider) string {
	switch app {
	case AppGlobal:
		baseURL := strings.TrimSpace(stringValue(provider.SettingsConfig["base_url"]))
		if baseURL == "" {
			return "未设置 Base URL"
		}
		return summarizeURL(baseURL)
	case AppClaude:
		env := getOrCreateMap(provider.SettingsConfig, "env")
		baseURL := strings.TrimSpace(stringValue(env["ANTHROPIC_BASE_URL"]))
		if baseURL == "" {
			return "官方登录"
		}
		return summarizeURL(baseURL)
	case AppCodex:
		configText := stringValue(provider.SettingsConfig["config"])
		baseURL := strings.TrimSpace(extractFirstMatch(baseURLRe, configText))
		if baseURL == "" {
			return "官方登录"
		}
		return summarizeURL(baseURL)
	case AppPi:
		baseURL := strings.TrimSpace(stringValue(provider.SettingsConfig["baseUrl"]))
		if baseURL == "" {
			return "未设置 Base URL"
		}
		return summarizeURL(baseURL)
	default:
		return "-"
	}
}

func (s *Store) ensureSchema() error {
	stmts := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS providers (
			id TEXT NOT NULL,
			app_type TEXT NOT NULL,
			name TEXT NOT NULL,
			settings_config TEXT NOT NULL,
			website_url TEXT,
			category TEXT,
			created_at INTEGER,
			sort_index INTEGER,
			notes TEXT,
			icon TEXT,
			icon_color TEXT,
			meta TEXT NOT NULL DEFAULT '{}',
			is_current BOOLEAN NOT NULL DEFAULT 0,
			in_failover_queue BOOLEAN NOT NULL DEFAULT 0,
			PRIMARY KEY (id, app_type)
		)`,
		`CREATE TABLE IF NOT EXISTS provider_endpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id TEXT NOT NULL,
			app_type TEXT NOT NULL,
			url TEXT NOT NULL,
			added_at INTEGER,
			FOREIGN KEY (provider_id, app_type) REFERENCES providers(id, app_type) ON DELETE CASCADE
		)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("初始化数据库失败: %w", err)
		}
	}

	return nil
}

func (s *Store) importCurrentLive(app AppType) (bool, error) {
	if app == AppPi {
		return s.importPiLiveProviders()
	}

	live, err := s.readLiveSettings(app)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	id := uniqueProviderID("imported-"+app.DisplayName(), nil, app)
	now := time.Now().UnixMilli()
	sortIndex := int64(0)
	provider := Provider{
		ID:             id,
		Name:           "Imported " + app.DisplayName(),
		SettingsConfig: live,
		CreatedAt:      &now,
		SortIndex:      &sortIndex,
		Meta:           map[string]any{},
	}

	if err := s.saveProviderRow(app, provider); err != nil {
		return false, err
	}
	if err := s.setCurrentProvider(app, provider.ID); err != nil {
		return false, err
	}

	return true, nil
}

func (s *Store) saveProviderRow(app AppType, provider Provider) error {
	if strings.TrimSpace(provider.Name) == "" {
		return fmt.Errorf("供应商名称不能为空")
	}
	if provider.SettingsConfig == nil {
		provider.SettingsConfig = map[string]any{}
	}
	if provider.Meta == nil {
		provider.Meta = map[string]any{}
	}

	settingsJSON, err := json.Marshal(provider.SettingsConfig)
	if err != nil {
		return fmt.Errorf("序列化 settingsConfig 失败: %w", err)
	}
	metaJSON, err := json.Marshal(provider.Meta)
	if err != nil {
		return fmt.Errorf("序列化 meta 失败: %w", err)
	}

	isCurrent := false
	var existingCurrent, existingInFailover bool
	err = s.db.QueryRow(`
		SELECT is_current, in_failover_queue
		FROM providers
		WHERE id = ? AND app_type = ?
	`, provider.ID, app.String()).Scan(&existingCurrent, &existingInFailover)
	if err == nil {
		isCurrent = existingCurrent
		provider.InFailoverQueue = existingInFailover
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("读取现有供应商状态失败: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO providers (
			id, app_type, name, settings_config, website_url, category, created_at, sort_index, notes, icon, icon_color, meta, is_current, in_failover_queue
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, app_type) DO UPDATE SET
			name = excluded.name,
			settings_config = excluded.settings_config,
			website_url = excluded.website_url,
			category = excluded.category,
			created_at = excluded.created_at,
			sort_index = excluded.sort_index,
			notes = excluded.notes,
			icon = excluded.icon,
			icon_color = excluded.icon_color,
			meta = excluded.meta,
			is_current = ?,
			in_failover_queue = ?
	`,
		provider.ID,
		app.String(),
		provider.Name,
		string(settingsJSON),
		nullableString(provider.WebsiteURL),
		nullableString(provider.Category),
		nullableInt64(provider.CreatedAt),
		nullableInt64(provider.SortIndex),
		nullableString(provider.Notes),
		nullableString(provider.Icon),
		nullableString(provider.IconColor),
		string(metaJSON),
		isCurrent,
		provider.InFailoverQueue,
		isCurrent,
		provider.InFailoverQueue,
	)
	if err != nil {
		return fmt.Errorf("保存供应商失败: %w", err)
	}

	return nil
}

func (s *Store) setCurrentProvider(app AppType, id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`UPDATE providers SET is_current = 0 WHERE app_type = ?`, app.String()); err != nil {
		return fmt.Errorf("重置当前供应商失败: %w", err)
	}
	if _, err := tx.Exec(`UPDATE providers SET is_current = 1 WHERE id = ? AND app_type = ?`, id, app.String()); err != nil {
		return fmt.Errorf("设置当前供应商失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交当前供应商失败: %w", err)
	}

	s.settings.setString(currentProviderKey(app), id)
	if err := s.settings.save(); err != nil {
		return err
	}

	return nil
}

func (s *Store) providerExists(app AppType, id string) (bool, error) {
	var exists int
	if err := s.db.QueryRow(`
		SELECT 1 FROM providers WHERE id = ? AND app_type = ? LIMIT 1
	`, id, app.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("检查供应商是否存在失败: %w", err)
	}
	return true, nil
}

func (s *Store) buildProvider(app AppType, existing *Provider, input ProviderInput, id string) (Provider, error) {
	var provider Provider
	if existing != nil {
		provider = existing.Clone()
	} else {
		provider = Provider{
			ID:             id,
			SettingsConfig: map[string]any{},
			Meta:           map[string]any{},
		}
	}

	provider.ID = id
	provider.Name = strings.TrimSpace(input.Name)
	provider.WebsiteURL = stringPtrOrNil(input.Website)
	provider.Notes = stringPtrOrNil(input.Notes)

	switch app {
	case AppGlobal:
		settings := map[string]any{}
		patchStringField(settings, "base_url", input.BaseURL)
		patchStringField(settings, "api_key", input.APIKey)
		patchStringField(settings, "model", input.Model)
		patchStringField(settings, "reasoning_model", input.ReasoningModel)
		patchStringField(settings, "haiku_model", input.HaikuModel)
		patchStringField(settings, "reasoning_effort", input.ReasoningEffort)
		patchStringField(settings, "reasoning_summary", input.ReasoningSummary)
		patchStringField(settings, "model_verbosity", input.ModelVerbosity)
		patchStringField(settings, "service_tier", input.ServiceTier)
		patchStringField(settings, "wire_api", input.WireAPI)
		patchStringField(settings, "api_type", input.APIType)
		patchStringField(settings, "reasoning", input.Reasoning)
		patchStringField(settings, "max_tokens_field", input.MaxTokensField)
		patchStringField(settings, "supports_developer_role", input.SupportsDeveloperRole)
		patchStringField(settings, "supports_reasoning_effort", input.SupportsReasoningEffort)
		patchStringField(settings, "supports_usage_in_streaming", input.SupportsUsageInStreaming)
		patchPositiveIntField(settings, "context_window", input.ContextWindow)
		patchPositiveIntField(settings, "max_tokens", input.MaxTokens)
		provider.SettingsConfig = settings

	case AppClaude:
		settings := CloneMap(provider.SettingsConfig)
		env := getOrCreateMap(settings, "env")
		keyField := "ANTHROPIC_AUTH_TOKEN"
		if existing != nil {
			if stringValue(existing.Meta["apiKeyField"]) == "ANTHROPIC_API_KEY" {
				keyField = "ANTHROPIC_API_KEY"
			}
			existingEnv := getOrCreateMap(existing.SettingsConfig, "env")
			if stringValue(existingEnv["ANTHROPIC_API_KEY"]) != "" && stringValue(existingEnv["ANTHROPIC_AUTH_TOKEN"]) == "" {
				keyField = "ANTHROPIC_API_KEY"
			}
		}

		delete(env, "ANTHROPIC_AUTH_TOKEN")
		delete(env, "ANTHROPIC_API_KEY")
		if value := strings.TrimSpace(input.APIKey); value != "" {
			env[keyField] = value
		}
		patchStringField(env, "ANTHROPIC_BASE_URL", input.BaseURL)

		model := strings.TrimSpace(input.Model)
		haiku := strings.TrimSpace(input.HaikuModel)
		reasoningModel := strings.TrimSpace(input.ReasoningModel)
		// ANTHROPIC_SMALL_FAST_MODEL 已被官方标记 DEPRECATED，不再写入，顺手清掉历史遗留
		delete(env, "ANTHROPIC_SMALL_FAST_MODEL")
		if model == "" {
			delete(env, "ANTHROPIC_MODEL")
			delete(env, "ANTHROPIC_DEFAULT_HAIKU_MODEL")
			delete(env, "ANTHROPIC_DEFAULT_SONNET_MODEL")
			delete(env, "ANTHROPIC_DEFAULT_OPUS_MODEL")
		} else {
			env["ANTHROPIC_MODEL"] = model
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = model
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = model
			if haiku != "" {
				env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = haiku
			} else {
				env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = model
			}
		}
		if reasoningModel == "" {
			delete(env, "ANTHROPIC_REASONING_MODEL")
		} else {
			env["ANTHROPIC_REASONING_MODEL"] = reasoningModel
		}

		settings["env"] = env
		provider.SettingsConfig = settings

	case AppCodex:
		settings := CloneMap(provider.SettingsConfig)
		auth := getOrCreateMap(settings, "auth")
		patchStringField(auth, "OPENAI_API_KEY", input.APIKey)
		settings["auth"] = auth

		configText, err := patchCodexConfig(stringValue(settings["config"]), input)
		if err != nil {
			return Provider{}, err
		}
		settings["config"] = configText
		provider.SettingsConfig = settings

	case AppPi:
		settings := CloneMap(provider.SettingsConfig)
		providerID := strings.TrimSpace(stringValue(settings["providerId"]))
		if providerID == "" {
			providerID = slugify(input.Name)
		}
		if providerID == "" {
			providerID = "custom"
		}
		apiType := strings.TrimSpace(input.APIType)
		if apiType == "" {
			apiType = firstNonEmpty(stringValue(settings["api"]), "openai-completions")
		}
		modelID := strings.TrimSpace(input.Model)
		contextWindow := normalizePiContextWindow(input.ContextWindow)
		reasoningEnabled := parseBoolDefault(input.Reasoning, true)
		maxTokens := input.MaxTokens

		models := []any{}
		if rawModels, ok := settings["models"].([]any); ok {
			for _, item := range rawModels {
				modelMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if modelID == "" || stringValue(modelMap["id"]) == modelID {
					cloned := CloneMap(modelMap)
					if modelID != "" && stringValue(cloned["id"]) == modelID {
						applyPiModelFields(cloned, modelID, contextWindow, maxTokens, reasoningEnabled)
					}
					models = append(models, cloned)
				}
			}
		}
		if modelID != "" {
			found := false
			for _, item := range models {
				if modelMap, ok := item.(map[string]any); ok && stringValue(modelMap["id"]) == modelID {
					found = true
					break
				}
			}
			if !found {
				modelMap := map[string]any{
					"id":    modelID,
					"name":  modelID,
					"input": []any{"text"},
				}
				applyPiModelFields(modelMap, modelID, contextWindow, maxTokens, reasoningEnabled)
				models = append(models, modelMap)
			}
		}

		next := map[string]any{
			"providerId":    providerID,
			"baseUrl":       strings.TrimSpace(input.BaseURL),
			"api":           apiType,
			"apiKey":        strings.TrimSpace(input.APIKey),
			"model":         modelID,
			"contextWindow": contextWindow,
			"maxTokens":     maxTokens,
			"reasoning":     boolString(reasoningEnabled),
			"models":        models,
		}
		compat := buildPiCompat(settings, input)
		if len(compat) > 0 {
			next["compat"] = compat
		}
		if headers, ok := settings["headers"]; ok {
			next["headers"] = headers
		}
		provider.SettingsConfig = next
	}

	return provider, nil
}

func (s *Store) readLiveSettings(app AppType) (map[string]any, error) {
	switch app {
	case AppClaude:
		return s.readClaudeLive()
	case AppCodex:
		return s.readCodexLive()
	case AppPi:
		return s.readPiLive()
	default:
		return nil, fmt.Errorf("不支持的应用类型: %s", app)
	}
}

func (s *Store) writeLiveSettings(app AppType, provider Provider) error {
	if !app.IsLiveApp() {
		return fmt.Errorf("%s 不支持写入 live 配置", app.DisplayName())
	}
	switch app {
	case AppClaude:
		return writeJSONAtomic(s.claudeSettingsPath(), provider.SettingsConfig)
	case AppCodex:
		auth := getOrCreateMap(provider.SettingsConfig, "auth")
		config := stringValue(provider.SettingsConfig["config"])
		return writeCodexLiveAtomic(s.codexAuthPath(), s.codexConfigPath(), auth, config)
	case AppPi:
		return s.writePiLive(provider)
	default:
		return fmt.Errorf("不支持的应用类型: %s", app)
	}
}

func (s *Store) configDirFor(app AppType) string {
	key := configDirKey(app)
	if custom := strings.TrimSpace(s.settings.getString(key)); custom != "" {
		return resolveOverridePath(custom)
	}

	switch app {
	case AppClaude:
		return filepath.Join(homeDir(), ".claude")
	case AppCodex:
		return filepath.Join(homeDir(), ".codex")
	case AppPi:
		return filepath.Join(homeDir(), ".pi", "agent")
	default:
		return homeDir()
	}
}

func uniqueProviderID(name string, providers []Provider, app AppType) string {
	base := slugify(name)
	if base == "" {
		base = "provider"
	}

	if len(providers) == 0 {
		return base
	}

	used := map[string]struct{}{}
	for _, provider := range providers {
		used[provider.ID] = struct{}{}
	}
	if _, exists := used[base]; !exists {
		return base
	}

	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s-%d", base, index)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func slugify(input string) string {
	var builder strings.Builder
	lastDash := false

	for _, char := range strings.ToLower(strings.TrimSpace(input)) {
		switch {
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			builder.WriteRune(char)
			lastDash = false
		case char == '-' || char == '_' || unicode.IsSpace(char):
			if !lastDash && builder.Len() > 0 {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}

	out := strings.Trim(builder.String(), "-")
	return out
}

func nextSortIndex(providers []Provider) int64 {
	var maxValue int64 = -1
	for _, provider := range providers {
		if provider.SortIndex != nil && *provider.SortIndex > maxValue {
			maxValue = *provider.SortIndex
		}
	}
	return maxValue + 1
}
