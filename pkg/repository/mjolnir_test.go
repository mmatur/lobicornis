package repository

import (
	"context"
	"testing"

	"github.com/google/go-github/v74/github"
	"github.com/stretchr/testify/assert"
)

const (
	testOwner = "owner"
	testRepo  = "repo"

	branchMain = "main"
)

func Test_parseIssueFixes(t *testing.T) {
	testCases := []struct {
		name         string
		text         string
		allowedRepos []string
		expected     []issueRef
	}{
		{
			name: "only letters",
			text: `
	Fixes dlsqj
`,
			expected: nil,
		},
		{
			name: "valid issue numbers coma",
			text: `
	Fixes #13 #14, #15,#16,
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 13},
				{owner: testOwner, name: testRepo, number: 14},
				{owner: testOwner, name: testRepo, number: 15},
				{owner: testOwner, name: testRepo, number: 16},
			},
		},
		{
			name: "valid issue numbers space",
			text: `
	Fixes #13 #14 #15 #16
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 13},
				{owner: testOwner, name: testRepo, number: 14},
				{owner: testOwner, name: testRepo, number: 15},
				{owner: testOwner, name: testRepo, number: 16},
			},
		},
		{
			name: "invalid pattern",
			text: `
	Fixes #13#14,#15,#16,
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 13},
			},
		},
		{
			name: "french style",
			text: `
	Fixes : #13,#14,#15,#16,
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 13},
				{owner: testOwner, name: testRepo, number: 14},
				{owner: testOwner, name: testRepo, number: 15},
				{owner: testOwner, name: testRepo, number: 16},
			},
		},
		{
			name: "valid issue numbers coma and :",
			text: `
	Fixes: #13,#14,#15,#16,
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 13},
				{owner: testOwner, name: testRepo, number: 14},
				{owner: testOwner, name: testRepo, number: 15},
				{owner: testOwner, name: testRepo, number: 16},
			},
		},
		{
			name: "multiple separate fixes blocks",
			text: `
	This partially fixes #11375 by proposing an alternative.
	Fixes #12786
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 11375},
				{owner: testOwner, name: testRepo, number: 12786},
			},
		},
		{
			name: "URL form same repo",
			text: `
	Fixes https://github.com/owner/repo/issues/12820
	Fixes https://github.com/owner/repo/issues/12748
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 12820},
				{owner: testOwner, name: testRepo, number: 12748},
			},
		},
		{
			name: "URL form cross-repo denied by default",
			text: `
	Fixes https://github.com/other/different/issues/9999
`,
			expected: nil,
		},
		{
			name: "URL form cross-repo allowed explicitly",
			text: `
	Fixes https://github.com/owner/hub-issues/issues/42
`,
			allowedRepos: []string{"owner/hub-issues"},
			expected: []issueRef{
				{owner: testOwner, name: "hub-issues", number: 42},
			},
		},
		{
			name: "URL form cross-org allowed explicitly",
			text: `
	Fixes https://github.com/other/different/issues/9999
`,
			allowedRepos: []string{"other/different"},
			expected: []issueRef{
				{owner: "other", name: "different", number: 9999},
			},
		},
		{
			name: "URL form org wildcard",
			text: `
	Fixes https://github.com/owner/sibling/issues/7
	Fixes https://github.com/owner/another/issues/8
`,
			allowedRepos: []string{"owner/*"},
			expected: []issueRef{
				{owner: testOwner, name: "sibling", number: 7},
				{owner: testOwner, name: "another", number: 8},
			},
		},
		{
			name: "URL form allow-list does not match unrelated repo",
			text: `
	Fixes https://github.com/owner/hub-issues/issues/1
	Fixes https://github.com/owner/stranger/issues/2
`,
			allowedRepos: []string{"owner/hub-issues"},
			expected: []issueRef{
				{owner: testOwner, name: "hub-issues", number: 1},
			},
		},
		{
			name: "mixed hash and URL",
			text: `
	Closes #100, https://github.com/owner/repo/issues/200
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 100},
				{owner: testOwner, name: testRepo, number: 200},
			},
		},
		{
			name: "trailing period",
			text: `
	Closes #12956.
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 12956},
			},
		},
		{
			name: "closed and resolved keywords",
			text: `
	Closed #1
	Resolved #2
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 1},
				{owner: testOwner, name: testRepo, number: 2},
			},
		},
		{
			name: "deduplicate same issue",
			text: `
	Fixes #13 #13
	Closes #13
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 13},
			},
		},
		{
			name: "deduplicate hash and URL referring to same issue",
			text: `
	Fixes #42
	Closes https://github.com/owner/repo/issues/42
`,
			expected: []issueRef{
				{owner: testOwner, name: testRepo, number: 42},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mjolnir := newMjolnir(nil, testOwner, testRepo, true, test.allowedRepos)

			refs := mjolnir.parseIssueFixes(context.Background(), test.text)

			assert.Equal(t, test.expected, refs)
		})
	}
}

func Test_isAutoLinked(t *testing.T) {
	testCases := []struct {
		name          string
		baseRef       string
		defaultBranch string
		ref           issueRef
		expected      bool
	}{
		{
			name:          "same repo on default branch master",
			baseRef:       "master",
			defaultBranch: "master",
			ref:           issueRef{owner: testOwner, name: testRepo, number: 1},
			expected:      true,
		},
		{
			name:          "same repo on default branch main",
			baseRef:       branchMain,
			defaultBranch: branchMain,
			ref:           issueRef{owner: testOwner, name: testRepo, number: 1},
			expected:      true,
		},
		{
			name:          "same repo on custom default branch",
			baseRef:       "develop",
			defaultBranch: "develop",
			ref:           issueRef{owner: testOwner, name: testRepo, number: 1},
			expected:      true,
		},
		{
			name:          "same repo on non-default branch",
			baseRef:       "v1.0",
			defaultBranch: branchMain,
			ref:           issueRef{owner: testOwner, name: testRepo, number: 1},
			expected:      false,
		},
		{
			name:          "cross-repo on default branch",
			baseRef:       branchMain,
			defaultBranch: branchMain,
			ref:           issueRef{owner: testOwner, name: "other-repo", number: 1},
			expected:      false,
		},
		{
			name:          "cross-org on default branch",
			baseRef:       branchMain,
			defaultBranch: branchMain,
			ref:           issueRef{owner: "other", name: testRepo, number: 1},
			expected:      false,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mjolnir := newMjolnir(nil, testOwner, testRepo, true, nil)

			pr := &github.PullRequest{
				Base: &github.PullRequestBranch{
					Ref: github.Ptr(test.baseRef),
					Repo: &github.Repository{
						DefaultBranch: github.Ptr(test.defaultBranch),
					},
				},
			}

			assert.Equal(t, test.expected, mjolnir.isAutoLinked(pr, test.ref))
		})
	}
}
