package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_parseIssueFixes(t *testing.T) {
	testCases := []struct {
		name            string
		text            string
		expectedNumbers []int
	}{
		{
			name: "only letters",
			text: `
	Fixes dlsqj
`,
			expectedNumbers: nil,
		},
		{
			name: "valid issue numbers coma",
			text: `
	Fixes #13 #14, #15,#16,
`,
			expectedNumbers: []int{13, 14, 15, 16},
		},
		{
			name: "valid issue numbers space",
			text: `
	Fixes #13 #14 #15 #16
`,
			expectedNumbers: []int{13, 14, 15, 16},
		},
		{
			name: "invalid pattern",
			text: `
	Fixes #13#14,#15,#16,
`,
			expectedNumbers: []int{13},
		},
		{
			name: "french style",
			text: `
	Fixes : #13,#14,#15,#16,
`,
			expectedNumbers: []int{13, 14, 15, 16},
		},
		{
			name: "valid issue numbers coma and :",
			text: `
	Fixes: #13,#14,#15,#16,
`,
			expectedNumbers: []int{13, 14, 15, 16},
		},
		{
			name: "multiple separate fixes blocks",
			text: `
	This partially fixes #11375 by proposing an alternative.
	Fixes #12786
`,
			expectedNumbers: []int{11375, 12786},
		},
		{
			name: "URL form same repo",
			text: `
	Fixes https://github.com/owner/repo/issues/12820
	Fixes https://github.com/owner/repo/issues/12748
`,
			expectedNumbers: []int{12820, 12748},
		},
		{
			name: "URL form cross-repo ignored",
			text: `
	Fixes https://github.com/other/different/issues/9999
`,
			expectedNumbers: nil,
		},
		{
			name: "mixed hash and URL",
			text: `
	Closes #100, https://github.com/owner/repo/issues/200
`,
			expectedNumbers: []int{100, 200},
		},
		{
			name: "trailing period",
			text: `
	Closes #12956.
`,
			expectedNumbers: []int{12956},
		},
		{
			name: "closed and resolved keywords",
			text: `
	Closed #1
	Resolved #2
`,
			expectedNumbers: []int{1, 2},
		},
		{
			name: "deduplicate same issue",
			text: `
	Fixes #13 #13
	Closes #13
`,
			expectedNumbers: []int{13},
		},
	}

	mjolnir := newMjolnir(nil, "owner", "repo", true)

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			issueNumbers := mjolnir.parseIssueFixes(context.Background(), test.text)

			assert.Equal(t, test.expectedNumbers, issueNumbers)
		})
	}
}
