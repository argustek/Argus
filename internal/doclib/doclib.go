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
	Parent       string   `yaml:"parent" json:"parent"`
	OwnerRole    string   `yaml:"owner_role" json:"owner_role"`
	Title        string   `yaml:"title" json:"title"`
	Summary      string   `yaml:"summary,omitempty" json:"summary,omitempty"`
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

type DocTree struct {
	Root     *DocNode
	AllDocs  map[string]*DocNode
	Children map[string][]*DocNode
	Orphans  []*DocNode
	Warnings []string
}

const cacheDir = ".argus/cache"
const cacheFile = ".argus/cache/tree.json"

func ParseFrontmatter(data []byte) (*DocNode, string, error) {
	content := string(data)
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
		AllDocs:  make(map[string]*DocNode),
		Children: make(map[string][]*DocNode),
	}

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

		tree.AllDocs[node.ID] = node
	}

	if root, ok := tree.AllDocs[""]; ok {
		tree.Root = root
	} else {
		for _, node := range tree.AllDocs {
			if node.Parent == "" {
				if tree.Root != nil {
					tree.Warnings = append(tree.Warnings, "多个根节点，请检查")
				}
				tree.Root = node
			}
		}
	}

	if tree.Root == nil {
		return nil, fmt.Errorf("未找到根节点（parent 为空的文档）")
	}

	for _, node := range tree.AllDocs {
		if node.ID == tree.Root.ID {
			continue
		}
		parentID := node.Parent
		if parentID == "" {
			tree.Orphans = append(tree.Orphans, node)
			continue
		}
		if _, ok := tree.AllDocs[parentID]; !ok {
			tree.Orphans = append(tree.Orphans, node)
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("孤儿文档 %q: parent %q 不存在", node.ID, parentID))
			continue
		}
		tree.Children[parentID] = append(tree.Children[parentID], node)
	}

	for _, children := range tree.Children {
		sort.Slice(children, func(i, j int) bool {
			return children[i].ID < children[j].ID
		})
	}

	if err := detectCycles(tree); err != nil {
		return nil, err
	}

	return tree, nil
}

func detectCycles(tree *DocTree) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(id string) error
	dfs = func(id string) error {
		visited[id] = true
		inStack[id] = true

		for _, child := range tree.Children[id] {
			if !visited[child.ID] {
				if err := dfs(child.ID); err != nil {
					return err
				}
			} else if inStack[child.ID] {
				return fmt.Errorf("检测到循环依赖: id=%q", child.ID)
			}
		}

		inStack[id] = false
		return nil
	}

	return dfs(tree.Root.ID)
}

func PrintTree(tree *DocTree) string {
	var sb strings.Builder
	if tree.Root == nil {
		return ""
	}

	roleLabel := tree.Root.OwnerRole
	sb.WriteString(fmt.Sprintf("%s (%s)\n", tree.Root.ID, roleLabel))
	printChildren(tree, tree.Root.ID, "", &sb)

	return sb.String()
}

func printChildren(tree *DocTree, parentID string, prefix string, sb *strings.Builder) {
	children := tree.Children[parentID]
	for i, child := range children {
		isLast := i == len(children)-1
		branch := "├── "
		childPrefix := "│   "
		if isLast {
			branch = "└── "
			childPrefix = "    "
		}

		displayID := child.ID
		sb.WriteString(fmt.Sprintf("%s%s%s (%s)\n", prefix, branch, displayID, child.OwnerRole))
		printChildren(tree, child.ID, prefix+childPrefix, sb)
	}
}

func GetDepth(tree *DocTree, id string) int {
	depth := 0
	current := id
	for {
		node, ok := tree.AllDocs[current]
		if !ok || node.Parent == "" {
			break
		}
		depth++
		current = node.Parent
	}
	return depth
}

func ValidateTree(tree *DocTree) []string {
	var errors []string

	if tree.Root == nil {
		errors = append(errors, "缺少根节点")
		return errors
	}

	if len(tree.Orphans) > 0 {
		for _, o := range tree.Orphans {
			errors = append(errors, fmt.Sprintf("孤儿文档: %q (parent=%q)", o.ID, o.Parent))
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

	data := struct {
		Nodes    map[string]*DocNode `json:"nodes"`
		Children map[string][]string `json:"children"`
		RootID   string              `json:"root_id"`
		Updated  string              `json:"updated"`
	}{
		Nodes:    tree.AllDocs,
		Children: make(map[string][]string),
		RootID:   tree.Root.ID,
		Updated:  time.Now().UTC().Format(time.RFC3339),
	}

	for parent, children := range tree.Children {
		var ids []string
		for _, c := range children {
			ids = append(ids, c.ID)
		}
		data.Children[parent] = ids
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化缓存失败: %w", err)
	}

	return os.WriteFile(cachePath, jsonData, 0644)
}

func LoadCache(rootDir string) (*DocTree, error) {
	cachePath := filepath.Join(rootDir, cacheFile)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("读取缓存失败: %w", err)
	}

	var cached struct {
		Nodes    map[string]*DocNode `json:"nodes"`
		Children map[string][]string `json:"children"`
		RootID   string              `json:"root_id"`
	}
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("解析缓存失败: %w", err)
	}

	tree := &DocTree{
		AllDocs:  cached.Nodes,
		Children: make(map[string][]*DocNode),
	}

	if root, ok := cached.Nodes[cached.RootID]; ok {
		tree.Root = root
	}

	for parent, childIDs := range cached.Children {
		for _, id := range childIDs {
			if child, ok := cached.Nodes[id]; ok {
				tree.Children[parent] = append(tree.Children[parent], child)
			}
		}
	}

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

	var validIDs []string
	for _, id := range docIDs {
		if _, ok := tree.AllDocs[id]; ok {
			validIDs = append(validIDs, id)
		}
	}
	if len(validIDs) == 0 {
		return nil
	}

	sort.Slice(validIDs, func(i, j int) bool {
		return GetDepth(tree, validIDs[i]) > GetDepth(tree, validIDs[j])
	})

	seen := make(map[string]bool)
	var toProcess []string
	for _, id := range validIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		toProcess = append(toProcess, id)
		current := id
		for {
			node, ok := tree.AllDocs[current]
			if !ok || node.Parent == "" {
				break
			}
			if seen[node.Parent] {
				break
			}
			seen[node.Parent] = true
			toProcess = append(toProcess, node.Parent)
			current = node.Parent
		}
	}

	sort.Slice(toProcess, func(i, j int) bool {
		return GetDepth(tree, toProcess[i]) > GetDepth(tree, toProcess[j])
	})

	for _, id := range toProcess {
		path, ok := idToPath[id]
		if !ok {
			continue
		}
		node, body, err := ReadDocFile(path)
		if err != nil || node == nil {
			continue
		}
		node.SetDirty(true)
		if err := WriteDocFile(path, node, body); err != nil {
			return fmt.Errorf("写入文档 %s 失败: %w", id, err)
		}
	}

	if err := SaveCache(tree, rootDir); err != nil {
		return fmt.Errorf("保存缓存失败: %w", err)
	}

	return nil
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
