# AGENT WORKFLOW & TASK COORDINATION SPECIFICATION

> [!IMPORTANT]
> **MANDATORY PROTOCOL**: All AI agents, automated runners, and pair programmers operating in this repository **MUST** use `tk` for tracking tasks, claiming work, documenting progress, and attaching commit evidence. No orphan work or undocumented code modifications are permitted.

---

## 1. Core Operating Principles

1. **Every Work Item Must Have a Task**:
   - Before writing or refactoring code, find an existing task (`tk list` or `tk search`) or create one (`tk new`).
2. **Explicit Atomic Claims**:
   - Always claim the task before starting implementation: `tk claim <ID> --actor "<agent-name>"`.
   - Never overwrite another agent's in-progress claim unless explicitly asked or coordinated using `--force`.
3. **Transparent Progress & Handoffs**:
   - Document technical decisions, blockers, or milestones directly into task notes using `tk edit <ID> -m "..."`.
4. **Verified Completion with Commit Evidence**:
   - Once implementation passes tests (`go test -race ./...`), mark the task done with `tk done <ID>`.
   - `tk done` automatically anchors the current Git HEAD commit SHA and diff summary into the tasklog.

---

## 2. Agent Command Reference

### Discovering & Steering Work
```bash
# Find the next actionable, unblocked task (steers agents to ready work)
tk next
tk next --json

# Render hierarchical dependency DAG tree
tk tree

# View only ready tasks whose prerequisites are complete
tk list --ready

# View only tasks blocked by unfinished prerequisites
tk list --blocked

# List all tasks in current repository (ordered by status, priority, and ID)
tk list

# View open or in-progress tasks
tk list -s open,in_progress

# Search using boolean DSL
tk search "priority:urgent OR (bug AND status:open)"

# Inspect full task details, notes, prerequisites, blockers, and attached Git diffs
tk show <ID>
```

### Claiming & Linking Work
```bash
# Create a new scoped task (optionally link prerequisites via --after)
tk new -t "Descriptive task title" -p urgent -l "backend,storage" --after T-1 -d "Detailed spec..."

# Atomically claim a task as your agent identity
tk claim <ID> --actor "agent-antigravity"

# Link dependencies (T-2 depends on T-1; T-1 blocks T-2)
tk link T-2 T-1
tk unlink T-2 T-1

# Inspect dependency blockers and dependents for a task
tk dep show <ID>
```

### Updating & Documenting Progress
```bash
# Append progress notes or milestone logs
tk edit <ID> -m "Implemented atomic backup mechanism; running test suite."

# Add or remove tags
tk edit <ID> --add-tag "tested" --remove-tag "blocked"

# Reassign if delegating or handing off
tk edit <ID> --assignee "reviewer-agent"
```

### Completing Tasks
```bash
# Complete task and automatically attach HEAD commit evidence
tk done <ID>

# Or complete with an explicit commit SHA
tk done <ID> -c abc1234
```

### Disaster Recovery & Undo
```bash
# List recoverable deleted tasks from tombstone archive
tk restore -l

# Restore an accidentally deleted task with full git diff and notes intact
tk restore <ID>

# Create an atomic rolling backup snapshot
tk backup create [optional-name]

# List available point-in-time snapshots
tk backup -l
```

### Real-Time Monitoring & Automation
```bash
# Stream live repository task mutations and see latest active backlog
tk watch

# Use --json on inspection commands for reliable structured parsing:
tk list --json
tk show <ID> --json
tk restore -l --json
tk backup -l --json
```

---

## 3. Storage & Backup Safety Invariants

- Repository tasks are persisted in `.tasks/tasks.db` (WAL mode enabled).
- Backups and atomic snapshots are stored in `.tasks/backups/`.
- Deleted tasks are archived to `task_tombstones` and can be retrieved using `tk restore <ID>`.
- Never delete or manually overwrite `.tasks/tasks.db` directly. Always use `tk` CLI commands or `tk backup` for state inspection and recovery.
