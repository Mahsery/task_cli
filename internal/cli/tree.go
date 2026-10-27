package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var (
	treeStatus   string
	treeReverse  bool
	treeAll      bool
	treeHideDone bool
	treeTopic    string
)

// TreeNode represents a node in the tree hierarchy for JSON and rendering.
//
// Dependencies form a graph (a task may unblock many tasks and depend on many),
// so a task reachable along several paths is rendered in full once; later
// occurrences are Pointer nodes. When a topic is selected, related tasks that
// live in other topics appear as Ghost leaf nodes, and roots list their
// out-of-topic prerequisites in After.
type TreeNode struct {
	Task     *domain.Task `json:"task"`
	Children []*TreeNode  `json:"children,omitempty"`
	Pointer  bool         `json:"pointer,omitempty"`
	Ghost    bool         `json:"ghost,omitempty"`
	After    []string     `json:"after,omitempty"`
}

// ghostLookup resolves a task outside the rendered set (nil disables ghost nodes).
type ghostLookup func(id string) *domain.Task

var treeCmd = &cobra.Command{
	Use:     "tree [task-id|@topic] [flags]",
	Aliases: []string{"dag"},
	Short:   "Render hierarchical dependency tree of tasks",
	Long: `Display tasks in a hierarchical tree view based on dependency relationships.
By default, shows execution order (root tasks flow into dependent tasks that they unblock).
Use --reverse to view prerequisite breakdown (tasks flow into the prerequisites they require).
Use --hide-done (-H) to hide completed tasks while preserving transitive dependency connections.`,
	Example: `  tk tree                      # View full dependency tree
  tk tree -H                   # Hide done tasks while bridging remaining connections
  tk tree T-1                  # View subtree rooted at T-1
  tk tree -r                   # Reverse view: tasks -> required prerequisites
  tk tree -s open,in_progress  # View active tasks only
  tk tree @hero-system         # One topic; cross-topic deps shown as ⇢ ghost nodes
  tk tree --json               # Export hierarchical tree as JSON`,
	RunE: func(_ *cobra.Command, args []string) error {
		var rootID string
		if len(args) > 0 {
			if strings.HasPrefix(args[0], "@") {
				treeTopic = args[0]
			} else {
				rootID = normID(args[0])
			}
		}

		baseFilter := domain.TaskFilter{
			Scope:    scope,
			RepoPath: repoRoot,
			Topic:    treeTopic,
			Kind:     domain.KindTask,
		}
		filter := baseFilter
		if err := applyStatusFilter(&filter, treeStatus); err != nil {
			return err
		}
		hideDone := treeHideDone && !treeAll

		allTasks, err := taskClient.ListTasks(baseFilter)
		if err != nil {
			return err
		}

		tasks := allTasks
		if treeStatus != "" {
			tasks, err = taskClient.ListTasks(filter)
			if err != nil {
				return err
			}
		}
		if hideDone {
			filtered := make([]*domain.Task, 0, len(tasks))
			for _, t := range tasks {
				if t.Status != domain.StatusDone && t.Status != domain.StatusClosed {
					filtered = append(filtered, t)
				}
			}
			tasks = filtered
		}

		var lookup ghostLookup
		if treeTopic != "" {
			cache := map[string]*domain.Task{}
			lookup = func(id string) *domain.Task {
				t, ok := cache[id]
				if !ok {
					var getErr error
					t, getErr = taskClient.GetTask(id)
					if getErr != nil {
						t = nil
					}
					cache[id] = t
				}
				if t != nil && hideDone && (t.Status == domain.StatusDone || t.Status == domain.StatusClosed) {
					return nil
				}
				return t
			}
		}

		if len(tasks) == 0 {
			fmt.Println("No tasks found.")
			return nil
		}

		tasks = bridgeHiddenDependencies(tasks, allTasks)

		taskMap := make(map[string]*domain.Task, len(tasks))
		for _, t := range tasks {
			taskMap[t.ID] = t
		}

		var roots []*TreeNode
		if rootID != "" {
			rootTask, ok := taskMap[rootID]
			if !ok {
				// Try fetching directly
				rt, err := taskClient.GetTask(rootID)
				if err != nil {
					return fmt.Errorf("task %s not found: %w", rootID, err)
				}
				rootTask = rt
				taskMap[rootID] = rt
			}
			roots = append(roots, buildSubtree(rootTask, taskMap, treeReverse, make(map[string]bool), make(map[string]bool), lookup))
		} else {
			roots = buildForestWith(tasks, taskMap, treeReverse, lookup)
		}

		if jsonFlag {
			return outputJSON(roots)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Task Dependency Tree (%s scope)\n", scope)
		if treeTopic != "" {
			fmt.Fprintf(&sb, "Topic: @%s  (⇢ = task in another topic, ↪ = shown above)\n", domain.NormalizeTopicRef(treeTopic))
		}
		if treeReverse {
			sb.WriteString("Mode: Prerequisite Breakdown (Task -> Needs)\n")
		} else {
			sb.WriteString("Mode: Execution Flow (Prerequisite -> Unblocks)\n")
		}
		sep := "─"
		if IsASCIIOnly() {
			sep = "-"
		}
		sb.WriteString(strings.Repeat(sep, 80) + "\n\n")

		for i, root := range roots {
			isLast := i == len(roots)-1
			renderTreeNode(&sb, root, "", isLast, true, IsASCIIOnly())
		}

		return PrintWithPager(sb.String())
	},
}

// bridgeHiddenDependencies rewrites DependsOn and Blocks on a cloned slice of visible
// tasks so paths across hidden tasks in allTasks are transitively bridged (with
// transitive reduction) and hidden in-topic tasks are excluded from ghost lookups.
func bridgeHiddenDependencies(visible, allTasks []*domain.Task) []*domain.Task {
	if len(visible) == 0 || len(allTasks) <= len(visible) {
		return visible
	}
	inSet := make(map[string]bool, len(visible))
	for _, t := range visible {
		inSet[t.ID] = true
	}
	hiddenSet := make(map[string]*domain.Task)
	for _, t := range allTasks {
		if !inSet[t.ID] {
			hiddenSet[t.ID] = t
		}
	}
	if len(hiddenSet) == 0 {
		return visible
	}

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
	for _, t := range allTasks {
		addStart(t.ID)
		for _, dep := range t.DependsOn {
			addRaw(dep, t.ID)
		}
		for _, b := range t.Blocks {
			addRaw(t.ID, b)
		}
	}

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

	hiddenReach := func(u string) []string {
		var out []string
		seen := make(map[string]bool)
		var queue []string
		for _, nxt := range rawNext[u] {
			if hiddenSet[nxt] != nil && !seen[nxt] {
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
				if hiddenSet[nxt] != nil {
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

	cloned := make([]*domain.Task, len(visible))
	byID := make(map[string]*domain.Task, len(visible))
	depsAdded := make(map[[2]string]bool)
	blocksAdded := make(map[[2]string]bool)

	for i, orig := range visible {
		cp := *orig
		cp.DependsOn = nil
		cp.Blocks = nil
		for _, dep := range orig.DependsOn {
			if hiddenSet[dep] == nil {
				cp.DependsOn = append(cp.DependsOn, dep)
				depsAdded[[2]string{cp.ID, dep}] = true
			}
		}
		for _, blk := range orig.Blocks {
			if hiddenSet[blk] == nil {
				cp.Blocks = append(cp.Blocks, blk)
				blocksAdded[[2]string{cp.ID, blk}] = true
			}
		}
		cloned[i] = &cp
		byID[cp.ID] = &cp
	}

	for _, u := range startNodes {
		if hiddenSet[u] != nil {
			continue
		}
		targets := hiddenReach(u)
		for _, v := range targets {
			if hiddenSet[v] != nil || hasVisibleIntermediate(u, v) {
				continue
			}
			if uTask := byID[u]; uTask != nil {
				k := [2]string{u, v}
				if !blocksAdded[k] {
					blocksAdded[k] = true
					uTask.Blocks = append(uTask.Blocks, v)
				}
			}
			if vTask := byID[v]; vTask != nil {
				k := [2]string{v, u}
				if !depsAdded[k] {
					depsAdded[k] = true
					vTask.DependsOn = append(vTask.DependsOn, u)
				}
			}
		}
	}

	return cloned
}

func buildForest(tasks []*domain.Task, taskMap map[string]*domain.Task, reverse bool) []*TreeNode {
	return buildForestWith(tasks, taskMap, reverse, nil)
}

func buildForestWith(tasks []*domain.Task, taskMap map[string]*domain.Task, reverse bool, ghost ghostLookup) []*TreeNode {
	var roots []*TreeNode
	visited := make(map[string]bool)

	// Identify roots:
	// If !reverse (execution flow): root has NO DependsOn in taskMap.
	// If reverse (prerequisite breakdown): root has NO Blocks in taskMap.
	for _, t := range tasks {
		isRoot := false
		if !reverse {
			hasPrereqInSet := false
			for _, depID := range t.DependsOn {
				if _, ok := taskMap[depID]; ok {
					hasPrereqInSet = true
					break
				}
			}
			if !hasPrereqInSet {
				isRoot = true
			}
		} else {
			hasDependentInSet := false
			for _, blkID := range t.Blocks {
				if _, ok := taskMap[blkID]; ok {
					hasDependentInSet = true
					break
				}
			}
			if !hasDependentInSet {
				isRoot = true
			}
		}

		if isRoot && !visited[t.ID] {
			branchVisited := make(map[string]bool)
			node := buildSubtree(t, taskMap, reverse, branchVisited, visited, ghost)
			roots = append(roots, node)
		}
	}

	// Any unvisited tasks (e.g. disconnected components) get added as top-level roots
	for _, t := range tasks {
		if !visited[t.ID] {
			branchVisited := make(map[string]bool)
			roots = append(roots, buildSubtree(t, taskMap, reverse, branchVisited, visited, ghost))
		}
	}

	// Roots whose upstream lives in other topics: name those tasks.
	if ghost != nil {
		for _, r := range roots {
			upstream := r.Task.DependsOn
			if reverse {
				upstream = r.Task.Blocks
			}
			for _, id := range upstream {
				if _, in := taskMap[id]; in {
					continue
				}
				label := id
				if g := ghost(id); g != nil && g.TopicPath != "" {
					label += " @" + g.TopicPath
				}
				r.After = append(r.After, label)
			}
		}
	}

	return roots
}

func buildSubtree(task *domain.Task, taskMap map[string]*domain.Task, reverse bool, branchVisited, allVisited map[string]bool, ghost ghostLookup) *TreeNode {
	node := &TreeNode{Task: task}
	branchVisited[task.ID] = true
	allVisited[task.ID] = true

	var childIDs []string
	if !reverse {
		childIDs = task.Blocks
	} else {
		childIDs = task.DependsOn
	}

	for _, cid := range childIDs {
		childTask, ok := taskMap[cid]
		if !ok {
			if ghost != nil {
				if g := ghost(cid); g != nil {
					node.Children = append(node.Children, &TreeNode{Task: g, Ghost: true})
				}
			}
			continue
		}
		// Prevent cycle loops in branch rendering
		if branchVisited[cid] {
			continue
		}
		// Already rendered via another parent: point at it instead of repeating the subtree.
		if allVisited[cid] {
			node.Children = append(node.Children, &TreeNode{Task: childTask, Pointer: true})
			continue
		}
		childBranchVisited := make(map[string]bool, len(branchVisited)+1)
		for k, v := range branchVisited {
			childBranchVisited[k] = v
		}
		childNode := buildSubtree(childTask, taskMap, reverse, childBranchVisited, allVisited, ghost)
		node.Children = append(node.Children, childNode)
	}

	return node
}

func renderTreeNode(sb *strings.Builder, node *TreeNode, prefix string, isLast, isRoot, ascii bool) {
	branchGlyph := "├── "
	lastGlyph := "└── "
	contGlyph := "│   "
	emptyGlyph := "    "

	if ascii {
		branchGlyph = "|-- "
		lastGlyph = "\\-- "
		contGlyph = "|   "
		emptyGlyph = "    "
	}

	linePrefix := prefix
	if !isRoot {
		if isLast {
			linePrefix += lastGlyph
		} else {
			linePrefix += branchGlyph
		}
	}

	t := node.Task
	if node.Pointer || node.Ghost {
		glyph, suffix := "↪", "(shown above)"
		if ascii {
			glyph = "->"
		}
		if node.Ghost {
			glyph, suffix = "⇢", fmt.Sprintf("[%s] @%s", strings.ToUpper(t.StatusLabel()), t.TopicPath)
			if t.TopicPath == "" {
				suffix = fmt.Sprintf("[%s] (unfiled)", strings.ToUpper(t.StatusLabel()))
			}
			if ascii {
				glyph = "=>"
			}
		}
		fmt.Fprintf(sb, "%s%s %s %s %s\n", linePrefix, glyph, t.ID, t.Title, suffix)
		return
	}
	statusBadge := fmt.Sprintf("[%s]", strings.ToUpper(string(t.Status)))
	blockBadge := ""
	if t.IsBlocked {
		if ascii {
			blockBadge = fmt.Sprintf(" [BLOCKED by %s]", strings.Join(t.BlockedBy, ","))
		} else {
			blockBadge = fmt.Sprintf(" 🔒 [BLOCKED by %s]", strings.Join(t.BlockedBy, ","))
		}
	} else if t.Status != domain.StatusDone && t.Status != domain.StatusClosed {
		if len(t.DependsOn) > 0 {
			if ascii {
				blockBadge = " [READY]"
			} else {
				blockBadge = " ⚡ [READY]"
			}
		}
	}

	tagsStr := ""
	if len(node.After) > 0 {
		arrow := "⇠"
		if ascii {
			arrow = "<="
		}
		tagsStr = fmt.Sprintf(" %s after %s", arrow, strings.Join(node.After, ", "))
	}

	prioStr := fmt.Sprintf("(%s)", t.Priority)
	if t.Priority == domain.PriorityUrgent {
		prioStr = "(!URGENT!)"
	}

	line := fmt.Sprintf("%s%s %-11s %-8s %s%s%s\n",
		linePrefix, t.ID, statusBadge, prioStr, t.Title, tagsStr, blockBadge)
	sb.WriteString(line)

	newPrefix := prefix
	if !isRoot {
		if isLast {
			newPrefix += emptyGlyph
		} else {
			newPrefix += contGlyph
		}
	}

	for i, child := range node.Children {
		childLast := i == len(node.Children)-1
		renderTreeNode(sb, child, newPrefix, childLast, false, ascii)
	}
}

func init() {
	treeCmd.Flags().StringVarP(&treeStatus, "status", "s", "", "Filter tasks by status, comma-separated for multiple (open, in_progress, done, closed, orphan)")
	treeCmd.Flags().BoolVarP(&treeReverse, "reverse", "r", false, "Show reverse tree (prerequisite breakdown)")
	treeCmd.Flags().BoolVarP(&treeAll, "all", "a", false, "Include closed and done tasks")
	treeCmd.Flags().BoolVar(&treeAll, "include-done", false, "Include closed and done tasks (alias for -a)")
	treeCmd.Flags().BoolVarP(&treeHideDone, "hide-done", "H", false, "Hide completed (done and closed) tasks while bridging remaining connections")
	treeCmd.Flags().StringVarP(&treeTopic, "topic", "T", "", "Only tasks in this topic; related tasks elsewhere appear as ghost nodes")

	// Add 'tree' as subcommand of depCmd too: 'tk dep tree'
	depCmd.AddCommand(treeCmd)

	// Add 'tk tree' as top-level command
	RootCmd.AddCommand(treeCmd)
}
