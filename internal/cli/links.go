package cli

import (
	"fmt"
	"strconv"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

// resolvedLink is a numbered [[wiki link]] occurrence together with its resolved target.
type resolvedLink struct {
	N       int             `json:"n"`
	Link    domain.WikiLink `json:"link"`
	Item    *domain.Task    `json:"item,omitempty"`
	Topic   *domain.Topic   `json:"topic,omitempty"`
	Broken  bool            `json:"broken,omitempty"` // target does not exist
	Stale   bool            `json:"stale,omitempty"`  // target exists but the anchor is out of range
	Excerpt string          `json:"excerpt,omitempty"`
	Source  string          `json:"source"` // "description" or "note N"
}

// collectLinks returns every [[wiki link]] occurrence in the item's description and
// notes, numbered in reading order (the numbers used by 'tk go' and the TUI).
func collectLinks(t *domain.Task) []resolvedLink {
	type src struct {
		label, text string
	}
	sources := []src{{"description", t.Description}}
	for i, n := range t.Notes {
		sources = append(sources, src{fmt.Sprintf("note %d", i+1), n.Content})
	}

	items := map[string]*domain.Task{}
	topics := map[string]*domain.Topic{}
	var out []resolvedLink
	for _, s := range sources {
		for _, l := range domain.ParseWikiLinks(s.text, t.Scope) {
			rl := resolvedLink{N: len(out) + 1, Link: l, Source: s.label}
			if l.IsTopic {
				tp, ok := topics[l.Target]
				if !ok {
					tp, _ = taskClient.GetTopic(l.Target)
					topics[l.Target] = tp
				}
				rl.Topic = tp
				rl.Broken = tp == nil
			} else {
				it, ok := items[l.Target]
				if !ok {
					it, _ = taskClient.GetTask(l.Target)
					items[l.Target] = it
				}
				rl.Item = it
				rl.Broken = it == nil
				if it != nil {
					text, ok := l.AnchorText(it)
					rl.Excerpt = text
					rl.Stale = !ok
				}
			}
			out = append(out, rl)
		}
	}
	return out
}

// renderLinksSection renders numbered outgoing links and backlinks for 'tk show' / 'tk links'.
func renderLinksSection(t *domain.Task, links []resolvedLink) string {
	var sb strings.Builder
	if len(links) > 0 {
		sb.WriteString("Links:\n")
		for i := range links {
			rl := &links[i]
			fmt.Fprintf(&sb, "  [%d] %-16s %s\n", rl.N, rl.Link.Display(), describeLinkTarget(rl))
			if rl.Excerpt != "" && (rl.Link.LineStart > 0 || rl.Link.Note > 0) {
				for _, line := range strings.Split(rl.Excerpt, "\n") {
					fmt.Fprintf(&sb, "        │ %s\n", truncateWithEllipsis(line, 70))
				}
			}
		}
	}
	if len(t.Backlinks) > 0 {
		sb.WriteString("Backlinks (linked from):\n")
		for _, id := range t.Backlinks {
			if b, err := taskClient.GetTask(id); err == nil {
				fmt.Fprintf(&sb, "  ← %-6s %-9s %s%s\n", b.ID, "["+b.StatusLabel()+"]", b.Title, topicSuffix(b))
			} else {
				fmt.Fprintf(&sb, "  ← %s\n", id)
			}
		}
	}
	return sb.String()
}

func describeLinkTarget(rl *resolvedLink) string {
	switch {
	case rl.Broken && rl.Link.IsTopic:
		return "✗ topic not found"
	case rl.Broken:
		return "✗ item not found"
	case rl.Topic != nil:
		return fmt.Sprintf("→ topic %q (%s, %d open)", rl.Topic.Title, rl.Topic.Kind, rl.Topic.OpenCount)
	}
	desc := fmt.Sprintf("→ %s [%s]%s", rl.Item.Title, rl.Item.StatusLabel(), topicSuffix(rl.Item))
	if rl.Stale {
		desc += "  ⚠ anchor out of range"
	}
	if rl.Link.Alias != "" {
		desc = rl.Link.Target + rl.Link.Anchor() + " " + desc
	}
	return desc
}

var linksCmd = &cobra.Command{
	Use:   "links <id>",
	Short: "List an item's [[wiki links]] (numbered) and its backlinks",
	Long: `Links are written in descriptions and notes using Obsidian-style syntax:

  [[T-15]]            an item (forgiving: [[t15]])
  [[T-15#L12]]        line 12 of T-15's description (also [[T-15:12]])
  [[T-15#L12-L20]]    a line range
  [[T-15#n3]]         note 3 of T-15
  [[T-15|label]]      custom label
  [[@game/hero]]      a topic

Use 'tk go <id> <n>' to open link number n.`,
	Example: `  tk links T-15
  tk links t15 --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		t, err := taskClient.GetTask(normID(args[0]))
		if err != nil {
			return err
		}
		links := collectLinks(t)
		if jsonFlag {
			return outputJSON(map[string]any{"id": t.ID, "links": links, "backlinks": t.Backlinks})
		}
		if len(links) == 0 && len(t.Backlinks) == 0 {
			fmt.Printf("%s has no links or backlinks.\n", t.ID)
			return nil
		}
		fmt.Printf("%s: %s\n", t.ID, t.Title)
		fmt.Print(renderLinksSection(t, links))
		return nil
	},
}

var goCmd = &cobra.Command{
	Use:   "go <id> [n]",
	Short: "Follow link number n of an item (without n: list its links)",
	Example: `  tk go T-15        # list numbered links of T-15
  tk go T-15 2      # open the 2nd link (shows the anchored line/note first)`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return linksCmd.RunE(cmd, args)
		}
		t, err := taskClient.GetTask(normID(args[0]))
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n <= 0 {
			return fmt.Errorf("link number must be a positive integer, got %q", args[1])
		}
		links := collectLinks(t)
		if n > len(links) {
			return fmt.Errorf("%s has %d link(s); there is no link %d", t.ID, len(links), n)
		}
		rl := links[n-1]
		if rl.Broken {
			return fmt.Errorf("link [%d] %s points to something that does not exist", n, rl.Link.Raw)
		}
		if rl.Topic != nil {
			return topicShowCmd.RunE(cmd, []string{rl.Topic.Path})
		}
		if jsonFlag {
			return outputJSON(rl)
		}
		if rl.Link.LineStart > 0 || rl.Link.Note > 0 {
			fmt.Printf("%s%s:\n", rl.Item.ID, rl.Link.Anchor())
			for _, line := range strings.Split(rl.Excerpt, "\n") {
				fmt.Printf("  │ %s\n", line)
			}
			if rl.Stale {
				fmt.Println("  ⚠ anchor is beyond the end of the target text")
			}
			fmt.Println()
		}
		return showCmd.RunE(cmd, []string{rl.Item.ID})
	},
}

func init() {
	RootCmd.AddCommand(linksCmd, goCmd)
}
