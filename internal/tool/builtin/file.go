package builtin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// defaultReadLimit is the number of lines the read tool returns when the model
// does not specify a limit. Large enough for whole small files, bounded so a
// huge file does not flood the window; the byte cap (FS.maxReadBytes) is the
// independent backstop.
const defaultReadLimit = 2000

// readTool reads a file (optionally a line window of it) and records the content
// as a file_read block so /context can attribute window cost to the file and
// dedupe re-reads (PRD D3).
type readTool struct{ fs *FS }

func (t *readTool) Def() tool.Def {
	return tool.Def{
		Name: "read",
		Description: "Read a UTF-8 text file relative to the project root. " +
			"Optionally read a line window with 1-based offset and a line limit. " +
			"Output is recorded as a file_read block.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "File path relative to the project root."},
    "offset": {"type": "integer", "description": "1-based first line to read."},
    "limit": {"type": "integer", "description": "Maximum number of lines to read."}
  }
}`),
	}
}

func (t *readTool) Run(_ context.Context, args json.RawMessage) (tool.Output, error) {
	var in struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}

	abs, err := t.fs.resolve(in.Path)
	if err != nil {
		return errResult("%v", err), nil
	}

	info, err := os.Stat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errResult("file not found: %s", t.fs.rel(abs)), nil
	case err != nil:
		return errResult("cannot read %s: %v", t.fs.rel(abs), err), nil
	case info.IsDir():
		return errResult("%s is a directory, not a file", t.fs.rel(abs)), nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return errResult("cannot read %s: %v", t.fs.rel(abs), err), nil
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return errResult("%s looks like a binary file; the read tool only handles UTF-8 text", t.fs.rel(abs)), nil
	}

	content, rng := windowLines(string(data), in.Offset, in.Limit)
	content, truncated := capBytes(content, t.fs.maxReadBytes)
	if truncated {
		content += fmt.Sprintf("\n\n[read truncated at %d bytes]", t.fs.maxReadBytes)
	}

	// A successful read marks the file seen so a later write may overwrite it.
	t.fs.reads.mark(abs)

	sum := sha256.Sum256([]byte(content))
	return tool.Output{
		FileRead: &schema.FileReadBody{
			Path:        t.fs.rel(abs),
			Range:       rng,
			Content:     content,
			ContentHash: hex.EncodeToString(sum[:]),
			MediaType:   "text",
		},
	}, nil
}

// windowLines selects the [offset, offset+limit) line window (1-based offset)
// from content and reports the range it returned. A non-positive offset starts
// at line 1; a non-positive limit uses defaultReadLimit. The returned range is
// nil when the whole file was read, so the file_read block does not claim a
// partial window it did not take.
func windowLines(content string, offset, limit int) (string, *schema.LineRange) {
	if content == "" {
		return "", nil
	}
	lines := strings.Split(content, "\n")
	// A trailing newline yields a final empty element; drop it so line counts
	// match the file's visible lines.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	total := len(lines)

	start := offset
	if start <= 0 {
		start = 1
	}
	if start > total {
		start = total + 1 // empty selection past EOF
	}
	if limit <= 0 {
		limit = defaultReadLimit
	}
	end := start + limit - 1
	if end > total {
		end = total
	}

	whole := start == 1 && end == total
	if start > end {
		return "", lineRange(start, end)
	}
	out := strings.Join(lines[start-1:end], "\n")
	if whole {
		return out, nil
	}
	return out, lineRange(start, end)
}

func lineRange(start, end int) *schema.LineRange {
	s, e := start, end
	return &schema.LineRange{StartLine: &s, EndLine: &e}
}

// writeTool creates or overwrites a file. To avoid silently clobbering work the
// agent has not seen, it refuses to overwrite an existing file that has not been
// read this session (the read/edit tools mark files as read).
type writeTool struct{ fs *FS }

func (t *writeTool) Def() tool.Def {
	return tool.Def{
		Name: "write",
		Description: "Create or overwrite a file relative to the project root with the given content. " +
			"Refuses to overwrite an existing file that has not been read this session.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "content"],
  "properties": {
    "path": {"type": "string", "description": "File path relative to the project root."},
    "content": {"type": "string", "description": "Full file contents to write."}
  }
}`),
	}
}

func (t *writeTool) Run(_ context.Context, args json.RawMessage) (tool.Output, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}

	abs, err := t.fs.resolve(in.Path)
	if err != nil {
		return errResult("%v", err), nil
	}

	info, statErr := os.Stat(abs)
	switch {
	case statErr == nil && info.IsDir():
		return errResult("%s is a directory", t.fs.rel(abs)), nil
	case statErr == nil && !t.fs.reads.has(abs):
		return errResult("refusing to overwrite %s: read it first so changes are not lost", t.fs.rel(abs)), nil
	case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
		return errResult("cannot write %s: %v", t.fs.rel(abs), statErr), nil
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return errResult("cannot create directory for %s: %v", t.fs.rel(abs), err), nil
	}
	if err := os.WriteFile(abs, []byte(in.Content), 0o644); err != nil {
		return errResult("cannot write %s: %v", t.fs.rel(abs), err), nil
	}
	t.fs.reads.mark(abs) // we now know the file's contents

	return tool.Output{Text: fmt.Sprintf("Wrote %d bytes to %s", len(in.Content), t.fs.rel(abs))}, nil
}

// editTool replaces an exact substring in a file. The match must be unique
// unless replace_all is set, so an ambiguous edit fails loudly rather than
// changing the wrong occurrence.
type editTool struct{ fs *FS }

func (t *editTool) Def() tool.Def {
	return tool.Def{
		Name: "edit",
		Description: "Replace an exact string in a file. The old string must occur exactly once " +
			"unless replace_all is true. Fails if the string is absent or, without replace_all, ambiguous.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "old_string", "new_string"],
  "properties": {
    "path": {"type": "string", "description": "File path relative to the project root."},
    "old_string": {"type": "string", "description": "Exact text to replace."},
    "new_string": {"type": "string", "description": "Replacement text."},
    "replace_all": {"type": "boolean", "description": "Replace every occurrence instead of requiring a unique match."}
  }
}`),
	}
}

func (t *editTool) Run(_ context.Context, args json.RawMessage) (tool.Output, error) {
	var in struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	if in.OldString == "" {
		return errResult("old_string must not be empty"), nil
	}
	if in.OldString == in.NewString {
		return errResult("old_string and new_string are identical; nothing to change"), nil
	}

	abs, err := t.fs.resolve(in.Path)
	if err != nil {
		return errResult("%v", err), nil
	}

	data, err := os.ReadFile(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errResult("file not found: %s", t.fs.rel(abs)), nil
	case err != nil:
		return errResult("cannot read %s: %v", t.fs.rel(abs), err), nil
	}
	t.fs.reads.mark(abs) // editing implies reading the current contents

	content := string(data)
	count := strings.Count(content, in.OldString)
	switch {
	case count == 0:
		return errResult("old_string not found in %s", t.fs.rel(abs)), nil
	case count > 1 && !in.ReplaceAll:
		return errResult("old_string is ambiguous in %s: %d matches; pass replace_all or include more context", t.fs.rel(abs), count), nil
	}

	if in.ReplaceAll {
		content = strings.ReplaceAll(content, in.OldString, in.NewString)
	} else {
		content = strings.Replace(content, in.OldString, in.NewString, 1)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return errResult("cannot write %s: %v", t.fs.rel(abs), err), nil
	}

	return tool.Output{Text: fmt.Sprintf("Replaced %d occurrence(s) in %s", count, t.fs.rel(abs))}, nil
}

// capBytes truncates s to at most n bytes on a UTF-8 rune boundary, reporting
// whether it cut anything. A non-positive n disables the cap.
func capBytes(s string, n int) (string, bool) {
	if n <= 0 || len(s) <= n {
		return s, false
	}
	cut := n
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// utf8RuneStart reports whether b is the first byte of a UTF-8 rune (i.e. not a
// continuation byte).
func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// errResult builds a model-readable error Output: a single text part with the
// error flag set, so the Runtime records it as an error tool_result the model
// can react to rather than a Go error that aborts the turn.
func errResult(format string, args ...any) tool.Output {
	return tool.Output{Text: fmt.Sprintf(format, args...), IsError: true}
}
