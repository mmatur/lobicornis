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

// issueRef is a parsed issue reference scoped to a repository.
type issueRef struct {
	owner  string
	name   string
	number int
}

// Mjolnir the hammer of Thor.
type Mjolnir struct {
	client *github.Client

	globalFixesIssueRE *regexp.Regexp
	issueRefRE         *regexp.Regexp

	dryRun bool

	owner string
	name  string

	// allowedRepos lists the repositories (owner/name, lowercased) authorized for
	// cross-repo issue closing. An entry of the form "owner/*" allows the whole org.
	// The PR's own repository is always allowed.
	allowedRepos []string
}

func newMjolnir(client *github.Client, owner, name string, dryRun bool, allowedRepos []string) Mjolnir {
	normalized := make([]string, 0, len(allowedRepos))
	for _, item := range allowedRepos {
		normalized = append(normalized, strings.ToLower(item))
	}

	return Mjolnir{
		client: client,

		globalFixesIssueRE: regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\b[\s:]+(` + issueRefPattern + `(?:[\s,]+` + issueRefPattern + `)*)`),
		issueRefRE:         regexp.MustCompile(`#(\d+)|https?://github\.com/([\w.-]+)/([\w.-]+)/issues/(\d+)`),

		dryRun: dryRun,

		owner: owner,
		name:  name,

		allowedRepos: normalized,
	}
}

// CloseRelatedIssues Closes issues listed in the PR description.
func (m Mjolnir) CloseRelatedIssues(ctx context.Context, pr *github.PullRequest) error {
	logger := log.Ctx(ctx)

	refs := m.parseIssueFixes(ctx, pr.GetBody())

	for _, ref := range refs {
		logger.Info().Msgf("closes issue %s/%s#%d, add milestones %s", ref.owner, ref.name, ref.number, pr.Milestone.GetTitle())

		if !m.dryRun {
			err := m.closeIssue(ctx, pr, ref)
			if err != nil {
				return fmt.Errorf("unable to close issue %s/%s#%d: %w", ref.owner, ref.name, ref.number, err)
			}
		}

		// On the main branch, GitHub auto-links the PR to the issue, so the
		// "Closed by #X." comment is only useful for backport branches.
		// The auto-link only fires for same-repo references; for cross-repo we
		// always leave a back-pointer comment.
		if ref.owner == m.owner && ref.name == m.name && pr.Base.GetRef() == mainBranch {
			continue
		}

		message := fmt.Sprintf("Closed by %s/%s#%d.", m.owner, m.name, pr.GetNumber())

		logger.Debug().Msgf("issue %s/%s#%d, add comment: %s", ref.owner, ref.name, ref.number, message)

		if !m.dryRun {
			err := m.addComment(ctx, ref, message)
			if err != nil {
				return fmt.Errorf("unable to add comment on issue %s/%s#%d: %w", ref.owner, ref.name, ref.number, err)
			}
		}
	}

	return nil
}

func (m Mjolnir) closeIssue(ctx context.Context, pr *github.PullRequest, ref issueRef) error {
	issueRequest := &github.IssueRequest{
		State: github.Ptr("closed"),
	}

	// Only carry the PR milestone over to issues in the same repository:
	// milestone IDs are repo-scoped and would be invalid elsewhere.
	if pr.Milestone != nil && ref.owner == m.owner && ref.name == m.name {
		issueRequest.Milestone = pr.Milestone.Number
	}

	_, _, err := m.client.Issues.Edit(ctx, ref.owner, ref.name, ref.number, issueRequest)
	return err
}

func (m Mjolnir) addComment(ctx context.Context, ref issueRef, message string) error {
	issueComment := &github.IssueComment{
		Body: github.Ptr(message),
	}

	_, _, err := m.client.Issues.CreateComment(ctx, ref.owner, ref.name, ref.number, issueComment)
	return err
}

func (m Mjolnir) parseIssueFixes(ctx context.Context, text string) []issueRef {
	logger := log.Ctx(ctx)

	matches := m.globalFixesIssueRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	type key struct {
		owner, name string
		number      int
	}
	seen := make(map[key]bool)
	var refs []issueRef
	for _, group := range matches {
		found := m.issueRefRE.FindAllStringSubmatch(group[1], -1)
		for _, raw := range found {
			// raw[1] is the `#NNN` form; raw[2]/[3]/[4] are owner/repo/number from the URL form.
			var owner, name, numStr string
			switch {
			case raw[1] != "":
				owner, name, numStr = m.owner, m.name, raw[1]
			case raw[4] != "":
				owner, name, numStr = raw[2], raw[3], raw[4]
				if !m.repoAllowed(owner, name) {
					logger.Warn().Str("url", raw[0]).Msg("ignoring issue reference: repository not in allowCloseIssuesOn allow-list")
					continue
				}
			default:
				continue
			}

			n, err := strconv.Atoi(numStr)
			if err != nil {
				logger.Error().Err(err).Str("number", numStr).Msg("unable to parse int")
				continue
			}

			k := key{owner: strings.ToLower(owner), name: strings.ToLower(name), number: n}
			if seen[k] {
				continue
			}
			seen[k] = true
			refs = append(refs, issueRef{owner: owner, name: name, number: n})
		}
	}
	return refs
}

// repoAllowed reports whether issues in owner/name may be auto-closed.
// The PR's own repository is always allowed; cross-repo references must match
// an entry in allowedRepos, either exactly ("owner/name") or via an org wildcard ("owner/*").
func (m Mjolnir) repoAllowed(owner, name string) bool {
	if strings.EqualFold(owner, m.owner) && strings.EqualFold(name, m.name) {
		return true
	}

	target := strings.ToLower(owner + "/" + name)
	orgWildcard := strings.ToLower(owner) + "/*"
	for _, allowed := range m.allowedRepos {
		if allowed == target || allowed == orgWildcard {
			return true
		}
	}
	return false
}
