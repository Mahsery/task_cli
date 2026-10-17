package search

import (
	"fmt"
	"strconv"
	"strings"

	"task_cli/internal/domain"
)

// TokenType represents lexical token types for the search DSL.
type TokenType int

const (
	tokWord TokenType = iota
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
)

type token struct {
	typ   TokenType
	value string
}

// Tokenize converts an expression string into a stream of tokens.
func Tokenize(query string) []token {
	var tokens []token
	runes := []rune(query)
	n := len(runes)
	i := 0

	for i < n {
		// Skip whitespace
		if runes[i] == ' ' || runes[i] == '\t' || runes[i] == '\n' || runes[i] == '\r' {
			i++
			continue
		}

		if runes[i] == '(' {
			tokens = append(tokens, token{typ: tokLParen, value: "("})
			i++
			continue
		}
		if runes[i] == ')' {
			tokens = append(tokens, token{typ: tokRParen, value: ")"})
			i++
			continue
		}

		// Quoted phrase
		if runes[i] == '"' || runes[i] == '\'' {
			quote := runes[i]
			i++
			start := i
			for i < n && runes[i] != quote {
				i++
			}
			tokens = append(tokens, token{typ: tokWord, value: string(runes[start:i])})
			if i < n && runes[i] == quote {
				i++
			}
			continue
		}

		// Check for field:"quoted value" or field:'quoted value' e.g. tag:"bug fix" or -tag:"bug fix"
		colonQuote := false
		start := i
		for j := i; j < n && runes[j] != ' ' && runes[j] != '\t' && runes[j] != '\n' && runes[j] != '\r' && runes[j] != '(' && runes[j] != ')'; j++ {
			if runes[j] != ':' || j+1 >= n || (runes[j+1] != '"' && runes[j+1] != '\'') {
				continue
			}
			quote := runes[j+1]
			field := string(runes[start : j+1])
			j += 2
			valStart := j
			for j < n && runes[j] != quote {
				j++
			}
			val := string(runes[valStart:j])
			if j < n && runes[j] == quote {
				j++
			}
			if strings.HasPrefix(field, "-") && len(field) > 2 {
				tokens = append(tokens, token{typ: tokNot, value: "NOT"}, token{typ: tokWord, value: field[1:] + val})
			} else {
				tokens = append(tokens, token{typ: tokWord, value: field + val})
			}
			i = j
			colonQuote = true
			break
		}
		if colonQuote {
			continue
		}

		// Regular word or operator
		start = i
		for i < n && runes[i] != ' ' && runes[i] != '\t' && runes[i] != '\n' && runes[i] != '\r' && runes[i] != '(' && runes[i] != ')' {
			i++
		}
		word := string(runes[start:i])
		upper := strings.ToUpper(word)

		switch upper {
		case "AND", "&&":
			tokens = append(tokens, token{typ: tokAnd, value: word})
		case "OR", "||":
			tokens = append(tokens, token{typ: tokOr, value: word})
		case "NOT", "!":
			tokens = append(tokens, token{typ: tokNot, value: word})
		default:
			// Check for leading minus sign e.g. -wip or -tag:bug
			if strings.HasPrefix(word, "-") && len(word) > 1 {
				tokens = append(tokens, token{typ: tokNot, value: "NOT"}, token{typ: tokWord, value: word[1:]})
			} else {
				tokens = append(tokens, token{typ: tokWord, value: word})
			}
		}
	}

	return tokens
}

// Node is an AST node in the search expression tree.
type Node interface {
	Eval(task *domain.Task) bool
}

type andNode struct {
	left  Node
	right Node
}

func (n *andNode) Eval(task *domain.Task) bool {
	if n == nil {
		return true
	}
	left := true
	if n.left != nil {
		left = n.left.Eval(task)
	}
	right := true
	if n.right != nil {
		right = n.right.Eval(task)
	}
	return left && right
}

type orNode struct {
	left  Node
	right Node
}

func (n *orNode) Eval(task *domain.Task) bool {
	if n == nil {
		return false
	}
	left := false
	if n.left != nil {
		left = n.left.Eval(task)
	}
	right := false
	if n.right != nil {
		right = n.right.Eval(task)
	}
	return left || right
}

type notNode struct {
	child Node
}

func (n *notNode) Eval(task *domain.Task) bool {
	if n == nil || n.child == nil {
		return true
	}
	return !n.child.Eval(task)
}

type termNode struct {
	field string // empty for general, or "tag", "status", "priority", "assignee", "author", "id"
	val   string
}

// matchesNormalizedID checks whether target is a valid forgiving item ID reference and whether task.ID matches it.
func matchesNormalizedID(target string, task *domain.Task) (matched, isIDRef bool) {
	raw := strings.TrimSpace(target)
	if num, err := strconv.Atoi(raw); err == nil && num > 0 {
		taskPrefix := domain.PrefixTask
		postPrefix := domain.PrefixPost
		if task.Scope == domain.ScopeGlobal {
			taskPrefix = domain.PrefixGlobalTask
			postPrefix = domain.PrefixGlobalPost
		}
		taskID := fmt.Sprintf("%s-%d", taskPrefix, num)
		postID := fmt.Sprintf("%s-%d", postPrefix, num)
		return strings.EqualFold(task.ID, taskID) || strings.EqualFold(task.ID, postID), true
	}
	if norm, err := domain.NormalizeID(raw, task.Scope); err == nil {
		return strings.EqualFold(task.ID, norm), true
	}
	return false, false
}

func (n *termNode) Eval(task *domain.Task) bool {
	target := strings.ToLower(n.val)

	switch n.field {
	case "tag", "t":
		for _, tag := range task.Tags {
			if strings.EqualFold(tag, target) || strings.Contains(strings.ToLower(tag), target) {
				return true
			}
		}
		return false

	case "status", "s":
		if (target == "orphan" || target == "orphaned") && task.IsOrphaned() {
			return true
		}
		return strings.Contains(strings.ToLower(string(task.Status)), target) ||
			strings.Contains(strings.ToLower(task.StatusLabel()), target)

	case "priority", "pri", "p":
		return strings.Contains(strings.ToLower(string(task.Priority)), target)

	case "assignee", "a":
		return strings.Contains(strings.ToLower(task.Assignee), target)

	case "reviewer", "rev":
		return strings.Contains(strings.ToLower(task.Reviewer), target)

	case "author":
		return strings.Contains(strings.ToLower(task.Author), target)

	case "id":
		if matched, isIDRef := matchesNormalizedID(target, task); isIDRef {
			return matched
		}
		return strings.Contains(strings.ToLower(task.ID), target)

	case "topic", "in":
		ref := domain.NormalizeTopicRef(target)
		if ref == domain.UnfiledTopic {
			return task.TopicPath == ""
		}
		// Matches the topic itself or any sub-topic: topic:game matches game/hero-system.
		return task.TopicPath == ref || strings.HasPrefix(task.TopicPath, ref+"/") ||
			strings.HasSuffix(task.TopicPath, "/"+ref) || strings.Contains(task.TopicPath, "/"+ref+"/")

	case "kind", "type":
		kind := task.Kind
		if kind == "" {
			kind = domain.KindTask
		}
		want, err := domain.ParseItemKind(target)
		return err == nil && kind == want

	case "links", "linksto", "link":
		return containsLinkTarget(task.Links, target, task.Scope)

	case "linkedfrom", "backlink", "backlinks":
		return containsLinkTarget(task.Backlinks, target, task.Scope)

	case "branch", "b":
		return strings.Contains(strings.ToLower(task.Branch), target)

	case "ext", "external":
		return strings.Contains(strings.ToLower(task.ExternalRef), target)

	case "pr":
		return strings.Contains(strings.ToLower(task.PRURL), target)

	default:
		// General match: ID, Title, Description, Tags, Assignee, Reviewer, Author, Branch, ExternalRef, PRURL, Notes, ReviewerNotes, Orphaned state
		if (target == "orphan" || target == "orphaned") && task.IsOrphaned() {
			return true
		}
		if matched, isIDRef := matchesNormalizedID(target, task); isIDRef {
			if matched {
				return true
			}
		} else if strings.Contains(strings.ToLower(task.ID), target) {
			return true
		}
		if strings.Contains(strings.ToLower(task.Title), target) {
			return true
		}
		if strings.Contains(strings.ToLower(task.Description), target) {
			return true
		}
		for _, tag := range task.Tags {
			if strings.Contains(strings.ToLower(tag), target) {
				return true
			}
		}
		if strings.Contains(strings.ToLower(task.Assignee), target) {
			return true
		}
		if strings.Contains(strings.ToLower(task.Reviewer), target) {
			return true
		}
		if strings.Contains(strings.ToLower(task.Author), target) {
			return true
		}
		if strings.Contains(strings.ToLower(task.Branch), target) {
			return true
		}
		if strings.Contains(strings.ToLower(task.ExternalRef), target) {
			return true
		}
		if strings.Contains(strings.ToLower(task.PRURL), target) {
			return true
		}
		for _, note := range task.Notes {
			if strings.Contains(strings.ToLower(note.Content), target) {
				return true
			}
		}
		for _, note := range task.ReviewerNotes {
			if strings.Contains(strings.ToLower(note.Content), target) {
				return true
			}
		}
		return false
	}
}

// Parser constructs an AST from tokens using recursive descent.
type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() *token {
	if p.pos < len(p.tokens) {
		return &p.tokens[p.pos]
	}
	return nil
}

// advance consumes the current token, if any.
func (p *parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// parseExpr parses an OR expression.
func (p *parser) parseExpr() Node {
	left := p.parseAnd()
	for {
		tok := p.peek()
		if tok == nil || tok.typ != tokOr {
			break
		}
		p.advance()
		right := p.parseAnd()
		if right != nil {
			if left == nil {
				left = right
			} else {
				left = &orNode{left: left, right: right}
			}
		}
	}
	return left
}

// parseAnd parses an AND expression (explicit AND or implicit juxtaposition).
func (p *parser) parseAnd() Node {
	left := p.parseNot()
	for {
		tok := p.peek()
		if tok == nil || tok.typ == tokOr || tok.typ == tokRParen {
			break
		}

		if tok.typ == tokAnd {
			p.advance()
			right := p.parseNot()
			if right != nil {
				if left == nil {
					left = right
				} else {
					left = &andNode{left: left, right: right}
				}
			}
		} else {
			// Implicit AND
			right := p.parseNot()
			if right == nil {
				break
			}
			if left == nil {
				left = right
			} else {
				left = &andNode{left: left, right: right}
			}
		}
	}
	return left
}

// parseNot parses NOT / negation.
func (p *parser) parseNot() Node {
	tok := p.peek()
	if tok != nil && tok.typ == tokNot {
		p.advance()
		child := p.parseNot()
		if child != nil {
			return &notNode{child: child}
		}
		return nil
	}
	return p.parsePrimary()
}

// parsePrimary parses parenthesized expressions or terms.
func (p *parser) parsePrimary() Node {
	tok := p.peek()
	if tok == nil {
		return nil
	}

	if tok.typ == tokLParen {
		p.advance()
		expr := p.parseExpr()
		if p.peek() != nil && p.peek().typ == tokRParen {
			p.advance()
		}
		return expr
	}

	if tok.typ == tokWord {
		p.advance()
		val := tok.value
		// Check for field qualifiers: tag:foo, s:open, etc.
		if idx := strings.Index(val, ":"); idx > 0 {
			field := strings.ToLower(val[:idx])
			termVal := val[idx+1:]
			switch field {
			case "tag", "t", "status", "s", "priority", "pri", "p", "assignee", "a", "reviewer", "rev", "author", "id",
				"topic", "in", "kind", "type", "links", "linksto", "link", "linkedfrom", "backlink", "backlinks",
				"branch", "b", "ext", "external", "pr":
				return &termNode{field: field, val: termVal}
			}
		}
		return &termNode{val: val}
	}

	p.advance()
	return nil
}

// Query represents a pre-compiled search expression for efficient repeated evaluation.
type Query struct {
	ast Node
	raw string
}

// Compile parses and prepares a search DSL query once for reuse across multiple tasks.
func Compile(query string) *Query {
	q := strings.TrimSpace(query)
	if q == "" {
		return &Query{raw: ""}
	}

	tokens := Tokenize(q)
	if len(tokens) == 0 {
		return &Query{raw: q}
	}

	p := &parser{tokens: tokens}
	ast := p.parseExpr()
	if ast == nil {
		ast = &termNode{val: q}
	}

	return &Query{ast: ast, raw: q}
}

// Match evaluates whether a task satisfies the compiled search query.
func (q *Query) Match(task *domain.Task) bool {
	if q == nil || q.ast == nil {
		if q != nil && q.raw != "" {
			return (&termNode{val: q.raw}).Eval(task)
		}
		return true
	}
	return q.ast.Eval(task)
}

// Match evaluates whether a task matches the search DSL query.
// If query is empty, it returns true.
func Match(query string, task *domain.Task) bool {
	return Compile(query).Match(task)
}

// containsLinkTarget reports whether targets contains the (forgivingly written) item ID or @topic.
func containsLinkTarget(targets []string, want, scope string) bool {
	var altWant string
	raw := strings.TrimSpace(want)
	if strings.HasPrefix(raw, "@") {
		want = "@" + domain.NormalizeTopicRef(raw)
	} else if num, err := strconv.Atoi(raw); err == nil && num > 0 {
		taskPrefix := domain.PrefixTask
		postPrefix := domain.PrefixPost
		if scope == domain.ScopeGlobal {
			taskPrefix = domain.PrefixGlobalTask
			postPrefix = domain.PrefixGlobalPost
		}
		want = fmt.Sprintf("%s-%d", taskPrefix, num)
		altWant = fmt.Sprintf("%s-%d", postPrefix, num)
	} else if id, err := domain.NormalizeID(raw, scope); err == nil {
		want = id
	}
	for _, t := range targets {
		if strings.EqualFold(t, want) || (altWant != "" && strings.EqualFold(t, altWant)) {
			return true
		}
	}
	return false
}
