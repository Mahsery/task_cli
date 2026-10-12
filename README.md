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

### Build & Install

```bash
# Clone repository
git clone <repo-url> task_cli
cd task_cli

# Build binary locally with embedded version ldflags
make build

# Install to ~/.local/bin/tk, restart background daemon, and verify migration
make install
```

### Quickstart Walkthrough

```bash
# 1. Create a task in current git repo (opens $EDITOR if flags omitted)
tk new -t "Refactor authentication handler" -p high -T security

# 2. View active tasks in formatted table
tk list

# 3. Inspect task details, notes, and attached changes (forgiving IDs: '1', 't1', 'T-1')
tk show 1

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
| `tk` | — | — | Launch interactive 3-pane TUI (Topic sidebar, tasks/board, details & links) | `tk` |
| `tk topic` | `topics` | `[tree\|new\|ls\|mv\|rm\|use\|import-tags]` | Organise work into a tree of topics (boards for posts, task topics, groups) | `tk topic tree`<br>`tk topic new "Bugs" -k board`<br>`tk topic import-tags` |
| `tk post` | — | `<topic> [title] [-m desc]` | Create a board post (P-n) in a board topic (e.g. bug report or idea) | `tk post bugs "UI glitch on resize"` |
| `tk reply` | — | `<post-id> [message]` | Add a discussion reply/note to a board post | `tk reply P-1 "Reproduced on Linux"` |
| `tk promote` | — | `<post-id> [tasks-topic]` | Promote a board post into a full task with dependency tracking | `tk promote P-1 hero-system` |
| `tk move` | `mv` | `<id...> <topic>` | Move task(s) or post(s) into another topic | `tk move T-1 T-2 @engine` |
| `tk links` | — | `[id]` | Inspect outgoing Obsidian-style wikilinks and incoming backlinks | `tk links T-1` |
| `tk go` | — | `<target>` | Resolve and jump to a wikilink target (task, note, line anchor, or topic) | `tk go "T-1#L12"` |
| `tk new` | — | `[-t title] [-p prio] [-T topic] [-a assignee] [-m desc]` | Create task via flags, piped stdin, or `$EDITOR` template | `tk new -t "Fix bug" -p high -T bugs`<br>`echo "Piped Task" \| tk new` |
| `tk tree` | `dag` | `[task-id\|@topic] [-s status] [-r/--reverse] [--all] [--json]` | Render hierarchical tree of task dependencies with `↪` pointers and `⇢` ghost rows | `tk tree`<br>`tk tree @hero-system`<br>`tk tree -r` (breakdown)<br>`tk tree --json` |
| `tk dep` | `deps` | `[add\|rm\|show\|tree]` | Manage task dependencies, blocker links, and inspect prerequisites | `tk dep add T-2 T-1`<br>`tk dep rm T-2 T-1`<br>`tk dep show T-2` |
| `tk link` | — | `<task-id> <prereq-id...>` | Link a task to depend on prerequisite tasks (alias for `tk dep add`) | `tk link T-2 T-1` |
| `tk unlink` | — | `<task-id> <prereq-id...>` | Unlink a task from prerequisite tasks (alias for `tk dep rm`) | `tk unlink T-2 T-1` |
| `tk next` | — | `[--json]` | Identify the next actionable unblocked task to steer developers and AI agents | `tk next`<br>`tk next --json` |
| `tk list` | `ls` | `[@topic] [-s status] [-p prio] [-T topic] [-a assignee] [-q search] [--tree] [--ready] [--blocked] [--after id] [--blocks id] [--sort order] [--short] [--limit N]` | Query tasks & posts (supports tree view, unblocked ready tasks, and blockers) | `tk list @backend`<br>`tk list --tree`<br>`tk list --ready`<br>`tk list --blocked` |
| `tk search` | `find` | `<query...> [-s status] [-p prio] [-a assignee] [--short] [--limit N]` | Search items with boolean DSL (`AND`, `OR`, `NOT`, `topic:`, `kind:`, `links:`, `linkedfrom:`, `s:`, `p:`) | `tk search topic:bugs AND urgent`<br>`tk search links:T-1`<br>`tk search "memory leak" NOT closed` |
| `tk show` | — | `<id>` | Display metadata, links, backlinks, notes, commit SHA, and diff (forgiving IDs: `1`, `t1`, `T-1`) | `tk show 1`<br>`tk show G-2 --json` |
| `tk claim` | — | `<id> [--actor actor] [--force]` | Atomically claim a task for the current actor; prevents race conditions | `tk claim 1`<br>`tk claim T-1 --actor bot-1` |
| `tk takeover` | `take` | `<id> [-m message] [--actor actor] [--force]` | Take over a task from another actor or an orphaned task with attribution | `tk takeover 1 -m "Taking over"`<br>`tk take T-1` |
| `tk transfer` | — | `<id> [new-assignee] [--to assignee] [-m note]` | Hand off and transfer task ownership to another actor | `tk transfer 1 agent-gamma -m "Handing off"`<br>`tk transfer T-1 --to bob` |
| `tk orphan` | — | `[id] [--older-than dur] [--all] [--dry-run] [-l/--list]` | Detect and mark abandoned tasks as orphaned; drops assignee and resets to open (default: 1h) | `tk orphan T-1`<br>`tk orphan reap --dry-run`<br>`tk orphan --all`<br>`tk orphan list` |
| `tk edit` | — | `<id> [-t title] [-s status] [-p prio] [--claim] [-a assignee] [-m/-n note] [-d desc]` | Modify task fields via CLI flags, append notes/wikilinks, or open `$EDITOR` | `tk edit T-1 -s in_progress`<br>`tk edit T-1 -m "Relates to [[T-5]]"`<br>`tk edit T-1 --claim` |
| `tk done` | `close`, `resolve` | `<id> [-c commit] [--diff] [--no-commit] [-f/--force]` | Mark task done; enforces prerequisite completion check (override with `-f`) | `tk done T-1`<br>`tk done T-1 --diff`<br>`tk done T-1 -f` |
| `tk attach` | — | `<id> [commit-ref] [--diff]` | Snapshot git commit or uncommitted working tree diff into task | `tk attach T-1 HEAD`<br>`tk attach T-1 --diff` |
| `tk delete` | `rm` | `<id> [-y/--yes] [--force]` | Remove a task to the tombstone archive (prompts unless `-y` or `--force`) | `tk delete T-1`<br>`tk delete -y T-1`<br>`tk rm --force G-4` |
| `tk restore` | `undo-delete` | `<id> [-l/--list] [--db] [-f]` | Restore deleted task from tombstone archive, or restore database with `--db` | `tk restore T-1`<br>`tk restore -l`<br>`tk restore --db latest -f` |
| `tk backup` | `db` | `[list\|create\|verify\|restore] [-l] [-f]` | Manage atomic SQLite snapshots, central backup vault, and disaster recovery | `tk backup -l`<br>`tk backup create`<br>`tk db restore latest -f` |
| `tk watch` | — | `[--json]` | Stream real-time task lifecycle events from socket daemon | `tk watch`<br>`tk watch --json \| jq .` |
| `tk daemon start` | — | `[--socket path] [--tcp addr]` | Launch daemon process in the background (detached session) | `tk daemon start` |
| `tk daemon restart` | — | `[--socket path] [--tcp addr]` | Restart background daemon process | `tk daemon restart` |
| `tk daemon run` | — | `[--socket path] [--tcp addr]` | Run daemon process in foreground (debugging / headless / remote) | `tk daemon run --tcp 127.0.0.1:4567` |
| `tk daemon status` | — | — | Ping daemon health and report active socket path | `tk daemon status` |
| `tk daemon stop` | — | — | Transmit graceful shutdown signal to running daemon | `tk daemon stop` |

### Global Flags

- `-g, --global`: Operate on the global task database (`~/.local/share/task_cli/global.db`) instead of the current git repository.
- `--actor <name>`: Explicit actor identity for task attribution (overrides `TK_ACTOR`, `TK_AUTHOR`, and git author).
- `--no-pager`: Disable automatic conditional pager (`less -RFX`) for long outputs.
- `--json`: Output machine-readable JSON for scripting, CI/CD, or LLM agent pipelines.

---

## TUI Keybindings & Vim Motions

Running `tk` without subcommands inside any standard terminal opens the interactive Bubble Tea TUI. Wide terminals display a 3-pane layout: **Topic Sidebar** (left), **Item / Tree View** (middle), and **Details & Links** (right).

| Keybinding | Action | Description |
|:----------:|--------|-------------|
| `Tab` / `Shift+Tab` | Cycle Active Pane | Switch focus between Topic Sidebar, Item List, and Details Pane |
| `h` / `l` / `←` / `→` | Navigate Panes | Move focus horizontally between panes |
| `j` / `k` / `↓` / `↑` | Move / Scroll | In **Sidebar**: select topic; in **List**: navigate items; in **Details**: scroll content |
| `gg` / `G` | Top / Bottom | Jump to top or bottom of the active pane |
| `Ctrl+D` / `Ctrl+U` / `J` / `K` | Fast Page Scroll | Page scroll content in split view |
| `gt` | Fuzzy Topic Picker | Open fuzzy topic search modal (`Tab` inside picker toggles repo vs global topics) |
| `]t` / `[t` | Cycle Topics | Jump to next or previous topic in the tree |
| `]l` / `[l` | Link Cursor | Move cursor to next / previous `[[...]]` wikilink in the details pane |
| `Enter` / `gf` | Follow Wikilink | Follow focused wikilink to target task, note anchor, or topic (`{n}gf` jumps to link #n) |
| `gb` / `gB` | Jump History | Navigate backward / forward through navigation history stack |
| `gl` | Insert Link Modal | Open modal to select an item and append an Obsidian-style wikilink `[[...]]` |
| `M` | Move Items / Topic | In **List**: move selected items to another topic; in **Sidebar**: move topic under a new parent |
| `p` | Promote Post | In **List**: promote selected board post (`P-n`) into a task (`T-n`) |
| `N` / `R` / `D` | Topic Management | In **Sidebar**: `N` creates topic, `R` renames topic, `D` deletes topic |
| `t` | Toggle Tree Mode | In **Tasks topic**: toggle between flat list and hierarchical DAG graph tree |
| `L` | Link Dependency Modal | Open interactive modal to add a prerequisite dependency |
| `Space` | Multi-Select Toggle | Toggle item selection (`●`/`○` marker) and advance cursor down |
| `v` / `V` | Select / Select All | Toggle item selection / select all visible items |
| `u` | Multi-Level Undo | Revert recent mutations (restores deleted tasks, claims, done, status) |
| `d` | Done / Batch Done | Mark task(s) done / resolve post(s) (enforces prerequisite blockers) |
| `s` | Status / Batch Toggle | Cycle status between `open` and `in_progress` |
| `a` | Claim / Batch Claim | Atomically claim selected task(s) for current actor |
| `x` | Delete / Batch Delete | Prompt for confirmation (`[y/N]`) and remove selected items |
| `n` | New Item | Create task in tasks topic or post in board topic |
| `e` | Edit Item | Launch `$EDITOR` with selected item pre-populated |
| `/` | Search Filter | Enter live search query (`Enter` applies, `Esc` cancels) |
| `Esc` / `c` | Clear Filter / Selection | Return focus to list, clear active search filter, or deselect all items |
| `S` | Toggle Scope | Switch view between `local` (git repository) and `global` scopes |
| `r` | Refresh | Re-query tasks and topics from daemon |
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
