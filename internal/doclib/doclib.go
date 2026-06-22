package doclib

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type DocNode struct {
	ID           string   `yaml:"id" json:"id"`
	NodeID       string   `yaml:"node_id" json:"node_id"`
	NodeTitle    string   `yaml:"node_title" json:"node_title"`
	Parent       string   `yaml:"parent" json:"parent"`
	OwnerRole    string   `yaml:"owner_role" json:"owner_role"`
	Title        string   `yaml:"title" json:"title"`
	Summary      string   `yaml:"summary,omitempty" json:"summary,omitempty"`
	FilePath     string   `yaml:"-" json:"file_path"`
	CodeRef      string   `yaml:"code_ref,omitempty" json:"code_ref,omitempty"`
	CodeRefType  string   `yaml:"code_ref_type,omitempty" json:"code_ref_type,omitempty"`
	Dirty        bool     `yaml:"dirty" json:"dirty"`
	LastUpdated  string   `yaml:"last_updated" json:"last_updated"`
	Exports      []Export `yaml:"exports,omitempty" json:"exports,omitempty"`
	Dependencies []string `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

type Export struct {
	Name      string `yaml:"name" json:"name"`
	Signature string `yaml:"signature" json:"signature"`
}

type NodeGroup struct {
	NodeID    string     `json:"node_id"`
	NodeTitle string     `json:"node_title"`
	Parent    string     `json:"parent"`
	Files     []*DocNode `json:"files"`
}

type DocTree struct {
	Root       *DocNode
	AllDocs    map[string]*DocNode
	Children   map[string][]*DocNode
	Orphans    []*DocNode
	Warnings   []string
	NodeGroups map[string]*NodeGroup
	NodeRoot   string
	NodeOrder  []string
}

const cacheDir = ".argus/cache"
const cacheFile = ".argus/cache/tree.json"

func ParseFrontmatter(data []byte) (*DocNode, string, error) {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return nil, content, nil
	}

	endIdx := strings.Index(content[4:], "\n---")
	if endIdx == -1 {
		endIdx = strings.Index(content[4:], "\n---\n")
		if endIdx == -1 {
			return nil, content, nil
		}
	}
	endIdx += 4

	yamlStr := content[4:endIdx]
	body := ""
	if len(content) > endIdx+5 {
		body = strings.TrimLeft(content[endIdx+5:], "\n")
	}

	var node DocNode
	if err := yaml.Unmarshal([]byte(yamlStr), &node); err != nil {
		return nil, content, fmt.Errorf("解析 frontmatter YAML 失败: %w", err)
	}

	if node.ID == "" || node.OwnerRole == "" || node.Title == "" {
		return nil, content, fmt.Errorf("frontmatter 缺少必填字段 (id, owner_role, title)")
	}

	node.OwnerRole = strings.ToUpper(node.OwnerRole)
	if node.LastUpdated == "" {
		node.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	}

	return &node, body, nil
}

func WriteFrontmatter(node *DocNode, body string) ([]byte, error) {
	node.LastUpdated = time.Now().UTC().Format(time.RFC3339)

	yamlData, err := yaml.Marshal(node)
	if err != nil {
		return nil, fmt.Errorf("序列化 frontmatter 失败: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(string(yamlData))
	sb.WriteString("---\n")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}

	return []byte(sb.String()), nil
}

func ReadDocFile(path string) (*DocNode, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("读取文件失败 %s: %w", path, err)
	}
	return ParseFrontmatter(data)
}

func WriteDocFile(path string, node *DocNode, body string) error {
	data, err := WriteFrontmatter(node, body)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func ScanForDocs(rootDir string) (map[string]string, error) {
	docs := make(map[string]string)

	projectPlan := filepath.Join(rootDir, ".argus", "PROJECT_PLAN.md")
	if _, err := os.Stat(projectPlan); err == nil {
		docs[projectPlan] = ".argus/PROJECT_PLAN.md"
	}

	treeDir := filepath.Join(rootDir, ".argus", "tree")
	if _, err := os.Stat(treeDir); err == nil {
		err = filepath.WalkDir(treeDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
				rel, _ := filepath.Rel(rootDir, path)
				docs[path] = rel
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("扫描 tree 目录失败: %w", err)
		}
	}

	return docs, nil
}

func BuildTree(rootDir string) (*DocTree, error) {
	docPaths, err := ScanForDocs(rootDir)
	if err != nil {
		return nil, err
	}

	tree := &DocTree{
		AllDocs:    make(map[string]*DocNode),
		Children:   make(map[string][]*DocNode),
		NodeGroups: make(map[string]*NodeGroup),
	}

	// Phase 1: Parse all docs
	for absPath, relPath := range docPaths {
		node, _, err := ReadDocFile(absPath)
		if err != nil {
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("跳过 %s: %v", relPath, err))
			continue
		}
		if node == nil {
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("跳过 %s: 无 frontmatter", relPath))
			continue
		}

		if existing, ok := tree.AllDocs[node.ID]; ok {
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("重复 id %q: %s 和 %s", node.ID, relPath, existing.ID))
			continue
		}

		node.FilePath = relPath

		if node.NodeID == "" {
			node.NodeID = node.ID
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("文档 %q 缺少 node_id，使用 id 作为回退", relPath))
		}
		if node.NodeTitle == "" {
			node.NodeTitle = node.Title
		}

		tree.AllDocs[node.ID] = node
	}

	// Phase 2: Build node groups
	for _, node := range tree.AllDocs {
		g, ok := tree.NodeGroups[node.NodeID]
		if !ok {
			g = &NodeGroup{
				NodeID:    node.NodeID,
				NodeTitle: node.NodeTitle,
				Parent:    node.Parent,
			}
			tree.NodeGroups[node.NodeID] = g
		}
		g.Files = append(g.Files, node)
	}

	// Phase 3: Detect root via parent=""
	for id, g := range tree.NodeGroups {
		if g.Parent == "" {
			if tree.NodeRoot != "" {
				tree.Warnings = append(tree.Warnings, fmt.Sprintf("多个根节点: %s 和 %s", tree.NodeRoot, id))
			}
			tree.NodeRoot = id
		}
	}

	if tree.NodeRoot == "" {
		return nil, fmt.Errorf("未找到根节点（parent 为空的 node_id）")
	}

	// Phase 4: Detect orphans at node level
	for id, g := range tree.NodeGroups {
		if id == tree.NodeRoot {
			continue
		}
		if g.Parent == "" {
			for _, node := range g.Files {
				tree.Orphans = append(tree.Orphans, node)
			}
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("孤儿节点 %q: parent 为空", id))
			continue
		}
		if _, ok := tree.NodeGroups[g.Parent]; !ok {
			for _, node := range g.Files {
				tree.Orphans = append(tree.Orphans, node)
			}
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("孤儿节点 %q: parent %q 不存在", id, g.Parent))
			continue
		}
	}

	// Phase 5: Build legacy Children (node_id → child nodegroups, backwards compat)
	for id, g := range tree.NodeGroups {
		if g.Parent == "" || id == tree.NodeRoot {
			continue
		}
		tree.Children[g.Parent] = append(tree.Children[g.Parent], &DocNode{
			ID:        id,
			NodeID:    id,
			NodeTitle: g.NodeTitle,
			Parent:    g.Parent,
			OwnerRole: g.Files[0].OwnerRole,
			Title:     g.NodeTitle,
		})
	}

	// Phase 6: Sort children & topological order
	for _, children := range tree.Children {
		sort.Slice(children, func(i, j int) bool {
			return children[i].ID < children[j].ID
		})
	}
	tree.NodeOrder = nodeTopoSort(tree.NodeGroups, tree.NodeRoot)

	// Legacy root for backwards compat
	if root, ok := tree.AllDocs[""]; ok {
		tree.Root = root
	} else if len(tree.NodeGroups) > 0 {
		tree.Root = tree.NodeGroups[tree.NodeRoot].Files[0]
	}

	return tree, nil
}

func nodeTopoSort(groups map[string]*NodeGroup, rootID string) []string {
	visited := make(map[string]bool)
	var order []string
	var dfs func(id string)
	dfs = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		for _, g := range groups {
			if g.Parent == id {
				dfs(g.NodeID)
			}
		}
		order = append([]string{id}, order...)
	}
	dfs(rootID)
	return order
}

func detectCycles(tree *DocTree) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(id string) error
	dfs = func(id string) error {
		if visited[id] {
			if inStack[id] {
				return fmt.Errorf("检测到循环依赖: node_id=%q", id)
			}
			return nil
		}
		visited[id] = true
		inStack[id] = true

		for _, g := range tree.NodeGroups {
			if g.Parent == id {
				if err := dfs(g.NodeID); err != nil {
					return err
				}
			}
		}

		inStack[id] = false
		return nil
	}

	return dfs(tree.NodeRoot)
}

func PrintTree(tree *DocTree) string {
	var sb strings.Builder
	if tree.NodeRoot == "" {
		return ""
	}
	printNodeGroup(tree, tree.NodeRoot, "", &sb)
	return sb.String()
}

func printNodeGroup(tree *DocTree, nodeID string, prefix string, sb *strings.Builder) {
	g, ok := tree.NodeGroups[nodeID]
	if !ok {
		return
	}
	children := nodeChildren(tree, nodeID)

	if prefix == "" {
		sb.WriteString(fmt.Sprintf("%s (%s) — %s\n", g.NodeID, g.NodeTitle, joinRoles(g.Files)))
	} else {
		sb.WriteString(fmt.Sprintf("%s%s %s (%s)\n", prefix, g.NodeID, g.NodeTitle, joinRoles(g.Files)))
	}

	for _, f := range g.Files {
		dirtyMark := ""
		if f.Dirty {
			dirtyMark = " ⚡"
		}
		sb.WriteString(fmt.Sprintf("%s    ├── %s%s\n", prefix, f.Title, dirtyMark))
	}

	for _, childID := range children {
		printNodeGroup(tree, childID, prefix+"  ", sb)
	}
}

func nodeChildren(tree *DocTree, nodeID string) []string {
	var ids []string
	for _, g := range tree.NodeGroups {
		if g.Parent == nodeID {
			ids = append(ids, g.NodeID)
		}
	}
	sort.Strings(ids)
	return ids
}

func joinRoles(files []*DocNode) string {
	seen := make(map[string]bool)
	var roles []string
	for _, f := range files {
		r := strings.ToUpper(f.OwnerRole)
		if !seen[r] {
			seen[r] = true
			roles = append(roles, r)
		}
	}
	return strings.Join(roles, "/")
}

func GetDepth(tree *DocTree, id string) int {
	depth := 0
	current := id
	maxIter := 100
	for maxIter > 0 {
		maxIter--
		node, ok := tree.AllDocs[current]
		if ok && node.Parent != "" {
			depth++
			current = node.Parent
			continue
		}
		// Try as node_id
		g, ok := tree.NodeGroups[current]
		if !ok || g.Parent == "" {
			break
		}
		depth++
		current = g.Parent
	}
	return depth
}

func ValidateTree(tree *DocTree) []string {
	var errors []string

	if tree.NodeRoot == "" {
		errors = append(errors, "缺少根节点 (parent 为空的 node_id)")
		return errors
	}

	if len(tree.Orphans) > 0 {
		for _, o := range tree.Orphans {
			errors = append(errors, fmt.Sprintf("孤儿文档: id=%q node_id=%q (parent=%q)", o.ID, o.NodeID, o.Parent))
		}
	}

	idSet := make(map[string]bool)
	for _, node := range tree.AllDocs {
		if idSet[node.ID] {
			errors = append(errors, fmt.Sprintf("重复 id: %q", node.ID))
		}
		idSet[node.ID] = true
	}

	return errors
}

func SaveCache(tree *DocTree, rootDir string) error {
	cachePath := filepath.Join(rootDir, cacheFile)
	if err := os.MkdirAll(filepath.Join(rootDir, cacheDir), 0755); err != nil {
		return fmt.Errorf("创建缓存目录失败: %w", err)
	}

	groups := make(map[string]NodeGroupCache)
	for id, g := range tree.NodeGroups {
		var fileIDs []string
		for _, f := range g.Files {
			fileIDs = append(fileIDs, f.ID)
		}
		groups[id] = NodeGroupCache{
			NodeTitle: g.NodeTitle,
			Parent:    g.Parent,
			FileIDs:   fileIDs,
		}
	}

	data := struct {
		Nodes   map[string]*DocNode       `json:"nodes"`
		Groups  map[string]NodeGroupCache `json:"groups"`
		RootID  string                    `json:"root_id"`
		Updated string                    `json:"updated"`
	}{
		Nodes:   tree.AllDocs,
		Groups:  groups,
		RootID:  tree.NodeRoot,
		Updated: time.Now().UTC().Format(time.RFC3339),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化缓存失败: %w", err)
	}

	return os.WriteFile(cachePath, jsonData, 0644)
}

type NodeGroupCache struct {
	NodeTitle string   `json:"node_title"`
	Parent    string   `json:"parent"`
	FileIDs   []string `json:"file_ids"`
}

func LoadCache(rootDir string) (*DocTree, error) {
	cachePath := filepath.Join(rootDir, cacheFile)
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("读取缓存失败: %w", err)
	}

	var cached struct {
		Nodes  map[string]*DocNode       `json:"nodes"`
		Groups map[string]NodeGroupCache `json:"groups"`
		RootID string                    `json:"root_id"`
	}
	if err := json.Unmarshal(raw, &cached); err != nil {
		return nil, fmt.Errorf("解析缓存失败: %w", err)
	}

	tree := &DocTree{
		AllDocs:    make(map[string]*DocNode),
		Children:   make(map[string][]*DocNode),
		NodeGroups: make(map[string]*NodeGroup),
		NodeRoot:   cached.RootID,
	}

	for id, n := range cached.Nodes {
		tree.AllDocs[id] = n
	}

	for id, gc := range cached.Groups {
		g := &NodeGroup{
			NodeID:    id,
			NodeTitle: gc.NodeTitle,
			Parent:    gc.Parent,
		}
		for _, fid := range gc.FileIDs {
			if n, ok := cached.Nodes[fid]; ok {
				g.Files = append(g.Files, n)
			}
		}
		tree.NodeGroups[id] = g
	}

	// Rebuild legacy Children
	for id, g := range tree.NodeGroups {
		if g.Parent == "" || id == tree.NodeRoot {
			continue
		}
		tree.Children[g.Parent] = append(tree.Children[g.Parent], &DocNode{
			ID:        id,
			NodeID:    id,
			NodeTitle: g.NodeTitle,
			Parent:    g.Parent,
		})
	}

	if len(tree.NodeGroups) > 0 {
		tree.Root = tree.NodeGroups[tree.NodeRoot].Files[0]
	}

	tree.NodeOrder = nodeTopoSort(tree.NodeGroups, tree.NodeRoot)

	return tree, nil
}

func GetImpactedDocs(tree *DocTree, docID string) []string {
	var impacted []string
	for _, node := range tree.AllDocs {
		for _, dep := range node.Dependencies {
			if dep == docID {
				impacted = append(impacted, node.ID)
				break
			}
		}
	}
	sort.Strings(impacted)
	return impacted
}

func IsValidRole(role string) bool {
	switch strings.ToUpper(role) {
	case "PM", "SE", "AP":
		return true
	}
	return false
}

func (n *DocNode) SetDirty(dirty bool) {
	n.Dirty = dirty
	n.LastUpdated = time.Now().UTC().Format(time.RFC3339)
}

func docIDToPath(rootDir string) (map[string]string, error) {
	docPaths, err := ScanForDocs(rootDir)
	if err != nil {
		return nil, err
	}
	idToPath := make(map[string]string)
	for absPath := range docPaths {
		node, _, err := ReadDocFile(absPath)
		if err != nil || node == nil {
			continue
		}
		idToPath[node.ID] = absPath
	}
	return idToPath, nil
}

func PropagateDirty(rootDir string, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}

	tree, err := BuildTree(rootDir)
	if err != nil {
		return fmt.Errorf("构建文档树失败: %w", err)
	}

	idToPath, err := docIDToPath(rootDir)
	if err != nil {
		return fmt.Errorf("扫描文档路径失败: %w", err)
	}

	// Collect all node_ids that need dirty propagation
	nodeSet := make(map[string]bool)
	for _, id := range docIDs {
		node, ok := tree.AllDocs[id]
		if !ok {
			continue
		}
		nodeSet[node.NodeID] = true
	}

	// Walk up node hierarchy
	seenNode := make(map[string]bool)
	var ancestors []string
	var walkNode func(nid string)
	walkNode = func(nid string) {
		if seenNode[nid] {
			return
		}
		seenNode[nid] = true
		ancestors = append(ancestors, nid)
		g, ok := tree.NodeGroups[nid]
		if !ok || g.Parent == "" {
			return
		}
		walkNode(g.Parent)
	}
	for nid := range nodeSet {
		walkNode(nid)
	}

	// Mark all docs in ancestor nodes as dirty
	seenFile := make(map[string]bool)
	for _, nid := range ancestors {
		g, ok := tree.NodeGroups[nid]
		if !ok {
			continue
		}
		for _, f := range g.Files {
			if seenFile[f.ID] {
				continue
			}
			seenFile[f.ID] = true
			path, ok := idToPath[f.ID]
			if !ok {
				continue
			}
			node, body, err := ReadDocFile(path)
			if err != nil || node == nil {
				continue
			}
			node.SetDirty(true)
			if err := WriteDocFile(path, node, body); err != nil {
				return fmt.Errorf("写入文档 %s 失败: %w", f.ID, err)
			}
		}
	}

	return SaveCache(tree, rootDir)
}

func ClearDirty(rootDir string, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}
	idToPath, err := docIDToPath(rootDir)
	if err != nil {
		return err
	}
	for _, id := range docIDs {
		path, ok := idToPath[id]
		if !ok {
			continue
		}
		node, body, err := ReadDocFile(path)
		if err != nil || node == nil {
			continue
		}
		node.SetDirty(false)
		if err := WriteDocFile(path, node, body); err != nil {
			return err
		}
	}
	return nil
}

type ExtractedExport struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Kind      string `json:"kind"`
}

func ExtractExportsFromFile(filePath string) ([]ExtractedExport, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("解析文件失败: %w", err)
	}

	var exports []ExtractedExport

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			sig := formatSignature(d)
			exports = append(exports, ExtractedExport{
				Name:      d.Name.Name,
				Signature: sig,
				Kind:      "function",
			})

		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				kind := "type"
				sig := ts.Name.Name
				switch ts.Type.(type) {
				case *ast.StructType:
					kind = "struct"
				case *ast.InterfaceType:
					kind = "interface"
				}
				exports = append(exports, ExtractedExport{
					Name:      ts.Name.Name,
					Signature: sig,
					Kind:      kind,
				})
			}
		}
	}

	return exports, nil
}

func formatSignature(fd *ast.FuncDecl) string {
	var sb strings.Builder
	if fd.Recv != nil {
		sb.WriteString("func (")
		for i, field := range fd.Recv.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			for _, name := range field.Names {
				sb.WriteString(name.Name)
				sb.WriteString(" ")
			}
			sb.WriteString(exprString(field.Type))
		}
		sb.WriteString(") ")
	} else {
		sb.WriteString("func ")
	}
	sb.WriteString(fd.Name.Name)
	sb.WriteString("(")
	for i, field := range fd.Type.Params.List {
		if i > 0 {
			sb.WriteString(", ")
		}
		for j, name := range field.Names {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(name.Name)
		}
		sb.WriteString(" ")
		sb.WriteString(exprString(field.Type))
	}
	sb.WriteString(")")
	if fd.Type.Results != nil {
		sb.WriteString(" ")
		results := fd.Type.Results
		if len(results.List) == 1 && results.List[0].Names == nil {
			sb.WriteString(exprString(results.List[0].Type))
		} else {
			sb.WriteString("(")
			for i, field := range results.List {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(exprString(field.Type))
			}
			sb.WriteString(")")
		}
	}
	return sb.String()
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprString(t.Elt)
		}
		return "[" + exprString(t.Len) + "]" + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + exprString(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	default:
		return fmt.Sprintf("%T", e)
	}
}

func AutoSyncExports(rootDir, docID string) (string, error) {
	idToPath, err := docIDToPath(rootDir)
	if err != nil {
		return "", err
	}
	docPath, ok := idToPath[docID]
	if !ok {
		return "", fmt.Errorf("文档 %s 不存在", docID)
	}

	node, body, err := ReadDocFile(docPath)
	if err != nil {
		return "", err
	}
	if node == nil {
		return "", fmt.Errorf("文档 %s 无 frontmatter", docID)
	}
	if node.CodeRef == "" {
		return "", fmt.Errorf("文档 %s 未设置 code_ref", docID)
	}

	codePath := filepath.Join(rootDir, node.CodeRef)
	if _, err := os.Stat(codePath); err != nil {
		return "", fmt.Errorf("代码文件 %s 不存在: %w", node.CodeRef, err)
	}

	exports, err := ExtractExportsFromFile(codePath)
	if err != nil {
		return "", err
	}

	var newExports []Export
	for _, e := range exports {
		newExports = append(newExports, Export{
			Name:      e.Name,
			Signature: e.Signature,
		})
	}
	node.Exports = newExports
	node.SetDirty(true)

	if err := WriteDocFile(docPath, node, body); err != nil {
		return "", err
	}

	changed := len(newExports)
	return fmt.Sprintf("已同步 %s 的导出: %d 个导出项", docID, changed), nil
}

type VerifyResult struct {
	DocID         string
	CodeFile      string
	AddedInCode   []string
	MissingInCode []string
	Match         bool
	Detail        string
}

func VerifyDocExports(rootDir, docID string) (*VerifyResult, error) {
	idToPath, err := docIDToPath(rootDir)
	if err != nil {
		return nil, err
	}
	docPath, ok := idToPath[docID]
	if !ok {
		return nil, fmt.Errorf("文档 %s 不存在", docID)
	}

	node, _, err := ReadDocFile(docPath)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("文档 %s 无 frontmatter", docID)
	}

	result := &VerifyResult{
		DocID:    docID,
		CodeFile: node.CodeRef,
		Match:    true,
	}

	if node.CodeRef == "" {
		result.Detail = "文档未设置 code_ref，无法验证"
		result.Match = false
		return result, nil
	}

	codePath := filepath.Join(rootDir, node.CodeRef)
	if _, err := os.Stat(codePath); err != nil {
		// code_ref 指向的文件可能不存在（尚未创建），这不是错误
		result.Detail = fmt.Sprintf("代码文件 %s 尚未创建", node.CodeRef)
		result.Match = false
		return result, nil
	}

	codeExports, err := ExtractExportsFromFile(codePath)
	if err != nil {
		return nil, err
	}

	codeExportSet := make(map[string]bool)
	for _, e := range codeExports {
		codeExportSet[e.Name] = true
	}

	docExportSet := make(map[string]bool)
	for _, e := range node.Exports {
		docExportSet[e.Name] = true
	}

	for _, e := range codeExports {
		if !docExportSet[e.Name] {
			result.AddedInCode = append(result.AddedInCode, e.Name)
			result.Match = false
		}
	}

	for _, e := range node.Exports {
		if !codeExportSet[e.Name] {
			result.MissingInCode = append(result.MissingInCode, e.Name)
			result.Match = false
		}
	}

	if result.Match {
		result.Detail = fmt.Sprintf("文档与代码一致，共 %d 个导出项", len(node.Exports))
	} else {
		var sb strings.Builder
		if len(result.AddedInCode) > 0 {
			sb.WriteString(fmt.Sprintf("代码中新增了导出但文档未记录: %v\n", result.AddedInCode))
		}
		if len(result.MissingInCode) > 0 {
			sb.WriteString(fmt.Sprintf("文档记录了导出但代码中已不存在: %v\n", result.MissingInCode))
		}
		result.Detail = strings.TrimSpace(sb.String())
	}

	return result, nil
}
