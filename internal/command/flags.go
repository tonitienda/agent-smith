package command

import (
	"context"
	"flag"
	"io"
	"strings"
)

// Flags is a command's parsed command-specific flags, read by a Handler from
// its context (AS-104). A command declares its flags once via Command.Flags;
// both faces parse them through ParseFlags before Run — the TUI after lexing the
// slash line (Parse), the CLI after permuting argv — so the slash form and its
// subcommand can't disagree about a flag. A nil *Flags reads as all-unset, so a
// handler can call FlagsFrom(ctx).Bool(...) unconditionally.
type Flags struct{ fs *flag.FlagSet }

// Bool reports a boolean flag's parsed value; false when unset or undeclared.
func (f *Flags) Bool(name string) bool {
	fl := f.lookup(name)
	if fl == nil {
		return false
	}
	if g, ok := fl.Value.(flag.Getter); ok {
		if b, ok := g.Get().(bool); ok {
			return b
		}
	}
	return fl.Value.String() == "true"
}

// String returns a flag's parsed value; "" when unset or undeclared.
func (f *Flags) String(name string) string {
	if fl := f.lookup(name); fl != nil {
		return fl.Value.String()
	}
	return ""
}

func (f *Flags) lookup(name string) *flag.Flag {
	if f == nil || f.fs == nil {
		return nil
	}
	return f.fs.Lookup(name)
}

// Set reports whether the flag was present on the command line, distinct from a
// flag left at its zero value. A string flag like /rewind --mark needs this: an
// empty label still means "mark requested" (the handler then explains the label
// is required), where a bare /rewind with no --mark must list instead.
func (f *Flags) Set(name string) bool {
	if f == nil || f.fs == nil {
		return false
	}
	seen := false
	f.fs.Visit(func(fl *flag.Flag) {
		if fl.Name == name {
			seen = true
		}
	})
	return seen
}

type flagsKey struct{}

// WithFlags carries parsed flags on ctx for the handler. Faces call it through
// ParseFlags; it is exported so a test can drive a handler with set flags.
func WithFlags(ctx context.Context, f *Flags) context.Context {
	return context.WithValue(ctx, flagsKey{}, f)
}

// FlagsFrom returns the flags carried on ctx, or an empty (all-unset) set when a
// face parsed none — so a handler need not nil-check before reading a flag.
func FlagsFrom(ctx context.Context) *Flags {
	if ctx == nil {
		return &Flags{}
	}
	if f, ok := ctx.Value(flagsKey{}).(*Flags); ok {
		return f
	}
	return &Flags{}
}

// ParseFlags binds the command's declared flags, permutes flag tokens ahead of
// the positionals (so a flag may follow a positional, matching the CLI), parses
// them, and returns the positional remainder plus a context carrying the parsed
// values for the handler. A command with no Flags is a pass-through: args is
// returned unchanged with the original ctx. ParseFlags only sees already
// tokenized args, so slash lexing (Parse) stays isolated from flag parsing.
func (c Command) ParseFlags(ctx context.Context, args []string) (context.Context, []string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.Flags == nil {
		return ctx, args, nil
	}
	fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c.Flags(fs)
	if err := fs.Parse(PermuteFlags(fs, args)); err != nil {
		return ctx, nil, err
	}
	return WithFlags(ctx, &Flags{fs: fs}), fs.Args(), nil
}

// PermuteFlags reorders args so every flag precedes the positionals, letting the
// stdlib flag parser (which stops at the first non-flag) accept a flag written
// after a positional. A bare "--" ends flag parsing: everything after it is
// positional. A non-bool flag carries its following value token along unless
// written as `--flag=value`. internal/cli permutes argv through this same helper
// (its runLeaf), so both faces order flags and positionals identically.
func PermuteFlags(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			return append(flags, positional...)
		case len(a) > 1 && a[0] == '-':
			flags = append(flags, a)
			// `--name=value` is self-contained; a non-bool `--name value` needs the
			// next token too.
			if !strings.Contains(a, "=") && flagTakesValue(fs, a) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

// flagTakesValue reports whether the flag token names a registered non-bool flag
// (so the next token is its value). Unknown flags are treated as value-less; the
// flag parser then reports the error.
func flagTakesValue(fs *flag.FlagSet, token string) bool {
	f := fs.Lookup(strings.TrimLeft(token, "-"))
	if f == nil {
		return false
	}
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !ok || !bf.IsBoolFlag()
}
