package tui

import "task_cli/internal/domain"

func (m Model) currentTask() *domain.Task {
	if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
		return nil
	}
	if m.treeViewMode {
		rows := m.getTreeRows()
		if m.cursor < len(rows) {
			return rows[m.cursor].Task
		}
	}
	return m.tasks[m.cursor]
}

func (m Model) getTreeRows() []TreeRow {
	if len(m.tasks) == 0 {
		return nil
	}
	taskMap := make(map[string]*domain.Task, len(m.tasks))
	for _, t := range m.tasks {
		taskMap[t.ID] = t
	}

	var roots []*domain.Task
	visited := make(map[string]bool)

	for _, t := range m.tasks {
		hasPrereqInSet := false
		for _, depID := range t.DependsOn {
			if _, ok := taskMap[depID]; ok {
				hasPrereqInSet = true
				break
			}
		}
		if !hasPrereqInSet {
			roots = append(roots, t)
		}
	}

	var rows []TreeRow
	var traverse func(t *domain.Task, prefix string, isLast bool, isRoot bool, branchVisited map[string]bool)
	traverse = func(t *domain.Task, prefix string, isLast bool, isRoot bool, branchVisited map[string]bool) {
		branchGlyph := "├── "
		lastGlyph := "└── "
		contGlyph := "│   "
		emptyGlyph := "    "
		if isASCIIOnly() {
			branchGlyph = "|-- "
			lastGlyph = "\\-- "
			contGlyph = "|   "
			emptyGlyph = "    "
		}

		rowPrefix := prefix
		if !isRoot {
			if isLast {
				rowPrefix += lastGlyph
			} else {
				rowPrefix += branchGlyph
			}
		}

		rows = append(rows, TreeRow{
			Task:   t,
			Prefix: rowPrefix,
			Depth:  len(branchVisited),
		})
		visited[t.ID] = true
		branchVisited[t.ID] = true

		newPrefix := prefix
		if !isRoot {
			if isLast {
				newPrefix += emptyGlyph
			} else {
				newPrefix += contGlyph
			}
		}

		var children []*domain.Task
		for _, blkID := range t.Blocks {
			if child, ok := taskMap[blkID]; ok && !visited[blkID] && !branchVisited[blkID] {
				children = append(children, child)
			}
		}

		for i, child := range children {
			childLast := (i == len(children)-1)
			childBranchVisited := make(map[string]bool, len(branchVisited)+1)
			for k, v := range branchVisited {
				childBranchVisited[k] = v
			}
			traverse(child, newPrefix, childLast, false, childBranchVisited)
		}
	}

	for _, root := range roots {
		if !visited[root.ID] {
			traverse(root, "", true, true, make(map[string]bool))
		}
	}

	for _, t := range m.tasks {
		if !visited[t.ID] {
			traverse(t, "", true, true, make(map[string]bool))
		}
	}

	return rows
}
