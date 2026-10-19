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

// isDoneTask reports whether a task or post is completed/closed.
func isDoneTask(t *domain.Task) bool {
	return t != nil && (t.Status == domain.StatusDone || t.Status == domain.StatusClosed)
}

// visibleTasks returns m.tasks filtered by view-level visibility toggles (such as hideDone).
func (m Model) visibleTasks() []*domain.Task {
	if !m.hideDone {
		return m.tasks
	}
	out := make([]*domain.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if !isDoneTask(t) {
			out = append(out, t)
		}
	}
	return out
}

// displayRows returns the rows the list pane shows; the cursor indexes into it.
func (m Model) displayRows() []TreeRow {
	if m.treeActive() {
		return m.getTreeRows()
	}
	vis := m.visibleTasks()
	rows := make([]TreeRow, len(vis))
	for i, t := range vis {
		rows[i] = TreeRow{Task: t}
	}
	return rows
}

func (m Model) currentRow() (TreeRow, bool) {
	if m.cursor < 0 {
		return TreeRow{}, false
	}
	rows := m.displayRows()
	if m.cursor >= len(rows) {
		return TreeRow{}, false
	}
	return rows[m.cursor], true
}

func (m Model) currentTask() *domain.Task {
	if r, ok := m.currentRow(); ok {
		return r.Task
	}
	return nil
}

// reanchorCursor keeps the cursor anchored on prevID across list reloads, status
// reorders, and view-mode toggles (tree / hide-done).
func (m *Model) reanchorCursor(prevID string, prevPointer, prevGhost bool) {
	if prevID == "" {
		m.clampScroll()
		return
	}
	rows := m.displayRows()
	idx := -1
	for i, r := range rows {
		if r.Task != nil && r.Task.ID == prevID && r.Pointer == prevPointer && r.Ghost == prevGhost {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, r := range rows {
			if r.Task != nil && r.Task.ID == prevID && !r.Pointer && !r.Ghost {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		for i, r := range rows {
			if r.Task != nil && r.Task.ID == prevID {
				idx = i
				break
			}
		}
	}
	if idx >= 0 {
		m.cursor = idx
		m.clampScroll()
		return
	}
	m.detailScrollOffset = 0
	m.linkCursor = 0
	m.clampScroll()
}

// recomputeBlockers updates BlockedBy and IsBlocked in memory for any task in
// m.tasks that depends on one of changedIDs, so optimistic status changes
// immediately update downstream lock/ready indicators.
func (m *Model) recomputeBlockers(changedIDs ...string) {
	if len(changedIDs) == 0 {
		return
	}
	changed := make(map[string]bool, len(changedIDs))
	for _, id := range changedIDs {
		changed[id] = true
	}
	for _, t := range m.tasks {
		if len(t.DependsOn) == 0 {
			continue
		}
		affected := false
		for _, depID := range t.DependsOn {
			if changed[depID] {
				affected = true
				break
			}
		}
		if !affected {
			continue
		}
		prevBlocked := make(map[string]bool, len(t.BlockedBy))
		for _, b := range t.BlockedBy {
			prevBlocked[b] = true
		}
		var blockedBy []string
		for _, depID := range t.DependsOn {
			if dep := m.lookupItem(depID); dep != nil {
				if !isDoneTask(dep) {
					blockedBy = append(blockedBy, depID)
				}
			} else if prevBlocked[depID] {
				blockedBy = append(blockedBy, depID)
			}
		}
		sort.Strings(blockedBy)
		t.BlockedBy = blockedBy
		t.IsBlocked = len(blockedBy) > 0
	}
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

// getTreeRows renders the dependency DAG of the visible tasks as a tree.
//
// The graph is many-to-many, so a task with several prerequisites would appear
// under each of them: it is rendered in full the first time and as a one-line
// pointer row afterwards (also breaking cycles). When done tasks are hidden,
// paths through hidden tasks are transitively bridged (with transitive reduction)
// so remaining tasks stay connected without turning hidden in-topic tasks into ghosts.
// When the list is one topic, prerequisites and dependents living in other topics
// appear as ghost rows: prerequisites as parents of the tasks that need them,
// dependents as leaves.
func (m Model) getTreeRows() []TreeRow {
	visible := m.visibleTasks()
	if len(visible) == 0 {
		return nil
	}
	inSet := make(map[string]*domain.Task, len(visible))
	for _, t := range visible {
		inSet[t.ID] = t
	}

	hiddenByID := make(map[string]*domain.Task)
	if m.hideDone {
		for _, t := range m.tasks {
			if isDoneTask(t) {
				hiddenByID[t.ID] = t
			}
		}
		for id, t := range m.related {
			if isDoneTask(t) {
				hiddenByID[id] = t
			}
		}
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
	hasInSetPrereq := make(map[string]bool, len(visible))
	ghostPrereqs := make(map[string][]string)
	seenGhostPrereq := make(map[[2]string]bool)
	ghostDependents := make(map[string][]string)
	seenGhostDep := make(map[[2]string]bool)

	addEdge := func(parent string, child *domain.Task) {
		k := [2]string{parent, child.ID}
		if parent == child.ID || seenEdge[k] {
			return
		}
		seenEdge[k] = true
		children[parent] = append(children[parent], child)
		if _, ok := inSet[parent]; ok {
			hasInSetPrereq[child.ID] = true
		}
	}
	addGhostPrereq := func(taskID, prereqID string) {
		k := [2]string{taskID, prereqID}
		if taskID == prereqID || seenGhostPrereq[k] {
			return
		}
		seenGhostPrereq[k] = true
		ghostPrereqs[taskID] = append(ghostPrereqs[taskID], prereqID)
	}
	addGhostDep := func(taskID, depID string) {
		k := [2]string{taskID, depID}
		if taskID == depID || seenGhostDep[k] {
			return
		}
		seenGhostDep[k] = true
		ghostDependents[taskID] = append(ghostDependents[taskID], depID)
	}

	// Direct edges between visible tasks (and non-hidden ghosts when ghosts == true).
	for _, c := range visible {
		for _, dep := range c.DependsOn {
			if hiddenByID[dep] != nil {
				continue
			}
			if _, ok := inSet[dep]; ok {
				addEdge(dep, c)
			} else if ghosts {
				addEdge(dep, c)
				addGhostPrereq(c.ID, dep)
			}
		}
	}
	for _, p := range visible {
		for _, b := range p.Blocks {
			if hiddenByID[b] != nil {
				continue
			}
			if c, ok := inSet[b]; ok {
				addEdge(p.ID, c)
			} else if ghosts {
				addGhostDep(p.ID, b)
			}
		}
	}

	// If any tasks are hidden (e.g. hideDone), bridge connections across hidden nodes.
	if len(hiddenByID) > 0 {
		rawNext := make(map[string][]string)
		rawSeen := make(map[[2]string]bool)
		var startNodes []string
		seenStart := make(map[string]bool)
		addStart := func(id string) {
			if id != "" && !seenStart[id] {
				seenStart[id] = true
				startNodes = append(startNodes, id)
			}
		}
		addRaw := func(u, v string) {
			if u == "" || v == "" || u == v {
				return
			}
			k := [2]string{u, v}
			if rawSeen[k] {
				return
			}
			rawSeen[k] = true
			rawNext[u] = append(rawNext[u], v)
			addStart(u)
		}
		for _, t := range m.tasks {
			addStart(t.ID)
			for _, dep := range t.DependsOn {
				addRaw(dep, t.ID)
			}
			for _, b := range t.Blocks {
				addRaw(t.ID, b)
			}
		}
		if len(m.related) > 0 {
			relIDs := make([]string, 0, len(m.related))
			for id := range m.related {
				relIDs = append(relIDs, id)
			}
			sort.Strings(relIDs)
			for _, id := range relIDs {
				rt := m.related[id]
				if rt == nil {
					continue
				}
				for _, dep := range rt.DependsOn {
					addRaw(dep, rt.ID)
				}
				for _, b := range rt.Blocks {
					addRaw(rt.ID, b)
				}
			}
		}

		// generalReach[u][v] is true if v is reachable from u via any path of length >= 1.
		generalReach := make(map[string]map[string]bool, len(startNodes))
		for _, u := range startNodes {
			reach := make(map[string]bool)
			queue := append([]string(nil), rawNext[u]...)
			for _, nxt := range queue {
				reach[nxt] = true
			}
			for len(queue) > 0 {
				curr := queue[0]
				queue = queue[1:]
				for _, nxt := range rawNext[curr] {
					if !reach[nxt] {
						reach[nxt] = true
						queue = append(queue, nxt)
					}
				}
			}
			generalReach[u] = reach
		}

		// hiddenReach(u) returns non-hidden nodes reachable from u via >= 1 hidden nodes.
		hiddenReach := func(u string) []string {
			var out []string
			seen := make(map[string]bool)
			var queue []string
			for _, nxt := range rawNext[u] {
				if hiddenByID[nxt] != nil && !seen[nxt] {
					seen[nxt] = true
					queue = append(queue, nxt)
				}
			}
			for len(queue) > 0 {
				curr := queue[0]
				queue = queue[1:]
				for _, nxt := range rawNext[curr] {
					if seen[nxt] {
						continue
					}
					seen[nxt] = true
					if hiddenByID[nxt] != nil {
						queue = append(queue, nxt)
					} else {
						out = append(out, nxt)
					}
				}
			}
			return out
		}

		hasVisibleIntermediate := func(u, v string) bool {
			uReach := generalReach[u]
			for w := range inSet {
				if w == u || w == v {
					continue
				}
				if uReach[w] && generalReach[w][v] {
					return true
				}
			}
			return false
		}

		for _, u := range startNodes {
			if hiddenByID[u] != nil {
				continue
			}
			_, uInSet := inSet[u]
			if !uInSet && !ghosts {
				continue
			}
			targets := hiddenReach(u)
			if len(targets) == 0 {
				continue
			}
			targetSet := make(map[string]bool, len(targets))
			for _, v := range targets {
				targetSet[v] = true
			}
			// Add in-set children in visible list order so sibling ordering stays deterministic.
			for _, c := range visible {
				if !targetSet[c.ID] || hasVisibleIntermediate(u, c.ID) {
					continue
				}
				if uInSet {
					addEdge(u, c)
				} else if ghosts {
					addEdge(u, c)
					addGhostPrereq(c.ID, u)
				}
			}
			// If u is inSet and ghosts are enabled, also bridge to non-hidden ghost dependents.
			if uInSet && ghosts {
				for _, v := range targets {
					if _, vInSet := inSet[v]; vInSet {
						continue
					}
					if !hasVisibleIntermediate(u, v) {
						addGhostDep(u, v)
					}
				}
			}
		}
		visIdx := make(map[string]int, len(visible))
		for i, t := range visible {
			visIdx[t.ID] = i
		}
		for u := range children {
			kids := children[u]
			sort.SliceStable(kids, func(i, j int) bool {
				return visIdx[kids[i].ID] < visIdx[kids[j].ID]
			})
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
			for _, b := range ghostDependents[t.ID] {
				ghostKids = append(ghostKids, ghost(b))
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

	for _, t := range visible {
		if hasInSetPrereq[t.ID] {
			continue
		}
		if ghosts {
			for _, dep := range ghostPrereqs[t.ID] {
				if !visited[dep] {
					traverse(ghost(dep), true, "", true, true, 0)
				}
			}
		}
		if !visited[t.ID] {
			traverse(t, false, "", true, true, 0)
		}
	}
	// Tasks only reachable through cycles have no root; render them so nothing is lost.
	for _, t := range visible {
		if !visited[t.ID] {
			traverse(t, false, "", true, true, 0)
		}
	}
	return rows
}
