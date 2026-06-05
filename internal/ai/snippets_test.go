package ai

import (
	"strings"
	"testing"
)

func TestSnippetStore_Search(t *testing.T) {
	store := NewSnippetStore()

	if store.Count() < 8 {
		t.Fatalf("Expected at least 8 default snippets, got %d", store.Count())
	}

	tests := []struct {
		query string
		min   int
	}{
		{"http server", 1},
		{"crud api", 1},
		{"auth", 1},
		{"database", 1},
		{"test", 1},
		{"concurrency", 1},
		{"config", 1},
		{"cli", 1},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		results := store.Search(tt.query)
		if len(results) < tt.min {
			t.Errorf("Search(%q) = %d results, want >= %d", tt.query, len(results), tt.min)
		}
	}

	// Test format
	formatted := store.FormatResults(store.Search("http"))
	if !strings.Contains(formatted, "Go HTTP Server") {
		t.Error("Formatted output should contain snippet name")
	}
}
