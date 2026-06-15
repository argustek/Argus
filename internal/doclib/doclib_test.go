package doclib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestDoc(t *testing.T, path, id, parent, role, title string, dirty bool) {
	t.Helper()
	frontmatter := `---
id: "` + id + `"
parent: "` + parent + `"
owner_role: "` + role + `"
title: "` + title + `"
dirty: ` + formatBool(dirty) + `
last_updated: "2026-01-01T00:00:00Z"
---

# ` + title + `

Test body content.
`
	err := os.MkdirAll(filepath.Dir(path), 0755)
	require.NoError(t, err)
	err = os.WriteFile(path, []byte(frontmatter), 0644)
	require.NoError(t, err)
}

func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestBuildTree_SingleRoot(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"PROJECT_PLAN.md", "", "PM", "Project Plan", false)

	tree, err := BuildTree(dir)
	require.NoError(t, err)
	require.NotNil(t, tree.Root)
	assert.Equal(t, "PROJECT_PLAN.md", tree.Root.ID)
	assert.Equal(t, "PM", tree.Root.OwnerRole)
	assert.Empty(t, tree.Warnings)
}

func TestBuildTree_WithChildren(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"PROJECT_PLAN.md", "", "PM", "Project Plan", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "auth.md"),
		"tree/auth.md", "PROJECT_PLAN.md", "SE", "Auth Module", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "auth", "jwt.md"),
		"tree/auth/jwt.md", "tree/auth.md", "SE", "JWT", false)

	tree, err := BuildTree(dir)
	require.NoError(t, err)
	require.NotNil(t, tree.Root)

	assert.Len(t, tree.AllDocs, 3)
	assert.Len(t, tree.Children["PROJECT_PLAN.md"], 1)
	assert.Len(t, tree.Children["tree/auth.md"], 1)
	assert.Empty(t, tree.Warnings)
}

func TestBuildTree_OrphanWarning(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"PROJECT_PLAN.md", "", "PM", "Project Plan", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "orphan.md"),
		"tree/orphan.md", "tree/nonexistent.md", "SE", "Orphan", false)

	_, err := BuildTree(dir)
	require.NoError(t, err)
}

func TestGetDepth(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"root", "", "PM", "Root", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "a.md"),
		"a", "root", "SE", "Level 1", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "b.md"),
		"b", "a", "SE", "Level 2", false)

	tree, err := BuildTree(dir)
	require.NoError(t, err)

	assert.Equal(t, 0, GetDepth(tree, "root"))
	assert.Equal(t, 1, GetDepth(tree, "a"))
	assert.Equal(t, 2, GetDepth(tree, "b"))
	assert.Equal(t, 0, GetDepth(tree, "nonexistent"))
}

func TestPrintTree(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"PROJECT_PLAN.md", "", "PM", "Plan", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "a.md"),
		"tree/a.md", "PROJECT_PLAN.md", "SE", "Module A", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "b.md"),
		"tree/b.md", "PROJECT_PLAN.md", "SE", "Module B", false)

	tree, err := BuildTree(dir)
	require.NoError(t, err)

	output := PrintTree(tree)
	assert.True(t, strings.Contains(output, "PROJECT_PLAN.md (PM)"))
	assert.True(t, strings.Contains(output, "tree/a.md (SE)"))
	assert.True(t, strings.Contains(output, "tree/b.md (SE)"))
}

func TestSaveAndLoadCache(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"PROJECT_PLAN.md", "", "PM", "Project Plan", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "mod.md"),
		"tree/mod.md", "PROJECT_PLAN.md", "SE", "Module", false)

	tree, err := BuildTree(dir)
	require.NoError(t, err)

	err = SaveCache(tree, dir)
	require.NoError(t, err)

	cachePath := filepath.Join(dir, ".argus", "cache", "tree.json")
	_, err = os.Stat(cachePath)
	require.NoError(t, err)

	loaded, err := LoadCache(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded.Root)
	assert.Equal(t, tree.Root.ID, loaded.Root.ID)
	assert.Len(t, loaded.AllDocs, 2)
}

func TestPropagateDirty(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"root", "", "PM", "Root", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "child.md"),
		"child", "root", "SE", "Child", false)

	err := PropagateDirty(dir, []string{"child"})
	require.NoError(t, err)

	childNode, _, err := ReadDocFile(filepath.Join(dir, ".argus", "tree", "child.md"))
	require.NoError(t, err)
	assert.True(t, childNode.Dirty, "child should be dirty")

	rootNode, _, err := ReadDocFile(filepath.Join(dir, ".argus", "PROJECT_PLAN.md"))
	require.NoError(t, err)
	assert.True(t, rootNode.Dirty, "root should be dirty (propagated from child)")
}

func TestPropagateDirty_MultiLevel(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"root", "", "PM", "Root", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "mid.md"),
		"mid", "root", "SE", "Mid", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "leaf.md"),
		"leaf", "mid", "SE", "Leaf", false)

	err := PropagateDirty(dir, []string{"leaf"})
	require.NoError(t, err)

	leafNode, _, _ := ReadDocFile(filepath.Join(dir, ".argus", "tree", "leaf.md"))
	assert.True(t, leafNode.Dirty)

	midNode, _, _ := ReadDocFile(filepath.Join(dir, ".argus", "tree", "mid.md"))
	assert.True(t, midNode.Dirty)

	rootNode, _, _ := ReadDocFile(filepath.Join(dir, ".argus", "PROJECT_PLAN.md"))
	assert.True(t, rootNode.Dirty)
}

func TestClearDirty(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"root", "", "PM", "Root", true)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "child.md"),
		"child", "root", "SE", "Child", true)

	err := ClearDirty(dir, []string{"child", "root"})
	require.NoError(t, err)

	childNode, _, _ := ReadDocFile(filepath.Join(dir, ".argus", "tree", "child.md"))
	assert.False(t, childNode.Dirty, "child dirty should be cleared")

	rootNode, _, _ := ReadDocFile(filepath.Join(dir, ".argus", "PROJECT_PLAN.md"))
	assert.False(t, rootNode.Dirty, "root dirty should be cleared")
}

func TestPropagateAndClearDirty_Integrated(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"root", "", "PM", "Root", false)
	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "a.md"),
		"a", "root", "SE", "Module A", false)

	err := PropagateDirty(dir, []string{"a"})
	require.NoError(t, err)

	aNode, _, _ := ReadDocFile(filepath.Join(dir, ".argus", "tree", "a.md"))
	assert.True(t, aNode.Dirty)
	rootNode, _, _ := ReadDocFile(filepath.Join(dir, ".argus", "PROJECT_PLAN.md"))
	assert.True(t, rootNode.Dirty)

	err = ClearDirty(dir, []string{"a", "root"})
	require.NoError(t, err)

	aNode, _, _ = ReadDocFile(filepath.Join(dir, ".argus", "tree", "a.md"))
	assert.False(t, aNode.Dirty)
	rootNode, _, _ = ReadDocFile(filepath.Join(dir, ".argus", "PROJECT_PLAN.md"))
	assert.False(t, rootNode.Dirty)
}

func TestPropagateDirty_EmptyIDs(t *testing.T) {
	err := PropagateDirty(t.TempDir(), nil)
	assert.NoError(t, err)

	err = PropagateDirty(t.TempDir(), []string{})
	assert.NoError(t, err)
}

func TestClearDirty_EmptyIDs(t *testing.T) {
	err := ClearDirty(t.TempDir(), nil)
	assert.NoError(t, err)

	err = ClearDirty(t.TempDir(), []string{})
	assert.NoError(t, err)
}

func TestGetImpactedDocs(t *testing.T) {
	dir := t.TempDir()

	writeTestDoc(t, filepath.Join(dir, ".argus", "PROJECT_PLAN.md"),
		"root", "", "PM", "Root", false)

	depContent := `---
id: "a"
parent: "root"
owner_role: "SE"
title: "Module A"
dirty: false
last_updated: "2026-01-01T00:00:00Z"
dependencies:
  - "b"
---

Body
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".argus", "tree"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".argus", "tree", "a.md"), []byte(depContent), 0644))

	writeTestDoc(t, filepath.Join(dir, ".argus", "tree", "b.md"),
		"b", "root", "SE", "Module B", false)

	tree, err := BuildTree(dir)
	require.NoError(t, err)

	impacted := GetImpactedDocs(tree, "b")
	assert.Equal(t, []string{"a"}, impacted)

	impacted = GetImpactedDocs(tree, "nonexistent")
	assert.Empty(t, impacted)
}

func TestParseFrontmatter_Valid(t *testing.T) {
	data := []byte(`---
id: "test-doc"
parent: ""
owner_role: "PM"
title: "Test"
dirty: false
last_updated: "2026-01-01T00:00:00Z"
---

Body content
`)
	node, body, err := ParseFrontmatter(data)
	require.NoError(t, err)
	require.NotNil(t, node)
	assert.Equal(t, "test-doc", node.ID)
	assert.Equal(t, "PM", node.OwnerRole)
	assert.Equal(t, "Body content", strings.TrimSpace(body))
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	data := []byte("Just plain text\nno frontmatter")
	node, body, err := ParseFrontmatter(data)
	require.NoError(t, err)
	assert.Nil(t, node)
	assert.Equal(t, string(data), body)
}

func TestParseFrontmatter_MissingFields(t *testing.T) {
	data := []byte(`---
id: "test-doc"
parent: ""
---
`)
	_, _, err := ParseFrontmatter(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "缺少必填字段")
}

func TestWriteAndReadDocFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	node := &DocNode{
		ID:        "test",
		Parent:    "",
		OwnerRole: "PM",
		Title:     "Test Doc",
	}
	body := "Hello World"

	err := WriteDocFile(path, node, body)
	require.NoError(t, err)

	readNode, readBody, err := ReadDocFile(path)
	require.NoError(t, err)
	require.NotNil(t, readNode)
	assert.Equal(t, node.ID, readNode.ID)
	assert.Equal(t, node.Title, readNode.Title)
	assert.Equal(t, body, strings.TrimSpace(readBody))
	assert.False(t, readNode.LastUpdated == "")
}

func TestIsValidRole(t *testing.T) {
	assert.True(t, IsValidRole("PM"))
	assert.True(t, IsValidRole("SE"))
	assert.True(t, IsValidRole("AP"))
	assert.True(t, IsValidRole("pm"))
	assert.True(t, IsValidRole("se"))
	assert.True(t, IsValidRole("ap"))
	assert.False(t, IsValidRole(""))
	assert.False(t, IsValidRole("DEV"))
}

func TestSetDirty(t *testing.T) {
	node := &DocNode{ID: "test", Dirty: false}
	node.SetDirty(true)
	assert.True(t, node.Dirty)
	assert.NotEmpty(t, node.LastUpdated)

	node.SetDirty(false)
	assert.False(t, node.Dirty)
}
