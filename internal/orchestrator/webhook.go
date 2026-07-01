package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Normalize maps one raw GitHub webhook delivery into a [GitHubEvent] the scheduler
// can match against loaded jobs (AS-147). eventType is the X-GitHub-Event header
// (e.g. "issues"), deliveryID is the X-GitHub-Delivery GUID, and payload is the raw
// JSON body.
//
// ok is false for a well-formed delivery that names no Smith trigger — an issue
// opened rather than labeled, a PR closed without merging, a comment carrying no
// command. The caller drops such a delivery without error; only a malformed body
// (unparseable JSON) is an error. Because the mapping is total and side-effect free,
// the same delivery always normalises to the same record, and the delivery id keeps
// [Daemon.EnqueueGitHub] idempotent under GitHub's at-least-once redelivery.
//
// This is deliberately transport-agnostic: verifying the webhook HMAC signature and
// authenticating the deterministic action steps that answer these events belong to
// AS-148 (GitHub auth), and the PR-lifecycle policy that composes the actions is
// AS-149. Normalize only turns bytes into a trigger record.
func Normalize(eventType, deliveryID string, payload []byte) (GitHubEvent, bool, error) {
	var p ghPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return GitHubEvent{}, false, fmt.Errorf("orchestrator: normalize %s delivery %q: %w", eventType, deliveryID, err)
	}
	ev := GitHubEvent{
		DeliveryID: deliveryID,
		Repository: p.Repository.FullName,
		Actor:      p.Sender.Login,
	}
	switch eventType {
	case "issues":
		if p.Action != "labeled" || p.Issue == nil {
			return GitHubEvent{}, false, nil
		}
		ev.Kind = "github.issue_labeled"
		ev.Label = p.Label.Name
		ev.Number = p.Issue.Number
		ev.Labels = labelNames(p.Issue.Labels)
		ev.EventTime = p.Issue.UpdatedAt
	case "pull_request":
		if p.PullRequest == nil {
			return GitHubEvent{}, false, nil
		}
		ev.Number = p.PullRequest.Number
		ev.Base = p.PullRequest.Base.Ref
		ev.Labels = labelNames(p.PullRequest.Labels)
		ev.EventTime = p.PullRequest.UpdatedAt
		switch {
		case p.Action == "labeled":
			ev.Kind = "github.pr_labeled"
			ev.Label = p.Label.Name
		case p.Action == "closed" && p.PullRequest.Merged:
			ev.Kind = "github.pr_merged"
		default:
			return GitHubEvent{}, false, nil
		}
	case "issue_comment":
		if p.Action != "created" || p.Comment == nil || p.Issue == nil {
			return GitHubEvent{}, false, nil
		}
		cmd := parseCommand(p.Comment.Body)
		if cmd == "" {
			return GitHubEvent{}, false, nil
		}
		ev.Kind = "github.comment_command"
		ev.Command = cmd
		ev.Number = p.Issue.Number
		ev.Labels = labelNames(p.Issue.Labels)
		ev.EventTime = p.Comment.CreatedAt
		if ev.Actor == "" {
			ev.Actor = p.Comment.User.Login
		}
	default:
		return GitHubEvent{}, false, nil
	}
	return ev, true, nil
}

// parseCommand extracts a comment-command trigger's bare command word from a comment
// body: the first non-empty line must start with "/", and its first word is the
// command (a leading "/smith" prefix is allowed, so "/smith implement" and
// "/implement" both yield "implement"). Anything else yields "" (not a command).
func parseCommand(body string) string {
	// First non-empty line only; slice to the first newline rather than splitting
	// the whole (untrusted, possibly large) body into a slice of every line.
	line := strings.TrimLeft(body, " \t\r\n")
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return ""
	}
	fields := strings.Fields(line[1:])
	if len(fields) == 0 {
		return ""
	}
	if fields[0] == "smith" && len(fields) > 1 {
		return fields[1]
	}
	return fields[0]
}

func labelNames(ls []ghLabel) []string {
	if len(ls) == 0 {
		return nil
	}
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}

// ghPayload is the subset of a GitHub webhook payload Normalize reads. Unlisted
// fields are ignored; GitHub adds fields additively, so a wider payload still maps.
type ghPayload struct {
	Action      string     `json:"action"`
	Repository  ghRepo     `json:"repository"`
	Sender      ghUser     `json:"sender"`
	Label       ghLabel    `json:"label"`
	Issue       *ghIssue   `json:"issue"`
	PullRequest *ghPR      `json:"pull_request"`
	Comment     *ghComment `json:"comment"`
}

type ghRepo struct {
	FullName string `json:"full_name"`
}

type ghUser struct {
	Login string `json:"login"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghIssue struct {
	Number    int       `json:"number"`
	Labels    []ghLabel `json:"labels"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ghPR struct {
	Number    int       `json:"number"`
	Merged    bool      `json:"merged"`
	Labels    []ghLabel `json:"labels"`
	UpdatedAt time.Time `json:"updated_at"`
	Base      struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type ghComment struct {
	Body      string    `json:"body"`
	User      ghUser    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}
