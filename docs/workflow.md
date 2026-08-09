# Workflow: Issue → Branch → Implementation → Code Review → PR → CI → Merge

This document describes the complete workflow for handling issues in the
[crazy-goat/go-mesi](https://github.com/crazy-goat/go-mesi) repository using
`gh` and `git`. Adapted from the crazy-goat/workerman-bundle workflow.

---

## 1. Triage: Work Is Milestone-Driven

**The only source of tasks is the lowest-numbered OPEN milestone that still
has open issues.** If the lowest open milestone is empty (or no open
milestone has open issues), **all planned work is done — stop and report to
the user** ("everything in the milestone has been completed"). Do not invent
tasks outside milestones.

### 1a. Find the lowest open milestone

```bash
gh api graphql -f query='query { repository(owner:"crazy-goat", name:"go-mesi") { milestones(first:50, orderBy:{field:NUMBER, direction:ASC}) { nodes { number state openIssueCount } } } }' \
  | jq -r '.data.repository.milestones.nodes[] | select(.state=="OPEN" and .openIssueCount>0) | "\(.number)\t\(.openIssueCount) open"'
```

Take the first line — that is the current milestone. Milestone number ≠
version (#3 == 0.9.0, #7 == 0.13.0). If the query returns **nothing**, stop:
all work is done.

### 1b. Pick an issue from that milestone

Selection inside the milestone is deterministic, in this order:

1. `bug` + `mesi` (core parsing bugs)
2. other `bug`
3. `enhancement` (prefer parity gaps on the weakest platform)
4. `docs`
5. `good first issue`

Skip: `duplicate`, `invalid`, `wontfix`, `question`.

List the milestone's issues and pick per the priority order (**`--limit`
matters: `gh issue list` returns at most 30 issues by default — raise it,
e.g. `--limit 100`, or filter by milestone):

```bash
gh issue list --repo crazy-goat/go-mesi --state open --limit 100 --json number,title,labels
```

Issue bodies are token-heavy — if several candidates tie, delegate reading
their bodies to a subagent that returns a short summary per issue (number,
title, labels, one-paragraph description of the requested change). The
subagent does **not** decide: milestone + priority order are the selection
criteria. The main session picks the winning issue and proceeds to step 2.

> **Why a subagent for bodies:** issue bodies and comments can easily exceed
> thousands of tokens. Keeping them out of the main session protects its
> budget for implementation and review.

---

## 2. Create a Fresh Branch off `main`

```bash
# Make sure you're on main with the latest changes
git checkout main
git pull --ff-only origin main

# Create a feature/fix branch
git checkout -b fix/issue-<NUMBER>-<short-description>
```

**Branch naming convention:**
`fix/issue-<NUMBER>-<kebab-case>` or `feat/issue-<NUMBER>-<kebab-case>`
(e.g. `fix/issue-106-apache-config`). Some older branches omit the
`/issue-` segment (`feat/222-cli-cache-key-template`) — new branches should
follow the `fix/issue-<N>-…` form.

Existing examples in this repository:
- `fix/issue-106-apache-config`
- `fix/issue-237-nginx-redis-cache-backend`
- `fix/issue-90-output-filter-eos`
- `feat/222-cli-cache-key-template`
- `feat/260-caddy-debug-directive`

---

## 3. Implement the Change (via Worker/Coder Subagent)

Implementation is delegated to a subagent (`worker` or `coder`) so the main
session stays free to orchestrate, review findings, and handle the next steps.

```bash
# The subagent receives a task like:
# "Implement issue #<NUMBER> on branch fix/issue-<NUMBER>-<description>.
#  Read CHANGELOG.md, docs/features.md and docs/specs/ first — they record
#  project decisions (e.g. errors are never silent defaults) that apply
#  to this task.
#  Read the issue body first, then make the smallest correct change,
#  matching existing mesi/* patterns.
#  Add tests in the repo's convention: unit → integration (httptest) →
#  e2e fixture when the behaviour is user-visible.
#  Commit and push when done.
#
#  Your report must ALWAYS contain:
#  1. Files changed and why
#  2. What was the BIGGEST problem or obstacle during implementation
#     (with details: where, why it was hard, how you solved it)
#  3. Any bugs or places to improve you discovered along the way
#     (also outside the scope of this issue) - each with file/line,
#     short description, and suggested fix"
```

After the subagent reports, commit and push if it did not do so already:

```bash
# Ensure everything is committed and pushed
git add -A
git commit -m "fix(mesi): <1-liner> (#<NUMBER>)"
git push -u origin fix/issue-<NUMBER>-<description>
```

**Commit message convention:**
- Type: `feat`, `fix`, `docs`, `refactor`, `ci`, `test`, `chore`
- Scope (area): `(mesi)`, `(cli)`, `(apache)`, `(nginx)`, `(caddy)`,
  `(traefik)`, `(roadrunner)`, `(frankenphp)`, `(proxy)`, `(php-ext)`,
  `(libgomesi)`, `(docs)`, `(tests)`, `(e2e)`, `(ci)`
- Reference to issue: `(#<NUMBER>)` — e.g.
  `feat(roadrunner): add block_private_ips config option (#198)`.
  The PR body carries the `Fixes #<NUMBER>` line that auto-closes the issue;
  GitHub appends the PR number on squash-merge.

> **Coder output contract (non-negotiable):** the subagent must always return
> (1) changed files, (2) the biggest problem it faced with details, and
> (3) any discovered bugs / places to improve - even ones outside the current
> issue's scope. The main session stores these findings for the final report
> (step 14).

---

## 4. Code Review via Subagent

After implementation, run a code review using a subagent (separate agent with
its own context). The subagent checks:

- Alignment with project structure and existing `mesi/*` patterns
- No silent-default / silent-substitution in any parser
- No silent `uint(x)` downcast feeding `make([]…, n)` or `rng(...)`
- Exported API contract changes (libgomesi entry points are ABI-relevant —
  new symbols must be additive / fall back gracefully)
- Error handling and edge cases (`*Err…` with context, never silent defaults)
- Missing or weak test coverage (boundary classes each get a subtest)
- CHANGELOG.md and docs/features.md accuracy
- Security (SSRF: private-IP blocking, allowed-hosts, dial-time transport)

```bash
# The subagent receives a task like:
# "You are reviewing PR #<N> for crazy-goat/go-mesi (branch fix/issue-<N>-…).
#  Read the diff against main. Read CHANGELOG.md, docs/features.md and
#  docs/specs/ first and flag any violations of documented decisions.
#  Check: type correctness, error handling, silent defaults, uint
#  downcasts, gofmt formatting, missing tests, outdated
#  documentation (CHANGELOG.md + docs/features.md).
#  Return: PASS, or a numbered list of blocking issues with file:line."
```

> **Why a subagent:** code review reads the full diff plus surrounding code,
> runs static analysis, and produces a structured findings list. Delegating
> keeps the main session focused on fixes and the next workflow step.

---

## 5. Fix Issues Found in Code Review

```bash
# For each problem found:
# 1. Apply the fix
# 2. Commit with a descriptive message
git add -A
git commit -m "fix(mesi): <description of fix>"
git push origin fix/issue-<NUMBER>-<description>
```

**All issues must be fixed – even the least significant ones.**

---

## 6. Repeat Code Review

After fixing, invoke the subagent for another code review.

Repeat steps 5→6 until the subagent reports no issues.

> **Acceptance criteria:** The subagent responds: "Code looks good, no issues
> to fix."

---

## 7. Run Linters and Tests Locally

Before opening a PR, verify that all linters and tests pass on your machine:

```bash
# CI guard: no generated artifacts may be tracked
./scripts/check-no-generated-artifacts.sh

# Build libgomesi (shared lib used by all modules)
(cd libgomesi && make build)

# Lint and vet the core library
golangci-lint run ./mesi/...
go vet ./mesi/...

# Unit tests with fresh cache (-count=1)
go test -count=1 -v ./mesi/...

# CLI unit + e2e tests
(cd cli && go test -count=1 ./...)
(cd cli && bash test.sh)

# Optional, needs Docker: server-module integration suites
(cd servers/apache && ./test.sh)      # apache, nginx, caddy, traefik,
                                       # roadrunner, frankenphp, proxy
```

> **Note:** there is no pre-push hook and no `composer` in this repo — the
> artifact guard (`check-no-generated-artifacts.sh`) and `golangci-lint` run
> in CI. The full integration matrix (Apache, nginx, Caddy, Traefik,
> RoadRunner, FrankenPHP, proxy, PHP ext, Yaegi) is Docker-heavy — CI is the
> authoritative gate for it; running it locally is optional.

> **Note:** `libgomesi.so`, `libgomesi.a`, `*.test` and `coverage.*` are
> generated artifacts and must never be committed.

After auto-fixing lint issues (e.g. `gofmt -w`), commit any fixes:

```bash
git add -A
git commit -m "style: gofmt/lint fixes"
```

**Only create the PR when all lints and tests pass locally.**

---

## 8. Update CHANGELOG.md and docs/features.md

```bash
# Edit CHANGELOG.md:
# - Add entry under the current [Unreleased] section
# - Follow Keep a Changelog format (https://keepachangelog.com/en/1.1.0/)
# - Use appropriate section: Added, Changed, Fixed, Removed, Deprecated
# - Include issue number, e.g. (#198)
# - Mark breaking changes explicitly (e.g. "BREAKING CHANGE: …")
#
# Edit docs/features.md if the change alters documented behaviour,
# CLI flags, server directives or default values.
```

---

## 9. Create a Pull Request

```bash
# Body file starts with "Fixes #<NUMBER>" on the first line so the
# issue auto-closes on merge
gh pr create --repo crazy-goat/go-mesi \
  --base main --head fix/issue-<NUMBER>-<description> \
  --title "fix(mesi): <short description> (#<NUMBER>)" \
  --body-file <body-file>
```

Body template:

```
Fixes #<NUMBER>

## Description

<what this PR does and why>

## Changes

- <list of changes>

## Changelog

<!-- Describe the changelog entry for this PR -->

## Code Review

- [ ] Passed subagent code review
- [ ] All review comments addressed
```

> **Note:** If you don't use `gh`, create the PR manually via GitHub UI.
> Branch protection requires all status checks passing; depending on branch
> settings, at least one approving review may be required before merge.

---

## 10. Wait for CI

```bash
# Check PR status
gh pr view <PR> --repo crazy-goat/go-mesi --json statusCheckRollup \
  | jq -r '.statusCheckRollup[] | "\(.name)=\(.conclusion)"'

# Wait for all checks to finish
gh pr checks <PR> --repo crazy-goat/go-mesi --watch
```

CI workflow (`.github/workflows/tests.yaml`) runs 14 jobs (13 checks + the
`ci` aggregator):
1. **lint** – artifacts guard + `golangci-lint run ./mesi/...`
2. **test** – `go test -v ./mesi/...`
3. **apache-test**, **nginx-test**, **caddy-test**, **traefik-test**,
   **roadrunner-test**, **frankenphp-test**, **proxy-test** – Docker
   integration suites (`servers/<x>/test.sh`)
4. **cli-test**, **cli-e2e-test** – CLI unit + e2e
5. **php-ext-test** – PHP extension unit + integration
6. **yaegi-check** – Traefik plugin Yaegi compatibility
7. **ci** – aggregator confirming all of the above passed

---

## 11. Handle CI Failures

If CI fails:

```bash
# 1. See which checks failed
gh pr checks <PR> --repo crazy-goat/go-mesi

# 2. View logs
gh run view --log --job <job-name>

# 3. Fix the issues locally
# 4. Run code review via subagent again (repeat steps 4-6)
# 5. Commit the fixes
git add -A
git commit -m "fix(mesi): <description of CI fix>"
git push origin fix/issue-<NUMBER>-<description>

# 6. Wait for CI to re-run
gh pr checks <PR> --repo crazy-goat/go-mesi --watch
```

> **Note:** there is no pre-push hook in this repo, so pushes are never
> blocked locally. If a CI failure is not reproducible locally, suspect
> environment differences (Docker image versions, Go toolchain) and check
> the failing job's logs first.

**Repeat until all CI checks pass.**

---

## 12. Merge PR and Close Issue

```bash
# Merge PR (squash merge recommended for clean history)
gh pr merge <PR> --repo crazy-goat/go-mesi --squash --delete-branch

# Close the issue (automatic if the PR body contains "Fixes #<NUMBER>")
# Alternatively:
gh issue close <NUMBER> --repo crazy-goat/go-mesi

# Verify the issue was actually closed
gh issue view <N> --repo crazy-goat/go-mesi --json state,closedAt
```

> **Note:** If branch protection requires a review, `gh pr merge` may be
> blocked. In that case, use the GitHub UI to squash-merge after obtaining
> approval.

---

## 13. Switch Back to main and Close Empty Milestones

```bash
git checkout main
git pull --ff-only origin main
git branch -d fix/issue-<NUMBER>-<slug>
git remote prune origin
```

Then re-check the milestone worked on; close it if no open issues remain:

```bash
gh api graphql -f query='query { repository(owner:"crazy-goat", name:"go-mesi") { milestones(first:50, orderBy:{field:NUMBER, direction:ASC}) { nodes { number state openIssueCount } } } }'

# If openIssueCount == 0:
gh api repos/crazy-goat/go-mesi/milestones/<N> -X PATCH -f state=closed
```

**Stop condition:** if the query above returns no OPEN milestone with open
issues, **all planned work is done** — report this to the user and stop.
Do not start issues outside milestones on your own.

---

## 14. Report Implementation Problems and Offer a GitHub Issue

At the end of the workflow, present the findings collected from the
implementation subagent(s) and decide with the user whether they deserve a
dedicated GitHub issue.

**Display to the user:**

1. **Biggest problem(s) faced during implementation** - as reported by the
   worker/coder subagent in step 3.
2. **Discovered bugs / places to improve** - each with file/line, short
   description, and suggested fix (including findings outside the scope of the
   issue just closed).

**Verify each candidate finding with a review subagent (read-only) before
offering or creating an issue.** For every candidate finding the subagent
must confirm:

1. **The finding is real** - read the cited file/line(s) on the current
   branch and confirm the behavior actually occurs and is reachable; check
   whether it is by-design and already documented (in docs/features.md,
   docs/specs/ or CHANGELOG.md — those are skipped, not filed).
2. **No similar issue exists on GitHub** - search open *and* closed issues.
   `gh issue list` returns at most 30 issues by default, so always pass an
   explicit limit:

   ```bash
   gh issue list --repo crazy-goat/go-mesi --state open --limit 150 --json number,title,labels,body
   gh issue list --repo crazy-goat/go-mesi --state closed --limit 150 --json number,title,labels
   gh search issues --repo crazy-goat/go-mesi --state open --limit 50 "<keyword>"
   ```

   Same or overlapping scope counts as tracked; known related issues (e.g.
   referenced from CHANGELOG entries) must be checked explicitly. Milestones
   often contain the full list of planned work — check them too.
3. **A recommendation per finding**: (a) create a new issue - with proposed
   title and labels per the project's conventions (`bug` / `enhancement` /
   `docs` / `good first issue` / …), (b) skip - already tracked (cite the
   issue number), or (c) skip - not real or by-design and documented.

The verification subagent must not modify files and must not create/close/
edit issues itself. Like steps 3 and 4, it reads CHANGELOG.md,
docs/features.md and docs/specs/ first. Only findings that pass verification
(real + untracked) are offered to the user / created.

**Then ask:** "Create GitHub issue(s) for these findings?"

- If yes, create an issue via `gh`:

```bash
gh issue create --repo crazy-goat/go-mesi \
  --title "<short title of the discovered problem>" \
  --body "## Description

<what was found>

## Where

- <file:line>

## Suggested fix

<short description>" \
  --label bug
```

- Assign `--label bug` for confirmed bugs or `enhancement` / `docs` /
  `good first issue` for improvement candidates. One issue per distinct
  finding keeps them actionable.
- If the user declines or the findings are already tracked, just record the
  outcome and finish.

> **Note:** findings that were already fixed as part of this workflow do not
> need an issue - only newly discovered, still-open problems should be
> reported.

---

## Project Knowledge

Implementation and review subagents should **read** the project docs before
starting a task and **record** learnings when they finish:

- `README.md` — project overview, supported servers, quick start
- `docs/features.md` — documented behaviour of every feature (the source of
  truth for what a change may break)
- `docs/specs/*.md` — design and plan documents for specific changes
- `CHANGELOG.md` — release history; see how past fixes/features were framed
  and reference the related issue numbers
- `examples/` — usage examples that may need updating
- This file's **Rules** section (below) — non-negotiable project decisions

If a task surfaces a recurring pitfall or a decision worth keeping, record it
as a short entry:
- **Decisions** (rationale for a behaviour) → add to `docs/features.md`
- **Recurring pitfalls** (tooling traps, test setup quirks) → add to the
  **Notes** section of this file, or ask the user before creating a new
  docs file.

Entries are committed as part of the regular fix/feat commits — no extra PRs.

---

## Quick Reference – Full Cycle

```bash
# 1. Pick an issue from the lowest OPEN milestone
#    if no open milestone has issues → all work done, stop
#    selection inside the milestone: bug+mesi → other bug → enhancement
#    → docs → good first issue; subagent only summarizes issue bodies
# 2. Branch
git checkout main && git pull --ff-only origin main
git checkout -b fix/issue-<N>-<description>

# 3. Implementation (worker/coder subagent)
#    subagent: "Implement issue #<N>…"
#    report must include: files changed, BIGGEST problem, discovered bugs
#    / places to improve (also out of scope)
git add -A && git commit -m "fix(mesi): <1-liner> (#<N>)"
git push -u origin fix/issue-<N>-<description>

# 4. Code Review (subagent)
# … fix issues … (repeat until clean)

# 5. Run linters and tests locally
./scripts/check-no-generated-artifacts.sh
(cd libgomesi && make build)
golangci-lint run ./mesi/...
go vet ./mesi/...
go test -count=1 -v ./mesi/...
(cd cli && go test -count=1 ./... && bash test.sh)

# 6. Update CHANGELOG.md and docs/features.md

# 7. PR
gh pr create --repo crazy-goat/go-mesi --base main \
  --title "fix(mesi): <desc> (#<N>)" --body-file <body>

# 8. CI
gh pr checks <PR> --repo crazy-goat/go-mesi --watch
# … if failures → fix, code review, push → wait for CI (repeat)

# 9. Merge
gh pr merge <PR> --repo crazy-goat/go-mesi --squash --delete-branch
gh issue view <N> --repo crazy-goat/go-mesi --json state,closedAt

# 10. Switch back to main
git checkout main && git pull --ff-only origin main
# close the milestone if empty (see step 13)

# 11. Report + offer GitHub issue for discovered problems
#    show: biggest problem(s), discovered bugs / places to improve
#    verify each candidate with a review subagent (finding is real?
#    no duplicate on GitHub? use --limit >30 in issue lists)
#    then ask: "Create GitHub issue(s)?" → if yes: gh issue create …
```

---

## Subagent Usage Summary

Four steps of this workflow are delegated to subagents to keep the main
session's context lean:

| Step | Subagent task                              | Why delegate                          |
| ---- | ------------------------------------------ | ------------------------------------- |
| 1    | Summarize issue bodies from the lowest open milestone (read-only; selection stays deterministic: milestone + priority order) | Issue bodies are token-heavy |
| 3    | Implement the issue (worker/coder)         | Coding context is token-heavy; agent returns structured report (files, biggest problem, discovered bugs) |
| 4    | Code review of the implementation diff     | Full diff + surrounding code is token-heavy |
| 14   | Verify candidate findings before creating GitHub issues (read-only: is the finding real? is it already tracked?) | GitHub duplicate search (open + closed, `--limit` > 30) plus code verification across several findings is query-heavy |

All subagents have read/write/edit/bash tools and operate on the same
repository (the step-14 verifier is instructed to run read-only). Give each
one a clear, scoped instruction and a defined output format (per-issue body
summaries / numbered findings list / coder report with biggest problem
+ discovered bugs / per-finding verification verdict).

**Project knowledge:** implementation and review subagents read
CHANGELOG.md, docs/features.md and docs/specs/ before starting and update
the docs when their change affects documented behaviour (see "Project
Knowledge" above).

---

## Rules

- One ticket = one PR, single squash commit. No bundling.
- `mesi/` errors are never silent defaults — use `Err…` / `*Err…` with
  context. This is the project's #1 review focus: any parser that silently
  substitutes a default for malformed input is a bug.
- Exported API changes (libgomesi entry points, CLI flags, server
  directives, config options) are deliberate, additive where possible
  (fallback to the old symbol/flag), and documented in CHANGELOG.md +
  docs/features.md.
- New code paths get tests in the repo's convention: unit → integration
  (httptest) → e2e fixture when the behaviour is user-visible. Boundary
  classes (accepted max, rejected at max+1, both-zero, negatives, decimals,
  non-integer) each get a subtest.
- Tests are deterministic; no upstream-fetch races in race-prone cases.
- Milestone number ≠ version (#3 == 0.9.0, #7 == 0.13.0).

## Anti

- Silent-default fallback in parsers.
- Silent `uint(x)` downcasts feeding `make([]…, n)` or `rng(...)`.
- Force-push `main`.
- Commit `libgomesi.so` / `libgomesi.a` / `*.test` / `coverage.*`.
- Proposing `composer`, PHP version matrices, or FoundationDB for this
  repo's CI — it is a Go project.

---

## Notes

- **gh** must be configured and authenticated (`gh auth status`) and the
  repo remote must be `git@github.com:crazy-goat/go-mesi.git` (commands in
  this workflow pass `--repo crazy-goat/go-mesi` explicitly).
- Branch protection on `main` requires:
  - All status checks passing (14 jobs in `.github/workflows/tests.yaml`)
  - Possibly at least 1 approving review before merge — if `gh pr merge`
    is blocked, merge via UI after approval
- There is no pre-push hook — CI is the gate,
  `./scripts/check-no-generated-artifacts.sh` locally catches the most
  common mistake (committing build artifacts).
- Keep fix/feature branches short-lived. If a rebase is needed:
  ```bash
  git fetch origin main
  git rebase origin/main
  git push --force-with-lease origin fix/issue-<N>-<description>
  ```
- Code review via subagent runs locally – the subagent has access to
  read/write/edit/bash tools. Give it clear instructions on what to check.
