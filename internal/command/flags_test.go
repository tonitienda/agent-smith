package command

import (
	"context"
	"flag"
	"testing"
)

// flagCmd declares the apply bool and mark string flags used to exercise the
// shared parse path (AS-104).
func flagCmd() Command {
	return Command{Name: "clean", Flags: func(fs *flag.FlagSet) {
		fs.Bool("apply", false, "confirm")
		fs.String("mark", "", "label")
	}}
}

func TestParseFlagsAfterPositional(t *testing.T) {
	// A flag written after a positional is still parsed (the permutation), and the
	// positional is returned to the handler — never the flag token.
	ctx, rest, err := flagCmd().ParseFlags(context.Background(), []string{"blk1", "--apply"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if !FlagsFrom(ctx).Bool("apply") {
		t.Error("--apply after a positional was not parsed")
	}
	if len(rest) != 1 || rest[0] != "blk1" {
		t.Errorf("positionals = %v, want [blk1]", rest)
	}
}

func TestParseFlagsStringValue(t *testing.T) {
	ctx, _, err := flagCmd().ParseFlags(context.Background(), []string{"--mark", "before refactor"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if got := FlagsFrom(ctx).String("mark"); got != "before refactor" {
		t.Errorf("mark = %q, want %q", got, "before refactor")
	}
}

func TestParseFlagsUnknownErrors(t *testing.T) {
	if _, _, err := flagCmd().ParseFlags(context.Background(), []string{"--nope"}); err == nil {
		t.Error("an undeclared flag should be a parse error")
	}
}

func TestParseFlagsNilIsPassThrough(t *testing.T) {
	c := Command{Name: "x"}
	ctx := context.Background()
	got, rest, err := c.ParseFlags(ctx, []string{"a", "--b"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if got != ctx {
		t.Error("a flag-free command must return the original context unchanged")
	}
	if len(rest) != 2 || rest[0] != "a" || rest[1] != "--b" {
		t.Errorf("args = %v, want them returned verbatim", rest)
	}
}

func TestFlagsFromEmptyReadsUnset(t *testing.T) {
	// A handler reached without a parse (a face that parsed no flags) still reads
	// safely: every flag is unset, no nil check needed.
	f := FlagsFrom(context.Background())
	if f.Bool("apply") || f.String("mark") != "" {
		t.Error("an empty flag set must read as all-unset")
	}
}
