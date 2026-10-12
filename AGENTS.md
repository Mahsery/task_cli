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
   - Once implementation passes the full quality gate (`make check`, see §4), commit, then mark the task done with `tk done <ID>`.
   - `tk done` automatically anchors the current Git HEAD commit SHA and diff summary into the tasklog.
5. **Install & Migrate After Significant Changes**:
   - Always run `make install` after significant modifications so that the binary is built with version ldflags (`tk version`), installed to `~/.local/bin/tk`, and any running daemon is cleanly restarted with migrations applied.

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

# Filter tasks or posts specifically, or use convenience shortcuts
tk list --tasks
tk list --posts
tk list -k post
tk tasks @hero-system
tk posts @bugs

# Search using boolean DSL
tk search "priority:urgent OR (bug AND status:open)"

# Inspect full task details, notes, prerequisites, blockers, and attached Git diffs
tk show <ID>
```

### Claiming & Linking Work
```bash
# Create a new scoped task (optionally specify topic via -T and link prerequisites via --after)
tk new -t "Descriptive task title" -p urgent -T backend --after T-1 -d "Detailed spec..."

# Forgiving IDs: 't15', 'T15', 't-15', or '15' normalize automatically to 'T-15' (or 'G-15' in global)
tk show 15
tk claim 15 --actor "agent-antigravity"

# Link dependencies (T-2 depends on T-1; T-1 blocks T-2)
tk link T-2 T-1
tk unlink T-2 T-1

# Inspect dependency blockers and dependents for a task
tk dep show <ID>
```

### Updating & Documenting Progress
```bash
# Append progress notes or milestone logs (supports Obsidian-style [[T-15]] wikilinks)
tk edit <ID> -m "Implemented atomic backup mechanism; relates to [[T-12#L40]]."

# Move task(s) or post(s) to another topic
tk move <ID> <topic>

# Reassign if delegating or handing off
tk edit <ID> --assignee "reviewer-agent"
```

### Stale Tasks, Orphaning & Handover
```bash
# Take over a task from another actor or claim an orphaned task with a reason
tk takeover <ID> -m "Taking over implementation for sprint"
tk take <ID> -m "Assignee unresponsive; taking over"

# Hand off / transfer a task directly to another actor
tk transfer <ID> <new-actor> -m "Handing off database work"

# List orphaned tasks or stale candidates
tk list --orphaned
tk orphan list
tk orphan list --older-than 30m

# Preview or reap stale tasks inactive past threshold (default: 1h; checks commits, updates, and dependencies)
tk orphan reap --dry-run
tk orphan reap --older-than 2h

# Manually orphan an abandoned task (drops assignee, resets to open, tags 'orphaned')
tk orphan <ID> -m "Agent timed out"
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

# Restore whole database from latest backup (central vault or local)
tk restore --db
tk restore --db latest -f
tk db restore latest -f
tk backup restore latest -f

# Create an atomic rolling backup snapshot (mirrored in central vault)
tk backup create [optional-name]
tk db backup [optional-name]

# List available point-in-time snapshots across local & central vaults
tk backup -l
tk db list
```

### Daemon Management
```bash
# Start the daemon in the background (detached session)
tk daemon start

# Restart running daemon in background
tk daemon restart

# Gracefully stop background daemon
tk daemon stop

# Check daemon status and socket connectivity
tk daemon status
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
- Backups and atomic snapshots are stored both locally in `.tasks/backups/` and mirrored centrally in `~/.local/share/task_cli/backups/repos/<slug>/`.
- If `.tasks` is accidentally deleted, the running daemon auto-resurrects it to disk from live memory, or restores from the central backup vault if restarting fresh.
- `tk restore --db` (or `tk db restore`) safely restores repository state from the central vault at any time.
- Deleted tasks are archived to `task_tombstones` and can be retrieved using `tk restore <ID>`.
- Never delete or manually overwrite `.tasks/tasks.db` directly. Always use `tk` CLI commands, `tk db`, or `tk backup` for state inspection and recovery.

---

## 4. Code Quality Gate (MANDATORY)

> [!IMPORTANT]
> Zero tolerance: every commit must be gofmt/goimports-clean and produce **zero** findings under the strict
> `golangci-lint` configuration in `.golangci.yml`. Lint findings are treated as build errors, not suggestions.

```bash
make fmt     # rewrite: gofmt + goimports (local imports grouped under task_cli)
make check   # REQUIRED before every commit / tk done: fmt-check + golangci-lint + go vet + go test -race
make hooks   # install the pre-commit hook that enforces fmt-check + lint locally
```

Rules:
- **Fix, don't silence.** Handle every error (`errcheck`): propagate it, or discard explicitly with `_ =` only
  where failure is genuinely irrelevant (e.g. closing a read-only file, rolling back a committed tx).
  Errors from closing written files must be returned.
- **`//nolint` is a last resort**: it must name the specific linter and carry a reason
  (`//nolint:gosec // G201: only fixed column names are interpolated`). Bare or unexplained `//nolint` fails lint.
- **Never loosen `.golangci.yml`** to get a change through. Adding an exclusion requires a justification
  comment in the config and should be discussed with the user first.
- Wrap errors with `%w` and compare with `errors.Is` / `errors.As` (`errorlint`).
- New behaviour needs tests that assert the specification, not the current implementation.
- Linter version is pinned in the `Makefile` (`GOLANGCI_LINT_VERSION`); `make tools` installs it.
