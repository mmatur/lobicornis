package repository

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/v74/github"
	"github.com/rs/zerolog/log"
)

// issueRefPattern matches a single issue reference: either `#NNN` or a full GitHub issue URL.
const issueRefPattern = `(?:#\d+|https?://github\.com/[\w.-]+/[\w.-]+/issues/\d+)`

// Mjolnir the hammer of Thor.
type Mjolnir struct {
	client *github.Client

	globalFixesIssueRE *regexp.Regexp
	issueRefRE         *regexp.Regexp

	dryRun bool

	owner string
	name  string
}

func newMjolnir(client *github.Client, owner, name string, dryRun bool) Mjolnir {
	return Mjolnir{
		client: client,

		globalFixesIssueRE: regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\b[\s:]+(` + issueRefPattern + `(?:[\s,]+` + issueRefPattern + `)*)`),
		issueRefRE:         regexp.MustCompile(`#(\d+)|https?://github\.com/([\w.-]+)/([\w.-]+)/issues/(\d+)`),

		dryRun: dryRun,

		owner: owner,
		name:  name,
	}
}

// CloseRelatedIssues Closes issues listed in the PR description.
func (m Mjolnir) CloseRelatedIssues(ctx context.Context, pr *github.PullRequest) error {
	logger := log.Ctx(ctx)

	issueNumbers := m.parseIssueFixes(ctx, pr.GetBody())

	for _, issueNumber := range issueNumbers {
		logger.Info().Msgf("closes issue #%d, add milestones %s", issueNumber, pr.Milestone.GetTitle())

		if !m.dryRun {
			err := m.closeIssue(ctx, pr, issueNumber)
			if err != nil {
				return fmt.Errorf("unable to close issue #%d: %w", issueNumber, err)
			}
		}

		// On the main branch, GitHub auto-links the PR to the issue, so the
		// "Closed by #X." comment is only useful for backport branches.
		if pr.Base.GetRef() == mainBranch {
			continue
		}

		message := fmt.Sprintf("Closed by #%d.", pr.GetNumber())

		logger.Debug().Msgf("issue #%d, add comment: %s", issueNumber, message)

		if !m.dryRun {
			err := m.addComment(ctx, issueNumber, message)
			if err != nil {
				return fmt.Errorf("unable to add comment on issue #%d: %w", issueNumber, err)
			}
		}
	}

	return nil
}

func (m Mjolnir) closeIssue(ctx context.Context, pr *github.PullRequest, issueNumber int) error {
	var milestone *int
	if pr.Milestone != nil {
		milestone = pr.Milestone.Number
	}

	issueRequest := &github.IssueRequest{
		Milestone: milestone,
		State:     github.Ptr("closed"),
	}

	_, _, err := m.client.Issues.Edit(ctx, m.owner, m.name, issueNumber, issueRequest)
	return err
}

func (m Mjolnir) addComment(ctx context.Context, issueNumber int, message string) error {
	issueComment := &github.IssueComment{
		Body: github.Ptr(message),
	}

	_, _, err := m.client.Issues.CreateComment(ctx, m.owner, m.name, issueNumber, issueComment)
	return err
}

func (m Mjolnir) parseIssueFixes(ctx context.Context, text string) []int {
	logger := log.Ctx(ctx)

	matches := m.globalFixesIssueRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[int]bool)
	var issueNumbers []int
	for _, group := range matches {
		refs := m.issueRefRE.FindAllStringSubmatch(group[1], -1)
		for _, ref := range refs {
			// ref[1] is the `#NNN` form; ref[2]/[3]/[4] are owner/repo/number from the URL form.
			var numStr string
			switch {
			case ref[1] != "":
				numStr = ref[1]
			case ref[4] != "":
				if !strings.EqualFold(ref[2], m.owner) || !strings.EqualFold(ref[3], m.name) {
					logger.Warn().Str("url", ref[0]).Msg("ignoring cross-repo issue reference")
					continue
				}
				numStr = ref[4]
			default:
				continue
			}

			n, err := strconv.Atoi(numStr)
			if err != nil {
				logger.Error().Err(err).Str("number", numStr).Msg("unable to parse int")
				continue
			}

			if seen[n] {
				continue
			}
			seen[n] = true
			issueNumbers = append(issueNumbers, n)
		}
	}
	return issueNumbers
}
