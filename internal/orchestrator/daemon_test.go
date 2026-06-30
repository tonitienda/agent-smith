package orchestrator

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
)

const manualJob = `
id: job-manual
version: 1
owner: me
repository: o/r
triggers:
  - manual: {}
concurrency: { key: "repo:${repository}:manual", limit: 1 }
timeout: 10m
budget: { run: 1.0 }
permissions: { github: { issues: write } }
secrets: [github-token]
steps:
  - id: comment
    uses: github.comment
    with: { body_template: hi }
`

const cronJob = `
id: job-cron
version: 1
owner: me
repository: o/r
triggers:
  - cron: { schedule: "0 3 * * *", timezone: UTC }
concurrency: { key: "repo:${repository}:cron", limit: 1 }
timeout: 10m
budget: { run: 1.0 }
permissions: { github: { contents: read } }
secrets: [github-token]
steps:
  - id: comment
    uses: github.comment
    with: { body_template: hi }
`

const ghJob = `
id: job-gh
version: 1
owner: me
repository: o/r
triggers:
  - github.issue_labeled: { label: implementation }
concurrency: { key: "repo:${repository}:gh", limit: 1, on_conflict: drop }
timeout: 10m
budget: { run: 1.0 }
permissions: { github: { issues: read } }
known_labels: [implementation]
secrets: [github-token]
steps:
  - id: comment
    uses: github.comment
    with: { body_template: hi }
`

func newTestDaemon(t *testing.T, now *time.Time) *Daemon {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	fsys := fstest.MapFS{
		"job-manual.yaml": {Data: []byte(manualJob)},
		"job-cron.yaml":   {Data: []byte(cronJob)},
		"job-gh.yaml":     {Data: []byte(ghJob)},
	}
	specs, err := LoadJobs(fsys)
	if err != nil {
		t.Fatalf("load jobs: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("want 3 specs, got %d", len(specs))
	}
	d := New(st, Options{Now: func() time.Time { return *now }})
	if err := d.Publish(specs); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return d
}

func TestManualEnqueueAndRun(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d := newTestDaemon(t, &now)

	run, err := d.EnqueueManual("job-manual", nil)
	if err != nil {
		t.Fatalf("manual enqueue: %v", err)
	}
	if run.Status != store.StatusQueued || run.ConcurrencyKey != "repo:o/r:manual" {
		t.Fatalf("run = %+v", run)
	}
	done, ok, err := d.RunOne(context.Background())
	if err != nil || !ok {
		t.Fatalf("run one: ok=%v err=%v", ok, err)
	}
	if done.Status != store.StatusSucceeded || done.SessionID == "" {
		t.Fatalf("done = %+v", done)
	}
	if _, ok, _ := d.RunOne(context.Background()); ok {
		t.Fatal("queue should be empty")
	}
}

func TestEnqueueManualUnknownJob(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d := newTestDaemon(t, &now)
	if _, err := d.EnqueueManual("job-cron", nil); err == nil {
		t.Fatal("job-cron has no manual trigger; want error")
	}
	if _, err := d.EnqueueManual("nope", nil); err == nil {
		t.Fatal("unknown job; want error")
	}
}

func TestCronFiresOncePerSlot(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d := newTestDaemon(t, &now)

	// Before 03:00: nothing due.
	fired, err := d.FireDueCron(now)
	if err != nil || len(fired) != 0 {
		t.Fatalf("early fire = %v err=%v", fired, err)
	}
	// At 03:00: one run.
	at3 := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	fired, err = d.FireDueCron(at3)
	if err != nil || len(fired) != 1 {
		t.Fatalf("fire at 3 = %v err=%v", fired, err)
	}
	// Firing again at the same instant must not double-enqueue (slot advanced).
	fired, _ = d.FireDueCron(at3)
	if len(fired) != 0 {
		t.Fatalf("second fire double-enqueued: %v", fired)
	}
}

func TestGitHubTriggerMatch(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d := newTestDaemon(t, &now)

	none, err := d.EnqueueGitHub(GitHubEvent{DeliveryID: "d1", Kind: "github.issue_labeled", Repository: "o/r", Label: "other"})
	if err != nil || len(none) != 0 {
		t.Fatalf("non-matching label enqueued %v err=%v", none, err)
	}
	got, err := d.EnqueueGitHub(GitHubEvent{DeliveryID: "d2", Kind: "github.issue_labeled", Repository: "o/r", Label: "implementation"})
	if err != nil || len(got) != 1 {
		t.Fatalf("matching label = %v err=%v", got, err)
	}
	// Re-delivery of the same event id is idempotent.
	again, _ := d.EnqueueGitHub(GitHubEvent{DeliveryID: "d2", Kind: "github.issue_labeled", Repository: "o/r", Label: "implementation"})
	if again[0].ID != got[0].ID {
		t.Fatalf("re-delivery created a new run: %s vs %s", again[0].ID, got[0].ID)
	}
}

func TestOnConflictDrop(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d := newTestDaemon(t, &now)
	// job-gh uses on_conflict: drop. A second event (different delivery id) while
	// the first run is still queued is dropped.
	first, _ := d.EnqueueGitHub(GitHubEvent{DeliveryID: "a", Kind: "github.issue_labeled", Repository: "o/r", Label: "implementation"})
	if len(first) != 1 {
		t.Fatalf("first should enqueue: %v", first)
	}
	second, _ := d.EnqueueGitHub(GitHubEvent{DeliveryID: "b", Kind: "github.issue_labeled", Repository: "o/r", Label: "implementation"})
	if len(second) != 0 {
		t.Fatalf("drop policy should suppress second: %v", second)
	}
}

func TestRetryExecutor(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	specs, _ := LoadJobs(fstest.MapFS{"job-manual.yaml": {Data: []byte(manualJobRetry)}})
	fe := &flakyExecutor{failClass: store.FailureInternal, failTimes: 1}
	d := New(st, Options{Now: func() time.Time { return now }, Executor: fe})
	if err := d.Publish(specs); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := d.EnqueueManual("job-manual", nil); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// First attempt fails (internal, retryable) → requeued.
	r1, _, _ := d.RunOne(context.Background())
	if r1.Status != store.StatusQueued {
		t.Fatalf("after first try = %+v, want requeued", r1)
	}
	// Second attempt succeeds.
	r2, _, _ := d.RunOne(context.Background())
	if r2.Status != store.StatusSucceeded {
		t.Fatalf("after retry = %+v, want succeeded", r2)
	}
	atts, _ := st.Attempts(r2.ID)
	if len(atts) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(atts))
	}
}

const manualJobRetry = `
id: job-manual
version: 1
owner: me
repository: o/r
triggers:
  - manual: {}
concurrency: { key: "k", limit: 1 }
timeout: 10m
retries: { max: 1, backoff: fixed, initial: 1s }
budget: { run: 1.0 }
permissions: { github: { issues: write } }
secrets: [github-token]
steps:
  - id: comment
    uses: github.comment
    with: { body_template: hi }
`

// flakyExecutor fails its first failTimes calls with failClass, then succeeds.
type flakyExecutor struct {
	failClass store.FailureClass
	failTimes int
	calls     int
}

func (f *flakyExecutor) Execute(_ context.Context, run store.Run, _ *spec.Spec) (store.Outcome, error) {
	f.calls++
	if f.calls <= f.failTimes {
		return store.Outcome{Status: store.StatusFailed, FailureClass: f.failClass, Error: "boom"}, nil
	}
	return store.Outcome{Status: store.StatusSucceeded, SessionID: "ok-" + run.ID}, nil
}
