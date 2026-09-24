package search

import (
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

		// Regular word or operator
		start := i
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
				tokens = append(tokens, token{typ: tokNot, value: "NOT"})
				tokens = append(tokens, token{typ: tokWord, value: word[1:]})
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
	return n.left.Eval(task) && n.right.Eval(task)
}

type orNode struct {
	left  Node
	right Node
}

func (n *orNode) Eval(task *domain.Task) bool {
	return n.left.Eval(task) || n.right.Eval(task)
}

type notNode struct {
	child Node
}

func (n *notNode) Eval(task *domain.Task) bool {
	return !n.child.Eval(task)
}

type termNode struct {
	field string // empty for general, or "tag", "status", "priority", "assignee", "author", "id"
	val   string
}

func (n *termNode) Eval(task *domain.Task) bool {
	target := strings.ToLower(n.val)

	switch n.field {
	case "tag", "t":
		for _, tag := range task.Tags {
			if strings.ToLower(tag) == target || strings.Contains(strings.ToLower(tag), target) {
				return true
			}
		}
		return false

	case "status", "s":
		return strings.Contains(strings.ToLower(string(task.Status)), target)

	case "priority", "pri", "p":
		return strings.Contains(strings.ToLower(string(task.Priority)), target)

	case "assignee", "a":
		return strings.Contains(strings.ToLower(task.Assignee), target)

	case "author":
		return strings.Contains(strings.ToLower(task.Author), target)

	case "id":
		return strings.Contains(strings.ToLower(task.ID), target)

	default:
		// General match: ID, Title, Description, Tags, Assignee, Author, Notes
		if strings.Contains(strings.ToLower(task.ID), target) {
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
		if strings.Contains(strings.ToLower(task.Author), target) {
			return true
		}
		for _, note := range task.Notes {
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

func (p *parser) next() *token {
	tok := p.peek()
	if tok != nil {
		p.pos++
	}
	return tok
}

// parseExpr parses an OR expression.
func (p *parser) parseExpr() Node {
	left := p.parseAnd()
	for {
		tok := p.peek()
		if tok != nil && tok.typ == tokOr {
			p.next()
			right := p.parseAnd()
			if right != nil {
				left = &orNode{left: left, right: right}
			}
		} else {
			break
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
			p.next()
			right := p.parseNot()
			if right != nil {
				left = &andNode{left: left, right: right}
			}
		} else {
			// Implicit AND
			right := p.parseNot()
			if right != nil {
				left = &andNode{left: left, right: right}
			} else {
				break
			}
		}
	}
	return left
}

// parseNot parses NOT / negation.
func (p *parser) parseNot() Node {
	tok := p.peek()
	if tok != nil && tok.typ == tokNot {
		p.next()
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
		p.next()
		expr := p.parseExpr()
		if p.peek() != nil && p.peek().typ == tokRParen {
			p.next()
		}
		return expr
	}

	if tok.typ == tokWord {
		p.next()
		val := tok.value
		// Check for field qualifiers: tag:foo, s:open, etc.
		if idx := strings.Index(val, ":"); idx > 0 {
			field := strings.ToLower(val[:idx])
			termVal := val[idx+1:]
			switch field {
			case "tag", "t", "status", "s", "priority", "pri", "p", "assignee", "a", "author", "id":
				return &termNode{field: field, val: termVal}
			}
		}
		return &termNode{val: val}
	}

	p.next()
	return nil
}

// Match evaluates whether a task matches the search DSL query.
// If query is empty, it returns true.
func Match(query string, task *domain.Task) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return true
	}

	tokens := Tokenize(q)
	if len(tokens) == 0 {
		return true
	}

	p := &parser{tokens: tokens}
	ast := p.parseExpr()
	if ast == nil {
		// Fall back to simple substring match
		return (&termNode{val: q}).Eval(task)
	}

	return ast.Eval(task)
}
