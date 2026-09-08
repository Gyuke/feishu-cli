package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/riba2534/feishu-cli/internal/client"
	"github.com/riba2534/feishu-cli/internal/wikisync"
)

// resolveQueryRoots 解析一个同步任务的入口根节点集合。
//
// 单节点任务（wiki_url）返回一个根；space 任务（space_id）枚举整个知识库的
// 全部顶层节点（parent="" 即 space 根）作为多个根。返回结果供 collectWikiForest
// 统一走遍历。pull 与 reconcile 共用本函数，保证两者对同一任务的枚举范围一致。
func resolveQueryRoots(q *wikisync.Query, userAccessToken string) ([]*client.WikiNode, error) {
	if q.SpaceID != "" {
		roots, err := listAllWikiChildren(q.SpaceID, "", userAccessToken)
		if err != nil {
			return nil, fmt.Errorf("枚举知识空间 %s 顶层节点失败: %w", q.SpaceID, err)
		}
		return roots, nil
	}

	rootToken, err := extractWikiToken(q.WikiURL)
	if err != nil {
		return nil, err
	}
	root, err := client.GetWikiNode(rootToken, userAccessToken)
	if err != nil {
		return nil, fmt.Errorf("获取根节点失败: %w", err)
	}
	return []*client.WikiNode{root}, nil
}

// collectWikiForest 由一组入口根节点递归枚举出全部待导出节点及其本地路径。
//
//   - 单根（len==1）：委托 collectWikiTree，维持现有单节点布局（根落 local_dir 顶、
//     子文档平铺），不破坏旧任务。
//   - 多根（space 任务）：每个顶层根按结构嵌套——有子节点 → <local_dir>/<标题>/<标题>.md，
//     其子文档挂在该子目录下；叶子 → <local_dir>/<标题>.md。这样镜像知识库目录树，
//     不同一级文档的文件彼此隔离，避免同名子文档相互覆盖。
func collectWikiForest(roots []*client.WikiNode, outputDir string, maxDepth int, userAccessToken string) ([]treeJob, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	if len(roots) == 1 {
		return collectWikiTree(roots[0], outputDir, maxDepth, userAccessToken)
	}

	var jobs []treeJob
	for _, root := range roots {
		rootName := sanitizeWikiTitle(root.Title)
		if root.HasChild {
			mirrorDir := filepath.Join(outputDir, rootName)
			jobs = append(jobs, treeJob{Node: root, OutputPath: filepath.Join(mirrorDir, rootName+".md")})
			if err := walkChildren(root, mirrorDir, 1, maxDepth, userAccessToken, &jobs); err != nil {
				return nil, fmt.Errorf("收集 %q 的子节点失败: %w", root.Title, err)
			}
		} else {
			jobs = append(jobs, treeJob{Node: root, OutputPath: filepath.Join(outputDir, rootName+".md")})
		}
	}
	return jobs, nil
}
