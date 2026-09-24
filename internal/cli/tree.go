package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var (
	treeStatus  string
	treeReverse bool
	treeAll     bool
)

// TreeNode represents a node in the tree hierarchy for JSON and rendering.
type TreeNode struct {
	Task     *domain.Task `json:"task"`
	Children []*TreeNode  `json:"children,omitempty"`
}

var treeCmd = &cobra.Command{
	Use:     "tree [task-id] [flags]",
	Aliases: []string{"dag"},
	Short:   "Render hierarchical dependency tree of tasks",
	Long: `Display tasks in a hierarchical tree view based on dependency relationships.
By default, shows execution order (root tasks flow into dependent tasks that they unblock).
Use --reverse to view prerequisite breakdown (tasks flow into the prerequisites they require).`,
	Example: `  tk tree                      # View full dependency tree
  tk tree T-1                  # View subtree rooted at T-1
  tk tree -r                   # Reverse view: tasks -> required prerequisites
  tk tree -s open,in_progress  # View active tasks only
  tk tree --json               # Export hierarchical tree as JSON`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var rootID string
		if len(args) > 0 {
			rootID = args[0]
		}

		filter := domain.TaskFilter{
			Scope:    scope,
			RepoPath: repoRoot,
		}
		if treeStatus != "" {
			s, err := domain.ParseStatus(treeStatus)
			if err != nil {
				return err
			}
			filter.Status = &s
		}

		tasks, err := taskClient.ListTasks(filter)
		if err != nil {
			return err
		}

		if len(tasks) == 0 {
			fmt.Println("No tasks found.")
			return nil
		}

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
			roots = append(roots, buildSubtree(rootTask, taskMap, treeReverse, make(map[string]bool), make(map[string]bool)))
		} else {
			roots = buildForest(tasks, taskMap, treeReverse)
		}

		if jsonFlag {
			return outputJSON(roots)
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Task Dependency Tree (%s scope)\n", scope))
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
			renderTreeNode(&sb, root, "", isLast, true, IsASCIIOnly(), treeReverse)
		}

		return PrintWithPager(sb.String())
	},
}

func buildForest(tasks []*domain.Task, taskMap map[string]*domain.Task, reverse bool) []*TreeNode {
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
			node := buildSubtree(t, taskMap, reverse, branchVisited, visited)
			roots = append(roots, node)
		}
	}

	// Any unvisited tasks (e.g. disconnected components) get added as top-level roots
	for _, t := range tasks {
		if !visited[t.ID] {
			branchVisited := make(map[string]bool)
			roots = append(roots, buildSubtree(t, taskMap, reverse, branchVisited, visited))
		}
	}

	return roots
}

func buildSubtree(task *domain.Task, taskMap map[string]*domain.Task, reverse bool, branchVisited map[string]bool, allVisited map[string]bool) *TreeNode {
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
			continue
		}
		// Prevent cycle loops in branch rendering
		if branchVisited[cid] {
			continue
		}
		childBranchVisited := make(map[string]bool, len(branchVisited)+1)
		for k, v := range branchVisited {
			childBranchVisited[k] = v
		}
		childNode := buildSubtree(childTask, taskMap, reverse, childBranchVisited, allVisited)
		node.Children = append(node.Children, childNode)
	}

	return node
}

func renderTreeNode(sb *strings.Builder, node *TreeNode, prefix string, isLast bool, isRoot bool, ascii bool, reverse bool) {
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
	if len(t.Tags) > 0 {
		tagsStr = fmt.Sprintf(" [%s]", strings.Join(t.Tags, ", "))
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
		renderTreeNode(sb, child, newPrefix, childLast, false, ascii, reverse)
	}
}

func init() {
	treeCmd.Flags().StringVarP(&treeStatus, "status", "s", "", "Filter tasks by status (open, in_progress, done, closed)")
	treeCmd.Flags().BoolVarP(&treeReverse, "reverse", "r", false, "Show reverse tree (prerequisite breakdown)")
	treeCmd.Flags().BoolVarP(&treeAll, "all", "a", false, "Include closed and done tasks")

	// Add 'tree' as subcommand of depCmd too: 'tk dep tree'
	depCmd.AddCommand(treeCmd)

	// Add 'tk tree' as top-level command
	RootCmd.AddCommand(treeCmd)
}
