package ui

import (
	"reflect"
	"testing"
)

func TestFuzzyFilterEmptyQuery(t *testing.T) {
	items := []string{"gpt-5.4", "claude-opus"}
	if got := fuzzyFilter(items, ""); !reflect.DeepEqual(got, items) {
		t.Fatalf("empty query: got %v", got)
	}
	if got := fuzzyFilter(items, "   "); !reflect.DeepEqual(got, items) {
		t.Fatalf("blank query: got %v", got)
	}
}

func TestFuzzyFilterSubstringBeforeSubsequence(t *testing.T) {
	items := []string{"glm-5.3", "gpt-5.4", "gpt-5.4-codex", "g5p4t"}
	got := fuzzyFilter(items, "gpt")
	want := []string{"gpt-5.4", "gpt-5.4-codex", "g5p4t"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFuzzyFilterCaseInsensitive(t *testing.T) {
	got := fuzzyFilter([]string{"DeepSeek-V4", "qwen3"}, "deepseek")
	if !reflect.DeepEqual(got, []string{"DeepSeek-V4"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFuzzyFilterCJK(t *testing.T) {
	got := fuzzyFilter([]string{"千问-32b", "gpt-5.4"}, "千问")
	if !reflect.DeepEqual(got, []string{"千问-32b"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFuzzyFilterNoMatch(t *testing.T) {
	if got := fuzzyFilter([]string{"gpt-5.4"}, "zzz"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
