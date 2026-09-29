package ui

import "strings"

// fuzzyFilter 按查询词过滤候选：子串命中优先，其次 fzf 风格的顺序子序列命中，
// 两组内部均保持原有顺序。查询词为空时返回原列表。
func fuzzyFilter(items []string, query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return items
	}
	lowered := strings.ToLower(query)
	sub := make([]string, 0, len(items))
	seq := make([]string, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), lowered) {
			sub = append(sub, item)
		} else if fuzzyMatch(item, lowered) {
			seq = append(seq, item)
		}
	}
	return append(sub, seq...)
}

// fuzzyMatch 判断 query 的字符是否按顺序出现在 candidate 中（rune 级，query 需已转小写）。
func fuzzyMatch(candidate, query string) bool {
	runes := []rune(query)
	if len(runes) == 0 {
		return true
	}
	pos := 0
	for _, r := range strings.ToLower(candidate) {
		if r == runes[pos] {
			pos++
			if pos == len(runes) {
				return true
			}
		}
	}
	return false
}
