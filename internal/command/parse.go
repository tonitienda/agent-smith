package command

import (
	"fmt"
	"strings"
)

// Parse splits a command line into the command name and its arguments. The line
// may start with a slash (`/clean "old api"`) or not. Arguments are whitespace
// separated, except that double-quoted spans are kept intact with the quotes
// stripped — needed for `/clean "<topic>"` (AS-029) and any later command that
// takes a phrase. A backslash inside quotes escapes the next character.
//
// It returns the name without the leading slash and the positional arguments.
// An empty or slash-only line, or an unterminated quote, is an error.
func Parse(line string) (name string, args []string, err error) {
	tokens, err := tokenize(line)
	if err != nil {
		return "", nil, err
	}
	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}
	name = strings.TrimPrefix(tokens[0], "/")
	if name == "" {
		return "", nil, fmt.Errorf("empty command")
	}
	return name, tokens[1:], nil
}

// tokenize splits s on whitespace, honoring double quotes and backslash escapes.
func tokenize(s string) ([]string, error) {
	var (
		tokens  []string
		cur     strings.Builder
		inToken bool
		inQuote bool
	)
	flush := func() {
		if inToken {
			tokens = append(tokens, cur.String())
			cur.Reset()
			inToken = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			// Escape: keep the next byte verbatim (lets a quote or space be literal).
			i++
			cur.WriteByte(s[i])
			inToken = true
		case c == '"':
			inQuote = !inQuote
			inToken = true // an empty "" is still a (empty) token
		case !inQuote && (c == ' ' || c == '\t' || c == '\n'):
			flush()
		default:
			cur.WriteByte(c)
			inToken = true
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return tokens, nil
}
