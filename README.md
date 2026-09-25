# tk — Kernel-Style Task & Patch Management

`tk` is a lightning-fast, socket-based local task management CLI and TUI written in Go. Built for developers who live in terminals and git trees, `tk` couples issue tracking directly to Linux kernel-style git diffs and commits, backed by an auto-spawning background daemon and pure-Go SQLite.

```
                     ┌───────────────────────────────────────┐
                     │          tk CLI / TUI Client          │
                     │  (Cobra CLI / Bubble Tea Dumb Term)   │
                     └──────────────────┬────────────────────┘
                                        │
                         Unix Socket    │  TCP (Optional)
                   /run/user/<uid>/tk.sock │  e.g. 127.0.0.1:4567
                                        ▼
                     ┌───────────────────────────────────────┐
                     │          tk Socket Daemon             │
                     │    (Auto-spawns on first demand)      │
                     └──────────┬─────────────────┬──────────┘
                                │                 │
                       WAL Mode │                 │ WAL Mode
                                ▼                 ▼
                 ┌────────────────────┐     ┌──────────────────────┐
                 │ Local Repo DB      │     │ Global DB            │
                 │ .tasks/tasks.db    │     │ ~/.local/share/      │
                 │ (Scope: T-1, T-2)  │     │ task_cli/global.db   │
                 └────────────────────┘     │ (Scope: G-1, G-2)    │
                                            └──────────────────────┘
```

---

## Key Features

- **Linux Kernel-Style Git Binding**: Automatically attaches commit hashes, commit subjects, diffstat summaries, and unified patch diffs to tasks upon completion or on demand.
- **Auto-Spawning Socket Daemon**: The client transparently launches the background daemon via `setsid` if not already running—zero manual service configuration required.
- **Dual Scope Isolation**:
  - **Local (`T-<id>`)**: Tasks stored in `<repo_root>/.tasks/tasks.db` tied strictly to the current git repository.
  - **Global (`G-<id>`)**: General tasks stored in `$XDG_DATA_HOME/task_cli/global.db` accessible anywhere with `-g` / `--global`.
- **Pure-Go Embedded SQLite**: Backed by `modernc.org/sqlite` (no CGO required) with WAL journal mode, busy timeouts, and single-writer concurrency.
- **Fast Dumb-Terminal TUI**: Interactive Bubble Tea interface with vim navigation, instant search filtering, diff toggles, and direct `$EDITOR` integration.
- **Real-Time Event Streaming**: Daemon broadcasts task lifecycle events over socket connections; stream live with `tk watch` or pipe JSON into tools.
- **Script & LLM Automation**: Global `--json` flag on all inspection commands outputs structured JSON payloads.

---

## Installation & Quickstart

### Build from Source

```bash
# Clone and build
git clone <repo-url> task_cli
cd task_cli
go build -o tk cmd/tk/main.go

# Install to $GOPATH/bin or /usr/local/bin
go install ./cmd/tk
```

### Quickstart Walkthrough

```bash
# 1. Create a task in current git repo (opens $EDITOR if flags omitted)
tk new -t "Refactor authentication handler" -p high -l auth,security

# 2. View active tasks in formatted table
tk list

# 3. Inspect task details, notes, and attached changes
tk show T-1

# 4. Work on your codebase and commit changes
git commit -am "fix(auth): sanitize JWT claims before signature check"

# 5. Mark task done (automatically snaps HEAD commit, diffstat, and diff)
tk done T-1

# 6. Launch interactive TUI (when run inside an interactive terminal)
tk
```

---

## CLI Cheat Sheet

| Command | Aliases | Arguments & Flags | Description | Example |
|---------|:-------:|-------------------|-------------|---------|
| `tk` | — | — | Launch interactive TUI in terminal; prints help if non-TTY | `tk` |
| `tk new` | — | `[-t title] [-p prio] [-l tags] [-a assignee] [-m desc]` | Create task via flags, piped stdin, or `$EDITOR` template | `tk new -t "Fix bug" -p high -l ui`<br>`echo "Piped Task" \| tk new` |
| `tk tree` | `dag` | `[task-id] [-s status] [-r/--reverse] [--all] [--json]` | Render hierarchical tree of task dependencies (`├──`, `└──`, `🔒 BLOCKED`, `⚡ READY`) | `tk tree`<br>`tk tree T-1`<br>`tk tree -r` (breakdown)<br>`tk tree --json` |
| `tk dep` | `deps` | `[add\|rm\|show\|tree]` | Manage task dependencies, blocker links, and inspect prerequisites | `tk dep add T-2 T-1`<br>`tk dep rm T-2 T-1`<br>`tk dep show T-2` |
| `tk link` | — | `<task-id> <prereq-id...>` | Link a task to depend on prerequisite tasks (alias for `tk dep add`) | `tk link T-2 T-1` |
| `tk unlink` | — | `<task-id> <prereq-id...>` | Unlink a task from prerequisite tasks (alias for `tk dep rm`) | `tk unlink T-2 T-1` |
| `tk next` | — | `[--json]` | Identify the next actionable unblocked task to steer developers and AI agents | `tk next`<br>`tk next --json` |
| `tk list` | `ls` | `[-s status] [-p prio] [-t/-l tag] [-a assignee] [-q search] [--tree] [--ready] [--blocked] [--after id] [--blocks id] [--sort order] [--short] [--limit N]` | Query tasks (supports tree view, unblocked ready tasks, and blockers) | `tk list -s open -p high`<br>`tk list --tree`<br>`tk list --ready`<br>`tk list --blocked` |
| `tk search` | `find` | `<query...> [-s status] [-p prio] [-l tag] [-a assignee] [--short] [--limit N]` | Search tasks with boolean DSL (`AND`, `OR`, `NOT`, `tag:`, `s:`, `p:`, `a:`, `"exact"`) | `tk search bug AND odin`<br>`tk search tag:security priority:high`<br>`tk search "memory leak" NOT closed` |
| `tk show` | — | `<id>` | Display task metadata, prerequisites, blockers, notes, commit SHA, and diff | `tk show T-1`<br>`tk show G-2 --json` |
| `tk claim` | — | `<id> [--actor actor] [--force]` | Atomically claim a task for the current actor; prevents race conditions | `tk claim T-1`<br>`tk claim T-1 --actor bot-1` |
| `tk takeover` | `take` | `<id> [-m message] [--actor actor] [--force]` | Take over a task from another actor or an orphaned task with attribution | `tk takeover T-1 -m "Taking over"`<br>`tk take T-1` |
| `tk transfer` | — | `<id> [new-assignee] [--to assignee] [-m note]` | Hand off and transfer task ownership to another actor | `tk transfer T-1 agent-gamma -m "Handing off"`<br>`tk transfer T-1 --to bob` |
| `tk orphan` | — | `[id] [--older-than dur] [--all] [--dry-run] [-l/--list]` | Detect and mark abandoned tasks as orphaned; drops assignee and resets to open (default: 1h) | `tk orphan T-1`<br>`tk orphan reap --dry-run`<br>`tk orphan --all`<br>`tk orphan list` |
| `tk edit` | — | `<id> [-t title] [-s status] [-p prio] [-l tags] [--add-tag tag] [--remove-tag tag] [--claim] [-a assignee] [-m/-n note] [-d desc]` | Modify task fields via CLI flags, atomically add/remove tags, or open `$EDITOR` | `tk edit T-1 -s in_progress`<br>`tk edit T-1 --add-tag review`<br>`tk edit T-1 --claim`<br>`tk edit T-1 -n "New note"` |
| `tk done` | `close`, `resolve` | `<id> [-c commit] [--diff] [--no-commit] [-f/--force]` | Mark task done; enforces prerequisite completion check (override with `-f`) | `tk done T-1`<br>`tk done T-1 --diff`<br>`tk done T-1 -f` |
| `tk attach` | — | `<id> [commit-ref] [--diff]` | Snapshot git commit or uncommitted working tree diff into task | `tk attach T-1 HEAD`<br>`tk attach T-1 --diff` |
| `tk delete` | `rm` | `<id> [-y/--yes] [--force]` | Remove a task to the tombstone archive (prompts unless `-y` or `--force`) | `tk delete T-1`<br>`tk delete -y T-1`<br>`tk rm --force G-4` |
| `tk restore` | `undo-delete` | `<id> [-l/--list]` | Restore deleted task from tombstone archive or list recoverable tasks | `tk restore T-1`<br>`tk restore -l` |
| `tk backup` | — | `[list\|create\|verify\|restore] [-l] [-f]` | Manage atomic SQLite snapshots and disaster recovery | `tk backup -l`<br>`tk backup create`<br>`tk backup verify` |
| `tk watch` | — | `[--json]` | Stream real-time task lifecycle events from socket daemon | `tk watch`<br>`tk watch --json \| jq .` |
| `tk daemon run` | — | `[--socket path] [--tcp addr]` | Run daemon process in foreground (debugging / headless / remote) | `tk daemon run --tcp 127.0.0.1:4567` |
| `tk daemon status` | — | — | Ping daemon health and report active socket path | `tk daemon status` |
| `tk daemon stop` | — | — | Transmit graceful shutdown signal to running daemon | `tk daemon stop` |

### Global Flags

- `-g, --global`: Operate on the global task database (`~/.local/share/task_cli/global.db`) instead of the current git repository.
- `--actor <name>`: Explicit actor identity for task attribution (overrides `TK_ACTOR`, `TK_AUTHOR`, and git author).
- `--no-pager`: Disable automatic conditional pager (`less -RFX`) for long outputs.
- `--json`: Output machine-readable JSON for scripting, CI/CD, or LLM agent pipelines.

---

## TUI Keybindings

Running `tk` without subcommands inside any standard terminal opens the interactive Bubble Tea TUI:

| Keybinding | Action | Description |
|:----------:|--------|-------------|
| `Tab` / `Shift+Tab` | Toggle Active Pane | Switch focus between Tasks List (left) and Details & Messages (right) |
| `l` / `→` | Focus Details Pane | Move focus from task list into details/messages pane |
| `h` / `←` | Focus Tasks List | Return focus from details pane back to task list |
| `>` / `.` / `Alt+→` | Widen Table Pane | Move split divider right (widen task list/tree, narrow details) |
| `<` / `,` / `Alt+←` | Narrow Table Pane | Move split divider left (narrow task list/tree, widen details) |
| `=` | Reset Split Ratio | Reset pane split to default (48/52 normal, 56/44 in Tree mode) |
| `t` | Toggle Tree Mode | Toggle between standard flat tabular list and hierarchical DAG tree view |
| `L` | Link Dependency Modal | Open interactive modal to make the current task depend on a prerequisite task ID |
| `j` / `k` / `↓` / `↑` | Move / Scroll | In **List**: navigate task selection; in **Details**: scroll message content line-by-line |
| `g` / `G` | Top / Bottom | In **List**: jump to first/last task; in **Details**: jump to top/bottom of notes |
| `Ctrl+D` / `Ctrl+U` / `J` / `K` | Fast Page Scroll | Page scroll details, notes, and patch diff content in split view |
| `Space` | Multi-Select Toggle | Toggle task selection (`●`/`○` marker) and advance cursor down |
| `v` | Select / Deselect | Toggle task selection for current row without advancing cursor |
| `V` | Select All / Deselect | Toggle selection across all currently visible tasks |
| `u` | Multi-Level Undo | Revert recent mutations (restores deleted tasks, claims, done, status) |
| `d` | Reversible / Batch Done | Mark task(s) done (enforces prerequisite blockers; warns if blocked) |
| `s` | Status / Batch Toggle | Cycle status between `open` and `in_progress` for selected tasks |
| `a` | Claim / Batch Claim | Atomically claim selected task(s) for current actor |
| `x` | Delete / Batch Delete | Prompt for confirmation (`[y/N]`) and remove selected tasks to tombstones |
| `n` | New Task | Launch `$EDITOR` with a new task template (supports `--after` to link prerequisites) |
| `e` | Edit Task | Launch `$EDITOR` with selected task pre-populated |
| `Enter` | Toggle Diff | Expand or collapse unified patch diff view |
| `/` | Search Filter | Enter live search query (`Enter` applies, `Esc` cancels) |
| `Esc` / `c` | Reset Filter / Deselect | In **Details**: return to list; in **List**: clear selection or search filter |
| `S` | Toggle Scope | Switch view between `local` (git repository) and `global` scopes |
| `r` | Refresh | Re-query tasks from daemon |
| `q` / `Ctrl+C` | Quit | Exit the TUI |

---

## Configuration & Environment Variables

`tk` requires zero configuration files by default. Behavior can be tailored using standard environment variables:

| Variable | Default Value | Purpose |
|----------|---------------|---------|
| `TK_ACTOR` | *(unset)* | Primary actor identity for task author, claim, and note attribution |
| `TK_AUTHOR` | *(unset)* | Secondary actor identity fallback before git author |
| `TK_PAGER` | `less -RFX` | Pager command for `tk list`, `tk search`, and `tk show` when output exceeds screen (falls back to `BAT_PAGER` -> `PAGER`) |
| `TK_NO_PAGER` | *(unset)* | Set to `1` or `true` to disable automatic paging completely |
| `TK_SOCKET` | `/run/user/<uid>/tk.sock`<br>*(fallback: `~/.local/share/task_cli/tk.sock`)* | Unix Domain Socket path used for client-daemon IPC communication |
| `TK_ADDR` | *(unset)* | Optional TCP address (`host:port`) to bind or connect to (e.g. `127.0.0.1:4567` for SSH tunneling or containers) |
| `EDITOR` | `$VISUAL` -> `nvim` | Text editor invoked when creating or editing tasks interactively |
| `VISUAL` | `nvim` | Fallback editor if `EDITOR` is unset |
| `XDG_DATA_HOME` | `~/.local/share` | Root directory for application state and the global SQLite database (`$XDG_DATA_HOME/task_cli/global.db`) |

---

## Storage & Data Locations

- **Local Repository Database**: Stored at `<repo_root>/.tasks/tasks.db`. You can commit `.tasks/` to version control or add it to `.gitignore` based on team preference.
- **Global Database**: Stored at `~/.local/share/task_cli/global.db` (or `$XDG_DATA_HOME/task_cli/global.db`).
- **Daemon Socket**: Default `/run/user/<uid>/tk.sock` (in-memory `tmpfs` on modern Linux) ensuring minimal disk I/O.
