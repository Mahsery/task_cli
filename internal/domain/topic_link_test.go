package domain

import (
	"reflect"
	"testing"
)

func TestNormalizeID(t *testing.T) {
	cases := []struct {
		in, scope, want string
		wantErr         bool
	}{
		{"T-15", ScopeLocal, "T-15", false},
		{"t-15", ScopeLocal, "T-15", false},
		{"t15", ScopeLocal, "T-15", false},
		{"T15", ScopeLocal, "T-15", false},
		{" t15 ", ScopeLocal, "T-15", false},
		{"15", ScopeLocal, "T-15", false},
		{"15", ScopeGlobal, "G-15", false},
		{"g7", ScopeGlobal, "G-7", false},
		{"p3", ScopeLocal, "P-3", false},
		{"gp-2", ScopeGlobal, "GP-2", false},
		{"T-015", ScopeLocal, "T-15", false},
		{"", ScopeLocal, "", true},
		{"0", ScopeLocal, "", true},
		{"-3", ScopeLocal, "", true},
		{"T-", ScopeLocal, "", true},
		{"T-0", ScopeLocal, "", true},
		{"X-5", ScopeLocal, "", true},
		{"T-12a", ScopeLocal, "", true},
		{"ST-1", ScopeLocal, "", true},
		{"T--1", ScopeLocal, "", true},
	}
	for _, c := range cases {
		got, err := NormalizeID(c.in, c.scope)
		if (err != nil) != c.wantErr {
			t.Errorf("NormalizeID(%q,%q) err=%v, wantErr=%v", c.in, c.scope, err, c.wantErr)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeID(%q,%q)=%q, want %q", c.in, c.scope, got, c.want)
		}
	}
}

func TestIsPostID(t *testing.T) {
	for id, want := range map[string]bool{"P-1": true, "GP-4": true, "T-1": false, "G-2": false, "junk": false} {
		if got := IsPostID(id); got != want {
			t.Errorf("IsPostID(%q)=%v want %v", id, got, want)
		}
	}
}

func TestNormalizeSlugAndValidate(t *testing.T) {
	cases := map[string]string{
		"Hero System":     "hero-system",
		"  Bug Reports! ": "bug-reports",
		"a__b":            "a__b",
		"UI / UX":         "ui-ux",
		"---":             "",
		"Ünïcode words":   "n-code-words",
	}
	for in, want := range cases {
		if got := NormalizeSlug(in); got != want {
			t.Errorf("NormalizeSlug(%q)=%q want %q", in, got, want)
		}
	}
	if err := ValidateSlug("hero-system"); err != nil {
		t.Errorf("valid slug rejected: %v", err)
	}
	for _, bad := range []string{"", "Hero", "a b", "unfiled", "all", "x-"} {
		if err := ValidateSlug(bad); err == nil {
			t.Errorf("ValidateSlug(%q) should fail", bad)
		}
	}
}

func TestTopicKindAccepts(t *testing.T) {
	if !TopicTasks.Accepts(KindTask) || TopicTasks.Accepts(KindPost) {
		t.Error("tasks topics must accept only tasks")
	}
	if !TopicBoard.Accepts(KindPost) || TopicBoard.Accepts(KindTask) {
		t.Error("boards must accept only posts")
	}
	if TopicGroup.Accepts(KindTask) || TopicGroup.Accepts(KindPost) {
		t.Error("groups must not accept items")
	}
}

func TestParseWikiLinks(t *testing.T) {
	text := "See [[T-15]] and [[t3#L12]], also [[T-4#L2-L5|the range]] plus [[p2#n3]].\n" +
		"Topic [[@Game/Hero-System]] and colon form [[T-9:7-9]].\n" +
		"Inline `[[T-100]]` is code.\n" +
		"```\n[[T-200]] fenced\n```\n" +
		"Broken: [[nope]] [[T-]] [[T-1#Lx]] [[15]] [[T-5#L9-L3]] [[@bad slug]]\n" +
		"Unclosed [[T-7 never closes\n" +
		"Nested [[a [[T-8]] ok"

	links := ParseWikiLinks(text, ScopeLocal)
	type got struct {
		Target    string
		IsTopic   bool
		LineStart int
		LineEnd   int
		Note      int
		Alias     string
	}
	var gotLinks []got
	for _, l := range links {
		gotLinks = append(gotLinks, got{l.Target, l.IsTopic, l.LineStart, l.LineEnd, l.Note, l.Alias})
		if text[l.Start:l.End] != l.Raw {
			t.Errorf("offsets for %q do not slice back to raw text (got %q)", l.Raw, text[l.Start:l.End])
		}
	}
	want := []got{
		{"T-15", false, 0, 0, 0, ""},
		{"T-3", false, 12, 0, 0, ""},
		{"T-4", false, 2, 5, 0, "the range"},
		{"P-2", false, 0, 0, 3, ""},
		{"game/hero-system", true, 0, 0, 0, ""},
		{"T-9", false, 7, 9, 0, ""},
		{"T-8", false, 0, 0, 0, ""},
	}
	if !reflect.DeepEqual(gotLinks, want) {
		t.Fatalf("ParseWikiLinks mismatch\n got: %+v\nwant: %+v", gotLinks, want)
	}
}

func TestWikiLinkCanonicalAndDisplay(t *testing.T) {
	l, err := ParseWikiLink("t4:2-5|range", ScopeLocal)
	if err != nil {
		t.Fatal(err)
	}
	if l.Canonical() != "[[T-4#L2-L5|range]]" {
		t.Errorf("Canonical=%q", l.Canonical())
	}
	if l.Display() != "range" {
		t.Errorf("Display=%q", l.Display())
	}
	l2, _ := ParseWikiLink("T-1#L3-L3", ScopeLocal)
	if l2.Anchor() != "#L3" {
		t.Errorf("single-line range should collapse to #L3, got %q", l2.Anchor())
	}
	l3, _ := ParseWikiLink("@ui", ScopeLocal)
	if l3.Display() != "@ui" || l3.Canonical() != "[[@ui]]" {
		t.Errorf("topic link rendering wrong: %q %q", l3.Display(), l3.Canonical())
	}
}

func TestLinkTargetsDedup(t *testing.T) {
	links := ParseWikiLinks("[[T-1]] [[t1#L2]] [[@a]] [[T-2]] [[@a]]", ScopeLocal)
	got := LinkTargets(links)
	want := []string{"T-1", "@a", "T-2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LinkTargets=%v want %v", got, want)
	}
}

func TestPostsNeverOrphaned(t *testing.T) {
	p := &Task{Kind: KindPost, Status: StatusInProgress, Assignee: "x", Tags: []string{"orphaned"}}
	if p.IsOrphaned() {
		t.Error("posts must never be classified as orphaned")
	}
	if (&Task{Kind: KindPost, Status: StatusDone}).StatusLabel() != "resolved" {
		t.Error("done posts should read as resolved")
	}
	if (&Task{Kind: KindTask, Status: StatusDone}).StatusLabel() != "done" {
		t.Error("done tasks should read as done")
	}
}

func TestWikiLinkAnchorText(t *testing.T) {
	task := &Task{Title: "T", Description: "one\ntwo\nthree", Notes: []TaskNote{{Content: "n1"}, {Content: "n2"}}}
	cases := []struct {
		inner, want string
		ok          bool
	}{
		{"T-1", "T", true},
		{"T-1#L2", "two", true},
		{"T-1#L2-L3", "two\nthree", true},
		{"T-1#L3-L9", "three", false},
		{"T-1#L4", "", false},
		{"T-1#n2", "n2", true},
		{"T-1#n3", "", false},
	}
	for _, c := range cases {
		l, err := ParseWikiLink(c.inner, ScopeLocal)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := l.AnchorText(task)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: AnchorText=%q,%v want %q,%v", c.inner, got, ok, c.want, c.ok)
		}
	}
}
