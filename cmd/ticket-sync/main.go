// Command ticket-sync pushes ticket markdown files to GitHub issues.
//
// The files in tickets/ are the source of truth. For each selected ticket:
//   - github_issue: null  -> a new issue is created and the number is written
//     back into the file's frontmatter
//   - github_issue: <n>   -> issue #n is overwritten from the file
//
// There is no merging: title, body, and labels on GitHub are replaced with
// whatever the file says.
//
// By default it selects ticket files that are added/edited but not yet pushed
// (uncommitted changes plus commits ahead of the upstream). Use -all to sync
// every ticket, or pass explicit paths.
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
)

const ticketsDir = "docs/project/tickets"

var (
	ticketFileRe = regexp.MustCompile(`^AS-\d+.*\.md$`)
	issueLineRe  = regexp.MustCompile(`(?m)^github_issue: *null *$`)
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
	dryRun := flag.Bool("dry-run", false, "print planned actions without calling GitHub or editing files")
	flag.Parse()

	files, err := selectFiles(flag.Args(), *all)
	if err != nil {
		fatal(err)
	}
	if len(files) == 0 {
		fmt.Println("no changed ticket files; use -all or pass paths explicitly")
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

	repo := ""
	if !*dryRun {
		if repo, err = resolveRepo(*repoFlag); err != nil {
			fatal(err)
		}
		ensureLabels(repo, tickets)
	}

	failed := 0
	for _, t := range tickets {
		if err := syncTicket(repo, t, *dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", t.path, err)
			failed++
		}
	}
	if failed > 0 {
		fatal(fmt.Errorf("%d ticket(s) failed to sync", failed))
	}
}

// selectFiles returns the ticket files to sync: explicit args if given,
// every ticket with -all, otherwise the ones git considers not yet pushed.
func selectFiles(args []string, all bool) ([]string, error) {
	if len(args) > 0 {
		return filterTickets(args), nil
	}
	if all {
		return filepath.Glob(filepath.Join(ticketsDir, "AS-*.md"))
	}
	changed, err := changedFiles()
	if err != nil {
		return nil, err
	}
	return filterTickets(changed), nil
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

func payload(t *ticket) ([]byte, error) {
	footer := fmt.Sprintf(
		"\n---\n_Synced from `%s` — the file is the source of truth; edits made directly to this issue will be overwritten._\n",
		t.path,
	)
	return json.Marshal(map[string]any{
		"title":  fmt.Sprintf("[%s] %s", t.id, t.title),
		"body":   t.body + footer,
		"labels": labels(t),
	})
}

func syncTicket(repo string, t *ticket, dryRun bool) error {
	if dryRun {
		action := "update issue #" + strconv.Itoa(t.issue)
		if t.issue == 0 {
			action = "create issue"
		}
		fmt.Printf("%s: would %s  [%s · %s · %s]\n", t.id, action, t.status, t.priority, t.area)
		return nil
	}

	body, err := payload(t)
	if err != nil {
		return err
	}
	if t.issue == 0 {
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
		fmt.Printf("%s: created issue #%d\n", t.id, res.Number)
		return nil
	}

	if _, err := ghAPI(fmt.Sprintf("repos/%s/issues/%d", repo, t.issue), "PATCH", body); err != nil {
		return err
	}
	fmt.Printf("%s: updated issue #%d\n", t.id, t.issue)
	return nil
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
	fmt.Fprintf(os.Stderr, "ticket-sync: %v\n", err)
	os.Exit(1)
}
