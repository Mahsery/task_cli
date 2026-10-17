# AGENT WORKFLOW & TASK COORDINATION SPECIFICATION

> [!IMPORTANT]
> **MANDATORY PROTOCOL**: All AI agents, automated runners, and pair programmers operating in this repository **MUST** use `tk` for tracking tasks, claiming work, documenting progress, and attaching commit evidence. No orphan work or undocumented code modifications are permitted.

---

## 1. Core Operating Principles

1. **Every Work Item Must Have a Task**:
   - Before writing or refactoring code, find an existing task (`tk list` or `tk search`) or create one (`tk new -t "..." -T <topic>`).
2. **Explicit Atomic Claims**:
   - Always claim the task before starting implementation: `tk claim <ID> --actor "<agent-name>"`.
   - Never overwrite another agent's in-progress claim unless explicitly asked or coordinated using `--force`.
3. **Transparent Progress & Handoffs**:
   - Document technical decisions, blockers, or milestones directly into task notes using `tk edit <ID> -m "..."` (supports `[[T-12#L40]]` wikilinks).
4. **Verified Completion with Commit Evidence**:
   - Once implementation passes the full quality gate (`make check`, see §4), commit, then mark the task done with `tk done <ID>`.
   - `tk done` automatically anchors the current Git HEAD commit SHA, message, and diff summary into the task.
5. **Install & Migrate After Significant Changes**:
   - Always run `make install` after significant modifications so that the binary is built with version ldflags (`tk version`), installed to `~/.local/bin/tk`, and any running daemon is cleanly restarted with migrations applied.

---

## 2. Agent Command Reference

| Action | Command | Notes |
| :--- | :--- | :--- |
| **Status Overview** | `tk status` | High-signal repo overview (open, active, orphaned, ready, blocked) |
| **Next Actionable** | `tk next [-q\|--json]` | Highest-priority unblocked task ready for work (`-q` prints only ID) |
| **Dependency Tree** | `tk tree [@topic] [-r]` | DAG graph; `-r` shows prerequisite breakdown |
| **List Backlog** | `tk list [--ready\|--blocked\|--tasks\|--posts\|--ids]` | Filterable table; supports `-s open,in_progress`, `@topic`, `-q`, `--ids` |
| **Inspect Details** | `tk show <ID> [-n]` | Full spec, notes, diffs, backlinks; `-n` numbers lines for anchors |
| **Create Task** | `tk new [-q] -t "Title" -p urgent -T <topic> [--after <ID>]` | Accepts `-d` description, positional title, or piped stdin; `-q` prints only ID |
| **Create Board Post**| `tk post [-q] -t "Title" -T <board> [-d "Body"]` | Untracked bug report or idea (P-n); `-q` prints only ID |
| **Reply / Note** | `tk reply <ID> -m "Comment text"` | Appends discussion note (supports `[[wiki links]]` and piped stdin) |
| **Promote Post** | `tk promote [-q] <post-id> --to <tasks-topic>` | Promotes P-n to T-n task with bidirectional link |
| **Claim Work** | `tk claim [-q] <ID> [--actor <name>]` | Atomic assignment; transitions `open` → `in_progress` |
| **Start & Branch** | `tk start [ID] [-b] [--no-checkout]` | Claims task, binds branch `tk/t-<id>-<slug>`, optional checkout |
| **Branch Context** | `tk branch [ID] [-c] [-b <branch>] [-q]` | Shows task bound to current Git branch, or binds/checks out `<ID>`'s branch |
| **Takeover / Steer**| `tk takeover <ID> -m "Reason" [--actor <name>]` | Takes over task; clears orphan status and audits reason |
| **Transfer** | `tk transfer <ID> <new-actor> -m "Handoff note"` | Reassigns task with audit note |
| **Manage Deps** | `tk link <ID> <prereq...>` / `tk unlink <ID> <prereq...>` | Declares `<ID>` depends on `<prereq...>` |
| **Inspect Deps** | `tk dep show <ID>` | Lists prerequisite blockers and dependent tasks |
| **Move Topic** | `tk move <ID...> --to <topic\|unfiled>` | Moves items to topic; dependencies/links preserved |
| **Wikilinks & Jump**| `tk links <ID>` / `tk go <ID> [n]` | `tk links` lists links/backlinks; `tk go <ID> 2` jumps to link #2 |
| **Progress Note** | `tk edit <ID> -m "Progress note [[T-5#n1]]"` | Appends chronological note to task |
| **Attach Commits** | `tk attach [ID] [ref\|A..B] [--scan [range]]` | Attaches single commit, commit range (`main..HEAD`), or trailer-scanned commits |
| **Export Patches** | `tk patch [ID] [-o dir] [--stdout]` | Exports attached commits or diff as a `git format-patch` / `git am` series |
| **Git Hooks** | `tk hooks [install\|uninstall\|status]` | Installs non-destructive `prepare-commit-msg` & `post-commit` hooks |
| **GitHub PR / Sync**| `tk pr [create\|status] [ID]` / `tk gh [import\|push]` | Zero-config GitHub PR creation/status and Issue import/push via `gh` CLI |
| **Complete Task** | `tk done [ID] [-c commit] [--diff] [-f] [-q]` | Resolves task + snaps HEAD SHA/diff. `-f` overrides blocked deps |
| **Orphan Stale** | `tk orphan reap --older-than 1h [--dry-run]` | Drops unresponsive assignee, resets to open, sets orphaned_at |
| **Undo / Restore** | `tk restore <ID>` / `tk restore -l` | Recovers deleted task from tombstone archive |
| **Portable Snapshot**| `tk snapshot <path>` / `tk export <path>` | Writes standalone WAL-free SQLite file (`VACUUM INTO`) or JSON export |
| **Disaster Recovery**| `tk db restore latest -f` / `tk db backup` | Atomic point-in-time recovery via central vault |
| **Daemon Control** | `tk daemon [status\|start\|restart\|stop]` | Controls background socket service (supports `--socket`, `--tcp`, `--db`) |
| **Live Stream** | `tk watch [--json]` | Live event stream (terminal UI or JSON event pipe) |

*Forgiving IDs: `t15`, `T15`, `t-15`, or `15` automatically resolve to `T-15` (or `G-15` in global mode `-g`).*

---

## 3. Storage & Safety Invariants

- **Storage**: Local repo tasks reside in `.tasks/tasks.db` (SQLite WAL mode). Global tasks (`-g`) in `~/.local/share/task_cli/global.db`.
- **Vault Mirroring**: Snapshots are saved to `.tasks/backups/` and mirrored centrally in `~/.local/share/task_cli/backups/repos/<slug>/`.
- **Resurrection**: If `.tasks/` is deleted, the active daemon auto-resurrects the database from memory or recovers from the central vault.
- **Tombstones**: `tk delete <ID>` moves items to `task_tombstones`. Undelete at any time via `tk restore <ID>`. Never delete SQLite DBs manually.

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
