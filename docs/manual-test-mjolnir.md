# Manual test runbook — issue auto-closing (mjolnir)

End-to-end manual test for the `Fixes/Closes/Resolves` issue parser in
`pkg/repository/mjolnir.go`, run against a throwaway GitHub org (`traefik-workshops`)
with dry-run **off** so issues actually close.

> **Verify against the final state of the PR.** This runbook targets the core bug
> fixes. The "comment is skipped on the default branch" behavior keys off the
> repository's actual default branch (`pr.Base.Repo.DefaultBranch`), so it works
> whether the default branch is `main`, `master`, or custom. Same-repo closes on a
> default-branch PR skip the "Closed by" comment (GitHub auto-links); everything
> else (non-default branch, cross-repo) still posts one.

## Behaviors under test (core)

1. **Multi-block** — multiple separate `Fixes ...` paragraphs → ALL closed
   (regression: `FindStringSubmatch` only captured the first).
2. **URL form same-repo** — `Fixes https://github.com/owner/repo/issues/N` → closed
   (was: ignored, only `#NNN` matched).
3. **Trailing period** — `Closes #N.` → closed (was: broken).
4. **Cross-repo deny by default** — URL to a repo NOT in `allowCloseIssuesOn` →
   ignored, warn logged, issue stays OPEN.
5. **Cross-repo allow (exact)** — same URL after adding the repo to
   `allowCloseIssuesOn` → closed + back-pointer comment.
6. **Dedup** — `#N` and `.../issues/N` for the same issue → closed once.

## Resources to create (2 repos, ~7 issues)

### Repositories
- `repo-a` — primary; PRs are created and merged here. Holds the same-repo issues.
  Needs the `status/3-needs-merge` label defined.
- `repo-b` — cross-repo target. Serves both the deny and the allow case via a config
  flip between runs (deny first, then add to the allow-list).

Each repo: one initial commit on the default branch `main`. Disable any branch
protection that requires reviews or status checks — the bot cannot bypass GitHub
branch protection; its own `minReview` is separate.

### Issues
- `repo-a`: `#a1, #a2` (multi-block), `#a3` (dedup: referenced as `#a3` AND as a URL),
  `#a4` (URL same-repo), `#a5` (trailing period). = 5
- `repo-b`: `#b1` (deny run — must stay OPEN), `#b2` (allow run — closed after the
  config flip). = 2

## Bot config (`lobicornis-test.yml`)

```yaml
github:
  user: traefik-workshops          # search: "user:traefik-workshops type:pr state:open"
  # token via GITHUB_TOKEN env (org PAT with repo + issues:write on both repos)

git:
  email: <bot-email>
  userName: <bot-name>

default:
  mergeMethod: squash
  minReview: 0                     # a single tester cannot approve their own PR; 0 = no approval needed
  minLightReview: 0
  needMilestone: false             # skip the milestone gate
  checkNeedUpToDate: false         # avoid the rebase-via-git-clone path
  forceNeedUpToDate: false
  allowCloseIssuesOn: []           # RUN 1: empty (deny). RUN 2: add traefik-workshops/repo-b

extra:
  dryRun: false                    # REQUIRED so issues actually close (default is true; no CLI flag)
  logLevel: debug
```

Defaults being overridden (see `pkg/conf/config.go`): `dryRun` defaults to true,
`minReview` to 1, `needMilestone` to true. The test repos use `main` as their
default branch, so a same-repo close on a `main`-base PR takes the skip path (no
"Closed by" comment — GitHub auto-links). To exercise the comment path, close from
a non-default branch (e.g. a backport branch) or a cross-repo reference.

## PRs and expected results

### RUN 1 — deny + same-repo behaviors

Config: `allowCloseIssuesOn: []`. PR1 in `repo-a`, base `main`, label
`status/3-needs-merge`, body:

```
This partially Fixes #a1 by proposing an alternative.

Fixes #a2
Fixes #a3
Closes https://github.com/traefik-workshops/repo-a/issues/a3
Resolves https://github.com/traefik-workshops/repo-a/issues/a4
Closes #a5.
Fixes https://github.com/traefik-workshops/repo-b/issues/b1
```

Expected after the bot merges PR1:
- `a1, a2, a3, a4, a5` CLOSED; `a3` closed exactly once (dedup of `#a3` + URL).
- No "Closed by" comment on `a1`–`a5`: same-repo close on the default branch
  (`main`) takes the skip path (GitHub auto-links).
- `b1` stays OPEN; debug log shows the warn "repository not in allowCloseIssuesOn allow-list".

### RUN 2 — cross-repo allow

Edit config: `allowCloseIssuesOn: [ traefik-workshops/repo-b ]`. New PR2 in `repo-a`,
base `main`, label `status/3-needs-merge`, body:

```
Fixes https://github.com/traefik-workshops/repo-b/issues/b2
```

Expected: `b2` CLOSED, with comment `Closed by traefik-workshops/repo-a#<PR2>.` on `b2`.

## How to run

```bash
GITHUB_TOKEN=<org-pat> go run ./cmd -config ./lobicornis-test.yml
```

One-shot per invocation (no `-server`). The bot processes one PR per repo per run, so
re-run until the labeled PR is picked up and merged. Flip the config between RUN 1 and
RUN 2. Watch the debug logs for `closes issue .../#N` lines and the allow-list warn.

## Verification checklist

- [ ] `repo-a` `a1`–`a5` closed; `a3` not double-processed (single close, no comment — default-branch skip path).
- [ ] `repo-b` `b1` OPEN after RUN 1 + warn logged (deny).
- [ ] `repo-b` `b2` closed after RUN 2 + "Closed by" comment (allow).

## Teardown

Delete the 2 test repos once verified (or reopen the issues and close the PRs). Revoke
the org PAT.
