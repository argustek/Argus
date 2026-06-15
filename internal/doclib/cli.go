package doclib

import (
	"fmt"
)

func CLITree(rootDir string) {
	tree, err := BuildTree(rootDir)
	if err != nil {
		fmt.Printf("❌ 构建文档树失败: %v\n", err)
		return
	}

	for _, w := range tree.Warnings {
		fmt.Printf("⚠️  %s\n", w)
	}

	fmt.Println(PrintTree(tree))
}

func CLIRebuildTree(rootDir string) {
	tree, err := BuildTree(rootDir)
	if err != nil {
		fmt.Printf("❌ 构建文档树失败: %v\n", err)
		return
	}

	validationErrors := ValidateTree(tree)
	if len(validationErrors) > 0 {
		for _, e := range validationErrors {
			fmt.Printf("❌ %s\n", e)
		}
	}

	if err := SaveCache(tree, rootDir); err != nil {
		fmt.Printf("❌ 保存缓存失败: %v\n", err)
		return
	}

	total := len(tree.AllDocs)
	fmt.Printf("✓ 扫描到 %d 个文档\n", total)
	if len(validationErrors) == 0 {
		fmt.Println("✓ 树结构验证通过")
	}
	cachePath := ".argus/cache/tree.json"
	fmt.Printf("✓ 已更新缓存 %s\n", cachePath)

	for _, w := range tree.Warnings {
		fmt.Printf("⚠️  %s\n", w)
	}
}

func CLICheckImpact(rootDir string, docID string) {
	tree, err := LoadCache(rootDir)
	if err != nil {
		tree, err = BuildTree(rootDir)
		if err != nil {
			fmt.Printf("❌ 无法加载文档树: %v\n", err)
			return
		}
	}

	if _, ok := tree.AllDocs[docID]; !ok {
		fmt.Printf("❌ 文档 %q 不存在\n", docID)
		return
	}

	impacted := GetImpactedDocs(tree, docID)
	if len(impacted) == 0 {
		fmt.Printf("文档 %s 没有被其他文档依赖\n", docID)
		return
	}

	fmt.Printf("文档 %s 被以下文档直接依赖：\n", docID)
	for _, id := range impacted {
		if node, ok := tree.AllDocs[id]; ok {
			reason := ""
			for _, dep := range node.Dependencies {
				if dep == docID {
					for _, exp := range tree.AllDocs[docID].Exports {
						reason = fmt.Sprintf(" (依赖原因: imports %s)", exp.Name)
						break
					}
				}
			}
			fmt.Printf("  - %s%s\n", id, reason)
		}
	}
	fmt.Println("\n建议检查这些文档是否需要更新。")
}
