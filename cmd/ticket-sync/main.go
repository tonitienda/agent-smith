// Command ticket-sync pushes ticket markdown files to GitHub issues.
//
// The files in tickets/ are the source of truth. For each selected ticket:
//   - github_issue: null  -> a new issue is created and the number is written
//     back into the file's frontmatter
//   - github_issue: <n>   -> issue #n is overwritten from the file
//
// There is no merging: title, body, and labels on GitHub are replaced with
// whatever the file says. Tickets whose frontmatter says `status: done` also
// close their related GitHub issue.
//
// By default it selects ticket files that are added/edited but not yet pushed
// (uncommitted changes plus commits ahead of the upstream). Use -all to sync
// every ticket, or pass explicit paths. Use -changed-since 12h to select
// tickets committed within a window plus any still unlinked (github_issue:
// null) — the selection the scheduled sync uses.
// Use -require-existing in merge automation when an unlinked ticket should be
// a hard error, or -skip-unlinked to quietly leave unlinked tickets for the
// scheduled sync to create.
//
// GitHub access goes through the gh CLI, so `gh auth login` must have been
// run. The target repo is taken from -repo, then $TICKET_SYNC_REPO, then the
// repo of the current git remote.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ticketsDir = "docs/project/tickets"

var (
	ticketFileRe  = regexp.MustCompile(`^AS-\d+.*\.md$`)
	issueLineRe   = regexp.MustCompile(`(?m)^github_issue: *null *$`)
	linkedIssueRe = regexp.MustCompile(`(?m)^github_issue: *(\d+) *$`)
)

type ticket struct {
	path     string
	id       string
	title    string
	status   string
	area     string
	priority string
	issue    int // 0 = not created yet
	body     string
}

func main() {
	repoFlag := flag.String("repo", "", "GitHub repo as owner/name (default: $TICKET_SYNC_REPO, then the current git remote)")
	all := flag.Bool("all", false, "sync every ticket file, not just the ones changed since the last push")
	changedSince := flag.Duration("changed-since", 0, "select tickets whose files were committed within this window, plus any with github_issue: null (e.g. 12h); ignored when -all or explicit paths are given")
	dryRun := flag.Bool("dry-run", false, "print planned actions without calling GitHub or editing files")
	requireExisting := flag.Bool("require-existing", false, "fail instead of creating issues for tickets with github_issue: null")
	skipUnlinked := flag.Bool("skip-unlinked", false, "skip (instead of creating or failing) tickets with github_issue: null")
	flag.Parse()

	files, err := selectFiles(flag.Args(), *all, *changedSince)
	if err != nil {
		fatal(err)
	}
	if len(files) == 0 {
		if _, err := fmt.Fprintln(os.Stdout, "no changed ticket files; use -all or pass paths explicitly"); err != nil {
			fatal(err)
		}
		return
	}
	sort.Strings(files)

	tickets := make([]*ticket, 0, len(files))
	for _, f := range files {
		t, err := parseTicket(f)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", f, err))
		}
		tickets = append(tickets, t)
	}

	if *requireExisting {
		for _, t := range tickets {
			if err := requireLinkedIssue(t); err != nil {
				fatal(fmt.Errorf("%s: %w", t.path, err))
			}
		}
	}

	repo := ""
	if !*dryRun {
		if repo, err = resolveRepo(*repoFlag); err != nil {
			fatal(err)
		}
		ensureLabels(repo, tickets)
	}

	failed := 0
	for _, t := range tickets {
		if err := syncTicket(repo, t, syncOptions{dryRun: *dryRun, requireExisting: *requireExisting, skipUnlinked: *skipUnlinked}); err != nil {
			if _, printErr := fmt.Fprintf(os.Stderr, "error: %s: %v\n", t.path, err); printErr != nil {
				fatal(printErr)
			}
			failed++
		}
	}
	if failed > 0 {
		fatal(fmt.Errorf("%d ticket(s) failed to sync", failed))
	}
}

// selectFiles returns the ticket files to sync: explicit args if given,
// every ticket with -all, the recently-committed-or-unlinked ones with
// -changed-since, otherwise the ones git considers not yet pushed.
func selectFiles(args []string, all bool, since time.Duration) ([]string, error) {
	if len(args) > 0 {
		return filterTickets(args), nil
	}
	if all {
		return filepath.Glob(filepath.Join(ticketsDir, "AS-*.md"))
	}
	if since > 0 {
		return selectChangedSince(since)
	}
	changed, err := changedFiles()
	if err != nil {
		return nil, err
	}
	return filterTickets(changed), nil
}

// selectChangedSince returns every ticket file that was committed within the
// window OR that has no linked issue yet (github_issue: null/missing). The
// latter is what makes the scheduled run self-healing: tickets merged without
// an issue get picked up no matter how long ago they landed.
//
// Requires full git history (fetch-depth: 0) so commit times older than HEAD
// are visible; on a shallow clone only HEAD's changes would be considered
// recent, but unlinked tickets are still caught by the github_issue check.
func selectChangedSince(since time.Duration) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(ticketsDir, "AS-*.md"))
	if err != nil {
		return nil, err
	}

	// One git log over the whole tickets dir, rather than one per file, so the
	// cost is a single process no matter how many tickets exist.
	cutoff := time.Now().Add(-since).Format(time.RFC3339)
	out, err := git("log", "--name-only", "--format=", "--since="+cutoff, "--", ticketsDir)
	if err != nil {
		return nil, err
	}
	recent := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			recent[filepath.Clean(p)] = true
		}
	}

	var selected []string
	for _, f := range matches {
		if recent[filepath.Clean(f)] || !isLinked(f) {
			selected = append(selected, f)
		}
	}
	sort.Strings(selected)
	return selected, nil
}

// isLinked reports whether the file already records a numeric github_issue.
// Missing, null, or empty values count as unlinked.
func isLinked(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return linkedIssueRe.Match(raw)
}

func filterTickets(paths []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		p = filepath.Clean(p)
		if filepath.Dir(p) != ticketsDir || !ticketFileRe.MatchString(filepath.Base(p)) || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// changedFiles lists paths that are staged, unstaged, untracked, or committed
// but not yet pushed to the upstream branch.
func changedFiles() ([]string, error) {
	var paths []string

	// -uall lists files inside untracked directories individually instead
	// of collapsing them to "dir/".
	out, err := git("status", "--porcelain", "-uall")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		paths = append(paths, p)
	}

	// Commits ahead of upstream; silently skipped when no upstream exists
	// (fresh repo) — uncommitted changes were already collected above.
	if out, err := git("diff", "--name-only", "@{upstream}..HEAD"); err == nil {
		for _, p := range strings.Split(out, "\n") {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths, nil
}

func parseTicket(path string) (*ticket, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(raw)
	if !strings.HasPrefix(content, "---\n") {
		return nil, errors.New("missing frontmatter")
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return nil, errors.New("unterminated frontmatter")
	}
	front := content[4 : 4+end]
	body := strings.TrimLeft(content[4+end+5:], "\n")

	t := &ticket{path: path, body: body}
	for _, line := range strings.Split(front, "\n") {
		if strings.HasPrefix(line, " ") { // nested values (e.g. list items) — not synced
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "id":
			t.id = val
		case "title":
			t.title = strings.Trim(val, `"`)
		case "status":
			t.status = val
		case "area":
			t.area = val
		case "priority":
			t.priority = val
		case "github_issue":
			if val != "null" && val != "" {
				n, err := strconv.Atoi(val)
				if err != nil {
					return nil, fmt.Errorf("invalid github_issue value %q", val)
				}
				t.issue = n
			}
		}
	}
	if t.id == "" || t.title == "" {
		return nil, errors.New("frontmatter must set id and title")
	}
	return t, nil
}

func labels(t *ticket) []string {
	var ls []string
	if t.status != "" {
		ls = append(ls, t.status)
	}
	if t.area != "" {
		ls = append(ls, "area:"+t.area)
	}
	if t.priority != "" {
		ls = append(ls, t.priority)
	}
	return ls
}

type payloadOptions struct {
	includeState bool
}

func payload(t *ticket, opts payloadOptions) ([]byte, error) {
	footer := fmt.Sprintf(
		"\n---\n_Synced from `%s` — the file is the source of truth; edits made directly to this issue will be overwritten._\n",
		t.path,
	)
	body := map[string]any{
		"title":  fmt.Sprintf("[%s] %s", t.id, t.title),
		"body":   t.body + footer,
		"labels": labels(t),
	}
	if opts.includeState && t.status == "done" {
		body["state"] = "closed"
		body["state_reason"] = "completed"
	}
	return json.Marshal(body)
}

func closePayload() ([]byte, error) {
	return json.Marshal(map[string]string{
		"state":        "closed",
		"state_reason": "completed",
	})
}

type syncOptions struct {
	dryRun          bool
	requireExisting bool
	skipUnlinked    bool
}

func requireLinkedIssue(t *ticket) error {
	if t.issue == 0 {
		return errors.New("github_issue is null; run ticket-sync before merging so this ticket is linked to an issue")
	}
	return nil
}

func syncTicket(repo string, t *ticket, opts syncOptions) error {
	if opts.requireExisting {
		if err := requireLinkedIssue(t); err != nil {
			return err
		}
	}

	if t.issue == 0 && opts.skipUnlinked {
		_, err := fmt.Printf("%s: skipped (github_issue is null; left for the scheduled sync)\n", t.id)
		return err
	}

	if opts.dryRun {
		action := "update issue #" + strconv.Itoa(t.issue)
		if t.issue == 0 {
			action = "create issue"
		}
		if t.status == "done" {
			action += " and close it"
		}
		_, err := fmt.Printf("%s: would %s  [%s · %s · %s]\n", t.id, action, t.status, t.priority, t.area)
		return err
	}

	if t.issue == 0 {
		body, err := payload(t, payloadOptions{})
		if err != nil {
			return err
		}
		resp, err := ghAPI(fmt.Sprintf("repos/%s/issues", repo), "POST", body)
		if err != nil {
			return err
		}
		var res struct {
			Number int `json:"number"`
		}
		if err := json.Unmarshal(resp, &res); err != nil || res.Number == 0 {
			return fmt.Errorf("could not read issue number from GitHub response: %s", resp)
		}
		if err := writeIssueNumber(t.path, res.Number); err != nil {
			return fmt.Errorf("issue #%d created but writing the number back failed: %w", res.Number, err)
		}
		if t.status == "done" {
			if err := closeIssue(repo, res.Number); err != nil {
				return fmt.Errorf("issue #%d created but closing it failed: %w", res.Number, err)
			}
			_, err = fmt.Printf("%s: created and closed issue #%d\n", t.id, res.Number)
			return err
		}
		_, err = fmt.Printf("%s: created issue #%d\n", t.id, res.Number)
		return err
	}

	body, err := payload(t, payloadOptions{includeState: true})
	if err != nil {
		return err
	}
	if _, err := ghAPI(fmt.Sprintf("repos/%s/issues/%d", repo, t.issue), "PATCH", body); err != nil {
		return err
	}
	_, err = fmt.Printf("%s: updated issue #%d\n", t.id, t.issue)
	return err
}

func closeIssue(repo string, issue int) error {
	body, err := closePayload()
	if err != nil {
		return err
	}
	_, err = ghAPI(fmt.Sprintf("repos/%s/issues/%d", repo, issue), "PATCH", body)
	return err
}

func writeIssueNumber(path string, n int) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := issueLineRe.ReplaceAll(raw, fmt.Appendf(nil, "github_issue: %d", n))
	if bytes.Equal(updated, raw) {
		return errors.New("no 'github_issue: null' line found to replace")
	}
	return os.WriteFile(path, updated, 0o644)
}

// ensureLabels best-effort creates the labels used by the tickets so issue
// creation can attach them. --force makes re-creation a no-op-ish update;
// failures are ignored because attaching pre-existing labels still works.
func ensureLabels(repo string, tickets []*ticket) {
	seen := map[string]bool{}
	for _, t := range tickets {
		for _, l := range labels(t) {
			if seen[l] {
				continue
			}
			seen[l] = true
			_ = exec.Command("gh", "label", "create", l, "--repo", repo, "--force").Run()
		}
	}
}

func resolveRepo(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if env := os.Getenv("TICKET_SYNC_REPO"); env != "" {
		return env, nil
	}
	out, err := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner").Output()
	if err == nil {
		if repo := strings.TrimSpace(string(out)); repo != "" {
			return repo, nil
		}
	}
	return "", errors.New("cannot determine GitHub repo: pass -repo owner/name, set TICKET_SYNC_REPO, or add a GitHub remote")
}

func ghAPI(endpoint, method string, input []byte) ([]byte, error) {
	cmd := exec.Command("gh", "api", endpoint, "--method", method, "--input", "-")
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api %s: %v: %s", endpoint, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func fatal(err error) {
	if _, printErr := fmt.Fprintf(os.Stderr, "ticket-sync: %v\n", err); printErr != nil {
		os.Exit(1)
	}
	os.Exit(1)
}
