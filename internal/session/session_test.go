package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

func TestSessionPersistsAndReloadsProjection(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(t.TempDir(), "repo")
	store, err := NewStore(root, project)
	if err != nil {
		t.Fatal(err)
	}

	s, err := store.Create("first task")
	if err != nil {
		t.Fatal(err)
	}
	first := textBlock("b1", schema.RoleUser, "hello", "gpt-5.5")
	second := textBlock("b2", schema.RoleAssistant, "world", "gpt-5.5")
	storedFirst, err := s.Log.Append(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Log.Append(second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Log.Append(eventlog.NewExclusion("/clean", storedFirst.ID)); err != nil {
		t.Fatal(err)
	}
	want := projection.Project(s.Log.Events(), projection.Options{}).Live()
	if err := s.Log.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := store.Open(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Log.Close()
	got := projection.Project(reloaded.Log.Events(), projection.Options{}).Live()
	if len(got) != len(want) || got[0].ID != want[0].ID || got[0].Text.Text != "world" {
		t.Fatalf("reloaded projection = %#v, want %#v", got, want)
	}
}

func TestListIsProjectScopedAndDerivedFromLog(t *testing.T) {
	root := t.TempDir()
	projectA := filepath.Join(t.TempDir(), "a")
	projectB := filepath.Join(t.TempDir(), "b")
	storeA, err := NewStore(root, projectA)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewStore(root, projectB)
	if err != nil {
		t.Fatal(err)
	}

	sA, err := storeA.Create("project a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sA.Log.Append(textBlock("a1", schema.RoleAssistant, "a", "claude-sonnet-4-5")); err != nil {
		t.Fatal(err)
	}
	if err := sA.Log.Close(); err != nil {
		t.Fatal(err)
	}

	sB, err := storeB.Create("project b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sB.Log.Append(textBlock("b1", schema.RoleAssistant, "b", "gpt-5.5")); err != nil {
		t.Fatal(err)
	}
	if err := sB.Log.Close(); err != nil {
		t.Fatal(err)
	}

	listA, err := storeA.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 1 {
		t.Fatalf("project A list length = %d, want 1", len(listA))
	}
	summary := listA[0]
	if summary.ID != sA.ID || summary.ProjectPath != storeA.ProjectDir() {
		t.Fatalf("wrong project summary: %#v", summary)
	}
	if summary.EventCount != 1 || summary.SizeBytes == 0 {
		t.Fatalf("summary did not derive log totals: %#v", summary)
	}
	if len(summary.Models) != 1 || summary.Models[0] != "claude-sonnet-4-5" {
		t.Fatalf("summary models = %#v", summary.Models)
	}
	if summary.UpdatedAt.IsZero() || summary.UpdatedAt.Before(summary.CreatedAt.Add(-time.Second)) {
		t.Fatalf("summary updated_at = %s, created_at = %s", summary.UpdatedAt, summary.CreatedAt)
	}
}

func TestListSkipsIncompleteSessionWithoutCreatingLog(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(t.TempDir(), "repo")
	store, err := NewStore(root, project)
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(store.ProjectSessionsDir(), "sess_incomplete")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := Metadata{ID: "sess_incomplete", ProjectPath: store.ProjectDir(), CreatedAt: time.Now().UTC(), Title: "partial"}
	if err := writeMetadata(dir, meta); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("summaries = %#v, want none for incomplete session", summaries)
	}
	if _, err := os.Stat(filepath.Join(dir, eventLogFile)); !os.IsNotExist(err) {
		t.Fatalf("List created event log: stat err = %v", err)
	}
}

func TestOpenRejectsTraversalIDs(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", ".", "..", "../outside", `..\outside`} {
		if _, err := store.Open(id); err == nil {
			t.Fatalf("Open(%q) succeeded, want error", id)
		}
	}
}

func textBlock(id string, role schema.Role, text, model string) schema.Block {
	return schema.Block{
		ID:       id,
		Kind:     schema.KindText,
		Role:     role,
		Provider: &schema.Provider{Vendor: "test", Model: model},
		Text:     &schema.TextBody{Text: text},
	}
}
