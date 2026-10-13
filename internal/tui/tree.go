package tui

import (
	"sort"

	"task_cli/internal/domain"
)

// treeActive reports whether the list renders the dependency tree. Board topics
// hold posts, which have no dependencies, so they always render as a flat list.
func (m Model) treeActive() bool {
	return m.treeViewMode && !m.isBoardView()
}

// showGhosts reports whether cross-topic dependencies are rendered as ghost rows:
// only when the list is restricted to one topic and no search narrows it further.
func (m Model) showGhosts() bool {
	return m.topicPath != "" && m.topicPath != domain.UnfiledTopic && m.filterText == ""
}

// displayRows returns the rows the list pane shows; the cursor indexes into it.
func (m Model) displayRows() []TreeRow {
	if m.treeActive() {
		return m.getTreeRows()
	}
	rows := make([]TreeRow, len(m.tasks))
	for i, t := range m.tasks {
		rows[i] = TreeRow{Task: t}
	}
	return rows
}

func (m Model) currentTask() *domain.Task {
	if m.cursor < 0 {
		return nil
	}
	if m.treeActive() {
		rows := m.getTreeRows()
		if m.cursor < len(rows) {
			return rows[m.cursor].Task
		}
		return nil
	}
	if m.cursor >= len(m.tasks) {
		return nil
	}
	return m.tasks[m.cursor]
}

// lookupItem finds an item by ID among the listed tasks and the related (out-of-list) items.
func (m Model) lookupItem(id string) *domain.Task {
	if m.taskByID != nil {
		if t, ok := m.taskByID[id]; ok {
			return t
		}
	} else {
		for _, t := range m.tasks {
			if t.ID == id {
				return t
			}
		}
	}
	if t, ok := m.related[id]; ok {
		return t
	}
	return nil
}

// sortBoard orders board posts by most recent activity first.
func sortBoard(tasks []*domain.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].LastActivityTime().After(tasks[j].LastActivityTime())
	})
}

// getTreeRows renders the dependency DAG of the listed tasks as a tree.
//
// The graph is many-to-many, so a task with several prerequisites would appear
// under each of them: it is rendered in full the first time and as a one-line
// pointer row afterwards (also breaking cycles). When the list is one topic,
// prerequisites and dependents living in other topics appear as ghost rows:
// prerequisites as parents of the tasks that need them, dependents as leaves.
func (m Model) getTreeRows() []TreeRow {
	if len(m.tasks) == 0 {
		return nil
	}
	inSet := make(map[string]*domain.Task, len(m.tasks))
	for _, t := range m.tasks {
		inSet[t.ID] = t
	}
	ghosts := m.showGhosts()
	ghost := func(id string) *domain.Task {
		if t, ok := m.related[id]; ok && t != nil {
			return t
		}
		return &domain.Task{ID: id, Title: "(not loaded)"}
	}

	// children[id] lists the in-set dependents of id (listed or ghost prerequisite), in list order.
	children := make(map[string][]*domain.Task)
	seenEdge := make(map[[2]string]bool)
	addEdge := func(parent string, child *domain.Task) {
		k := [2]string{parent, child.ID}
		if parent == child.ID || seenEdge[k] {
			return
		}
		seenEdge[k] = true
		children[parent] = append(children[parent], child)
	}
	for _, c := range m.tasks {
		for _, dep := range c.DependsOn {
			if _, ok := inSet[dep]; ok || ghosts {
				addEdge(dep, c)
			}
		}
	}
	for _, p := range m.tasks {
		for _, b := range p.Blocks {
			if c, ok := inSet[b]; ok {
				addEdge(p.ID, c)
			}
		}
	}

	branch, last, cont, empty := "├── ", "└── ", "│   ", "    "
	if isASCIIOnly() {
		branch, last, cont = "|-- ", "\\-- ", "|   "
	}

	var rows []TreeRow
	visited := make(map[string]bool)
	var traverse func(t *domain.Task, isGhost bool, prefix string, isLast, isRoot bool, depth int)
	traverse = func(t *domain.Task, isGhost bool, prefix string, isLast, isRoot bool, depth int) {
		rowPrefix := prefix
		childPrefix := prefix
		if !isRoot {
			if isLast {
				rowPrefix += last
				childPrefix += empty
			} else {
				rowPrefix += branch
				childPrefix += cont
			}
		}
		if visited[t.ID] {
			rows = append(rows, TreeRow{Task: t, Prefix: rowPrefix, Depth: depth, Pointer: true, Ghost: isGhost})
			return
		}
		visited[t.ID] = true
		rows = append(rows, TreeRow{Task: t, Prefix: rowPrefix, Depth: depth, Ghost: isGhost})

		kids := children[t.ID]
		var ghostKids []*domain.Task
		if ghosts && !isGhost {
			for _, b := range t.Blocks {
				if _, ok := inSet[b]; !ok {
					ghostKids = append(ghostKids, ghost(b))
				}
			}
		}
		n := len(kids) + len(ghostKids)
		for i, c := range kids {
			traverse(c, false, childPrefix, i == n-1, false, depth+1)
		}
		for i, g := range ghostKids {
			// Ghost dependents are leaves: their own subtree belongs to their topic.
			p := childPrefix + branch
			if len(kids)+i == n-1 {
				p = childPrefix + last
			}
			rows = append(rows, TreeRow{Task: g, Prefix: p, Depth: depth + 1, Ghost: true})
		}
	}

	hasListedPrereq := func(t *domain.Task) bool {
		for _, dep := range t.DependsOn {
			if _, ok := inSet[dep]; ok {
				return true
			}
		}
		return false
	}
	for _, t := range m.tasks {
		if hasListedPrereq(t) {
			continue
		}
		if ghosts {
			for _, dep := range t.DependsOn {
				if _, ok := inSet[dep]; !ok && !visited[dep] {
					traverse(ghost(dep), true, "", true, true, 0)
				}
			}
		}
		if !visited[t.ID] {
			traverse(t, false, "", true, true, 0)
		}
	}
	// Tasks only reachable through cycles have no root; render them so nothing is lost.
	for _, t := range m.tasks {
		if !visited[t.ID] {
			traverse(t, false, "", true, true, 0)
		}
	}
	return rows
}
