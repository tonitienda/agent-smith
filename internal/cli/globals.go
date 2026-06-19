package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
)

// OutputMode selects how a command renders its result (D-CLI-4).
type OutputMode string

const (
	// OutputPlain is human/bare text — the default, with or without color.
	OutputPlain OutputMode = "plain"
	// OutputJSON is a single structured result.
	OutputJSON OutputMode = "json"
	// OutputStreamJSON is incremental events from the same substrate the TUI
	// renders: `smith run --output stream-json` writes one JSON object per line as
	// the run progresses, then a final result object (AS-051).
	OutputStreamJSON OutputMode = "stream-json"
)

// Globals are the resolved cross-subcommand options every leaf shares (D-CLI-4,
// D-CLI-5, D-CLI-6, D-CLI-8).
type Globals struct {
	Output   OutputMode
	UseColor bool // resolved decision: should output carry ANSI color
	Quiet    bool
	Verbose  bool
	Config   string // explicit config file path (--config), "" for the default chain
	Yes      bool   // confirm destructive ops on a non-TTY (D-CLI-8)
}

// globalVars holds the raw flag pointers before resolution.
type globalVars struct {
	output  *string
	color   *string
	quiet   *bool
	verbose *bool
	config  *string
	yes     *bool
	help    *bool
}

// registerGlobals adds the shared flags to fs. They are registered on every
// leaf's flag set so `smith run "…" --output json` parses regardless of where the
// flag sits. `--help`/`-h` is a real bool (not flag's built-in) so a following
// `--output json` is still parsed — that powers `--help --output json`
// (D-CLI-10).
func registerGlobals(fs *flag.FlagSet) *globalVars {
	g := &globalVars{
		output:  fs.String("output", "", "output format: plain|json|stream-json"),
		color:   fs.String("color", "auto", "color: auto|always|never"),
		config:  fs.String("config", "", "path to a config file (overrides the default chain)"),
		quiet:   fs.Bool("quiet", false, "quieter diagnostics on stderr"),
		verbose: fs.Bool("verbose", false, "more diagnostics on stderr"),
		yes:     fs.Bool("yes", false, "assume yes for destructive operations on a non-TTY"),
		help:    fs.Bool("help", false, "show help for this command"),
	}
	fs.BoolVar(g.quiet, "q", false, "alias for --quiet")
	fs.BoolVar(g.verbose, "v", false, "alias for --verbose")
	fs.BoolVar(g.help, "h", false, "alias for --help")
	return g
}

// resolveGlobals validates and resolves the raw flags against the TTY and
// NO_COLOR, producing the Globals a handler reads.
func (a *App) resolveGlobals(g *globalVars) (Globals, error) {
	out := OutputMode(*g.output)
	switch out {
	case "":
		out = OutputPlain // default; non-TTY still plain (no ANSI) per D-CLI-4
	case OutputPlain, OutputJSON, OutputStreamJSON:
	default:
		return Globals{}, fmt.Errorf("invalid --output %q: want plain, json, or stream-json", *g.output)
	}

	useColor, err := a.resolveColor(*g.color)
	if err != nil {
		return Globals{}, err
	}

	return Globals{
		Output:   out,
		UseColor: useColor,
		Quiet:    *g.quiet,
		Verbose:  *g.verbose,
		Config:   *g.config,
		Yes:      *g.yes,
	}, nil
}

// resolveColor decides whether to emit ANSI color: an explicit --color wins;
// otherwise auto means "a TTY with NO_COLOR unset" (D-CLI-4, clig.dev).
func (a *App) resolveColor(mode string) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto", "":
		return a.StdoutTTY && a.Getenv("NO_COLOR") == "", nil
	default:
		return false, fmt.Errorf("invalid --color %q: want auto, always, or never", mode)
	}
}

// Emit writes a command's plain-text result honoring --output (D-CLI-4): plain
// prints the text as-is; json/stream-json wraps it as {"text": …}. Richer fields
// (cost, session id, stop reason) are additive in AS-051. Results always land on
// stdout (D-CLI-5).
func (c *Context) Emit(text string) error {
	switch c.Globals.Output {
	case OutputJSON, OutputStreamJSON:
		return writeJSON(c.Stdout, struct {
			Text string `json:"text"`
		}{text}, "")
	default:
		_, err := fmt.Fprintln(c.Stdout, text)
		return err
	}
}

// WriteJSON marshals v as one compact JSON line to the command's stdout, without
// HTML-escaping so prompts/answers carrying <, >, & stay readable (D-CLI-5: data
// to stdout). It is the seam a handler with a richer structured result than the
// shared {text} envelope uses — `smith run --output json` emits answer, cost,
// session id, and stop reason this way (AS-051).
func (c *Context) WriteJSON(v any) error { return writeJSON(c.Stdout, v, "") }

// writeJSON marshals v to w without HTML-escaping (so prompts containing <, >, &
// stay readable) and appends a newline. indent "" emits compact JSON.
func writeJSON(w io.Writer, v any, indent string) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	return enc.Encode(v)
}
