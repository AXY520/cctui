package ccswitch

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func readJSONFileMap(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func writeJSONFileMode(path string, data any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(path, mode)
	return nil
}

func parseBoolDefault(value string, defaultValue bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return defaultValue
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func triStateFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case bool:
		return boolString(typed)
	default:
		text := strings.ToLower(strings.TrimSpace(fmt.Sprint(typed)))
		switch text {
		case "", "<nil>":
			return ""
		case "true", "1", "yes", "on":
			return "true"
		case "false", "0", "no", "off":
			return "false"
		default:
			return strings.TrimSpace(fmt.Sprint(typed))
		}
	}
}

func patchPositiveIntField(target map[string]any, key string, value int) {
	if target == nil {
		return
	}
	if value <= 0 {
		delete(target, key)
		return
	}
	target[key] = value
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, err := typed.Int64()
		if err == nil {
			return int(n)
		}
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return n
		}
	}
	return 0
}

func writeJSONAtomic(path string, data any) error {
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return writeBytesAtomic(path, buf)
}

func writeTextAtomic(path, text string) error {
	return writeBytesAtomic(path, []byte(text))
}

func writeBytesAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	if runtime.GOOS == "windows" && fileExists(path) {
		_ = os.Remove(path)
	}
	return os.Rename(tmp, path)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func patchStringField(target map[string]any, key, value string) {
	if target == nil {
		return
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		delete(target, key)
		return
	}
	target[key] = trimmed
}

func patchGenericMapString(target map[string]any, key, value string) {
	if target == nil {
		return
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		delete(target, key)
		return
	}
	target[key] = trimmed
}

func getOrCreateMap(target map[string]any, key string) map[string]any {
	if target == nil {
		return map[string]any{}
	}
	if existing, ok := target[key].(map[string]any); ok && existing != nil {
		return existing
	}
	newMap := map[string]any{}
	target[key] = newMap
	return newMap
}

func mergeMaps(target, source map[string]any) {
	for key, value := range source {
		sourceMap, sourceIsMap := value.(map[string]any)
		targetMap, targetIsMap := target[key].(map[string]any)
		if sourceIsMap && targetIsMap {
			mergeMaps(targetMap, sourceMap)
			continue
		}
		target[key] = value
	}
}

func setNestedMapValue(target map[string]any, path []string, value any) {
	current := target
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]any)
		if !ok || next == nil {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
}

func extractFirstMatch(re *regexp.Regexp, input string) string {
	match := re.FindStringSubmatch(input)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func summarizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return truncate(raw, 28)
	}
	host := parsed.Host
	if host == "" {
		host = raw
	}
	if parsed.Path != "" && parsed.Path != "/" {
		host += parsed.Path
	}
	return truncate(host, 28)
}

func truncate(input string, limit int) string {
	runes := []rune(input)
	if len(runes) <= limit {
		return input
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func resolveOverridePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if strings.HasPrefix(trimmed, "~/") {
		return filepath.Join(homeDir(), strings.TrimPrefix(trimmed, "~/"))
	}
	return trimmed
}

func homeDir() string {
	if testHome := strings.TrimSpace(os.Getenv("CC_SWITCH_TEST_HOME")); testHome != "" {
		return testHome
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	return "."
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func stringValue(value any) string {
	switch cast := value.(type) {
	case string:
		return cast
	case fmt.Stringer:
		return cast.String()
	default:
		return ""
	}
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	number := value.Int64
	return &number
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
