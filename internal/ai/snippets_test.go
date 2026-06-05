package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnippetStore_Search(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnippetStore(tmpDir)

	if store.Count("") < 8 {
		t.Fatalf("Expected at least 8 default snippets, got %d", store.Count(""))
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
		results := store.SearchSimple(tt.query)
		if len(results) < tt.min {
			t.Errorf("Search(%q) = %d results, want >= %d", tt.query, len(results), tt.min)
		}
	}

	// Test format
	formatted := store.FormatResults(store.SearchSimple("http"))
	if !strings.Contains(formatted, "Go HTTP Server") {
		t.Error("Formatted output should contain snippet name")
	}
}

func TestSnippetStore_CRUD(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnippetStore(tmpDir)

	// Test Add
	newSn := Snippet{
		Name:        "Test Snippet",
		Language:    "Go",
		Description: "Test description",
		Tags:        []string{"test", "custom"},
		Code:        `fmt.Println("hello")`,
	}
	if err := store.Add(newSn); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if store.Count("") < 9 {
		t.Errorf("Expected >=9 after add, got %d", store.Count(""))
	}

	// Test List
	list := store.List("")
	var addedID string
	found := false
	for _, sn := range list {
		if sn.Name == "Test Snippet" {
			found = true
			addedID = sn.ID
			break
		}
	}
	if !found || addedID == "" {
		t.Fatal("List should contain newly added snippet with ID")
	}

	// Test GetByID
	sn, ok := store.GetByID(addedID)
	if !ok || sn.Name != "Test Snippet" {
		t.Error("GetByID should return the added snippet")
	}

	// Test Update
	sn.Description = "Updated description"
	if err := store.Update(addedID, sn); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	updated, _ := store.GetByID(addedID)
	if updated.Description != "Updated description" {
		t.Errorf("Update failed: got %s", updated.Description)
	}

	// Test Delete
	if err := store.Delete(addedID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if store.Count("") >= 9 {
		t.Errorf("Expected <9 after delete, got %d", store.Count(""))
	}
	_, ok = store.GetByID(addedID)
	if ok {
		t.Error("GetByID should return false after delete")
	}
}

func TestSnippetStore_BuiltinProtected(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnippetStore(tmpDir)

	// Cannot delete builtin
	err := store.Delete("builtin-go-http-server")
	if err == nil {
		t.Fatal("Should not be able to delete builtin snippet")
	}

	// Cannot update builtin
	builtin, _ := store.GetByID("builtin-go-http-server")
	builtin.Name = "Hacked"
	err = store.Update("builtin-go-http-server", builtin)
	if err == nil {
		t.Fatal("Should not be able to update builtin snippet")
	}
}

func TestSnippetStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store and add custom snippet
	store1 := NewSnippetStore(tmpDir)
	custom := Snippet{
		Name:        "Persistent",
		Language:    "Python",
		Description: "Should survive reload",
		Tags:        []string{"test"},
		Code:        `print("hello")`,
	}
	if err := store1.Add(custom); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Reload from same dir
	store2 := NewSnippetStore(tmpDir)
	list := store2.List("")
	found := false
	for _, sn := range list {
		if sn.Name == "Persistent" && !sn.IsBuiltin {
			found = true
			break
		}
	}
	if !found {
		t.Error("Custom snippet should persist across reloads")
	}

	// Verify file exists
	fp := filepath.Join(tmpDir, "snippets.json")
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		t.Error("snippets.json file should exist")
	}
}

func TestSnippetStore_SearchWithOptions(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnippetStore(tmpDir)

	// Search by language
	goResults := store.Search(SearchOptions{Query: "server", Language: "Go"})
	for _, r := range goResults {
		if r.Language != "Go" {
			t.Errorf("Language filter failed: got %s", r.Language)
		}
	}

	// Search with limit
	limited := store.Search(SearchOptions{Query: "", Limit: 3})
	if len(limited) > 3 {
		t.Errorf("Limit failed: got %d, want <=3", len(limited))
	}

	// Languages list
	langs := store.Languages()
	if len(langs) == 0 {
		t.Error("Languages() should return at least Go")
	}

	// Tags list
	tags := store.Tags()
	if len(tags) == 0 {
		t.Error("Tags() should return tags from builtins")
	}
}
