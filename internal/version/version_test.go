package version

import "testing"

func TestStringOmitsUnknownCommit(t *testing.T) {
	oldName, oldVersion, oldCommit := Name, Version, Commit
	t.Cleanup(func() {
		Name, Version, Commit = oldName, oldVersion, oldCommit
	})

	Name = "smith"
	Version = "test"
	Commit = "unknown"

	got := String()
	want := "smith test"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringIncludesKnownCommit(t *testing.T) {
	oldName, oldVersion, oldCommit := Name, Version, Commit
	t.Cleanup(func() {
		Name, Version, Commit = oldName, oldVersion, oldCommit
	})

	Name = "smith"
	Version = "test"
	Commit = "abc123"

	got := String()
	want := "smith test (abc123)"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
