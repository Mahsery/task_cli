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

Both build a static binary (`CGO_ENABLED=0`), so it runs on any Linux regardless of glibc version. A plain
`go build` / `go install` follows your Go environment instead; use `CGO_ENABLED=0 go install ./cmd/tk` for the same
result.

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
| `tk status` | — | `[--json]` | Repository status overview (counts, ready, blocked, orphans) | `tk status` |
| `tk topic` | `topics` | `[new\|tree\|show\|rename\|edit\|mv\|rm\|use\|import-tags]` | Organise work into a tree of topics (boards for posts, task topics, groups) | `tk topic tree`<br>`tk topic new "Bugs" -k board`<br>`tk topic import-tags` |
| `tk post` | — | `[title] [-t title] [-T topic] [-d body] [-p prio] [-q]` | Create a board post (P-n) in a board topic (`-q` prints only the ID) | `tk post -t "UI glitch" -T bugs`<br>`tk post -q "Crash on start"` |
| `tk reply` | `comment` | `[id] [-m <message>] [-q]` | Add a note/comment to a post or task (accepts `-m`/`-n` or piped stdin) | `tk reply P-1 -m "Reproduced on Linux"` |
| `tk promote` | — | `<post-id> [--to <tasks-topic>] [-q]` | Turn board post into a task with bidirectional links (`-q` prints task ID) | `tk promote P-1 --to hero-system` |
| `tk move` | `mv` | `<id...\|@topic...> --to <topic\|unfiled\|root>` | Move tasks/posts to topic, or reparent topic | `tk move T-1 T-2 --to @engine`<br>`tk move @bugs --to root` |
| `tk links` | — | `<id>` | Inspect outgoing Obsidian-style wikilinks and incoming backlinks | `tk links T-1` |
| `tk go` | — | `<id> [n]` | Follow link #n of an item (omitting n lists links) | `tk go T-1 1` |
| `tk new` | — | `[title] [-t title] [-p prio] [-T topic] [-a user] [-d desc] [--after id] [-q]` | Create task via flags, positional title, stdin pipe, or `$EDITOR` (`-q` prints only ID) | `tk new -t "Fix auth" -p high -T bugs`<br>`id=$(tk new -q "Fix auth")` |
| `tk next` | — | `[-q] [--json]` | Identify the next actionable unblocked task (`-q` prints only task ID) | `tk next`<br>`tk next -q` |
| `tk tree` | `dag` | `[task-id\|@topic] [-s status] [-r/--reverse] [-a/--all] [--json]` | Render hierarchical tree of task dependencies with `↪` pointers and `⇢` ghost rows | `tk tree @backend`<br>`tk tree -r`<br>`tk tree --json` |
| `tk dep` | `deps` | `[add\|rm\|show\|tree]` | Manage task dependencies, blocker links, and inspect prerequisites | `tk dep add T-2 T-1`<br>`tk dep rm T-2 T-1`<br>`tk dep show T-2` |
| `tk link` | — | `<task-id> <prereq-id...>` | Link a task to depend on prerequisite tasks (alias for `tk dep add`) | `tk link T-2 T-1` |
| `tk unlink` | — | `<task-id> <prereq-id...>` | Unlink a task from prerequisite tasks (alias for `tk dep rm`) | `tk unlink T-2 T-1` |
| `tk list` | `ls` | `[@topic] [-s status] [-p prio] [-T topic] [-k kind] [--tasks] [--posts] [-a user] [-q search] [--ready] [--blocked] [--short] [--ids]` | Query tasks & posts in formatted table, one-liners (`--short`), or bare IDs (`--ids`) | `tk list @backend --ready`<br>`tk ls -s open,in_progress --ids` |
| `tk tasks` | — | `[@topic] [flags]` | List execution tasks only (shortcut for `tk list --tasks`) | `tk tasks @hero-system` |
| `tk posts` | — | `[@topic] [flags]` | List board posts only (shortcut for `tk list --posts`) | `tk posts @bugs` |
| `tk search` | `find` | `<query...> [-s status] [-p prio] [-a user] [--short] [--limit N]` | Search items with boolean DSL (`AND`, `OR`, `NOT`, `topic:`, `kind:`, `links:`, `s:`, `p:`) | `tk search "mem leak" AND urgent`<br>`tk search links:T-1` |
| `tk show` | — | `<id> [-n/--lines] [--json]` | Display metadata, links, backlinks, notes, commit SHA, and diff; `-n` numbers lines | `tk show 1 -n`<br>`tk show G-2 --json` |
| `tk claim` | — | `<id> [--actor actor] [--force] [-q]` | Atomically claim a task for the current actor; prevents race conditions | `tk claim 1`<br>`tk claim T-1 --actor bot-1` |
| `tk start` | — | `<id> [-b/--branch] [--no-checkout] [--actor actor] [-q]` | Claim task, bind git branch (`tk/t-<id>-<slug>`), and checkout branch | `tk start T-1`<br>`tk start T-1 -b feat/custom` |
| `tk branch` | — | `[id] [-c/--checkout] [-b/--set branch] [-q]` | Show task bound to current git branch, or inspect/checkout a task's branch | `tk branch`<br>`tk branch T-1 -c` |
| `tk takeover` | `take` | `<id> -m <message> [--actor actor] [--force]` | Take over a task from another actor or an orphaned task with attribution | `tk takeover 1 -m "Taking over"`<br>`tk take T-1` |
| `tk transfer` | — | `<id> [new-actor] [--to actor] [-m note]` | Hand off and transfer task ownership to another actor | `tk transfer 1 agent-gamma -m "Handing off"` |
| `tk orphan` | — | `[id] [--older-than dur] [--all] [--dry-run] [-l/--list]` | Detect and mark abandoned tasks as orphaned; drops assignee and resets to open | `tk orphan reap --dry-run`<br>`tk orphan list` |
| `tk edit` | — | `[id] [-t title] [-s status] [-p prio] [-T topic] [--claim] [-a user] [-m note] [-d desc] [-q]` | Modify task fields via CLI flags, append notes/wikilinks, or open `$EDITOR` | `tk edit T-1 -s in_progress -m "Note"` |
| `tk done` | `close`, `resolve` | `[id] [-c commit] [--scan] [--diff] [--no-commit] [-f/--force] [-q]` | Mark task done (infers ID from branch if omitted); auto-snaps HEAD Git commit/diff | `tk done T-1`<br>`tk done` |
| `tk attach` | — | `[id] [commit-ref\|A..B] [-m msg] [--diff] [--scan [range]]` | Snapshot commit, commit range (`main..HEAD`), or `--scan` trailer commits into task | `tk attach T-1 HEAD`<br>`tk attach --scan main..HEAD` |
| `tk patch` | `format-patch` | `[id] [-o dir] [--stdout]` | Export attached task commits or unified diff as a Git patch series (`git am` ready) | `tk patch T-1 --stdout`<br>`tk patch T-1 -o ./patches` |
| `tk hooks` | — | `[install\|uninstall\|status]` | Install non-destructive `prepare-commit-msg` and `post-commit` Git hooks for auto task linking | `tk hooks install`<br>`tk hooks status` |
| `tk pr` | — | `[create\|status] [id] [--draft] [--base branch]` | Create or sync a GitHub Pull Request from a task via `gh` CLI | `tk pr create T-1`<br>`tk pr status T-1` |
| `tk gh` | — | `[import <num>\|push <id>] [-T topic]` | Import a GitHub Issue as a task (`gh:owner/repo#N`) or push a local task/post to GitHub Issues | `tk gh import 42`<br>`tk gh push T-1` |
| `tk delete` | `rm` | `<id> [-y/--yes] [-f/--force]` | Remove a task to the tombstone archive (prompts unless `-y` or `-f`) | `tk delete T-1 -y` |
| `tk restore` | `undo-delete` | `<id\|latest> [-l/--list] [-d/--db] [-f]` | Restore deleted task from tombstone archive, or restore database with `--db` | `tk restore T-1`<br>`tk restore --db latest -f` |
| `tk archive` | `arc` | `[id...] [-l] [-m reason]` | Archive tasks to hide from active backlog; `tk archive list` | `tk archive T-1 T-2` |
| `tk unarchive` | — | `<id...> [-m reason]` | Restore archived tasks to active status | `tk unarchive T-1` |
| `tk snapshot` | `export` | `[output-path] [-o path] [-f sqlite\|json] [-q]` | Write a standalone, WAL-free SQLite file (`VACUUM INTO`) or JSON export for safe copying | `tk snapshot ./tasks.db`<br>`tk export ./tasks.json` |
| `tk backup` | `db` | `[list\|create\|snapshot\|export\|verify\|restore] [-l] [-f]` | Manage atomic SQLite snapshots, central backup vault, and disaster recovery | `tk db snapshot ./tasks.db`<br>`tk db restore latest -f` |
| `tk watch` | — | `[--json]` | Stream real-time task lifecycle events from socket daemon | `tk watch` |
| `tk daemon` | — | `[start\|restart\|run\|status\|stop]` | Manage background socket daemon process (supports isolated `--socket`, `--tcp`, `--db`) | `tk daemon status` |
| `tk version` | — | `[--json]` | Display version, commit SHA, build date, and platform | `tk version` |

### Global Flags

- `-g, --global`: Operate on the global task database (`~/.local/share/task_cli/global.db`) instead of the current git repository.
- `--db <path>`: Custom SQLite database file path (overrides `<repo_root>/.tasks/tasks.db`, global DB, and `TK_DB`). Ideal for isolated tests or per-worktree databases.
- `--actor <name>`: Explicit actor identity for task attribution (overrides `TK_ACTOR`, `TK_AUTHOR`, and git author).
- `--socket <path>`: Custom Unix domain socket path for daemon IPC (overrides `TK_SOCKET`). Auto-spawns an isolated daemon on that socket if needed.
- `--tcp <addr>`: Optional TCP address (`host:port`) to connect to or listen on (overrides `TK_ADDR`). Passing `--tcp` without `--socket` runs an isolated TCP-only daemon without touching the default user socket.
- `--no-pager`: Disable automatic conditional pager (`less -RFX`) for long outputs.
- `--json`: Output machine-readable JSON for scripting, CI/CD, or LLM agent pipelines.

---

## Obsidian-Style Wikilinks

Descriptions and notes support rich, bidirectionally tracked `[[...]]` wikilinks. Anchored links jump directly to line numbers or activity notes:

| Syntax | Target | Description | Example |
| :--- | :--- | :--- | :--- |
| `[[T-15]]` | Task / Post | Links to item (forgiving: `[[t15]]`, `[[15]]`) | `Relates to [[T-15]]` |
| `[[T-15#L12]]` | Line Anchor | Anchors to line 12 of description (`[[T-15:12]]`) | `See spec at [[T-15#L12]]` |
| `[[T-15#L12-L20]]` | Line Range | Anchors to lines 12 through 20 | `Implemented in [[T-15#L12-L20]]` |
| `[[T-15#n3]]` | Note Anchor | Anchors to activity note #3 | `Addressed feedback in [[T-15#n3]]` |
| `[[T-15\|custom]]` | Display Alias | Custom link text | `Check [[T-15\|auth spec]]` |
| `[[@game/hero]]` | Topic | Links to topic board or folder | `Moved to [[@game/hero]]` |

*Tip: Run `tk show <ID> -n` to display description line numbers when authoring line anchors.*

---

## TUI Keybindings & Vim Motions

Running `tk` without subcommands inside any standard terminal opens the interactive Bubble Tea TUI. Wide terminals display a 3-pane layout: **Topic Sidebar** (left), **Item / Tree View** (middle), and **Details & Links** (right).

| Keybinding | Action | Description |
|:----------:|--------|-------------|
| `?` | Help Cheatsheet | Toggle full interactive keybinding cheatsheet modal |
| `Tab` / `Shift+Tab` | Cycle Active Pane | Switch focus between Topic Sidebar, Item List, and Details Pane |
| `h` / `l` / `←` / `→` | Navigate Panes | Move focus horizontally between panes |
| `B` | Topic Drawer / Sidebar | Toggle Topic Sidebar visibility on wide screens, or open slide-over Topic Drawer on narrow terminals (`<100` cols) |
| `j` / `k` / `↓` / `↑` | Move / Scroll | In **Sidebar**: select topic; in **List**: navigate items; in **Details**: scroll content (supports `{count}j`/`{count}k`) |
| `gg` / `G` / `Home` / `End` | Top / Bottom | Jump to top or bottom of the active pane (supports `{line}G`) |
| `Ctrl+D` / `Ctrl+U` / `J` / `K` / `PgDn` / `PgUp` | Fast Page Scroll | Page scroll content down/up in the active pane |
| `<` / `>` / `,` / `.` / `=` | Resize Split Pane | Adjust or reset (`=`) horizontal split ratio between List and Details panes |
| `O` | Cycle Kind Filter | Cycle item kind filter between **All Items**, **Tasks Only**, and **Posts Only** |
| `gt` | Fuzzy Topic Picker | Open fuzzy topic search modal (`Tab` inside picker toggles repo vs global topics) |
| `]t` / `[t` | Cycle Topics | Jump to next or previous topic in the tree |
| `]l` / `[l` | Link Cursor | Move cursor to next / previous `[[...]]` wikilink in the details pane |
| `Enter` / `gf` | Follow Link / Toggle Diff | Follow focused wikilink (`{n}gf` jumps to link #n), or toggle unified git diff in Details |
| `gb` / `gB` | Jump History | Navigate backward / forward through navigation history stack |
| `gl` | Insert Link Modal | Open modal to select an item and append an Obsidian-style wikilink `[[...]]` |
| `R` / `C` | Inline Reply / Note | In **List** / **Details**: open inline modal to append a note/comment without launching `$EDITOR` |
| `M` | Move Items / Topic | In **List**: move selected items to another topic; in **Sidebar**: move topic under a new parent |
| `p` | Promote Post | In **List**: promote selected board post (`P-n`) into a task (`T-n`) |
| `N` / `R` / `D` | Topic Management | In **Sidebar**: `N` creates topic, `R` renames topic, `D` deletes topic (`Space` folds group) |
| `t` | Toggle Tree Mode | In **Tasks topic**: toggle between flat list and hierarchical DAG graph tree |
| `L` | Link Dependency Modal | Open interactive fuzzy modal to add a prerequisite dependency |
| `U` | Unlink Dependency Modal | Open interactive fuzzy picker to remove a prerequisite dependency |
| `Space` | Multi-Select Toggle | Toggle item selection (`●`/`○` marker) and advance cursor down |
| `v` / `V` | Select / Select All | Toggle item selection / select all visible items |
| `u` | Multi-Level Undo | Revert recent mutations (restores deleted tasks, claims, done, status, archive) |
| `d` | Done / Batch Done | Mark task(s) done / resolve post(s) (prompts for force confirmation if blocked) |
| `s` | Status / Batch Toggle | Cycle status between `open` and `in_progress` |
| `a` | Claim / Batch Claim | Atomically claim selected task(s) for current actor |
| `A` / `Ctrl+A` / `Alt+A` | Archive / Toggle Archived | `A` archives/unarchives selected item(s); `Ctrl+A` / `Alt+A` toggles archived item visibility |
| `x` | Delete / Batch Delete | Prompt for confirmation (`[y/N]`) and remove selected items |
| `n` | New Item | Create task in tasks topic or post in board topic |
| `e` | Edit Item | Launch `$EDITOR` with selected item pre-populated |
| `/` | Search Filter | Enter live search query (`Enter` applies, `Esc` cancels) |
| `Esc` / `c` | Clear Filter / Selection | Cancel active chord, return focus to list, clear selection, or reset search filter |
| `S` | Toggle Scope | Switch view between `local` (git repository) and `global` scopes |
| `r` | Refresh | Re-query tasks and topics from daemon |
| `q` / `Ctrl+C` | Quit | `q` asks for confirmation (`q`/`y`/`Enter` to quit, `n`/`Esc` to stay); `Ctrl+C` exits immediately |

---

## Configuration & Environment Variables

`tk` requires zero configuration files by default. Behavior can be tailored using standard environment variables:

| Variable | Default Value | Purpose |
|----------|---------------|---------|
| `TK_ACTOR` | *(unset)* | Primary actor identity for task author, claim, and note attribution |
| `TK_AUTHOR` | *(unset)* | Secondary actor identity fallback before git author |
| `TK_PAGER` | `less -RFX` | Pager command for `tk list`, `tk search`, and `tk show` when output exceeds screen (falls back to `BAT_PAGER` -> `PAGER`) |
| `TK_NO_PAGER` | *(unset)* | Set to `1` or `true` to disable automatic paging completely |
| `TK_ASCII` | *(unset)* | Set to `1` or `true` to force ASCII-only glyphs in CLI and TUI output |
| `NO_COLOR` | *(unset)* | Disable ANSI color output across CLI and TUI |
| `TK_SOCKET` | `/run/user/<uid>/tk.sock`<br>*(fallback: `~/.local/share/task_cli/tk.sock`)* | Unix Domain Socket path used for client-daemon IPC communication |
| `TK_ADDR` | *(unset)* | Optional TCP address (`host:port`) to bind or connect to (e.g. `127.0.0.1:4567` for SSH tunneling, containers, or isolated daemons) |
| `TK_DB` | *(unset)* | Custom SQLite database file path (overrides `<repo_root>/.tasks/tasks.db` and global DB) |
| `TK_WORKTREE_DB` | *(unset)* | Set to `1` or `true` so linked git worktrees use their own worktree-local `.tasks/tasks.db` instead of sharing the main repository's DB (also triggered automatically if `<worktree>/.tasks/` exists) |
| `EDITOR` | `$VISUAL` -> `nvim` | Text editor invoked when creating or editing tasks interactively |
| `VISUAL` | `nvim` | Fallback editor if `EDITOR` is unset |
| `XDG_DATA_HOME` | `~/.local/share` | Root directory for application state and the global SQLite database (`$XDG_DATA_HOME/task_cli/global.db`) |

---

## Storage & Data Locations

- **Local Repository Database**: Stored at `<repo_root>/.tasks/tasks.db`. You can commit `.tasks/` to version control or add it to `.gitignore` based on team preference.
- **Global Database**: Stored at `~/.local/share/task_cli/global.db` (or `$XDG_DATA_HOME/task_cli/global.db`).
- **Daemon Socket**: Default `/run/user/<uid>/tk.sock` (in-memory `tmpfs` on modern Linux) ensuring minimal disk I/O.
