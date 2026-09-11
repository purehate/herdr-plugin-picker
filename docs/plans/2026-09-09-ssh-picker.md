# SSH Picker for Herdr Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `purehate.herdr-ssh`, a Herdr plugin that opens a floating fuzzy picker over `~/.ssh/config` hosts and SSHes into a new pane, tab, or zoomed pane on selection.

**Architecture:** One Go binary with four verbs (`plugin open-picker`, `picker`, `session`, `connect`) declared in `herdr-plugin.toml` as an action plus two pane entrypoints. The picker runs in an `overlay` pane; selecting a host opens the `session` entrypoint with the host passed via `--env`, and that process renames its own pane to `ssh:<alias>` before `exec`ing ssh. Every `herdr` CLI call goes through one injectable `Runner` seam so the whole flow is testable without a running Herdr.

**Tech Stack:** Go 1.27, `charm.land/bubbletea/v2 v2.0.9`, `charm.land/lipgloss/v2 v2.0.6`, `github.com/pelletier/go-toml/v2 v2.4.3`. Spec: `docs/specs/2026-09-09-ssh-picker-design.md`.

---

## Standing Rules for Every Task

These apply to all 21 tasks. They are stated once here rather than repeated in
each commit step.

- **Run `git status --short <dir>/` before every directory-wide `git add`.**
  Twenty-one commit steps in this plan use `git add <dir>/` rather than naming
  files — `rg -c '^git add .*[^ ]/(\s|$)'` on this file is the current number,
  and the remaining three `git add` lines name their files and are unaffected.
  Measured at `d643b6a`: 24 `git add` lines, 21 in the directory form, 3 naming
  files. The earlier form of this command, `rg -c '^git add [^ ]*/$'`, returned
  20 and so disagreed with the prose by one — its `/$` anchor only matches a line
  whose _last_ argument is a directory, and it missed Task 21 Step 6's
  `git add .github/ .golangci.yml README.md LICENSE`, where the directory comes
  first. A note whose own count command under-reports is a small instance of the
  thing the note is about. That form
  stages whatever happens to be sitting in the directory, including scratch
  harnesses and debug files that no task authorizes. This has already happened
  once: a `zzz_scratch_test.go` debug-print harness appeared in
  `internal/picker/` during Task 13 and `git add --dry-run` confirmed it would
  have been swept in. Delete stray files before committing — a commit that
  contains a file no task's Files list names is a defect even when it compiles.
- **`git add` by filename does not isolate your commit — commit by pathspec
  too.** `git add <file>` controls what _you_ put into the index, not what is
  already sitting in it, and a bare `git commit -m` commits the whole index.
  This project runs several agents in one worktree, so another agent's staged
  work is routinely in the index at the moment you commit: it is swept in under
  a commit message that describes none of it. `impl-cmd-session` caught exactly
  this live at `b87f79a`, with `impl-probe-bound`'s `internal/probe/probe.go`
  and `internal/probe/probe_test.go` staged (`M` in column 1) while an
  unrelated commit was about to be made.

  So the rule is: **stage by filename _and_ commit by pathspec.** Measured in a
  scratch repo with a second agent's file staged alongside, not assumed:

  | command                         | commit contains          | other agent's file |
  | ------------------------------- | ------------------------ | ------------------ |
  | `git commit -m msg`             | `mine.txt`, `theirs.txt` | swept in           |
  | `git commit -m msg -- mine.txt` | `mine.txt` only          | still staged       |

  One consequence to understand before relying on it: the pathspec form commits
  the **working-tree** content of the named paths, bypassing the index for them.
  A file that was staged and then edited again — `MM` in `git status --short` —
  is committed whole, unstaged hunk included. Verified: staging one hunk,
  editing a second, then `git commit -m msg -- mine.txt` produced a commit
  containing both hunks and left the working tree clean. That is the behavior
  you want when isolating your file from other agents, but it is not a partial
  commit and it is not `git add -p`.

  **Clause two is breadth, and it is the clause that reads as safe.** Exact
  filenames only — never a directory, never a glob. Flags go before the `--`;
  git reads a trailing flag as a pathspec. A wrong _filename_ is loud:
  `pathspec ... did not match any file(s) known to git`, exit 1. A too-broad
  _pathspec_ is silent — `git commit -m x -- sub/` and
  `git commit -m x -- 'sub/*.txt'` both exit 0 and sweep another agent's staged
  file into your commit. The asymmetry has a cause worth stating, because it is
  why no flag will ever catch it: git can verify that a path _exists_ and cannot
  verify that a path is _wider than you meant_, because breadth is what a
  pathspec is for. Directory and glob forms are not slightly-less-precise
  filenames; they are requests for whatever happens to be there.

  **This clause is specifically about concurrent agents**, and it is scoped that
  way on purpose so it does not read as cargo cult later: a single-agent reader
  following a step whose `Files:` list sits three lines above its commit command
  is not exposed to the hazard at all. Known, unclosed gap, stated rather than
  papered over: 21 of this plan's own 24 commit steps use a directory pathspec,
  retrofitted at `687d103` from each step's own `git add` line so that none
  widened relative to what the step already staged — they satisfy clause one and
  violate this one, and they are deliberately left alone rather than rewritten,
  because editing 24 historical steps to satisfy a rule adopted afterwards would
  change a record of work already executed.

  **Clause three is commit identity: do not `--amend` at all while other agents
  are committing.** `--amend` targets whatever `HEAD` is at that instant,
  regardless of any pathspec, so satisfying the first two clauses does not
  protect it — and both incidents this session were clause-three failures in
  different disguises. Stated as a measurement because it happened here: an
  amend made believing `HEAD` was still `6fc77c6` rewrote `impl-cmd-connect`'s
  `57efb52` four commits later, absorbing its `main.go` and `main_test.go` diff
  and destroying its message. Recovered with `git reset --soft 57efb52` and a
  pathspec commit — an instrument that touches no file content, so it could not
  have made things worse even if the diagnosis had been wrong. Checking
  `git log -1` first is _not_ the fix: that check has a race whose window is
  exactly as long as it takes to compose a commit message, which is the window
  that cost `57efb52`. A follow-up commit is always available and has no failure
  mode. Scoped to concurrent work — amending is fine in a single-agent session.

  **Clause four: restore with `git checkout HEAD -- <file>`, never from a file
  copy.** A `cp` from a scratch backup is a snapshot of a moment, and replaying
  it into a shared worktree is a revert of everything that landed since.
  Restoring this plan that way reverted `9851fd6` — 47 insertions and 57
  deletions of another agent's committed work — and was caught one command later
  only because `git diff --stat` on a file just "restored to clean" was not
  empty, which is the check that catches it. The reasoning generalizes past git:
  a backup replayed into shared state is a revert, not a repair. Two corollaries
  from using it: `git checkout HEAD -- <file>` is correct for discarding **your
  own** uncommitted edits and is destructive if a peer holds the same file dirty,
  since it discards their working-tree content wholesale; and to drop a bad edit
  while keeping good uncommitted work, re-derive the good work from a script
  rather than replay a copy of it.

  **Clause five: any operation whose safety depends on "everything here that
  isn't mine is noise" is unsafe the moment a peer holds the file dirty, and all
  of them fail silently.** A full-file `Write` presumes the whole file is yours;
  `git apply -R` on the not-mine hunks presumes every not-mine hunk is formatter
  collateral; `git checkout HEAD -- <file>` presumes the working-tree content is
  yours to discard. The failure leaves no conflict marker and nothing in any
  diff, and this is the sentence to keep intact through any future rewording of
  this clause, because it is the whole reason the failure is undetectable after
  the fact:
  **content was replaced by content, so nothing went wrong from git's side.**
  Prefer `Edit` on anchored text, which fails loudly when its anchor has moved.
  Two limits: `Edit` protects only the region it anchors, so a peer editing
  elsewhere is still invisible to it; and a full-file `Write` remains correct for
  a file you created and solely own. The concrete precondition, for the
  collateral-revert case specifically:
  **before reverse-applying the hunks you have classified as "not mine", check
  `git status --short` for any path other than your own and abort if one
  appears** — the classification is only sound while you are the file's only
  writer, and it is the step where you are most convinced you are being careful.
  That is what earns this a clause of its own rather than a footnote on clause
  four: clause four's failures announce themselves as obvious destruction, and
  **this one arrives while you believe you are being careful.** It has already
  cost work here — a full-file write of this plan silently dropped a peer's
  inserted item, which survived only because they had a script to re-derive it
  from.

- **A stamp records when a claim was true, not when it was last checked.** Where
  a figure in this plan carries a sha, that sha is the claim's subject: it says
  the number held _there_, and says nothing about `HEAD`. So re-measure before
  treating any stamped figure as current, and when a later commit breaks the
  property a stamp records, **extend the stamp rather than moving it** — moving
  it certifies the present at the cost of erasing that the property ever broke,
  which is usually the more useful half of the record. Task 21 Step 3 is the
  worked example: it carries both its original `250914b` measurement and the
  `fc01b5e` re-embed that repaired the fence `20f6166` had broken, so a reader
  learns the invariant is intact _and_ that it is breakable by an ordinary
  README edit. The same applies to a surviving-mutant list, which is the most
  perishable kind of stamped claim in this file, because the whole point of
  recording a survivor is that someone will go kill it.

  The rule that makes those stamps load-bearing, stated once: **a tick names the
  sha it was measured at, or the stamp under its task heading does.** A ticked
  box with no sha anywhere above it is a claim about an unspecified tree, which
  is the same defect as a count with no commit. Measured: 21 task headings carry
  a stamp and 125 boxes sit under them, which is why the stamp is per heading and
  not per box — per box repeats one sentence 125 times, while per heading still
  lands in front of someone reading Task 7 out of order, which is the case the
  no-cross-referencing rule exists for. Deliberately **not** wired to a CI
  check: a lint rule for checkbox provenance on a plan file that stops changing
  when the branch merges is machinery outliving its subject, which is the same
  shape as the `go mod tidy` ban in this section that outlived its condition —
  this section's own defect class volunteering for itself. The enforcement is a
  reader who can see the sha.

  The same rule with the enumeration taken out: **prefer a claim the reader can
  re-derive to an enumeration that needs maintenance.** An enumeration is a
  measurement frozen at a moment; it reads as exhaustive and goes stale the
  instant another instance lands, with nothing in the text to say that it has —
  which is the stamp failure above wearing a different costume. Recorded because
  it has now been proposed twice and declined twice: the first version of Task 21
  Step 6's staleness note named the single commit that had touched
  `.github/workflows/ci.yml` since the step ran, and a second one landed the
  commit after. Completing that list would have serviced the treadmill instead of
  removing it, and servicing it once teaches the next reader that it is supposed
  to be serviced. State the conclusion and hand over the command instead —
  touched again since, run `git log --oneline -- <path>` for the current history
  — which cannot go stale. The payoff is larger than saved maintenance: a
  re-derivable claim can be _checked_, and so can be caught being wrong, where an
  enumeration can only be caught being stale, and only by accident. This plan's
  count of directory-form `git add` lines is the worked example. Its check and
  its prose disagreed, 20 against 21, and the **check** was the part that was
  wrong: `'^git add [^ ]*/$'` tests only a line's last argument, so it missed the
  one step that lists a directory first. Corrected to
  `'^git add .*[^ ]/(\s|$)'` it returns 21 and agrees with the prose — measured
  at `79a2580`, where 24 `git add` lines split 21 directory-form and 3 naming
  files.

- **Edit this file with a script, not the `Edit` tool — but keep `Edit`'s
  anchoring.** Measured on this plan: four separate scripted replacements
  produced 4 hunks and no unrelated churn, where a single comparable `Edit`
  produced about 26 — table realignments and roughly twenty blank lines dropped
  after `git commit -m "…" \` continuation lines, spread over eleven regions
  nowhere near the edit. The cause is a `PostToolUse` hook that runs prettier on
  every `Edit` and `Write`; the repo's `.prettierignore` does not stop it,
  because the hook's working directory is outside this repo. A scripted write
  bypasses the hook entirely. What makes the substitution safe rather than merely
  quieter is keeping the property that makes `Edit` worth using under clause
  five: anchor every replacement on exact existing text and assert it matches
  exactly once, so a moved anchor aborts instead of writing to the wrong place.
  Reviewing the diff is not a substitute — 26 hunks of formatter noise is
  precisely the condition under which a real change stops being visible.

  One reasoning error is worth naming, because it is what put churn in this file
  rather than in theory: running prettier here by hand, on the argument that
  "the hook does it anyway," treated a tool's _output_ as equivalent to a tool's
  _invocation_. Re-measured to check that claim rather than repeat it — the
  hook's exact command, run by hand from outside the repo against a clean export
  of `e9134ed` — 12 insertions and 31 deletions across 27 hunks spread the length
  of the file, including a `<scratch>` continuation line dedented out of the
  bullet it belongs to. The argument is persuasive because the output really is
  what the hook would have produced; it is wrong because invoking it yourself
  makes those 27 hunks yours, in your commit, on a file other agents hold dirty.
  The check is the cheap one: read the file after running any tool that writes
  it, and before deciding the write was harmless.

- **Measure test counts only on a clean tree, and verify it is clean before
  measuring.** Several tasks state an expected test count. `go test` compiles
  every `*_test.go` in the directory and has no flag to skip untracked ones, so
  it counts _files, not commits_: a stray `_test.go` is indistinguishable from a
  real added test in the output and inflates the count by exactly the same
  magnitude. So do not run the count bare. Run it behind the cleanliness check,
  as one command:

  ```bash
  test -z "$(git status --porcelain --untracked-files=all <dir>/)" \
    && go test ./<dir>/ -count=1 -v
  ```

  The `test -z` is what makes this a gate rather than a suggestion. `git status`
  exits `0` whether or not it prints anything, so a bare status line followed by
  a bare `go test` produces a number no matter how dirty the tree is, and
  compliance reduces to _a human noticing output above the thing they were
  actually looking for_. Wired this way the count either does not appear or is
  trustworthy. **Note the sequencing** — this guards _measuring_, not
  committing. The `git add` check in the bullet above fires at the commit step,
  which comes after the count step in every task, so it cannot protect a count
  and must not be relied on to. Nor can that check be converted to this form:
  at a commit step the directory is _supposed_ to be dirty, so emptiness is the
  wrong assertion there and inspecting the list is the actual work.

  **The gate is deliberately stricter than the incident it prevents, and that
  has a cost.** It refuses to measure while _any_ edit is outstanding, including
  an implementer's own legitimate mid-task work, so it will fire during ordinary
  development and not only when something is wrong. That is the right default
  for a step whose output gets written into this plan as fact — you should not
  certify a count against a tree you have not committed. But do not let routine
  bypassing become the habit, because the habit is what will bypass it on the
  one run that mattered. If the friction is genuinely too high, narrow it to
  `git ls-files --others --exclude-standard <dir>/`, which catches the actual
  incident class — untracked strays — while accepting locally-modified tracked
  tests. Narrow it deliberately; do not drop it.

  This failure does not announce itself. During Task 13's fix pass a stray
  harness made `internal/picker` measure 27 tests when 26 were tracked; 27 was
  also the correct count once the real test landed. Both numbers are historical
  and neither is a target to hit — `internal/picker` measures 60 at `4a55f84`. The contaminated
  number and the right number were identical, so the arithmetic looked
  self-consistent while resting on a file that was never committed — which is how
  the stale `41` at Task 19 survived a review pass.

- **A mutant that fails to build is not a killed mutant, and scoring it as one
  hides the survivor.** Deleting the code a guard depends on frequently orphans
  something the compiler then rejects, and `go test` reports that as `FAIL` with
  a non-zero exit — the same shape as a test catching the regression. Measured
  here: removing both include-recursion guards from `internal/sshconfig` orphans
  `abs` and fails in 0.3s with
  `internal/sshconfig/sshconfig.go:454:4: declared and not used: abs`,
  `[build failed]`. Nothing was tested, and the run reads as a kill. The
  compile-preserving form is to neuter each condition rather than remove it —
  `if ancestors[abs] && false` and `if depth >= maxIncludeDepth && false` — which
  keeps the package building and gives the real answer. **So check that a mutant
  compiled before believing anything its run says**, and prefer neutering a
  condition over deleting the code that uses its operands.

  The real answer in this instance also corrects the record. That mutant does
  **not** hang: it exits 1 in about 0.3s with `fatal error: stack overflow` and
  501 lines of runtime stack, and under `-race` the trace names
  `sshconfig.TestParseIncludeCycle` directly. The process dies on a fatal runtime
  error before any timeout can fire, so the `-timeout 45s` in the scan that
  produced this result never fired either, and the earlier report of a hang was
  reading a build failure and a stack overflow as one. The distinction matters
  because the two failures need different tooling: a stack overflow is
  self-reporting, while a genuine deadlock reports nothing until a `-timeout`
  turns it into `panic: test timed out after 5s` followed by `running tests:` and
  the test's own name. `internal/probe` is where this repo's real candidates are,
  since it streams over an unbuffered channel and selects on `ctx.Done()`.

- **Every step that decides pass or fail from a test run must use `-count=1`, on
  every run, without exception.** There are two distinct ways a `go test`
  invocation reports success while telling you nothing about the code you just
  changed, and both are invisible to an exit-code check and to a `^ok` grep:

  - `go test -run` takes an **unanchored regex**, and a selector that matches
    nothing still exits `0` and prints `ok <pkg> [no tests to run]`. That
    silence is not a pass. Prefer the whole package for any gate; where a
    selector is unavoidable, measure how many tests it actually selects and
    write that number down. Measured: `-run TestSession` selects 3 in
    `cmd/herdr-ssh` where `-run Session` selects 8 at `0ff9e4d`, and
    `-run TestRank` in `internal/picker` also catches `rank_unicode_test.go`,
    not just `rank_test.go`.
  - A cached result prints `ok <pkg> (cached)`, which also satisfies a `^ok`
    grep and is not a statement about the current tree at all.

  `-count=1` defeats the second and makes the first visible. A harness that
  classifies a run by exit code, or by prefix-matching `ok`, is wrong for both
  reasons: classify on the `--- FAIL:` and `--- PASS:` lines instead.

  A third way a count can be true and still useless: **a point-in-time count
  must be measured at the point in time it describes.** A count in Task N's step
  is a claim about Task N's commit, not about `HEAD`. Measuring it at `HEAD` is
  not a stricter check, it is a different one, and like the two failures above it
  reports a result about something other than what it appears to measure. Task 12
  Step 4 is the worked example: `-run TestRank` selects 14 at `5d35d2c`, where
  that task ends, and 20 at `4a55f84`, so its "PASS (14 tests)" is correct as
  written and a `HEAD` reading would have "corrected" a correct line. Measure on
  a clean export of the task's own commit — `git archive <rev> | tar -x -C
<scratch>` — and not on the working tree.

- **A spec row with a conjunction in its condition column is two requirements
  wearing one row, and needs two assertions.** An "and", an "or", a "plus", or a
  comma series each make the row independently falsifiable in more than one way,
  so a single test that satisfies the row as a whole can leave one half entirely
  unpinned. Split the row during the traceability pass and write one assertion
  per conjunct. Deviation 8 below is an instance: `Warnings` carries both the
  unreadable-`Include` case and the malformed-line case, and one generic footer
  assertion pinned neither of them specifically.

- **In a concurrent test, assert what must be true, never how many results
  arrive.** Counting is the tempting strengthening and it is usually a flake
  pointed in the wrong direction. `TestRunForwardsItsContextToTheDial` in
  `internal/probe` runs one target under an already-canceled context and
  asserts only that nothing came back `Up` — it deliberately does _not_ assert
  that a result arrives. Under a canceled context the goroutine has to win two
  independent coin flips to deliver one: the semaphore select and the send
  select each have both arms ready, a free slot or a live receiver on one side
  and a closed `ctx.Done()` on the other, and Go picks uniformly at random
  among ready cases. Two fair selects put the arrival rate at 0.5² = **1 in 4**.

  Measured, 400 trials per run: 114/400 (28.5%) on the implementer's machine,
  and 88, 100, 100, 112 per 400 here — 22% to 28.5% around a theoretical 25%.
  So `len(got) != 1` would fail roughly **70-75% of runs against correct
  code**, which is the worst kind of flake because it fails in the direction
  that looks like a real bug. Do not tighten that assertion. The corrected
  figure and the two-select mechanism are recorded in the test's own comment,
  and that comment is the authority this plan follows — an earlier estimate of
  "about half the time" assumed a single select and was wrong about the
  mechanism, not just the number.

- **Do not run `go mod tidy` as a drive-by — but the reason this rule used to
  give was inverted.** Every dependency in `go.mod` is marked `// indirect`,
  which was accurate at scaffold time when nothing imported them. This rule
  claimed tidy "silently drops `bubbletea`, `lipgloss`, and `go-toml`, and the
  next build fails on unresolvable imports". Those three are precisely the ones
  tidy **promotes**. Measured on a clean export of `277f532`:

  ```
  go mod tidy  → exit 0
  go.mod  1024 → 1001 bytes   bubbletea/v2, lipgloss/v2 and go-toml/v2 move out
                              of the indirect block into a real require block;
                              the other fifteen stay indirect
  go.sum  3298 → 3937 bytes   +6 lines, nothing removed
  then    go build ./... ok · go vet ./... ok · 7/7 packages ok
  ```

  **The ban was correct when written and expired without being retired.** At
  Task 1, `go.mod` was hand-pinned with all eighteen dependencies before any
  code imported them, so tidy would legitimately have removed all eighteen as
  unused. Real imports landed across Tasks 11-19, the precondition went away,
  and the rule kept propagating on its original wording. That is the failure
  class this plan hunts everywhere else — a warning that reports a hazard about
  a state the repo left many commits ago — aimed at one of the plan's own rules.
  It survived by being repeated, which is the only reason it needed measuring.

  **Tidy is not free, which is why it is the operator's call and not a
  cleanup.** Beyond reclassifying three requires it records `go.sum` hashes for
  three modules that are not pinned today — `github.com/aymanbagabas/go-udiff`,
  `github.com/charmbracelet/x/exp/golden` and `golang.org/x/exp`, the test
  dependencies of `bubbletea` and `lipgloss`. They land in `go.sum` only, never
  in the require set, and they do not join the build; but "it only corrects
  metadata" undersells a change that hash-pins three modules that were not
  pinned before. Recommend it, do not commit it.

  **Guard follows tidy, in that order.** A `go mod tidy -diff` CI step is the
  right durable check and it cannot be added first: against today's tree it
  fails by design, so adding it before the tidy is accepted red-lights CI on
  arrival.

  **The stale markers stay until then — and they are documented now, which was
  the actual gap.** A wrong `// indirect` comment is metadata only: it changes
  no resolution, and the build, tests, `go vet`, `staticcheck` and `errcheck`
  are all indifferent to it. What was missing was any record of that where a
  contributor would look. Reading `go.mod` alone, eighteen indirect
  dependencies invite a reflexive tidy that produces churn the author cannot
  explain. `README.md` now has a Development section saying not to, and `go.mod`
  carries a comment above the require block naming the three misclassified
  modules and why nobody has fixed them. That comment is inert — require set,
  module path and `go` directive are identical under `go mod edit -json`, and
  `go mod edit -fmt` preserves it byte-for-byte.

  Task 21 Step 5 still asserts `git diff --exit-code go.mod go.sum`, but no
  longer as a stand-in for a tidy nobody may run. It is there to catch
  `go run <pkg>@latest` mutating the pinned set as a side effect, which is worth
  catching regardless of what is decided about tidy.

- **`go build ./...` fails from Task 15 through Task 18, and that is expected.**
  `cmd/herdr-ssh` is `package main`, but `func main()` does not arrive until Task 19. So the four `cmd` tasks before it produce a main package with no
  entrypoint, and the build fails at link time with `runtime.main_main·f:
function main is undeclared in the main package` — a message about the
  package's incompleteness, not about the code the task added.

  `go test ./...`, `go vet ./...`, and `gofmt -l .` all pass throughout, because
  a test binary supplies its own entrypoint and neither vet nor gofmt links. So
  those three are the gate for Tasks 15 through 18; `go build ./...` becomes a
  usable check only from Task 19 on. The failure mode this rule exists to
  prevent is an implementer treating the link error as its own defect and
  "fixing" it by adding a `func main()` several tasks early — which would put an
  undeclared file change into a task whose Files list names two files, and take
  the entrypoint out of the task that specifies it.

  Historical as of `9db050e`: Task 19 has landed, `func main()` exists, and
  `go build ./...` exits 0 at `HEAD` for the first time in the project's
  history — measured, not assumed. That does not retire this rule, and the rule
  is not a claim you can check against `HEAD`: it describes what an implementer
  sees while executing Tasks 15 through 18 from a clean start, which is still
  exactly right.

- **Before finishing a test file, name what every test in it discards, and what
  every fixture in it holds constant.** Two defects on this plan came from that,
  and neither was visible to line coverage.

  In `model_test.go`, the shared `press` helper drops the `tea.Cmd` that
  `Update` returns, and every test that presses a non-quit key goes through it.
  So the "does this key quit?" output channel was unasserted for every key
  except the two that are supposed to quit — a model that returned `tea.Quit`
  on _every_ keystroke would have passed the entire file. In `rank_test.go`,
  every `Alias` and `HostName` fixture was already lowercase, so the two
  `strings.ToLower` calls in `scoreHost` did no work under test: deleting both,
  or either one alone, left all 45 tests in `internal/picker` green — 45 being the count at that commit, not a current figure —, while in a
  real config it makes `Host GitHub-Work` unreachable by `gw` and `NixOS-Dev`
  unreachable by `nixos`.

  **Coverage cannot find these.** The discarded `Cmd` and both `ToLower` calls
  were executed by existing tests the whole time. Coverage records that a line
  ran, not that anything observed what it produced, nor that the input gave it
  any work to do. A fully-covered no-op is still a no-op.

  So spend two minutes at the end of each task that adds tests:

  - **What does every test discard?** Any return value, field, or output stream
    that no assertion in the file reads. Shared helpers are where this hides —
    one helper that drops a value drops it for all its callers at once.
  - **What do all the fixtures agree on?** Any property the code branches on or
    transforms where every fixture is the same: case, empty vs. populated, one
    vs. many, zero vs. non-zero, ASCII vs. multi-byte.
  - **What does no production path read?** Any field a test asserts is written
    that nothing outside the tests consumes. This one is grep-checkable: for
    each field an assertion names, search the non-test files for a read. A test
    like `TestWindowSizeIsRecorded` looks load-bearing and is not — it verifies
    the write it just made, so it passes on a build where the value is used for
    nothing. That is how `m.height` stayed unread through two tasks while the
    view overflowed the pane. `m.width` is still in that state deliberately;
    see the deviations list.

  - **What did a new seam just make observable?** A seam introduced for
    testability creates observability the tests don't yet use, and that unused
    observability is the gap. Enumerate the newly-observable arguments at
    seam-creation time — ask what the test stub _discards_. The tell is
    syntactic: a stub declared
    `func(context.Context, time.Duration, string) (net.Conn, error)`, with
    every parameter unnamed, cannot read any of them, so every argument the
    seam just exposed is unobserved by construction.

  For each, either add a test that observes or varies it, or write down why the
  code cannot depend on it. Then confirm by mutation — neutralize the code that
  only matters along that dimension and check the suite goes red. Run the full
  suite, never `-run`; a filter can hide the very test you just wrote. `-run`
  takes an _unanchored_ regex over the full test name, so a pattern selects
  every test whose name contains a match — more than you meant, or, if nothing
  matches, none at all. A run that selected nothing still exits 0 and prints
  `ok ... [no tests to run]`, so its silence is not a pass.

  **A surviving mutant is not automatically a gap.** Some code is genuinely
  unobservable, and no test can kill it. `rank.go:181` and `:183` lowercase the
  haystack a second time before calling `positionScore`, and mutants deleting
  those survive — correctly. `positionScore` reads its string only through
  `isSeparator`, whose seven separators are all case-invariant; checked across
  all 1,112,064 valid runes, none changes separator-ness under `ToLower` and
  none lowercases _into_ a separator. Those two calls provably cannot change a
  result. That is redundant work, not untested behavior, and the fix if any is
  to delete them — not to chase them with a test that cannot exist. Work out
  which kind of survivor you have before reacting to it.

  **The seam case, measured, because the attribution decides the lesson.**
  `dd54c30` added the `dialContext` seam to `internal/probe` so a test could
  observe the concurrency bound, and wrote its stub with all three parameters
  unnamed. Of the three arguments the seam newly exposed, two were observed by
  nothing. Mutating the dial at that commit, full package suite each run,
  revert diffed byte-identical between mutants:

  | mutant at `dd54c30`                                  | result                                |
  | ---------------------------------------------------- | ------------------------------------- |
  | `dialContext(ctx, 0, t.Addr)`                        | SURVIVED — suite green                |
  | `dialContext(context.Background(), timeout, t.Addr)` | SURVIVED — suite green                |
  | `dialContext(ctx, timeout, t.Alias)`                 | KILLED — `TestRunReportsReachability` |

  The one that died died to a test that predates the seam, so the seam
  contributed no coverage of its own. `6fe5674` then pinned the timeout and
  address and `edd2882` pinned the context, one at a time, with a green suite in
  between that proved nothing. `79d18a9` is the counter-example and the pattern
  to copy: it created a new delegation hop — `Run` became a one-line call into
  `run` with the dial passed in as a parameter — and pinned both of that hop's
  arguments in the same commit, so the hop never existed in an unobserved state.
  Found and reported by `impl-probe-bound`.

  **Documenting a gap in a doc comment reads like diligence and is the failure
  mode.** Both of `Run`'s delegation survivors were deterministically killable —
  no clock, no network, 0.00s — and the tempting move at the moment of discovery
  was a `// Known untested hop` comment instead. That comment would have frozen
  "untestable" into the source, and the asymmetry is the whole point: the gap is
  closed in one commit, while the comment outlives it and goes on asserting that
  the gap is permanent, so the next reader believes it and does not try. Write
  the failing test, not the note. Reported by `impl-probe-bound`.

  This does not contradict the paragraph above it. Recording _why code is
  provably unobservable_ — the `rank.go` `ToLower` case, where the property was
  checked across all 1,112,064 valid runes — is a proof, and a proof is worth
  writing down. Recording that something is _currently untested_ is a status,
  and a status in a comment is a claim that rots silently. State proofs; never
  state coverage.

- **A later task's text is compiled by nothing until its implementer reaches
  it.** Four defects on this plan came from that: the `Load` → `LoadDir` rename
  that left four undefined call sites in Tasks 16 and 19, two stale
  `design.md:<line>` citations, the test counts above, and an `errcheck` rule
  added in Task 21 that retro-failed Task 11's code. When a task renames or
  reshapes an exported symbol, adds a lint rule, or changes a count, grep this
  plan for the consequences before marking it done.

  Generalized, because the incident list keeps growing: **a fence in this plan is
  a claim about the tree at the moment it was written, not a fact about the tree
  now.** Before implementing any task, diff its fences against the current files
  with `git show` and treat any mismatch as the fence being stale until proven
  otherwise. Two have already drifted exactly this way — Task 19's `runPicker`
  fence, which predates Task 16's `fatalInPane` helper (see the note at Task 19
  Step 5), and Task 21's README fence, which still said
  `install github:purehate/...` after the committed `README.md` had moved to the
  bare form.

- **Fenced `yaml` and `json` blocks in this plan must carry
  `<!-- prettier-ignore -->`.** A `PostToolUse` hook runs
  `npx prettier --write --ignore-unknown` on every file edited in this session,
  and prettier formats embedded code for the languages it can parse. Verified by
  experiment rather than assumed: it rewrites `yaml` and `json` fences
  — collapsing indentation, turning `[ main ]` into `[main]` — and leaves `go`,
  `toml`, and `bash` fences untouched. The rewrites are semantics-preserving for
  valid YAML and JSON, so nothing breaks at runtime; the problem is that this
  plan's fences get checked byte-for-byte against committed files, and a fence
  that silently reformats itself cannot be. The three affected fences are marked
  already: Task 20's `plugin list` output, and Task 21's `ci.yml` and
  `.golangci.yml`. Mark any new one at the moment it is added.

  A repo `.prettierignore` is **not** sufficient on its own. Prettier resolves
  it relative to the process CWD, and the hook does not run from this repo's
  root — confirmed by running the hook's exact command from another directory,
  where the ignore file had no effect. The in-file marker is the guard that
  actually holds; the `.prettierignore` only covers someone running prettier
  from the repo root by hand.

---

## File Structure

| File                                    | Responsibility                                             |
| --------------------------------------- | ---------------------------------------------------------- |
| `herdr-plugin.toml`                     | Manifest: build, action, two pane entrypoints              |
| `internal/sshconfig/sshconfig.go`       | Parse SSH config → `[]Host`; `Exclude` for hidden globs    |
| `internal/theme/theme.go`               | Herdr `[theme].name` + `[ui].accent` → color tokens        |
| `internal/pluginconfig/pluginconfig.go` | Plugin `config.toml` → `Config` with defaults + validation |
| `internal/herdrapi/herdrapi.go`         | Every `herdr` CLI call behind a `Runner` seam              |
| `internal/probe/probe.go`               | Async TCP reachability                                     |
| `internal/picker/rank.go`               | Fuzzy ranking                                              |
| `internal/picker/model.go`              | Bubble Tea model: keymap, filter, cursor                   |
| `internal/picker/view.go`               | Rendering + `Run`                                          |
| `cmd/herdr-ssh/main.go`                 | Verb dispatch; `picker`, `open-picker`, `connect` verbs    |
| `cmd/herdr-ssh/caller.go`               | `caller.json` read/write                                   |
| `cmd/herdr-ssh/connect.go`              | `performSelection`: reuse-or-open                          |
| `cmd/herdr-ssh/session.go`              | Rename own pane, exec ssh                                  |
| `cmd/herdr-ssh/hosts.go`                | Load hosts from configs, build probe targets               |

Note: the spec's `internal/probe` interface was `Probe(ctx, []Host, timeout)`, but the spec also lists that package as depending on stdlib only. This plan keeps the stdlib-only boundary and has `probe.Run(ctx, []Target, timeout)` take its own `Target` type, with `cmd` doing the `Host → Target` conversion.

**Deliberate deviations from the spec:**

1. **`herdrapi` test seam.** Spec suggested a fake `herdr` binary on `PATH` (sesh's `HERDR_FAKE_LOG` pattern). This plan injects a `Runner func([]string) ([]byte, error)` instead — same argv assertions, no subprocess, faster tests. The real exec seam lives in `New()`.
2. **No `bubbles` dependency.** Spec's package table implied a text input component. Hand-rolling the query string (append `k.Text`, trim on backspace) drops a dependency and an API-surface risk, and is directly testable through `Update`.
3. **Signatures.** Four departures from the spec's `## Packages` table, not two — the first pass of this entry recorded only the first two, which made the log a sample rather than a record:
   - `picker.Run(Options) (Selection, bool, error)` rather than `Run([]Host, Theme, Config)` — five call-site arguments that all mean "config" is worse than one struct.
   - `FocusPane(pane, currentWorkspace, currentTab)` rather than `FocusPane(pane)`, because skipping already-current focus steps requires knowing what is current.
   - `theme.LoadFile(path) (Theme, error)` rather than `theme.Load(configPath) Theme`. This is the substantive one: it is not just a rename but an **added error return**, which is a contract change. `runPickerWith` acts on it by pushing a footer warning, so the error is load-bearing rather than ignored.
   - `pluginconfig.LoadDir(dir) (Config, error)` rather than `pluginconfig.Load(dir)`. Rename only; the `Dir` suffix says the argument is a directory rather than a file, which the picker's two config sources made worth distinguishing.
4. **Overlay-close argv corrected.** The spec's data flow says `herdr plugin pane close picker`. The live CLI is `herdr plugin pane close <PANE_ID>` — it takes a pane id, not an entrypoint name. The overlay closes itself with its own `$HERDR_PANE_ID`.
5. **"Overlay stays open" on a `herdr` CLI failure is implemented as hold-then-close.** The spec's error table wants the overlay to survive a failed `herdr` call with the error in the footer. `performSelection` runs after the Bubble Tea program has already exited, so this plan prints the error in the pane and waits for enter before closing — the message is readable, but the picker does not re-enter. Re-entering would mean moving pane-opening inside the event loop; that is a follow-up, not this cycle.
6. **`herdr config reload` and `herdr plugin search` do not exist.** Verified against herdr 0.9.0: the reload is `herdr server reload-config`, and there is no plugin search subcommand. Tasks 20 and 21 use the real commands.
7. **RE-OPENED — `m.width` is written and never read.** History, because the retirement is instructive: this was first recorded as a deviation (the spec was silent on wide-pane behavior, so recording a dimension with no consumer departed from a silent spec), then retired at `f7ec2bd` on the grounds that the spec's `### Layout` section had been amended to call pane width deliberately unused. The final review reversed that amendment: the spec now says width **is** used and over-wide lines are **truncated**, because the key hints alone need 77 columns and a pane narrower than that reflows the frame the height invariant exists to protect. So the retirement's own premise is gone and the deviation is open again — with the direction flipped. It is no longer "the code records a dimension the spec doesn't ask for"; it is "the code is behind the spec until row and preview truncation land."

   Worth keeping the retirement text rather than overwriting it: retiring a deviation by amending the spec to match the code is legitimate exactly once, when the spec was genuinely silent and the code's choice was genuinely right. It is illegitimate when the code's choice was never examined — which is what happened here. The amendment made the picker's behavior in a narrow pane a specified position without anyone checking what that behavior was.

   Two things unchanged across all three states: `m.width` is still written with no consumer, and `TestWindowSizeIsRecorded`'s width assertion still pins plumbing rather than behavior and must not be read as coverage of it.

   **CLOSED at `58c9762`.** Row and preview truncation landed, so `m.width` has a consumer and the code is level with the spec. The clause every state of this entry turned on — written, never read — is false for the first time since it was opened, and that is what closes it. Worth naming the difference from the `f7ec2bd` retirement, since the entry exists to make it visible: that one closed the gap by amending the spec to match unexamined code, this one by writing the code the spec asks for. Only the second kind is available once the spec's position has been checked.

   `TestWindowSizeIsRecorded`'s width assertion still pins plumbing rather than behavior. It is no longer the only thing that reads the field, so it is no longer mistakable for coverage of it — but it is still not that coverage, and the truncation tests are.

8. **The warning footer is generic where the spec names the condition.** Spec `:335` wants `1 include unreadable`; `view.go` renders `%d config warning(s)`. The code is right and the spec is too narrow — `Warnings` also carries malformed-line warnings (spec `:336`), so a hardcoded "include unreadable" would mislabel them. Recorded rather than reconciled because the spec row is about `Include` handling specifically and rewriting it is a wider edit than this cycle needs.

   **Still open after the final review, and for a reason this entry missed.** The argument above is sound about the _label_ and silent about the _payload_: whichever condition the footer names, it currently shows a count and never the warning text, so the operator learns that something was wrong with their config but not what or where — while every warning already carries a file and line. The spec's `## Error Handling` table separately promises "Footer shows the error", which the count does not satisfy under either wording. So this is spec non-compliance rather than a defensible divergence, and rendering the text is queued as a fix. Noting the failure mode of the original entry too, since it is the more portable lesson: it reasoned correctly about the difference the reviewer had raised and treated that as having reasoned about the line.

   **CLOSED at `aa342f6`.** The footer renders the warning text, capped at three lines with a "+N more" notice outside the cap, matching the host list's existing `maxRows`-plus-notice shape.

   The fix turned up a hazard neither this entry nor the review that reopened it had considered. `pluginconfig.LoadDir` joins its rejections with `errors.Join`, whose `Error()` separates them with a newline — so a config with two bad keys produces **one** warning value that draws **two** rows, under a reserve that counted it as one. The width clamp cannot see it either: by the time the frame is assembled, an embedded newline is indistinguishable from a row the renderer meant to emit. Flattened in `view.go`, the layer that owns the row budget, rather than at the producer — the newline is correct in `errors.Join`'s output and only wrong against a row budget.

   The measurement gap it exposed is the more portable half. Every height assertion in the suite was `<=`, so nothing in it could catch _over_-reserving: a frame that silently wasted rows was exactly as green as one that fit. Closed with an exact-equality sweep over the warning block. A one-sided bound reads like a check and is only half of one.

   A related one-line follow-on landed at `320521c`. The theme arm was still labelling an error that already named itself, so the footer read `theme: theme config <path>: ...`. The assertion that would appear to catch that — counting occurrences of the loader's phrase — provably cannot, because the label does not repeat the phrase it duplicates; under a mutation restoring it, the ordered warning test stays green. So the test requires each load warning to _begin_ with its loader's own text, which fails for any prefix at all.

9. **Two files exceed the spec's size guidance.** Spec `## Packages` says "None should exceed roughly 250 lines". `internal/sshconfig/sshconfig.go` is 506 and `internal/picker/view.go` is 351; every other file is inside the guidance. Recorded rather than fixed because a split late in the cycle would rewrite the two files under the most test pressure for a reason that is not a defect — and "roughly" is doing real work in that sentence. But the number is 2x on `sshconfig.go`, which is past what "roughly" covers, so this is a deviation and not a rounding difference. The natural seams if it is taken up: `sshconfig.go` separates tokenizing/`Include` expansion from pattern matching and host materialization, and `view.go` separates row rendering from the preview panel. Recommended as a follow-up rather than as part of this branch.

   _Re-measured at `320521c`: **583** and **489**, and a third file has crossed the line — `cmd/herdr-ssh/main.go` at **277**. The entry's "every other file is inside the guidance" no longer holds, so the counts above are a claim about the commit that wrote them and not about `HEAD`._

   Worth recording why it moved and not only that it did: the work that closed deviations 7 and 8 is what grew `view.go` by roughly 140 lines. Two deviations were closed by enlarging a third. That does not make either closure wrong — truncation and warning text are behavior the spec asks for, and the size number is guidance where the defects were not — but it does move this from housekeeping to the largest single item left open on the branch, and it means the seams above are now load-bearing rather than advisory.

10. **The spec said the host list was clipped; it has scrolled since `293eb18`.** The `### Layout` section claimed the window showed the first screenful and that hosts past it were unreachable without typing. `window()` — which scrolls to keep the cursor visible and reports the number of hosts outside the view — landed at `293eb18`, three commits _before_ `f7ec2bd` wrote the "clipped, not scrolled" clause. The spec has been corrected to describe scrolling, with the reason the code is right: cursor movement is clamped to the length of the filtered list, so clipping the view without also clamping the cursor would let the selection walk off-screen and `enter` connect to a host the operator cannot see.

    Logged as a deviation even though the code never diverged, because the record did. `f7ec2bd` presented itself as writing down existing behavior and instead wrote down the opposite of it, which made the spec a worse guide than its own silence had been — a reader who trusted it would have "fixed" the scrolling. This is the same defect the review kept turning up in its own tooling, in prose form: a statement that appears to report on one thing while actually reporting on another.

11. **`extra_config_paths` was built as planned and then removed as a defect.** Tasks 5 and 17 specify the key, the `ExtraConfigPaths` field, the `loadHosts` loop that parses each path, and the `seen`/`add` dedup closure that existed only to fold several parses together. All of it was implemented, tested, and then deleted at `5a31f54`.

    The reason is in that commit in full; the short version is that the feature's definition contradicted its mechanism. It was specified as "SSH configs outside the `Include` chain", and selecting a host execs `ssh <alias>` with no `-F` — so ssh resolves the alias against `~/.ssh/config` and its `Include` chain alone, which is precisely the set those files are defined to sit outside. Every row sourced from one carried a `HostName`, `Port`, `User` and `ProxyJump` that ssh never saw. Measured against `ssh -G` as the oracle on synthetic fixtures: four divergences from the one root cause. The `ProxyJump` case is the dangerous one — such a host is skipped by the probe and marked `~`, so nothing looked wrong while the operator selected a host they believed was reached through a bastion and ssh connected directly.

    **Task 5 and Task 17 bodies are left unedited on purpose.** They record what was planned and built, which is what a plan is for; rewriting them to match today's tree would erase the fact that this shipped before it was caught. Read them with this entry. The one place that matters operationally is Task 21's README sample, which still lists the key — the README itself no longer does.

    The lesson worth keeping is about where the review found it. Three spec-compliance reviews passed this feature, correctly: the code did what the spec said. What none of them asked was whether the spec's own two sentences could both be true at once. A spec-vs-code check cannot catch a spec that contradicts itself, and this plan had no gate that read the spec against `ssh`'s actual behavior.

**Deferred follow-ups:** confirmed, not defects, deliberately not fixed on this
branch. Recorded so they are neither lost nor re-litigated.

1. **`quitting` is write-only in production.** `internal/picker/model.go` declares it, three sites write it, and the only read anywhere is a test. Removing it is a real simplification with no coverage loss: the test pairing it appears in asserts both `quitting` and the returned `tea.Quit`, on the sound reasoning that setting the flag without returning `tea.Quit` would leave Bubble Tea's runtime loop running forever — but the assertion carrying that reasoning is the `tea.Quit` one, and the flag half is self-referential. Left for the `view.go` split above, so the package is edited once rather than twice.
2. **The file split in deviation 9.** The largest item left; see the seams named there.
3. **An empty `Placement` silently drops `TargetPane` and `Direction`.** `PluginPaneOpen` omits `--placement` when the field is empty and lets herdr apply its own default, which is reasonable on its own. But `PlacementTargetsPane("")` is false, so the `--target-pane` guard also declines — and the `--direction` guard tests `Placement == "split"` and declines too. An open with a pane id and a direction therefore sends neither, and says nothing: the flags are dropped by the same lines that exist to drop them correctly, so the omission is indistinguishable from the intended behavior. Unreachable today, and verified so rather than assumed: `choose` is called with only `split`, `tab` and `zoomed`; `openPicker` hardcodes `overlay` and passes no `TargetPane`; `runConnectWith` defaults to `split` and its flag loop rejects any other value. Not fixed because there is no failing behavior to fix and a guard for an unreachable input is a guard no test can exercise — the same reasoning that keeps `--width`/`--height` off `OpenOpts`. It is recorded because it is a trap laid for whoever adds a placement: the natural way to add one is a new constant threaded through `choose`, and nothing in the type, the guards, or the tests will mention that two other fields quietly depend on the value being non-empty and on `PlacementTargetsPane` knowing about it.
4. **`popup` is in herdr's placement vocabulary and in none of this plugin's paths.** `OpenOpts.Placement` documents it, `PlacementTargetsPane`'s comment reasons about it, and both the CLI flag loop and the picker's keys reject or never produce it. That is deliberate — the comment at `OpenOpts` records why (only one popup may be open at a time, which ruled it out for the _plugin-pane_ path) — so this is not a gap to close. Read that as "no popup is opened through `herdrapi`", not "the picker is not a popup": the picker floats in a popup **keybinding**, which never touches this API. See the correction under the placement findings below. It is listed next to the entry above because it is the same asymmetry from the other side: the type's vocabulary is herdr's, the code's is narrower, and the two are kept in agreement by comments rather than by anything that fails. A future placement inherits that.

**Rejected proposals:** declined designs, recorded so they are not re-proposed.
Distinct from the deviations above: a deviation records where the implementation
diverged from the spec, and nothing diverged here — a proposal was declined and
the plan was never changed.

1. **A `SkipProbe` knob for suppressing the probe from tests.** Proposed by
   `impl-probe-bound`, declined in favour of injecting the dial at the seam,
   which became `79d18a9`. The reason is the shape rather than the identifier: a
   knob in production code whose only consumer is a test adds production surface
   to serve the test suite, and a later reader cannot distinguish it from
   configuration. Dependency injection at the dial seam achieves the same
   testability with none of that surface — `run` takes a `dialFn` parameter and
   the fake is passed in at the call site. This entry is a paraphrase of the
   decision and not a quotation: the original proposal text is not in this repo,
   and reconstructing wording to make the record look complete would be the
   comment-that-lies failure mode named in the standing rules. What is sourceable
   is the shape, the reason, and what was done instead.

2. **Unexporting four identifiers with "no external consumers".** Raised by the
   final review, declined because the measurement behind it was invalid. The
   scan looked for `<pkg>.<Ident>` outside the defining package and reported
   zero consumers for ~170 identifiers — including `picker.Update`,
   `picker.View`, `picker.Init`, `herdrapi.PaneList` and `herdrapi.FocusPane`,
   all of which are heavily used. Two reasons, and both make absence the
   pattern's default output rather than a finding: methods are called as
   `m.Update(...)` and `api.PaneList()`, never through the package name, so the
   pattern cannot match a method however used it is; and Bubble Tea invokes
   `Init`, `Update` and `View` through its own interface, so those three are
   called by a framework that names none of them in this source at all.
   Re-measured with `golang.org/x/tools/cmd/deadcode`, which reports nothing
   unreachable in either `-test` or plain mode.

   What survives is a naming nit rather than a defect: an identifier exported
   from an `internal/` package but used only within it could be unexported, and
   nothing is actually exposed either way, because `internal/` has no external
   consumers by construction. Recorded here because the failure is the
   instructive part and it is this project's named defect class turning up in
   the review's own tooling — grepping for the representation the reviewer
   expected instead of the one the code uses, then reading a pattern failure as
   a result.

---

### Task 1: Repo scaffold

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `go.mod`, `.gitignore`, `herdr-plugin.toml`

- [x] **Step 1: Initialize the module**

```bash
cd ~/DEVELOPMENT/herdr-plugin-ssh
go mod init github.com/purehate/herdr-plugin-ssh
```

Expected: `go: creating new go.mod: module github.com/purehate/herdr-plugin-ssh`

- [x] **Step 2: Add the pinned dependencies**

```bash
go get charm.land/bubbletea/v2@v2.0.9 charm.land/lipgloss/v2@v2.0.6 github.com/pelletier/go-toml/v2@v2.4.3
```

Expected: `go: added charm.land/bubbletea/v2 v2.0.9` and similar lines. Requires network.

- [x] **Step 3: Write `.gitignore`**

```gitignore
bin/
.DS_Store
/tmp/
```

- [x] **Step 4: Write `herdr-plugin.toml`**

```toml
id = "purehate.herdr-ssh"
name = "SSH Picker"
version = "0.1.0"
min_herdr_version = "0.9.0"
description = "Fuzzy-pick a host from ~/.ssh/config and SSH into a new pane, tab, or zoomed pane."
platforms = ["macos", "linux"]

[[build]]
command = ["go", "build", "-o", "bin/", "./cmd/herdr-ssh"]

[[actions]]
id = "open-picker"
title = "Open SSH Picker"
contexts = ["workspace", "pane"]
command = ["./bin/herdr-ssh", "plugin", "open-picker"]

[[panes]]
id = "picker"
title = "SSH Hosts"
placement = "overlay"
command = ["./bin/herdr-ssh", "picker"]

[[panes]]
id = "session"
title = "ssh"
placement = "split"
command = ["./bin/herdr-ssh", "session"]
```

The `./bin/herdr-ssh` paths are correct — do not "fix" them to `$HERDR_PLUGIN_ROOT`. `action` and `pane` commands resolve relative to the plugin root, verified against the 13 plugins installed on this machine: `fullerzz.sesh` (the plugin this design is modeled on) ships `./bin/herdr-sesh picker` as a pane command and `./bin/herdr-sesh plugin open-picker` as an action, and three others use bare relative paths the same way. Plugins that do use `$HERDR_PLUGIN_ROOT` are the ones wrapping in `sh -c`, where the inherited cwd no longer applies — a different situation from ours.

**`placement` accepts five values, and `--help` under-reports them.** `overlay`, `popup`, `split`, `tab`, and `zoomed` all work — both in a manifest `[[panes]]` entry (the installed `chmarax.herdr-nvim` ships `placement = "popup"`) and on `herdr plugin pane open --placement`. `--help` lists only `[possible values: overlay, split, tab, zoomed]`, omitting `popup`; the bare `herdr plugin pane` usage line lists all five and is the accurate one here. We use `overlay` for the picker and `split` for the session, so this is inert — but do not add a placement validator built from the `--help` enum, because it would reject a value herdr accepts.

**Neither help text is authoritative. Only parse behavior is.** The two disagree in both directions, so verify an argv by running it against a deliberately-unresolvable target and reading the _exit code_:

```bash
herdr plugin pane open --plugin does.not.exist --entrypoint nope --placement popup
#   exit 2 + "invalid pane placement: banana"        → the CLI rejected the value at parse time
#   exit 1 + {"error":{"code":"plugin_not_found"}}   → the value parsed; the call failed later, on purpose
```

`popup` and `banana` separate cleanly this way (1 vs 2). This is read-only and safe against a live session: nothing resolves, so nothing changes. Prefer it over any help text for the flags in Tasks 15-19.

Probing the Task 15-19 argv this way established three more things:

**The parse layer validates exactly three things:** the placement enum, the split-direction enum (`--direction sideways` → exit 2 `invalid split direction`), and `KEY=VALUE` env shape (`--env JUSTKEY` → exit 2 `env must use KEY=VALUE`). Everything else is checked at resolution time. In particular a stray `--direction` on a `tab` **parses fine** (exit 1) — so the `Placement == "split"` guard in `PluginPaneOpen` is load-bearing against a _server-side_ rejection, not a parse error, and dropping it would produce a failed open rather than a usage message. Repeated `--env` is accepted, and `--focus` with `--no-focus` together parses without a conflict error.

**`--target-pane` is placement-conditional, and this is inferred from the binary's strings rather than probed.** herdr carries `tab plugin panes support workspace_id but not target_pane_id or direction`, `split and zoomed plugin panes target an existing pane; use target_pane_id`, `overlay and popup plugin panes target the active pane`, and `width and height are only supported when placement is popup`. Reaching that check requires a real plugin _and_ a real entrypoint — i.e. actually opening a pane — so it was deliberately not verified behaviorally against the operator's live session. `PluginPaneOpen` filters `--target-pane` to split/zoomed on this basis. The guard is safe under either reading: if those placements tolerate a target pane, omitting it changes nothing since they target the active pane or a workspace anyway. Whoever adds `--width`/`--height` should expect the same treatment (popup only) and should not assume it, since it comes from the same unprobed source.

**`--width`/`--height` are popup-only, and `--help` omits them entirely.** The constraint appears twice in the binary, in two separate validation clusters with distinct error codes: once on the open path (`width and height are only supported when placement is popup`, alongside the target-pane rules above) and once in manifest parsing (`pane width and height are only supported when placement is popup`, under `invalid_plugin_pane_size`). So sizing is rejected both when a manifest declares it on a non-popup entrypoint and when an open request carries it. Corroborated by the installed corpus without probing: across all 13 plugins, every pane entrypoint declaring a size is a popup (`ray.plugin-manager/manager` is `placement=popup, width=82, height=28`), and no split/tab/zoomed/overlay entrypoint declares either. This is firmer evidence than the `--target-pane` inference, which rests on a single open-path string.

`plugin pane open --help` lists ten options and **`--width`/`--height` are not among them**, yet the usage line documents `[--width SIZE] [--height SIZE]` and `--width 80 --height 24` parses (exit 1 `plugin_not_found`, not exit 2 `unknown option`). That is the third help-vs-reality divergence, and all three point the same way: **`--help` is a subset of what the parser accepts, never a superset.** We emit neither flag, so this is recorded for whoever adds sizing — expect a popup-only guard, and don't infer its absence from `--help`.

**Only one popup may be open at a time:** `ui_busy` / `a popup pane is already open`. The plugin-pane entrypoint uses `overlay`, so this is inert on the `plugin pane open` path — but if a later task reconsiders `popup` _there_, that is a hard single-instance limit and a second failure mode to handle on the open path, not just a placement swap.

**Correction, and the most expensive wrong assumption in this plan: `overlay` is not a floating box.** It was written throughout as if it were the floating placement, and it is not — it is a full-pane placement like `split`, `tab` and `zoomed`. Binding the picker to it produced a pane, which is exactly what the operator said it should not be. **The floating box is a keybinding type, not a placement:** in the operator's herdr config, `type = "popup"` with `width`/`height` in cells or percentages. That is a separate mechanism from `plugin pane open` and does not go through `herdrapi` at all.

The two launch modes therefore get **different environments**, which is the other half of the same mistake. A plugin pane gets `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR` and `HERDR_PANE_ID`. A popup gets none of those — measured on 0.9.0, it gets `HERDR_ACTIVE_PANE_CWD`, `HERDR_ACTIVE_PANE_ID`, `HERDR_ACTIVE_TAB_ID`, `HERDR_ACTIVE_WORKSPACE_ID`, `HERDR_BIN_PATH`, `HERDR_ENV` and `HERDR_SOCKET_PATH`. Reading the plugin-pane variables directly meant `theme.LoadFile("")` and `pluginconfig.LoadDir("")`, both of which return defaults for an empty path **without an error**, so in the one mode that actually floats the picker silently drew in the built-in blue and discarded the whole plugin config. Fixed in `cbd1249` (`cmd/herdr-ssh/env.go`): each resolver prefers its variable and falls back to the documented location. Anything added later that reads a `HERDR_*` variable must go through a resolver there, or it will work in a plugin pane and fail silently in the popup.

**`herdr pane rename --clear <id>` does not work — `--clear` is consumed as the pane id.** It must come _after_ the pane id: `pane rename <id> --clear`. Flag-first yields `pane --clear not found`, which reads like a missing pane rather than an argument-order bug. `--help` prints `Usage: herdr pane rename [OPTIONS] <PANE_ID> [LABEL]...` with `--clear` as a declared option, implying flag-first is legal; the runtime parser disagrees. That is the second independent case of the help text being wrong, which is why the exit-code probe above is the rule rather than a suggestion. `PaneRename` does not use `--clear` today; this is recorded so whoever adds label-clearing doesn't rediscover it in the field.

The manifest schema above also matches what herdr accepts: top-level `id`/`name`/`version`/`min_herdr_version`/`platforms`/`description`, `[[build]]` as an array of tables with `command`, `[[actions]]` with `id`/`title`/`contexts`/`command`, `[[panes]]` with `id`/`title`/`placement`/`command`. Note `herdr plugin list --json` reports the id back as `plugin_id`, but the manifest key is `id`.

- [x] **Step 5: Commit**

```bash
git add go.mod go.sum .gitignore herdr-plugin.toml
git commit -m "chore: scaffold go module and plugin manifest" \
  -- go.mod go.sum .gitignore herdr-plugin.toml
```

---

### Task 2: sshconfig — types and line splitting

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/sshconfig_test.go`

- [x] **Step 1: Write the failing test**

```go
package sshconfig

import "testing"

func TestSplitLine(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		key, val string
		ok            bool
	}{
		{"space separated", "HostName example.com", "HostName", "example.com", true},
		{"equals separated", "Port=2222", "Port", "2222", true},
		{"leading whitespace", "    User root", "User", "root", true},
		{"quoted value", `IdentityFile "~/.ssh/id ed"`, "IdentityFile", "~/.ssh/id ed", true},
		{"comment", "# Host nope", "", "", false},
		{"blank", "   ", "", "", false},
		{"keyword only", "Host", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, val, ok := splitLine(tc.in)
			if ok != tc.ok || key != tc.key || val != tc.val {
				t.Fatalf("splitLine(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, key, val, ok, tc.key, tc.val, tc.ok)
			}
		})
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run TestSplitLine -count=1`
Expected: FAIL — `undefined: splitLine`

- [x] **Step 3: Write the types and `splitLine`**

```go
// Package sshconfig parses OpenSSH client configuration into connectable hosts.
package sshconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ErrNoConfig reports a config file that does not exist. Callers treat this as
// "nothing to pick" rather than a failure.
var ErrNoConfig = errors.New("ssh config not found")

// Host is one connectable target with its keywords already resolved.
type Host struct {
	Alias        string
	HostName     string
	User         string
	Port         string
	IdentityFile string
	ProxyJump    string
	SourceFile   string
	SourceLine   int
}

// Warning is a non-fatal parse problem, surfaced in the picker footer.
type Warning struct {
	File string
	Line int
	Msg  string
}

func (w Warning) String() string { return fmt.Sprintf("%s:%d: %s", w.File, w.Line, w.Msg) }

type kv struct{ key, value string }

// block is one `Host <patterns>` stanza and the keywords under it.
type block struct {
	positive []string
	negative []string
	keys     []kv
	file     string
	line     int
}

func splitLine(raw string) (string, string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	i := strings.IndexAny(line, " \t=")
	if i <= 0 {
		return "", "", false
	}
	key := line[:i]
	value := strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
	value = strings.Trim(value, `"`)
	if value == "" {
		return "", "", false
	}
	return key, value, true
}

func expandTilde(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshconfig/ -run TestSplitLine -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): add host types and config line splitting" \
  -- internal/sshconfig/
```

---

### Task 3: sshconfig — pattern matching

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Modify: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/sshconfig_test.go`

- [x] **Step 1: Write the failing test**

```go
func TestBlockMatches(t *testing.T) {
	b := block{positive: []string{"*.internal", "nixos-dev"}, negative: []string{"secret.internal"}}
	cases := map[string]bool{
		"nixos-dev":       true,
		"box.internal":    true,
		"secret.internal": false,
		"unrelated":       false,
	}
	for alias, want := range cases {
		if got := b.matches(alias); got != want {
			t.Errorf("matches(%q) = %v, want %v", alias, got, want)
		}
	}
}

func TestIsPattern(t *testing.T) {
	for _, p := range []string{"*", "*.dev", "web?", "a*b", "?"} {
		if !isPattern(p) {
			t.Errorf("isPattern(%q) = false, want true", p)
		}
	}
	// Only `*` and `?` are ssh_config wildcards. Brackets have no character-class
	// meaning, and a mid-string `!` is literal — `!` negates only as the leading
	// character of a pattern-list entry, which newHostBlock strips into
	// block.negative before anything reaches isPattern. These are all connectable
	// host aliases, not wildcards.
	for _, p := range []string{"nixos-dev", "10.0.0.1", "build_box", "[ab]host", "web[12]", "foo!bar"} {
		if isPattern(p) {
			t.Errorf("isPattern(%q) = true, want false", p)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, alias string
		want           bool
	}{
		// Exact literal match.
		{"web1", "web1", true},
		{"web1", "web2", false},
		// '*' matches zero or more of any character.
		{"*", "anything", true},
		{"*", "", true},
		{"*.internal", "box.internal", true},
		{"*.internal", "internal", false},
		{"web*", "web1", true},
		{"web*", "web", true},
		{"web*", "xweb1", false},
		{"*web*", "xwebx", true},
		{"a*b*c", "aXbXXc", true},
		{"a*b*c", "abc", true},
		{"a*b*c", "ac", false},
		// '?' matches exactly one character.
		{"web?", "web1", true},
		{"web?", "web", false},
		{"web?", "web12", false},
		{"?ost", "host", true},
		// '[', ']', '\' are literal in ssh_config PATTERNS, unlike filepath.Match.
		{"web[12]", "web1", false},
		{"web[12]", "web[12]", true},
		{`a\b`, `a\b`, true},
		{`a\b`, "ab", false},
	}
	for _, tc := range cases {
		if got := matchPattern(tc.pattern, tc.alias); got != tc.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tc.pattern, tc.alias, got, tc.want)
		}
	}
}

func TestBlockDeclares(t *testing.T) {
	b := block{positive: []string{"*.internal", "nixos-dev"}}
	if !b.declares("nixos-dev") {
		t.Error("declares(nixos-dev) = false, want true")
	}
	if b.declares("box.internal") {
		t.Error("declares(box.internal) = true, want false — only exact patterns declare")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run 'TestBlock|TestIsPattern' -count=1`
Expected: FAIL — `b.matches undefined`

- [x] **Step 3: Add the matching methods**

```go
// matchPattern implements ssh_config(5) PATTERNS: `*` matches zero or more
// characters, `?` matches exactly one, and nothing else is special. `[`, `]` and
// `\` are literal, which is why filepath.Match cannot be used — it would both
// over-match (`web[12]` against `web1`) and fail to match a bracketed alias
// against itself.
func matchPattern(pattern, alias string) bool {
	// Greedy backtracking match, byte-wise: track the most recent '*' so a
	// failed literal/'?' match can retry by consuming one more alias byte
	// under that star instead of failing outright.
	var pi, si int
	starAt, starSi := -1, 0
	for si < len(alias) {
		switch {
		case pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == alias[si]):
			pi++
			si++
		case pi < len(pattern) && pattern[pi] == '*':
			starAt, starSi = pi, si
			pi++
		case starAt != -1:
			pi = starAt + 1
			starSi++
			si = starSi
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// isPattern reports whether a positive Host entry is a wildcard rather than a
// selectable alias. Only `*` and `?` qualify. `!` is deliberately absent: it
// negates a pattern-list entry only as a leading character, which newHostBlock
// has already stripped into block.negative, so testing for it here would only
// misfire on a literal alias containing a mid-string `!`.
func isPattern(s string) bool { return strings.ContainsAny(s, "*?") }

// matches reports whether this stanza applies to alias. A negated pattern wins
// over any positive match, mirroring ssh_config semantics.
func (b block) matches(alias string) bool {
	for _, n := range b.negative {
		if matchPattern(n, alias) {
			return false
		}
	}
	for _, p := range b.positive {
		if matchPattern(p, alias) {
			return true
		}
	}
	return false
}

// declares reports whether alias is named literally, which is what makes it a
// selectable target and fixes its source location.
func (b block) declares(alias string) bool {
	for _, p := range b.positive {
		if p == alias {
			return true
		}
	}
	return false
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshconfig/ -run 'TestBlock|TestIsPattern' -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): add host pattern matching with negation" \
  -- internal/sshconfig/
```

---

### Task 4: sshconfig — parse and resolve a single file

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Modify: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/parse_test.go`
- Create: `internal/sshconfig/testdata/basic`, `testdata/minimal`, `testdata/wildcard-first`, `testdata/malformed`

- [x] **Step 1: Write the fixture**

`internal/sshconfig/testdata/basic`:

```
# global defaults
ServerAliveInterval 30

Host nixos-dev
  HostName 192.0.2.10
  User operator
  Port 22
  IdentityFile ~/.ssh/id_nixos

Host build box
  HostName 10.0.0.12
  User root

Host web1
  HostName 10.0.0.20
  User root
  User ignored-second-value

Host jumped
  HostName 10.9.9.9
  ProxyJump bastion

Match exec "true"
  User never-applied

Host *.internal
  Port 2222

# general defaults last, per ssh_config(5)
Host *
  User fallback
  IdentityFile ~/.ssh/id_default
```

`Host *` must come **last**. `ssh_config(5)` takes the first obtained value for each
keyword and does **not** rank literal stanzas above patterns, so a wildcard placed first
would legitimately claim `User` for every alias. Bottom-of-file is both the real-world
convention and what the assertions below expect.

`internal/sshconfig/testdata/wildcard-first`:

```
Host *
  User fallback
  Port 2200

Host late
  HostName 10.3.3.3
  User ignored-because-wildcard-came-first
```

Also create `internal/sshconfig/testdata/malformed` — line 4 has no separator and line 5
has an empty value, so both must warn without costing us `good`:

```
Host good
  HostName 10.5.5.5

thisisnotavalidline
User
```

- [x] **Step 2: Write the failing test**

```go
package sshconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func hostByAlias(t *testing.T, hosts []Host, alias string) Host {
	t.Helper()
	for _, h := range hosts {
		if h.Alias == alias {
			return h
		}
	}
	t.Fatalf("alias %q not found in %d hosts", alias, len(hosts))
	return Host{}
}

func TestParseBasic(t *testing.T) {
	hosts, warns, err := Parse(filepath.Join("testdata", "basic"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}

	var aliases []string
	for _, h := range hosts {
		aliases = append(aliases, h.Alias)
	}
	want := []string{"nixos-dev", "build", "box", "web1", "jumped"}
	if len(aliases) != len(want) {
		t.Fatalf("aliases = %v, want %v", aliases, want)
	}
	for i := range want {
		if aliases[i] != want[i] {
			t.Fatalf("aliases = %v, want %v", aliases, want)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	dev := hostByAlias(t, hosts, "nixos-dev")
	if dev.HostName != "192.0.2.10" || dev.User != "operator" || dev.Port != "22" {
		t.Errorf("nixos-dev = %+v", dev)
	}
	if dev.SourceLine == 0 || dev.SourceFile == "" {
		t.Errorf("nixos-dev missing provenance: %+v", dev)
	}
	// nixos-dev sets its own IdentityFile, which must win over `Host *`'s, and
	// the leading ~/ must be expanded against the real home directory.
	if want := filepath.Join(home, ".ssh", "id_nixos"); dev.IdentityFile != want {
		t.Errorf("nixos-dev IdentityFile = %q, want %q", dev.IdentityFile, want)
	}

	// `Host build box` is two aliases sharing one stanza.
	if got := hostByAlias(t, hosts, "box").HostName; got != "10.0.0.12" {
		t.Errorf("box HostName = %q, want 10.0.0.12", got)
	}

	// First value wins within a stanza. Against `Host *` the winner is decided
	// by file order, and this fixture keeps the wildcard last, as ssh expects.
	if got := hostByAlias(t, hosts, "web1").User; got != "root" {
		t.Errorf("web1 User = %q, want root (first value wins)", got)
	}

	// jumped declares no User of its own, and the trailing `Match exec` stanza
	// must be skipped rather than contributing "never-applied" — only the
	// trailing `Host *` default should reach it.
	jumped := hostByAlias(t, hosts, "jumped")
	if jumped.User != "fallback" {
		t.Errorf("jumped User = %q, want fallback — Match keywords must not apply", jumped.User)
	}
	// `Host *` supplies defaults but is not selectable, and its ~/ must expand
	// against the real home directory too.
	if want := filepath.Join(home, ".ssh", "id_default"); jumped.IdentityFile != want {
		t.Errorf("jumped IdentityFile = %q, want %q (the Host * default, expanded)", jumped.IdentityFile, want)
	}
	if jumped.ProxyJump != "bastion" {
		t.Errorf("jumped ProxyJump = %q, want bastion", jumped.ProxyJump)
	}

	// Unset HostName falls back to the alias; unset Port defaults to 22.
	hosts2, _, err := Parse(filepath.Join("testdata", "minimal"))
	if err != nil {
		t.Fatalf("Parse minimal: %v", err)
	}
	only := hosts2[0]
	if only.HostName != "solo" || only.Port != "22" {
		t.Errorf("minimal host = %+v, want HostName=solo Port=22", only)
	}
}

func TestParseMissingFile(t *testing.T) {
	_, _, err := Parse(filepath.Join("testdata", "does-not-exist"))
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("err = %v, want ErrNoConfig", err)
	}
}

// A wildcard that precedes a literal stanza wins, because ssh takes the first
// obtained value and does not rank literal stanzas above patterns. Operators
// who put `Host *` first really do get the wildcard's values, and the picker
// must show what ssh will actually do.
func TestParseWildcardBeforeLiteralWins(t *testing.T) {
	hosts, _, err := Parse(filepath.Join("testdata", "wildcard-first"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	h := hostByAlias(t, hosts, "late")
	if h.User != "fallback" {
		t.Errorf("late User = %q, want fallback — earlier `Host *` must win", h.User)
	}
	if h.Port != "2200" {
		t.Errorf("late Port = %q, want 2200 — earlier `Host *` must win", h.Port)
	}
	if h.HostName != "10.3.3.3" {
		t.Errorf("late HostName = %q, want 10.3.3.3 — wildcard sets no HostName", h.HostName)
	}
	if h.SourceLine == 0 {
		t.Error("late lost its provenance; the literal stanza still declares it")
	}
}

// A literal alias containing brackets must remain connectable: ssh_config
// PATTERNS has no bracket character classes, so "web[12]" is not a wildcard
// and must not be filtered out of the picker as one.
func TestParseBracketAliasIsLiteral(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "root"), "Host web[12]\n  HostName 10.4.4.4\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h := hostByAlias(t, hosts, "web[12]")
	if h.HostName != "10.4.4.4" {
		t.Errorf("web[12] HostName = %q, want 10.4.4.4", h.HostName)
	}
}

// A mid-string `!` is literal, so the alias stays connectable, while a leading
// `!` still negates. Verified against OpenSSH_10.3p1: `ssh -G foo!bar` reports
// user=banguser, and `!nodefault` keeps the trailing `Host *` User away from
// nodefault while `other` receives it.
func TestParseBangAliasIsLiteralAndNegationStillApplies(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "root"),
		"Host foo!bar\n  HostName 10.9.9.9\n  User banguser\n\n"+
			"Host nodefault\n  HostName 10.9.9.8\n\n"+
			"Host * !nodefault\n  User fallback\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	bang := hostByAlias(t, hosts, "foo!bar")
	if bang.HostName != "10.9.9.9" || bang.User != "banguser" {
		t.Errorf("foo!bar = %+v, want HostName=10.9.9.9 User=banguser", bang)
	}

	// The negation keeps `Host *`'s User away from nodefault without removing
	// nodefault from the picker.
	nd := hostByAlias(t, hosts, "nodefault")
	if nd.User != "" {
		t.Errorf("nodefault User = %q, want empty — !nodefault must exclude it", nd.User)
	}

	if len(hosts) != 2 {
		t.Errorf("hosts = %+v, want exactly foo!bar and nodefault", hosts)
	}
}

// A malformed line (no key/value separator, or an empty value) is a warning,
// not silently dropped — the operator should know a line in their config
// didn't parse instead of quietly losing it.
func TestParseWarnsOnMalformedLines(t *testing.T) {
	hosts, warns, err := Parse(filepath.Join("testdata", "malformed"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := hostByAlias(t, hosts, "good").HostName; got != "10.5.5.5" {
		t.Errorf("good HostName = %q, want 10.5.5.5 — malformed lines must not break the rest of the file", got)
	}

	if len(warns) != 2 {
		t.Fatalf("warnings = %v, want exactly 2", warns)
	}
	wantLines := map[int]bool{4: false, 5: false}
	for _, w := range warns {
		if _, ok := wantLines[w.Line]; !ok {
			t.Errorf("unexpected warning line %d: %+v", w.Line, w)
			continue
		}
		wantLines[w.Line] = true
	}
	for line, seen := range wantLines {
		if !seen {
			t.Errorf("missing warning for malformed line %d", line)
		}
	}
}
```

`TestParseBracketAliasIsLiteral` uses the `write` helper. Define it once in the package —
the shipped tree keeps it in `include_test.go` (Task 5), which is fine because Go test
helpers are package-scoped:

```go
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
```

Also create `internal/sshconfig/testdata/minimal`:

```
Host solo
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run TestParse -count=1`
Expected: FAIL — `undefined: Parse`

- [x] **Step 4: Write `Parse`, `parseFile`, `newHostBlock`, `resolve`, and `resolveHost`**

```go
// Parse reads root and returns its connectable hosts in declaration order.
// Non-fatal problems come back as warnings; only an unreadable root is an error.
func Parse(root string) ([]Host, []Warning, error) {
	return parse(root, expandTilde("~/.ssh"))
}

// parse takes the include base explicitly so tests can root a config tree in a
// temp dir. ssh_config(5) resolves a relative Include against ~/.ssh for a user
// config at every nesting depth — the base never follows the including file.
func parse(root, includeBase string) ([]Host, []Warning, error) {
	visited := map[string]bool{}
	blocks, warns, err := parseFile(root, includeBase, block{positive: []string{"*"}}, visited, nil)
	if err != nil {
		return nil, warns, err
	}
	return resolve(blocks), warns, nil
}

// parseFile parses one config file. enclosing supplies the patterns that govern
// keywords appearing before this file's first Host stanza: for the root config
// that is an implicit `Host *`, but for an included file it is the stanza the
// Include sat inside, because ssh processes an Include with the caller's active
// block still in effect.
func parseFile(path, includeBase string, enclosing block, visited map[string]bool, warns []Warning) ([]block, []Warning, error) {
	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		return nil, warns, err
	}
	if visited[abs] {
		return nil, warns, nil
	}
	visited[abs] = true

	raw, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, warns, fmt.Errorf("%w: %s", ErrNoConfig, abs)
		}
		return nil, warns, err
	}

	var blocks []block
	// Inherit the enclosing stanza's patterns, not its keywords.
	cur := block{positive: enclosing.positive, negative: enclosing.negative, file: abs}

	for i, line := range strings.Split(string(raw), "\n") {
		lineNo := i + 1
		key, value, ok := splitLine(line)
		if !ok {
			// Spec: skip a malformed line, never abort, but surface it. Blanks and
			// comments are not malformed.
			if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				warns = append(warns, Warning{File: abs, Line: lineNo, Msg: "malformed line: " + truncate(trimmed, 60)})
			}
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			blocks = append(blocks, cur)
			cur = newHostBlock(value, abs, lineNo)
		case "match":
			// A Match stanza has no static host to offer, and `Match exec` would
			// mean running commands to build a picker list. Skip its keywords by
			// starting a stanza that matches nothing.
			blocks = append(blocks, cur)
			cur = block{file: abs, line: lineNo}
		case "include":
			blocks = append(blocks, cur)
			var included []block
			included, warns = parseIncludes(value, abs, includeBase, cur, lineNo, visited, warns)
			blocks = append(blocks, included...)
			resumed := cur
			resumed.keys = nil
			cur = resumed
		default:
			cur.keys = append(cur.keys, kv{strings.ToLower(key), value})
		}
	}
	return append(blocks, cur), warns, nil
}

func newHostBlock(value, file string, line int) block {
	b := block{file: file, line: line}
	for _, field := range strings.Fields(value) {
		if strings.HasPrefix(field, "!") {
			b.negative = append(b.negative, strings.TrimPrefix(field, "!"))
			continue
		}
		b.positive = append(b.positive, field)
	}
	return b
}

func resolve(blocks []block) []Host {
	var order []string
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, p := range b.positive {
			if isPattern(p) || seen[p] {
				continue
			}
			seen[p] = true
			order = append(order, p)
		}
	}

	hosts := make([]Host, 0, len(order))
	for _, alias := range order {
		hosts = append(hosts, resolveHost(alias, blocks))
	}
	return hosts
}

func resolveHost(alias string, blocks []block) Host {
	h := Host{Alias: alias}
	values := map[string]string{}

	// One ordered walk: ssh uses the first obtained value for each keyword and
	// does not rank literal stanzas above patterns. Ordering is the operator's
	// job, which is why `Host *` belongs at the end of a config.
	for _, b := range blocks {
		if !b.matches(alias) {
			continue
		}
		if h.SourceFile == "" && b.declares(alias) {
			h.SourceFile, h.SourceLine = b.file, b.line
		}
		for _, pair := range b.keys {
			if _, exists := values[pair.key]; !exists {
				values[pair.key] = pair.value
			}
		}
	}

	h.HostName = firstNonEmpty(values["hostname"], alias)
	h.User = values["user"]
	h.Port = firstNonEmpty(values["port"], "22")
	h.IdentityFile = expandTilde(values["identityfile"])
	h.ProxyJump = values["proxyjump"]
	return h
}
```

`resolveHost` is split out so `resolve` stays a short ordering pass; it also gives the
precedence rule one obvious home. Do **not** reintroduce a second pass that ranks
`declares` above `matches` — that would contradict `ssh_config(5)` and make the preview
disagree with what `ssh <alias>` actually does. `TestParseWildcardBeforeLiteralWins`
guards this.

- [x] **Step 5: Add a stub `parseIncludes` so the package compiles**

```go
func parseIncludes(value, parent, includeBase string, enclosing block, line int, visited map[string]bool, warns []Warning) ([]block, []Warning) {
	return nil, warns
}

// truncate shortens s for warning messages so one very long malformed line
// cannot blow up the picker footer.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Back off to a rune boundary. Cutting at a fixed byte count can sever a
	// multibyte character and leave invalid UTF-8 in the footer.
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
```

Append its test to `internal/sshconfig/sshconfig_test.go`, whose import block widens to
`"strings"`, `"testing"`, `"unicode/utf8"`:

```go
func TestTruncate(t *testing.T) {
	if got := truncate("short", 60); got != "short" {
		t.Errorf("truncate(%q, 60) = %q, want it unchanged", "short", got)
	}
	if got := truncate("abcdef", 3); got != "abc…" {
		t.Errorf("truncate(%q, 3) = %q, want abc…", "abcdef", got)
	}
	// A 3-byte rune straddling the limit must not be severed into invalid UTF-8.
	long := strings.Repeat("a", 58) + "日本語"
	got := truncate(long, 60)
	if !utf8.ValidString(got) {
		t.Errorf("truncate returned invalid UTF-8: %q", got)
	}
	if want := strings.Repeat("a", 58) + "…"; got != want {
		t.Errorf("truncate = %q, want %q", got, want)
	}
}
```

- [x] **Step 6: Run test to verify it passes**

Run: `go test ./internal/sshconfig/ -run TestParse -v -count=1`
Expected: PASS for `TestParseBasic` and `TestParseMissingFile`

- [x] **Step 7: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): parse and resolve hosts with ssh precedence rules" \
  -- internal/sshconfig/
```

---

### Task 5: sshconfig — Include expansion

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Modify: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/include_test.go`
- Create: `internal/sshconfig/testdata/with-include`, `testdata/included/extra`, `testdata/cyclic-a`, `testdata/cyclic-b`

- [x] **Step 1: Write the fixtures**

`internal/sshconfig/testdata/with-include`:

```
Host local-one
  HostName 10.1.1.1

Include included/*

Include ./missing/nothing-here

Host local-two
  HostName 10.1.1.2
```

`internal/sshconfig/testdata/included/extra`:

```
Host from-include
  HostName 10.2.2.2
  User included-user
```

`internal/sshconfig/testdata/cyclic-a`:

```
Host cyc-a
Include cyclic-b
```

`internal/sshconfig/testdata/cyclic-b`:

```
Host cyc-b
Include cyclic-a
```

- [x] **Step 2: Write the failing test**

These tests call the unexported `parse(root, includeBase)` rather than `Parse`, because
a relative `Include` always resolves against `~/.ssh` and the fixtures live in `testdata`.
`TestParseUsesSSHDirAsIncludeBase` is the one test that goes through `Parse` — it exists
precisely to pin that base.

```go
package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestParseIncludeExpansion(t *testing.T) {
	hosts, warns, err := parse(filepath.Join("testdata", "with-include"), "testdata")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	inc := hostByAlias(t, hosts, "from-include")
	if inc.HostName != "10.2.2.2" || inc.User != "included-user" {
		t.Errorf("from-include = %+v", inc)
	}
	if !strings.Contains(inc.SourceFile, filepath.Join("included", "extra")) {
		t.Errorf("SourceFile = %q, want the included file", inc.SourceFile)
	}

	// Hosts on both sides of the Include survive.
	hostByAlias(t, hosts, "local-one")
	hostByAlias(t, hosts, "local-two")

	// The unreadable include is a warning, not a failure.
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warns)
	}
	if !strings.Contains(warns[0].Msg, "include unreadable") {
		t.Errorf("warning = %q", warns[0].Msg)
	}
}

func TestParseIncludeCycle(t *testing.T) {
	hosts, _, err := parse(filepath.Join("testdata", "cyclic-a"), "testdata")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	hostByAlias(t, hosts, "cyc-a")
	hostByAlias(t, hosts, "cyc-b")
}

// ssh processes an Include with the enclosing stanza still active, so an
// included file's leading keywords must not become global defaults.
func TestParseIncludeInsideHostDoesNotLeakGlobally(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "inc.conf"), "User workuser\nPort 2022\n")
	write(t, filepath.Join(dir, "root"), "Host work1\n  HostName 10.0.0.1\n  Include inc.conf\n\nHost personal\n  HostName 10.0.0.5\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	work := hostByAlias(t, hosts, "work1")
	if work.User != "workuser" || work.Port != "2022" {
		t.Errorf("work1 = %+v, want the include applied to the enclosing stanza", work)
	}
	personal := hostByAlias(t, hosts, "personal")
	if personal.User != "" {
		t.Errorf("personal User = %q, want empty — the include was scoped to work1", personal.User)
	}
	if personal.Port != "22" {
		t.Errorf("personal Port = %q, want 22 — the include was scoped to work1", personal.Port)
	}
}

// The enclosing stanza's negations cross into the included file along with its
// positive patterns. Verified against OpenSSH_10.3p1: given `Host * !nope`
// wrapping an Include, `ssh -G yes` reports the included User and `ssh -G nope`
// does not.
func TestParseIncludeInheritsNegatedPatterns(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "inc"), "User inherited\n")
	write(t, filepath.Join(dir, "root"), "Host * !nope\n  Include inc\n\nHost nope\n  HostName 10.0.0.9\n\nHost yes\n  HostName 10.0.0.8\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
	if got := hostByAlias(t, hosts, "nope").User; got != "" {
		t.Errorf("nope User = %q, want empty — !nope must survive the Include", got)
	}
	if got := hostByAlias(t, hosts, "yes").User; got != "inherited" {
		t.Errorf("yes User = %q, want inherited", got)
	}
}

// The include base is fixed, so a nested relative Include resolves beside the
// base and not beside the file that included it.
func TestParseIncludeBaseIsFixedAcrossDepth(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "d"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write(t, filepath.Join(dir, "d", "work"), "Include shared\n")
	write(t, filepath.Join(dir, "shared"), "Host shared-host\n  HostName 10.8.8.8\n")
	write(t, filepath.Join(dir, "root"), "Include d/work\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
	if got := hostByAlias(t, hosts, "shared-host").HostName; got != "10.8.8.8" {
		t.Errorf("shared-host HostName = %q, want 10.8.8.8", got)
	}
}

// Parse itself must use ~/.ssh as the base, not the config's own directory.
func TestParseUsesSSHDirAsIncludeBase(t *testing.T) {
	dir := t.TempDir()
	const sibling = "herdr-ssh-test-sibling-absent"
	write(t, filepath.Join(dir, sibling), "Host sibling-host\n")
	write(t, filepath.Join(dir, "root"), "Include "+sibling+"\n")

	hosts, warns, err := Parse(filepath.Join(dir, "root"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, h := range hosts {
		if h.Alias == "sibling-host" {
			t.Fatal("Parse resolved a relative include against the config's own directory")
		}
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Msg, "include unreadable") {
		t.Errorf("warnings = %v, want one include-unreadable", warns)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run TestParseInclude -count=1`
Expected: FAIL — `alias "from-include" not found in 2 hosts` (the stub returns nothing)

- [x] **Step 4: Replace the stub with the real implementation**

```go
// parseIncludes expands one Include directive. Relative patterns resolve
// against includeBase, which is fixed at ~/.ssh for the primary config at
// every nesting depth per ssh_config(5) — never the including file's own
// directory. enclosing carries the caller's active stanza into the included
// file, since ssh processes an Include with that stanza still in effect. An
// unreadable or unmatched pattern is a warning: one bad include must not cost
// the operator the rest of their hosts.
func parseIncludes(value, parent, includeBase string, enclosing block, line int, visited map[string]bool, warns []Warning) ([]block, []Warning) {
	var out []block
	for _, rawPattern := range strings.Fields(value) {
		pattern := expandTilde(rawPattern)
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(includeBase, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			warns = append(warns, Warning{File: parent, Line: line, Msg: "include unreadable: " + rawPattern})
			continue
		}
		for _, match := range matches {
			blocks, updated, err := parseFile(match, includeBase, enclosing, visited, warns)
			warns = updated
			if err != nil {
				warns = append(warns, Warning{File: parent, Line: line, Msg: "include unreadable: " + match})
				continue
			}
			out = append(out, blocks...)
		}
	}
	return out, warns
}
```

`filepath.Glob` is correct **here** and must not be swapped for `matchPattern`. An
`Include` argument is a real filesystem glob, so bracket classes apply; a `Host` pattern
is `ssh_config(5)` PATTERNS, where `[` is literal. The two are different languages that
happen to share `*`.

- [x] **Step 5: Run the whole package**

Run: `go test ./internal/sshconfig/ -v -count=1`
Expected: PASS — all tests including the earlier ones

- [x] **Step 6: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): expand Include directives with cycle and error guards" \
  -- internal/sshconfig/
```

---

### Task 6: sshconfig — Exclude hidden hosts

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Modify: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/exclude_test.go`

- [x] **Step 1: Write the failing test**

```go
package sshconfig

import "testing"

func TestExclude(t *testing.T) {
	hosts := []Host{{Alias: "nixos-dev"}, {Alias: "colima"}, {Alias: "web-old"}, {Alias: "web1"}}

	kept := Exclude(hosts, []string{"colima", "*-old"})
	if len(kept) != 2 || kept[0].Alias != "nixos-dev" || kept[1].Alias != "web1" {
		t.Fatalf("kept = %+v, want nixos-dev and web1", kept)
	}
	if len(hosts) != 4 {
		t.Error("Exclude mutated its input")
	}

	if got := Exclude(hosts, nil); len(got) != 4 {
		t.Errorf("Exclude with no globs dropped hosts: %d", len(got))
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run TestExclude -count=1`
Expected: FAIL — `undefined: Exclude`

- [x] **Step 3: Implement `Exclude`**

```go
// Exclude returns a new slice without hosts whose alias matches any glob.
func Exclude(hosts []Host, globs []string) []Host {
	if len(globs) == 0 {
		return hosts
	}
	kept := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		hidden := false
		for _, g := range globs {
			if matchPattern(g, h.Alias) {
				hidden = true
				break
			}
		}
		if !hidden {
			kept = append(kept, h)
		}
	}
	return kept
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshconfig/ -run TestExclude -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): add Exclude for hidden host globs" \
  -- internal/sshconfig/
```

---

### Task 7: theme

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `internal/theme/theme.go`
- Test: `internal/theme/theme_test.go`

- [x] **Step 1: Write the failing test**

```go
package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadMissingFileYieldsDefault(t *testing.T) {
	if got := Load(filepath.Join(t.TempDir(), "absent.toml")); got != Default() {
		t.Fatalf("Load = %+v, want %+v", got, Default())
	}
}

func TestLoadAccentOverride(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"terminal\"\n\n[ui]\naccent = \"#14e21a\"\n")
	if got := Load(path).Accent; got != "#14e21a" {
		t.Fatalf("Accent = %q, want #14e21a", got)
	}
}

func TestLoadThemeNameAccent(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"tokyonight\"\n")
	if got := Load(path).Accent; got != "#7aa2f7" {
		t.Fatalf("Accent = %q, want the tokyonight accent", got)
	}
}

// The spec maps `terminal` to ANSI 16, so its tokens must be palette indices and
// its Text must stay empty (inherit the terminal foreground). A hex accent here
// is the exact regression this pins.
func TestLoadTerminalThemeUsesANSI(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"terminal\"\n")
	got := Load(path)
	if got.Accent != "4" || got.Muted != "8" || got.Up != "2" {
		t.Errorf("terminal theme = %+v, want ANSI indices 4/8/2", got)
	}
	if got.Text != "" {
		t.Errorf("Text = %q, want empty so the terminal's own foreground shows through", got.Text)
	}
	if strings.HasPrefix(got.Accent, "#") {
		t.Errorf("Accent = %q, want an ANSI index, not a hardcoded hex", got.Accent)
	}
}

// This machine's real configuration: theme `terminal` plus a bright green
// accent override. The override must win while the ANSI text token survives.
func TestLoadTerminalThemeWithAccentOverride(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"terminal\"\n\n[ui]\naccent = \"#14e21a\"\n")
	got := Load(path)
	if got.Accent != "#14e21a" {
		t.Errorf("Accent = %q, want the #14e21a override", got.Accent)
	}
	if got.Text != "" {
		t.Errorf("Text = %q, want empty — the accent override must not restore hex text", got.Text)
	}
}

// The terminal branch must not swallow the unknown-name path.
func TestLoadUnknownThemeNameYieldsDefault(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"no-such-theme\"\n")
	if got := Load(path); got != Default() {
		t.Errorf("Load = %+v, want %+v", got, Default())
	}
}

func TestLoadIgnoresBadAccentAndBadTOML(t *testing.T) {
	bad := writeConfig(t, "[ui]\naccent = \"not-a-color\"\n")
	if got := Load(bad).Accent; got != Default().Accent {
		t.Errorf("Accent = %q, want the default for an invalid hex value", got)
	}
	broken := writeConfig(t, "[theme\nname =")
	if got := Load(broken); got != Default() {
		t.Errorf("Load on malformed TOML = %+v, want default", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/theme/ -count=1`
Expected: FAIL — `undefined: Load`

- [x] **Step 3: Implement the package**

```go
// Package theme reads Herdr's active theme so the picker matches the rest of
// the workspace.
package theme

import (
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

// Theme holds the only tokens the picker draws with.
type Theme struct {
	Accent string
	Text   string
	Muted  string
	Up     string
	Down   string
}

const (
	defaultAccent = "#89b4fa"
	defaultText   = "#edf1f3"
	defaultMuted  = "#7b8496"
	defaultUp     = "#2ecc71"
)

// ANSI 16 indices for the `terminal` theme. Verified against lipgloss v2.0.6:
// Color("4") emits "\x1b[34m" and Color("8") emits "\x1b[90m", so these resolve
// against the terminal's own palette instead of emitting truecolor.
const (
	ansiBlue        = "4"
	ansiGreen       = "2"
	ansiBrightBlack = "8"
)

// accentByTheme covers the hex themes Herdr ships. An unknown name keeps the
// default, which is always readable. `terminal` is deliberately absent: the spec
// maps it to ANSI 16, which terminalTheme supplies — a hardcoded hex here would
// override the operator's palette with a dark-background guess.
var accentByTheme = map[string]string{
	"catppuccin-mocha": "#89b4fa",
	"catppuccin-latte": "#1e66f5",
	"tokyonight":       "#7aa2f7",
	"dracula":          "#bd93f9",
	"nord":             "#88c0d0",
	"gruvbox":          "#d79921",
	"solarized":        "#268bd2",
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type herdrConfig struct {
	Theme struct {
		Name string `toml:"name"`
	} `toml:"theme"`
	UI struct {
		Accent string `toml:"accent"`
	} `toml:"ui"`
}

// Default is the theme used when Herdr's config is absent or unreadable.
func Default() Theme {
	return Theme{Accent: defaultAccent, Text: defaultText, Muted: defaultMuted, Up: defaultUp, Down: defaultMuted}
}

// terminalTheme is herdr's `terminal` theme: ANSI 16 indices rather than hex, so
// the picker inherits whatever palette the terminal is configured with instead of
// imposing a dark-background guess. An empty Text emits no escape at all under
// lipgloss, which leaves the terminal's own foreground in place — correct on a
// light terminal as well as a dark one.
func terminalTheme() Theme {
	return Theme{Accent: ansiBlue, Text: "", Muted: ansiBrightBlack, Up: ansiGreen, Down: ansiBrightBlack}
}

// Load never fails. A picker that refuses to open because of a color lookup is
// worse than a picker with the wrong accent.
func Load(configPath string) Theme {
	t := Default()
	if configPath == "" {
		return t
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return t
	}
	var cfg herdrConfig
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return t
	}
	// Pick the base token set first, then let [ui].accent override it — that
	// ordering is the "with the accent override applied" half of the spec's
	// terminal-theme sentence.
	if cfg.Theme.Name == "terminal" {
		t = terminalTheme()
	} else if accent, ok := accentByTheme[cfg.Theme.Name]; ok {
		t.Accent = accent
	}
	if hexColor.MatchString(cfg.UI.Accent) {
		t.Accent = cfg.UI.Accent
	}
	return t
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/theme/ -v -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/theme/
git commit -m "feat(theme): resolve picker colors from herdr theme and accent" \
  -- internal/theme/
```

---

### Task 8: pluginconfig

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `internal/pluginconfig/pluginconfig.go`
- Test: `internal/pluginconfig/pluginconfig_test.go`

- [x] **Step 1: Write the failing test**

```go
package pluginconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg, Defaults()) {
		t.Fatalf("cfg = %+v, want %+v", cfg, Defaults())
	}
}

func TestLoadEmptyDirYieldsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil || !reflect.DeepEqual(cfg, Defaults()) {
		t.Fatalf("Load(\"\") = (%+v, %v)", cfg, err)
	}
}

func TestLoadPartialOverride(t *testing.T) {
	dir := writeConfig(t, "probe = false\nsplit_direction = \"down\"\nhidden = [\"colima\"]\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want false")
	}
	if cfg.SplitDirection != "down" {
		t.Errorf("SplitDirection = %q, want down", cfg.SplitDirection)
	}
	if len(cfg.Hidden) != 1 || cfg.Hidden[0] != "colima" {
		t.Errorf("Hidden = %v", cfg.Hidden)
	}
	// Untouched keys keep their defaults.
	if cfg.ProbeTimeoutMS != 300 || !cfg.ReusePanes || !cfg.ShowPreview {
		t.Errorf("defaults not preserved: %+v", cfg)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	dir := writeConfig(t, "split_direction = \"sideways\"\n")
	cfg, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	// The rejected key falls back to its default so the returned Config is usable.
	if cfg.SplitDirection != "right" {
		t.Errorf("SplitDirection = %q, want the right default after rejection", cfg.SplitDirection)
	}
	broken := writeConfig(t, "probe = yes-please\n")
	if _, err := Load(broken); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid for malformed TOML", err)
	}
}

// One bad key must not cost the operator the keys they got right. `probe = false`
// is the case that matters: discarding the whole Config for Defaults() turns
// probing back ON and fires a SYN per host — exactly the traffic this file asked
// us not to send. Both offenders must also be reported and reset together, so a
// second bad key can't ride out of Load still holding its invalid value.
func TestLoadKeepsValidKeysWhenAnotherIsInvalid(t *testing.T) {
	dir := writeConfig(t, "probe = false\nhidden = [\"colima\"]\nprobe_timeout_ms = 0\nsplit_direction = \"sideways\"\n")
	cfg, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want false — the operator's opt-out must survive an unrelated bad key")
	}
	if len(cfg.Hidden) != 1 || cfg.Hidden[0] != "colima" {
		t.Errorf("Hidden = %v, want [colima] preserved", cfg.Hidden)
	}
	if cfg.ProbeTimeoutMS != 300 {
		t.Errorf("ProbeTimeoutMS = %d, want 300", cfg.ProbeTimeoutMS)
	}
	if cfg.SplitDirection != "right" {
		t.Errorf("SplitDirection = %q, want right", cfg.SplitDirection)
	}
	// Both rejections are reported, not just the first.
	for _, want := range []string{"probe_timeout_ms", "split_direction"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

// A non-positive probe_timeout_ms can only come from the operator explicitly
// typing one — go-toml leaves absent keys untouched when unmarshalling over a
// populated struct, so an omitted key never reaches here. Silently substituting
// 300 would discard a value they wrote, and the spec's error-handling contract is
// that nothing is swallowed. Same treatment as a bad split_direction.
func TestLoadRejectsNonPositiveProbeTimeout(t *testing.T) {
	for _, body := range []string{"probe_timeout_ms = 0\n", "probe_timeout_ms = -1\n"} {
		dir := writeConfig(t, body)
		cfg, err := Load(dir)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("Load(%q) err = %v, want ErrInvalid", body, err)
		}
		if cfg.ProbeTimeoutMS != 300 {
			t.Errorf("Load(%q) returned %+v, want the defaults on rejection", body, cfg)
		}
	}
}

// A file that does not parse cannot tell us whether the operator opted out of
// probing, so probe fails closed rather than sending SYNs on a guess.
func TestLoadMalformedTOMLFailsClosedOnProbe(t *testing.T) {
	dir := writeConfig(t, "probe = false\nprobe_timeout_ms = yes\n")
	cfg, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if cfg.Probe {
		t.Error("Probe = true on a malformed config; it must fail closed")
	}
	if cfg.ProbeTimeoutMS != 300 || cfg.SplitDirection != "right" {
		t.Errorf("cfg = %+v, want the other keys at their defaults", cfg)
	}
}

// The opt-out below the syntax error must be honored exactly like the opt-out
// above it. go-toml applies keys as it parses and stops at the error, so
// preserving its partial result would make this case return Probe=true while
// the same file with the lines swapped returned false — a traffic opt-out must
// not depend on line order.
func TestLoadMalformedTOMLProbeIsOrderIndependent(t *testing.T) {
	dir := writeConfig(t, "this is not toml\nprobe = false\n")
	cfg, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if cfg.Probe {
		t.Error("Probe = true; a malformed config must fail closed regardless of where the bad line sits")
	}
}

// A file that exists but cannot be opened is in the same position as one that
// cannot be parsed: it may have said `probe = false`, so assume it did.
func TestLoadUnreadableFileFailsClosedOnProbe(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test relies on")
	}
	dir := writeConfig(t, "probe = false\n")
	path := filepath.Join(dir, "config.toml")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore before TempDir teardown, which cannot remove an unreadable file.
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	cfg, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if cfg.Probe {
		t.Error("Probe = true on an unreadable config; it must fail closed")
	}
}
```

These three need `"os"` and `"path/filepath"` in the test imports.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pluginconfig/ -count=1`
Expected: FAIL — `undefined: Load`

- [x] **Step 3: Implement the package**

```go
// Package pluginconfig loads the plugin's own config.toml from
// $HERDR_PLUGIN_CONFIG_DIR.
package pluginconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// ErrInvalid reports a config file that exists but cannot be used.
var ErrInvalid = errors.New("invalid plugin config")

const defaultProbeTimeoutMS = 300

// Config is the operator-facing plugin configuration. Every key is optional.
type Config struct {
	Probe            bool     `toml:"probe"`
	ProbeTimeoutMS   int      `toml:"probe_timeout_ms"`
	SplitDirection   string   `toml:"split_direction"`
	ShowPreview      bool     `toml:"show_preview"`
	ReusePanes       bool     `toml:"reuse_panes"`
	Hidden           []string `toml:"hidden"`
	ExtraConfigPaths []string `toml:"extra_config_paths"`
	SSHArgs          []string `toml:"ssh_args"`
}

// Defaults returns the configuration used when no file is present.
func Defaults() Config {
	return Config{
		Probe:          true,
		ProbeTimeoutMS: defaultProbeTimeoutMS,
		SplitDirection: "right",
		ShowPreview:    true,
		ReusePanes:     true,
	}
}

// Load reads dir/config.toml over the defaults. A missing file is not an error.
//
// Load ALWAYS returns a usable Config. When the file parses, every key the
// operator got right is kept and only the invalid ones fall back to their
// defaults. A non-nil error means at least one key was rejected. Callers must
// report that error and then use the returned Config — NOT discard it for
// Defaults(). Discarding it would undo an operator's valid `probe = false`
// because of an unrelated typo elsewhere in the same file, sending scan traffic
// the config asked it not to.
//
// When the file exists but cannot be parsed or read at all, nothing is kept and
// Probe is forced off — see unusable() below.
func Load(dir string) (Config, error) {
	cfg := Defaults()
	if dir == "" {
		return cfg, nil
	}

	// unusable returns the safe fallback for a config file that exists but
	// cannot be trusted. Probe fails closed: probing sends one SYN per host in
	// the operator's ssh config, and an unusable config means we cannot tell
	// whether they opted out. Losing the up/down indicators is cheap; putting
	// packets on a network the operator meant to leave alone is not.
	unusable := func() Config {
		c := Defaults()
		c.Probe = false
		return c
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	// A missing file means default behavior, so Probe stays on here.
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return unusable(), fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		// go-toml applies keys as it parses and stops at the syntax error, so
		// cfg now holds whatever happened to sit above it. That truncation
		// point is arbitrary — verified against v2.4.3, `probe = false` above
		// the bad line lands while the identical line below it does not — so
		// keeping the partial result would make a traffic opt-out depend on
		// line order. Discard it.
		return unusable(), fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	// Check every key and reset each offender individually. Returning on the
	// first bad key would hand back a Config still carrying the second one's
	// invalid value. errors.Join of an empty slice is nil, so the happy path
	// stays error-free, and errors.Is still matches ErrInvalid through the join.
	var errs []error
	if cfg.SplitDirection != "right" && cfg.SplitDirection != "down" {
		errs = append(errs, fmt.Errorf("%w: split_direction must be \"right\" or \"down\", got %q", ErrInvalid, cfg.SplitDirection))
		cfg.SplitDirection = Defaults().SplitDirection
	}
	if cfg.ProbeTimeoutMS <= 0 {
		errs = append(errs, fmt.Errorf("%w: probe_timeout_ms must be positive, got %d", ErrInvalid, cfg.ProbeTimeoutMS))
		cfg.ProbeTimeoutMS = defaultProbeTimeoutMS
	}
	return cfg, errors.Join(errs...)
}
```

Note: `Config` has `[]string` fields, so it is **not** comparable with `==`/`!=` — Go
rejects that at compile time (`invalid operation: ... (struct containing []string cannot
be compared)`), regardless of whether the slices happen to be `nil`. The tests above use
`reflect.DeepEqual` for whole-struct equality and direct field checks elsewhere. Do not
"simplify" those back to `!=`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pluginconfig/ -v -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/pluginconfig/
git commit -m "feat(pluginconfig): load and validate plugin config with defaults" \
  -- internal/pluginconfig/
```

---

### Task 9: herdrapi — Runner seam and PaneList

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `internal/herdrapi/herdrapi.go`
- Test: `internal/herdrapi/herdrapi_test.go`

Background: every `herdr` CLI call returns a JSON envelope `{"id": "<command>", "result": {...}}` — the `id` is a string like `"cli:pane:list"`, not a number. `herdr pane list` returns `{"result":{"panes":[{"pane_id":"w5:pB","tab_id":"w5:t7","workspace_id":"w5","label":"ssh:nixos-dev"}]}}`. `label` is **absent from the object entirely** when unset — not `null` (verified against herdr 0.9.0: of 23 live panes, 2 carried `"label":"Explorer"` and the other 21 had no `label` key at all). A `*string` decodes both cases to `nil`, so the field stays a pointer; the point is that `Label` must not be a plain `string`, because that would make an unlabeled pane indistinguishable from one labeled `""` and `FindLabeled("")` would match every pane. The picker only ever searches for `ssh:<alias>`, so this is defense, not a live bug. Real panes also carry `cwd`, `agent`, `terminal_title`, and about ten other fields; decoding only the four we need is correct — Go ignores unknown keys. `pane list` is global — it returns panes from every workspace, which is what makes cross-workspace reuse possible.

**Do not pass `--json` to `pane list`.** JSON is the only output format herdr's CLI has, so most subcommands have no such flag: `herdr pane list --json` exits 2 with `unknown option: --json` (verified against herdr 0.9.0; `herdr pane` usage shows only `pane list [--workspace <workspace_id>]`). Because the Runner is injected, a wrong flag here is invisible to unit tests and fails on every real call. The flag does exist on `plugin list`, which is a different command — do not generalize from it.

- [x] **Step 1: Write the failing test**

```go
package herdrapi

import (
	"errors"
	"strings"
	"testing"
)

// fakeRunner records argv and replays canned output.
func fakeRunner(out string, err error) (Runner, *[][]string) {
	var calls [][]string
	run := func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), err
	}
	return run, &calls
}

const panesJSON = `{"id":1,"result":{"panes":[
  {"pane_id":"w5:pA","tab_id":"w5:t1","workspace_id":"w5","label":null},
  {"pane_id":"w5:pB","tab_id":"w5:t7","workspace_id":"w5","label":"ssh:nixos-dev"}
]}}`

func TestPaneList(t *testing.T) {
	run, calls := fakeRunner(panesJSON, nil)
	c := Client{Run: run}

	panes, err := c.PaneList()
	if err != nil {
		t.Fatalf("PaneList: %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(panes))
	}
	if panes[0].Label != nil {
		t.Errorf("panes[0].Label = %v, want nil", panes[0].Label)
	}
	if panes[1].Label == nil || *panes[1].Label != "ssh:nixos-dev" {
		t.Errorf("panes[1].Label = %v", panes[1].Label)
	}
	if panes[1].PaneID != "w5:pB" || panes[1].TabID != "w5:t7" || panes[1].WorkspaceID != "w5" {
		t.Errorf("panes[1] = %+v", panes[1])
	}

	want := []string{"pane", "list"}
	if len(*calls) != 1 || strings.Join((*calls)[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", *calls, want)
	}
}

func TestPaneListPropagatesCLIError(t *testing.T) {
	boom := errors.New("exit status 1")
	run, _ := fakeRunner("socket not found", boom)
	c := Client{Run: run}

	_, err := c.PaneList()
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the runner error", err)
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("err = %v, want a *CLIError", err)
	}
	if !strings.Contains(cliErr.Error(), "socket not found") {
		t.Errorf("CLIError message lost the output: %q", cliErr.Error())
	}
}

func TestPaneListRejectsBadJSON(t *testing.T) {
	run, _ := fakeRunner("not json at all", nil)
	c := Client{Run: run}
	if _, err := c.PaneList(); err == nil {
		t.Fatal("err = nil, want a decode error")
	}
}

func TestFindLabeled(t *testing.T) {
	label := "ssh:nixos-dev"
	panes := []Pane{{PaneID: "w5:pA"}, {PaneID: "w5:pB", Label: &label}}

	got, ok := FindLabeled(panes, "ssh:nixos-dev")
	if !ok || got.PaneID != "w5:pB" {
		t.Fatalf("FindLabeled = (%+v, %v)", got, ok)
	}
	if _, ok := FindLabeled(panes, "ssh:absent"); ok {
		t.Error("FindLabeled matched a label that is not present")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/herdrapi/ -count=1`
Expected: FAIL — `undefined: Runner`

- [x] **Step 3: Implement the seam, error type, and `PaneList`**

```go
// Package herdrapi wraps the herdr CLI. Every call goes through one Runner so
// the whole plugin is testable without a live herdr socket.
package herdrapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes a herdr subcommand and returns its stdout.
type Runner func(args []string) ([]byte, error)

// Client talks to herdr through a Runner.
type Client struct {
	Run Runner
}

// CLIError carries the argv and output of a failed herdr call. The output is
// the only useful diagnostic when the socket or a flag is wrong.
type CLIError struct {
	Args   []string
	Output string
	Err    error
}

func (e *CLIError) Error() string {
	return fmt.Sprintf("herdr %s: %v: %s", strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Output))
}

func (e *CLIError) Unwrap() error { return e.Err }

// New returns a Client bound to the herdr binary the plugin was launched with.
func New() Client {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	return Client{Run: func(args []string) ([]byte, error) {
		cmd := exec.Command(bin, args...)
		out, err := cmd.Output()
		if err == nil {
			return out, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return append(out, exitErr.Stderr...), err
		}
		return out, err
	}}
}

// Pane is the subset of herdr's PaneInfo the picker needs. herdr returns about
// fifteen fields per pane; the rest are ignored.
//
// Label is a pointer so that "no label" stays distinguishable from a label of
// "". herdr omits the key entirely on an unlabeled pane rather than sending
// null, and both decode to nil here. A plain string would collapse the two
// cases and make FindLabeled("") match every unlabeled pane.
type Pane struct {
	PaneID      string  `json:"pane_id"`
	TabID       string  `json:"tab_id"`
	WorkspaceID string  `json:"workspace_id"`
	Label       *string `json:"label"`
}

type envelope struct {
	Result json.RawMessage `json:"result"`
}

func (c Client) call(args ...string) ([]byte, error) {
	out, err := c.Run(args)
	if err != nil {
		return nil, &CLIError{Args: args, Output: string(out), Err: err}
	}
	return out, nil
}

func (c Client) callJSON(target any, args ...string) error {
	out, err := c.call(args...)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("herdr %s: decode envelope: %w", strings.Join(args, " "), err)
	}
	if err := json.Unmarshal(env.Result, target); err != nil {
		return fmt.Errorf("herdr %s: decode result: %w", strings.Join(args, " "), err)
	}
	return nil
}

// PaneList returns every pane herdr knows about, across all workspaces.
func (c Client) PaneList() ([]Pane, error) {
	var result struct {
		Panes []Pane `json:"panes"`
	}
	// No --json flag: JSON is herdr's only output format and `pane list` rejects
	// the flag with `unknown option: --json` (exit 2).
	if err := c.callJSON(&result, "pane", "list"); err != nil {
		return nil, err
	}
	return result.Panes, nil
}

// FindLabeled returns the first pane carrying label.
func FindLabeled(panes []Pane, label string) (Pane, bool) {
	for _, p := range panes {
		if p.Label != nil && *p.Label == label {
			return p, true
		}
	}
	return Pane{}, false
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/herdrapi/ -v -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/herdrapi/
git commit -m "feat(herdrapi): add injectable herdr CLI client with pane listing" \
  -- internal/herdrapi/
```

---

### Task 10: herdrapi — rename, close, focus, and plugin pane open

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Modify: `internal/herdrapi/herdrapi.go`
- Test: `internal/herdrapi/commands_test.go`

Background on focus: `herdr pane focus` takes only `--direction`, so it cannot jump to an arbitrary pane id. The working sequence is `workspace focus <ws>` → `tab focus <tab>` → `plugin pane focus <pane>`, skipping any step already current.

- [x] **Step 1: Write the failing test**

```go
package herdrapi

import (
	"strings"
	"testing"
)

func recorder() (Runner, *[][]string) {
	var calls [][]string
	run := func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}
	return run, &calls
}

func argvLines(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func assertArgv(t *testing.T, calls [][]string, want []string) {
	t.Helper()
	got := argvLines(calls)
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPaneRename(t *testing.T) {
	run, calls := recorder()
	if err := (Client{Run: run}).PaneRename("w5:pB", "ssh:nixos-dev"); err != nil {
		t.Fatalf("PaneRename: %v", err)
	}
	assertArgv(t, *calls, []string{"pane rename w5:pB ssh:nixos-dev"})
}

func TestPaneClose(t *testing.T) {
	run, calls := recorder()
	if err := (Client{Run: run}).PaneClose("w5:pOverlay"); err != nil {
		t.Fatalf("PaneClose: %v", err)
	}
	assertArgv(t, *calls, []string{"plugin pane close w5:pOverlay"})
}

func TestFocusPaneFullSequence(t *testing.T) {
	run, calls := recorder()
	target := Pane{PaneID: "w8:p3", TabID: "w8:t2", WorkspaceID: "w8"}
	if err := (Client{Run: run}).FocusPane(target, "w5", "w5:t1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	assertArgv(t, *calls, []string{
		"workspace focus w8",
		"tab focus w8:t2",
		"plugin pane focus w8:p3",
	})
}

func TestFocusPaneSkipsCurrentSteps(t *testing.T) {
	run, calls := recorder()
	target := Pane{PaneID: "w5:pB", TabID: "w5:t1", WorkspaceID: "w5"}
	if err := (Client{Run: run}).FocusPane(target, "w5", "w5:t1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	assertArgv(t, *calls, []string{"plugin pane focus w5:pB"})
}

func TestPluginPaneOpenSplit(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "purehate.herdr-ssh",
		Entrypoint: "session",
		Placement:  "split",
		TargetPane: "w5:pA",
		Direction:  "right",
		Env:        map[string]string{"HERDR_SSH_TARGET": "nixos-dev"},
		Focus:      true,
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	assertArgv(t, *calls, []string{
		"plugin pane open --plugin purehate.herdr-ssh --entrypoint session " +
			"--placement split --target-pane w5:pA --direction right " +
			"--env HERDR_SSH_TARGET=nixos-dev --focus",
	})
}

func TestPluginPaneOpenTabOmitsDirection(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "purehate.herdr-ssh",
		Entrypoint: "session",
		Placement:  "tab",
		Direction:  "right",
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	// --direction is meaningless outside a split and herdr rejects it there.
	if got := argvLines(*calls)[0]; strings.Contains(got, "--direction") {
		t.Fatalf("argv = %q, want no --direction for a tab placement", got)
	}
}

func TestPluginPaneOpenEnvIsSorted(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "p",
		Entrypoint: "session",
		Placement:  "zoomed",
		Env:        map[string]string{"B": "2", "A": "1", "C": "3"},
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	got := argvLines(*calls)[0]
	if !strings.Contains(got, "--env A=1 --env B=2 --env C=3") {
		t.Fatalf("argv = %q, want env flags in sorted order", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/herdrapi/ -run 'TestPaneRename|TestFocus|TestPluginPaneOpen' -count=1`
Expected: FAIL — `c.PaneRename undefined`

- [x] **Step 3: Implement the three commands**

Add to `internal/herdrapi/herdrapi.go` (and add `"sort"` to the imports):

```go
// PaneRename sets a pane's label. The label is how the picker recognizes a pane
// it opened earlier.
func (c Client) PaneRename(paneID, label string) error {
	_, err := c.call("pane", "rename", paneID, label)
	return err
}

// PaneClose closes a plugin-owned pane. Verified signature: `herdr plugin pane
// close <PANE_ID>` — it takes a pane id, not an entrypoint name.
func (c Client) PaneClose(paneID string) error {
	_, err := c.call("plugin", "pane", "close", paneID)
	return err
}

// FocusPane moves the operator's view to p. herdr's `pane focus` only accepts a
// direction, so reaching an arbitrary pane means walking workspace → tab → pane.
// Steps that are already current are skipped: refocusing the current workspace
// is a visible flicker for no gain.
func (c Client) FocusPane(p Pane, currentWorkspace, currentTab string) error {
	if p.WorkspaceID != "" && p.WorkspaceID != currentWorkspace {
		if _, err := c.call("workspace", "focus", p.WorkspaceID); err != nil {
			return err
		}
	}
	if p.TabID != "" && p.TabID != currentTab {
		if _, err := c.call("tab", "focus", p.TabID); err != nil {
			return err
		}
	}
	_, err := c.call("plugin", "pane", "focus", p.PaneID)
	return err
}

// OpenOpts describes a pane to open from a declared plugin entrypoint.
type OpenOpts struct {
	Plugin     string
	Entrypoint string
	Placement  string // overlay | popup | split | tab | zoomed
	TargetPane string // only sent for split and zoomed; the others reject it
	Direction  string // right | down; only meaningful for a split
	Env        map[string]string
	Focus      bool
}

// PluginPaneOpen launches one of this plugin's pane entrypoints.
func (c Client) PluginPaneOpen(o OpenOpts) error {
	args := []string{"plugin", "pane", "open", "--plugin", o.Plugin, "--entrypoint", o.Entrypoint}
	if o.Placement != "" {
		args = append(args, "--placement", o.Placement)
	}
	// --target-pane is only accepted by the placements that target an existing
	// pane. herdr rejects it on the others: overlay and popup target the active
	// pane, and a tab takes a workspace id instead. Callers pass the caller's
	// pane id unconditionally and let placement decide, so the filter belongs
	// here rather than in every caller.
	if o.TargetPane != "" && (o.Placement == "split" || o.Placement == "zoomed") {
		args = append(args, "--target-pane", o.TargetPane)
	}
	if o.Placement == "split" && o.Direction != "" {
		args = append(args, "--direction", o.Direction)
	}
	keys := make([]string, 0, len(o.Env))
	for k := range o.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--env", k+"="+o.Env[k])
	}
	if o.Focus {
		args = append(args, "--focus")
	}
	_, err := c.call(args...)
	return err
}
```

- [x] **Step 4: Run the whole package**

Run: `go test ./internal/herdrapi/ -v -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/herdrapi/
git commit -m "feat(herdrapi): add pane rename, close, focus sequence, and pane open" \
  -- internal/herdrapi/
```

---

### Task 11: probe

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `internal/probe/probe.go`
- Test: `internal/probe/probe_test.go`

- [x] **Step 1: Write the failing test**

```go
package probe

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRunReportsReachability(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	targets := []Target{
		{Alias: "up", Addr: ln.Addr().String()},
		{Alias: "down", Addr: "127.0.0.1:1"},
		{Alias: "skipped", Addr: "127.0.0.1:1", Skip: true},
	}

	results := map[string]bool{}
	for r := range Run(context.Background(), targets, 500*time.Millisecond) {
		results[r.Alias] = r.Up
	}

	if len(results) != 2 {
		t.Fatalf("results = %v, want 2 entries (skipped host omitted)", results)
	}
	if !results["up"] {
		t.Error("listening host reported down")
	}
	if results["down"] {
		t.Error("closed port reported up")
	}
	if _, ok := results["skipped"]; ok {
		t.Error("skipped target was probed")
	}
}

func TestRunClosesChannelOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := Run(ctx, []Target{{Alias: "a", Addr: "127.0.0.1:1"}}, time.Second)
	for range ch {
		// Drain: results may or may not arrive, but the channel must close.
	}
}

func TestRunWithNoTargetsClosesImmediately(t *testing.T) {
	select {
	case _, open := <-Run(context.Background(), nil, time.Second):
		if open {
			t.Fatal("received a result for zero targets")
		}
	case <-time.After(time.Second):
		t.Fatal("channel never closed")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/probe/ -count=1`
Expected: FAIL — `undefined: Target`

- [x] **Step 3: Implement the package**

```go
// Package probe checks TCP reachability of SSH hosts. Results stream so the
// picker can render before the network answers.
package probe

import (
	"context"
	"net"
	"sync"
	"time"
)

// maxInFlight bounds concurrent dials. A large ssh config should not open a
// hundred sockets at once just to draw a status dot.
const maxInFlight = 16

// Target is one host to check. Skip marks hosts that cannot be reached
// directly, such as anything behind a ProxyJump.
type Target struct {
	Alias string
	Addr  string
	Skip  bool
}

// Result reports one host's reachability.
type Result struct {
	Alias string
	Up    bool
}

// Run dials every non-skipped target and streams results. The returned channel
// always closes, including on a canceled context.
func Run(ctx context.Context, targets []Target, timeout time.Duration) <-chan Result {
	out := make(chan Result)
	sem := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup

	for _, t := range targets {
		if t.Skip {
			continue
		}
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			d := net.Dialer{Timeout: timeout}
			conn, err := d.DialContext(ctx, "tcp", t.Addr)
			if err == nil {
				conn.Close()
			}
			select {
			case out <- Result{Alias: t.Alias, Up: err == nil}:
			case <-ctx.Done():
			}
		}(t)
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
```

- [x] **Step 4: Run test to verify it passes**

**Before adding a field to `Result`, read the conditional rule at the end of
Task 21 Step 1c.** This struct carrying only `Alias` and `Up` is load-bearing
for two tests there. Every dial error collapses into `Up: false`, which is
exactly why those tests need a live listener to mean anything; give `Result` an
`Err` field and both of them silently stop pinning what they claim to pin, while
simultaneously starting to look simplifiable. Extending this struct is not done
until the two `Run`-level delegation mutants have been re-scored.

Run: `go test ./internal/probe/ -race -v -count=1`
Expected: PASS with no race warnings

- [x] **Step 5: Commit**

```bash
git add internal/probe/
git commit -m "feat(probe): add bounded concurrent TCP reachability checks" \
  -- internal/probe/
```

---

### Task 12: picker — fuzzy ranking

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `internal/picker/rank.go`
- Test: `internal/picker/rank_test.go`

**Background — why `Rank` returns positions, not hosts.**

`Rank` returns a `[]Match`, where each `Match` carries the host plus the rune
indices the query matched. Task 14 renders those indices in the accent color, so
the operator can see _which_ characters earned a row its place — the fzf
behavior. This has to live in `Rank` rather than the renderer: the renderer
cannot reconstruct the positions, because which characters matched depends on
which tier matched, and only `Rank` knows the tier.

That last point is not a style preference. For `s = "deav_dev"`, `q = "dev"`
there are two valid readings: a greedy scattered walk lands on runes `[0, 1, 3]`
(`d`, `e`, `v` picked up across `"deav"`), while the contiguous substring sits at
runes `[5, 6, 7]`. The tier that matched decides which reading is correct — this
host matches `rankAliasSubstring`, so the contiguous run is what gets
highlighted, matching fzf's preference for contiguous matches. Compute positions
per tier, inside the branch that matched. Do not compute them once and reuse.

**Positions are rune indices, never byte offsets.** `strings.Index` returns a
byte offset. On any alias containing a multi-byte rune the two diverge, and the
renderer highlights the wrong characters. Convert with
`len([]rune(s[:byteIndex]))`.

**The word-boundary bonus applies inside the scattered tiers only.** Ranking's
outer order is the tier order the spec fixes (`docs/specs/2026-09-09-ssh-picker-design.md:283-285`):
exact alias, alias prefix, alias substring, scattered alias, hostname substring,
scattered hostname. Within the scattered tiers there was previously no
discrimination at all — every scattered match tied and fell back to config order.
The bonus breaks those ties using fzf's scoring shape (`src/algo/algo.go`): a
match landing at position 0 or right after a separator scores 8, a match
immediately following the previous match scores 4 more, and the first matched
character's bonus is doubled. Higher total wins; equal totals still keep config
order. The other four tiers keep config order untouched, so no spec-defined
ordering changes.

- [x] **Step 1: Write the failing test**

```go
package picker

import (
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

func aliases(matches []Match) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.Host.Alias)
	}
	return out
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Port is set on every host because sshconfig.Parse always resolves it,
// defaulting to "22". A fixture without it is a Host shape the parser cannot
// produce, and it makes the renderer emit a bare ":" that no real host shows.
var corpus = []sshconfig.Host{
	{Alias: "alpha", HostName: "10.0.0.1", Port: "22"},
	{Alias: "nixos-dev", HostName: "192.0.2.10", Port: "22"},
	{Alias: "devbox", HostName: "10.0.0.7", Port: "22"},
	{Alias: "prod-web", HostName: "dev.example.com", Port: "22"},
	{Alias: "dev", HostName: "10.0.0.9", Port: "22"},
}

func TestRankEmptyQueryPreservesOrder(t *testing.T) {
	got := aliases(Rank(corpus, ""))
	want := []string{"alpha", "nixos-dev", "devbox", "prod-web", "dev"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want config order %v", got, want)
	}
}

func TestRankOrdersExactThenPrefixThenSubstring(t *testing.T) {
	got := aliases(Rank(corpus, "dev"))
	want := []string{"dev", "devbox", "nixos-dev", "prod-web"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v", got, want)
	}
}

func TestRankIsCaseInsensitive(t *testing.T) {
	if got := aliases(Rank(corpus, "NIXOS")); len(got) == 0 || got[0] != "nixos-dev" {
		t.Fatalf("Rank = %v, want nixos-dev first", got)
	}
}

func TestRankMatchesScatteredCharacters(t *testing.T) {
	got := aliases(Rank(corpus, "nxd"))
	if len(got) != 1 || got[0] != "nixos-dev" {
		t.Fatalf("Rank = %v, want only nixos-dev", got)
	}
}

func TestRankDropsNonMatches(t *testing.T) {
	if got := Rank(corpus, "zzzz"); len(got) != 0 {
		t.Fatalf("Rank = %v, want empty", aliases(got))
	}
}

func TestRankDoesNotMutateInput(t *testing.T) {
	Rank(corpus, "dev")
	if corpus[0].Alias != "alpha" {
		t.Fatal("Rank reordered its input slice")
	}
}

func TestRankEmptyQueryHasNoPositions(t *testing.T) {
	// Nothing matched, so there is nothing to highlight. A renderer that sees
	// non-nil positions here would accent the whole unfiltered list.
	for _, m := range Rank(corpus, "") {
		if m.AliasPos != nil || m.HostNamePos != nil {
			t.Fatalf("%q: AliasPos = %v, HostNamePos = %v, want both nil for an empty query",
				m.Host.Alias, m.AliasPos, m.HostNamePos)
		}
	}
}

func TestRankExactPositionsCoverWholeAlias(t *testing.T) {
	got := Rank(corpus, "dev")
	if len(got) == 0 || got[0].Host.Alias != "dev" {
		t.Fatalf("Rank = %v, want dev first", aliases(got))
	}
	if want := []int{0, 1, 2}; !equalInts(got[0].AliasPos, want) {
		t.Errorf("AliasPos = %v, want %v", got[0].AliasPos, want)
	}
	if got[0].HostNamePos != nil {
		t.Errorf("HostNamePos = %v, want nil for an alias match", got[0].HostNamePos)
	}
}

func TestRankPrefixPositionsCoverQueryOnly(t *testing.T) {
	// devbox matches on its first three runes. The trailing "box" was not
	// typed, so it must not be highlighted.
	got := Rank(corpus, "dev")
	if len(got) < 2 || got[1].Host.Alias != "devbox" {
		t.Fatalf("Rank = %v, want devbox second", aliases(got))
	}
	if want := []int{0, 1, 2}; !equalInts(got[1].AliasPos, want) {
		t.Errorf("AliasPos = %v, want %v", got[1].AliasPos, want)
	}
}

func TestRankSubstringPositionsAreRuneIndicesNotBytes(t *testing.T) {
	// "héllo-dev" is nine runes but ten bytes, because é is two bytes.
	// strings.Index reports the match at byte 7; the renderer walks runes, so
	// the position must be 6.
	hosts := []sshconfig.Host{{Alias: "héllo-dev", HostName: "10.0.0.1"}}
	got := Rank(hosts, "dev")
	if len(got) != 1 {
		t.Fatalf("Rank returned %d matches, want 1", len(got))
	}
	if want := []int{6, 7, 8}; !equalInts(got[0].AliasPos, want) {
		t.Fatalf("AliasPos = %v, want %v (rune indices, not byte offsets)", got[0].AliasPos, want)
	}
}

func TestRankScatteredPositionsRecordEachMatchedRune(t *testing.T) {
	// nixos-dev: n(0) i x(2) o s - d(6) e v
	got := Rank(corpus, "nxd")
	if len(got) != 1 {
		t.Fatalf("Rank returned %d matches, want 1", len(got))
	}
	if want := []int{0, 2, 6}; !equalInts(got[0].AliasPos, want) {
		t.Fatalf("AliasPos = %v, want %v", got[0].AliasPos, want)
	}
}

func TestRankHostNameMatchSetsHostPositionsOnly(t *testing.T) {
	// Only prod-web matches, and it matches on HostName "dev.example.com" —
	// d(0) e v . e(4) x a m p l e(10). The alias contributed nothing, so
	// highlighting it would point the operator at the wrong column.
	got := Rank(corpus, "example")
	if len(got) != 1 || got[0].Host.Alias != "prod-web" {
		t.Fatalf("Rank = %v, want only prod-web", aliases(got))
	}
	if got[0].AliasPos != nil {
		t.Errorf("AliasPos = %v, want nil for a hostname-only match", got[0].AliasPos)
	}
	if want := []int{4, 5, 6, 7, 8, 9, 10}; !equalInts(got[0].HostNamePos, want) {
		t.Errorf("HostNamePos = %v, want %v", got[0].HostNamePos, want)
	}
}

func TestRankBoundaryBonusOutranksConfigOrder(t *testing.T) {
	// Both hosts land in the scattered-alias tier for "nx": neither contains
	// the literal "nx". nixos-dev matches its n at rune 0 — a word start, and
	// the first matched rune, so 8 doubled to 16. banana-xray matches its n at
	// rune 2, mid-word and worth nothing, then its x after the hyphen for 8.
	// 16 beats 8, so the bonus has to pull nixos-dev ahead of the host declared
	// above it; config order alone would keep banana-xray first.
	hosts := []sshconfig.Host{
		{Alias: "banana-xray", HostName: "10.0.0.1"},
		{Alias: "nixos-dev", HostName: "10.0.0.2"},
	}
	got := aliases(Rank(hosts, "nx"))
	want := []string{"nixos-dev", "banana-xray"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v — the word-boundary bonus should outrank config order inside the scattered tier", got, want)
	}
}

func TestRankBoundaryBonusDoesNotReorderSubstringTier(t *testing.T) {
	// Both are alias-substring matches for "dev". The spec fixes ties at config
	// order, so the bonus must not fire outside the scattered tiers. The scores
	// are deliberately lopsided: mid-dev's match starts after a hyphen and
	// would score 24, xdevy's starts mid-word and would score 8. A bonus
	// leaking into this tier flips the order; config order keeps it.
	hosts := []sshconfig.Host{
		{Alias: "xdevy", HostName: "10.0.0.1"},
		{Alias: "mid-dev", HostName: "10.0.0.2"},
	}
	got := aliases(Rank(hosts, "dev"))
	want := []string{"xdevy", "mid-dev"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v — config order must survive inside the substring tier", got, want)
	}
}

func TestRankPositionsComeFromTheTierThatMatched(t *testing.T) {
	// "deav_dev" contains "dev" as a contiguous run at runes 5,6,7 — the
	// substring tier. But a greedy left-to-right scattered walk for the same
	// query lands on runes 0,1,3 instead (d@0, e@1, then the first available
	// v@3, skipping the 'a'). scoreHost must stop at whichever tier actually
	// matched — the substring check runs before the scattered one — and
	// return THAT tier's positions, never a separately recomputed scattered
	// reading. Every host in the shared corpus happens to produce identical
	// positions under both readings, so only a fixture shaped like this one
	// can tell substring-first apart from scattered-first.
	hosts := []sshconfig.Host{{Alias: "deav_dev", HostName: "10.0.0.1", Port: "22"}}
	got := Rank(hosts, "dev")
	if len(got) != 1 {
		t.Fatalf("Rank returned %d matches, want 1", len(got))
	}
	if want := []int{5, 6, 7}; !equalInts(got[0].AliasPos, want) {
		t.Fatalf("AliasPos = %v, want %v (the contiguous substring run, not the greedy scattered walk)", got[0].AliasPos, want)
	}
}

func TestRankExactlyOnePositionFieldIsSet(t *testing.T) {
	// rank.go documents that a Match carries positions from whichever field
	// produced it — AliasPos for an alias-tier match, HostNamePos for a
	// hostname-tier match — and never both. Other tests spot-check one field
	// per tier; this walks every tier instead and asserts the other field is
	// nil every time, not just in the cases that happened to get checked.
	//
	// "dev" against the shared corpus alone reaches four tiers at once: dev
	// is exact, devbox is alias-prefix, nixos-dev is alias-substring, and
	// prod-web matches only on HostName. "nxd" adds the alias-scattered tier
	// (nixos-dev again). Nothing in the corpus reaches host-scattered, so
	// hostScattered is a local fixture built just for that last tier: its
	// alias "qqq" cannot match "abc" by any rule, but its HostName
	// "ax-by-cz" matches "abc" only as a scattered subsequence (a@0, b@3,
	// c@6) — there is no contiguous "abc" in it.
	hostScattered := []sshconfig.Host{{Alias: "qqq", HostName: "ax-by-cz", Port: "22"}}

	cases := []struct {
		name  string
		hosts []sshconfig.Host
		query string
	}{
		{"alias tiers + host-substring", corpus, "dev"},
		{"alias-scattered", corpus, "nxd"},
		{"host-scattered", hostScattered, "abc"},
	}

	for _, c := range cases {
		matches := Rank(c.hosts, c.query)
		if len(matches) == 0 {
			t.Fatalf("%s: Rank returned no matches, want at least one", c.name)
		}
		for _, m := range matches {
			aliasSet, hostSet := m.AliasPos != nil, m.HostNamePos != nil
			if aliasSet == hostSet {
				t.Errorf("%s: %q: AliasPos = %v, HostNamePos = %v, want exactly one non-nil",
					c.name, m.Host.Alias, m.AliasPos, m.HostNamePos)
			}
		}
	}
}

func TestRankMatchesRegardlessOfHaystackCase(t *testing.T) {
	// scoreHost lowercases both haystacks before matching (rank.go:125-126):
	//	alias := strings.ToLower(h.Alias)
	//	host := strings.ToLower(h.HostName)
	// Every Alias/HostName fixture elsewhere in this file is already
	// all-lowercase, so that lowercasing is a no-op under test and a mutant
	// that deletes it survives the whole suite undetected. These fixtures
	// are mixed case on purpose so each one actually exercises a ToLower
	// call: a mixed-case alias found by a lowercase query (three tiers,
	// since substringPos, scatteredPos, and the prefix check are separate
	// code paths), a mixed-case hostname found the same way with an alias
	// that cannot match by any rule (so the match can only come from the
	// host half of the lowercasing), and — the other direction — a
	// lowercase alias found by an uppercase query.
	cases := []struct {
		name         string
		hosts        []sshconfig.Host
		query        string
		wantAlias    string
		wantAliasSet bool
		wantHostSet  bool
	}{
		{
			name:         "alias prefix tier, mixed-case alias",
			hosts:        []sshconfig.Host{{Alias: "NixOS-Dev", HostName: "10.0.0.1", Port: "22"}},
			query:        "nixos",
			wantAlias:    "NixOS-Dev",
			wantAliasSet: true,
		},
		{
			name:         "alias substring tier (not a prefix), mixed-case alias",
			hosts:        []sshconfig.Host{{Alias: "Prod-GitHub-Box", HostName: "10.0.0.2", Port: "22"}},
			query:        "github",
			wantAlias:    "Prod-GitHub-Box",
			wantAliasSet: true,
		},
		{
			name:         "alias scattered tier, mixed-case alias",
			hosts:        []sshconfig.Host{{Alias: "GitHub-Work", HostName: "10.0.0.3", Port: "22"}},
			query:        "gw",
			wantAlias:    "GitHub-Work",
			wantAliasSet: true,
		},
		{
			// "server1" has none of the letters n/i/x/o/s, so it cannot match
			// "nixos" under any tier — the only way this host is found at all
			// is the mixed-case HostName being lowercased.
			name:        "hostname substring tier, mixed-case hostname, alias cannot match",
			hosts:       []sshconfig.Host{{Alias: "server1", HostName: "NixOS-Dev.example.com", Port: "22"}},
			query:       "nixos",
			wantAlias:   "server1",
			wantHostSet: true,
		},
		{
			name:         "uppercase query, already-lowercase alias",
			hosts:        []sshconfig.Host{{Alias: "nixos-dev", HostName: "10.0.0.4", Port: "22"}},
			query:        "NIXOS",
			wantAlias:    "nixos-dev",
			wantAliasSet: true,
		},
	}

	for _, c := range cases {
		got := Rank(c.hosts, c.query)
		if len(got) != 1 || got[0].Host.Alias != c.wantAlias {
			t.Fatalf("%s: Rank = %v, want [%s]", c.name, aliases(got), c.wantAlias)
		}
		if aliasSet := got[0].AliasPos != nil; aliasSet != c.wantAliasSet {
			t.Errorf("%s: AliasPos set = %v, want %v", c.name, aliasSet, c.wantAliasSet)
		}
		if hostSet := got[0].HostNamePos != nil; hostSet != c.wantHostSet {
			t.Errorf("%s: HostNamePos set = %v, want %v", c.name, hostSet, c.wantHostSet)
		}
	}
}

func TestRankPositionsAreCorrectForMixedCaseHaystacks(t *testing.T) {
	// Positions are computed against the lowercased haystack (rank.go:125-126)
	// but the renderer highlights the original, un-lowercased Alias/HostName
	// (view.go). That's only safe because strings.ToLower is rune-count- and
	// index-preserving simple case mapping for these fixtures — it never
	// merges or splits runes — so an index into the lowered string is always
	// the same index into the original. This test pins the indices exactly,
	// so it would fail if that assumption ever broke (e.g. a switch to full
	// Unicode case folding, where "ß" folds to "ss" and shifts everything
	// after it).
	t.Run("prefix tier", func(t *testing.T) {
		hosts := []sshconfig.Host{{Alias: "NixOS-Dev", HostName: "10.0.0.1", Port: "22"}}
		got := Rank(hosts, "nixos")
		if len(got) != 1 {
			t.Fatalf("Rank returned %d matches, want 1", len(got))
		}
		if want := []int{0, 1, 2, 3, 4}; !equalInts(got[0].AliasPos, want) {
			t.Errorf("AliasPos = %v, want %v", got[0].AliasPos, want)
		}
		if got[0].HostNamePos != nil {
			t.Errorf("HostNamePos = %v, want nil for an alias match", got[0].HostNamePos)
		}
	})

	t.Run("scattered tier", func(t *testing.T) {
		// github-work: g(0) i t h u b - w(7) o r k.
		hosts := []sshconfig.Host{{Alias: "GitHub-Work", HostName: "10.0.0.2", Port: "22"}}
		got := Rank(hosts, "gw")
		if len(got) != 1 {
			t.Fatalf("Rank returned %d matches, want 1", len(got))
		}
		if want := []int{0, 7}; !equalInts(got[0].AliasPos, want) {
			t.Errorf("AliasPos = %v, want %v", got[0].AliasPos, want)
		}
		if got[0].HostNamePos != nil {
			t.Errorf("HostNamePos = %v, want nil for an alias match", got[0].HostNamePos)
		}
	})

	t.Run("hostname tier", func(t *testing.T) {
		hosts := []sshconfig.Host{{Alias: "server1", HostName: "NixOS-Dev.example.com", Port: "22"}}
		got := Rank(hosts, "nixos")
		if len(got) != 1 {
			t.Fatalf("Rank returned %d matches, want 1", len(got))
		}
		if want := []int{0, 1, 2, 3, 4}; !equalInts(got[0].HostNamePos, want) {
			t.Errorf("HostNamePos = %v, want %v", got[0].HostNamePos, want)
		}
		if got[0].AliasPos != nil {
			t.Errorf("AliasPos = %v, want nil for a hostname match", got[0].AliasPos)
		}
	})
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/picker/ -run TestRank -count=1`
Expected: FAIL — `undefined: Rank`, `undefined: Match`

- [x] **Step 3: Implement ranking**

```go
// Package picker renders the SSH host overlay.
package picker

import (
	"sort"
	"strings"

	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

// Match quality, best first. Alias matches always beat hostname matches: the
// operator typed a name they chose, not an address they were assigned.
const (
	rankExact = iota
	rankAliasPrefix
	rankAliasSubstring
	rankAliasScattered
	rankHostSubstring
	rankHostScattered
	rankNone
)

// Scoring bonuses for the scattered tiers, in the shape fzf uses
// (src/algo/algo.go). They break ties *within* a tier and never move a host
// across tiers, so the ranking order the spec fixes is untouched.
const (
	bonusBoundary    = 8 // rune 0, or the rune after a separator
	bonusConsecutive = 4 // rune immediately after the previous match
	bonusFirstRune   = 2 // multiplier: fzf doubles the first matched rune
)

// Match is one ranked host plus the rune positions the query matched, so the
// picker can highlight exactly the characters that earned the row its place.
//
// For a non-empty query exactly one of AliasPos / HostNamePos is non-nil:
// scoreHost stops at the first tier that matches, and every alias tier is
// checked before every hostname tier. Both are nil for an empty query, where
// nothing matched and there is nothing to highlight.
//
// The indices are rune offsets, not byte offsets. The renderer walks runes.
type Match struct {
	Host        sshconfig.Host
	AliasPos    []int
	HostNamePos []int
}

// runeRange returns n consecutive indices starting at start.
func runeRange(start, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = start + i
	}
	return out
}

// substringPos returns the rune positions of the first occurrence of q in s, or
// nil. Both must already be lowercased.
func substringPos(s, q string) []int {
	b := strings.Index(s, q)
	if b < 0 {
		return nil
	}
	// strings.Index counts bytes; re-count the prefix in runes so the caller
	// gets an index it can use against []rune(s).
	return runeRange(len([]rune(s[:b])), len([]rune(q)))
}

// scatteredPos greedily matches every rune of q against s left to right and
// returns the positions it landed on, or nil if q does not fit.
func scatteredPos(s, q string) []int {
	rs := []rune(s)
	out := make([]int, 0, len(q))
	at := 0
	for _, want := range q {
		for at < len(rs) && rs[at] != want {
			at++
		}
		if at == len(rs) {
			return nil
		}
		out = append(out, at)
		at++
	}
	return out
}

// isSeparator reports whether r ends a word. SSH aliases and hostnames are
// segmented by punctuation, not whitespace, so "-", "_", and "." carry the
// boundaries an operator actually types around.
func isSeparator(r rune) bool {
	switch r {
	case '-', '_', '.', '/', ':', '@', ' ':
		return true
	}
	return false
}

// positionScore rates a scattered match; higher is better. Matches sitting at
// word starts beat matches buried mid-word, so typing "nd" prefers "nixos-dev"
// (n at the start, d after the hyphen) over a host where both land mid-word.
func positionScore(s string, pos []int) int {
	rs := []rune(s)
	total := 0
	for i, p := range pos {
		b := 0
		if p == 0 || isSeparator(rs[p-1]) {
			b = bonusBoundary
		}
		if i > 0 && pos[i-1] == p-1 {
			b += bonusConsecutive
		}
		if i == 0 {
			b *= bonusFirstRune
		}
		total += b
	}
	return total
}

// scoreHost classifies h against q and returns the tier plus the rune positions
// that matched. It stops at the first matching tier, so the positions always
// describe the reading that earned the tier — a contiguous run for the
// substring tiers, the greedy walk for the scattered ones.
func scoreHost(h sshconfig.Host, q string) (tier int, aliasPos, hostPos []int) {
	alias := strings.ToLower(h.Alias)
	host := strings.ToLower(h.HostName)

	switch {
	case alias == q:
		return rankExact, runeRange(0, len([]rune(q))), nil
	case strings.HasPrefix(alias, q):
		return rankAliasPrefix, runeRange(0, len([]rune(q))), nil
	}
	if p := substringPos(alias, q); p != nil {
		return rankAliasSubstring, p, nil
	}
	if p := scatteredPos(alias, q); p != nil {
		return rankAliasScattered, p, nil
	}
	if p := substringPos(host, q); p != nil {
		return rankHostSubstring, nil, p
	}
	if p := scatteredPos(host, q); p != nil {
		return rankHostScattered, nil, p
	}
	return rankNone, nil, nil
}

// Rank returns the hosts matching query, best first, each paired with the rune
// positions that matched. An empty query returns every host in config order.
// Ties keep config order, so the list never reshuffles for reasons the operator
// cannot see.
func Rank(hosts []sshconfig.Host, query string) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]Match, 0, len(hosts))
		for _, h := range hosts {
			out = append(out, Match{Host: h})
		}
		return out
	}

	type scored struct {
		match Match
		tier  int
		bonus int
		index int
	}
	matches := make([]scored, 0, len(hosts))
	for i, h := range hosts {
		tier, aliasPos, hostPos := scoreHost(h, q)
		if tier == rankNone {
			continue
		}
		// The bonus only discriminates inside the scattered tiers, which would
		// otherwise be entirely undifferentiated. Leaving it at zero everywhere
		// else keeps config order as the spec defines it.
		bonus := 0
		switch tier {
		case rankAliasScattered:
			bonus = positionScore(strings.ToLower(h.Alias), aliasPos)
		case rankHostScattered:
			bonus = positionScore(strings.ToLower(h.HostName), hostPos)
		}
		matches = append(matches, scored{
			match: Match{Host: h, AliasPos: aliasPos, HostNamePos: hostPos},
			tier:  tier,
			bonus: bonus,
			index: i,
		})
	}
	sort.SliceStable(matches, func(a, b int) bool {
		if matches[a].tier != matches[b].tier {
			return matches[a].tier < matches[b].tier
		}
		if matches[a].bonus != matches[b].bonus {
			return matches[a].bonus > matches[b].bonus
		}
		return matches[a].index < matches[b].index
	})

	out := make([]Match, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.match)
	}
	return out
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/picker/ -run TestRank -v -count=1`
Expected: PASS (14 tests)

- [x] **Step 5: Commit**

```bash
git add internal/picker/
git commit -m "feat(picker): add fuzzy host ranking with stable ordering" \
  -- internal/picker/
```

---

### Task 13: picker — model and keymap

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `internal/picker/model.go`
- Test: `internal/picker/model_test.go`

Background on Bubble Tea v2 (`charm.land/bubbletea/v2`), which differs from v1:

- `Update(tea.Msg) (tea.Model, tea.Cmd)` — returns `tea.Model`, not a concrete type
- `View() tea.View` — not `string`; build one with `tea.NewView(s)`
- Key presses arrive as `tea.KeyPressMsg{Code: rune-or-tea.KeyX, Mod: tea.ModCtrl, Text: "printable text"}`
- Special codes: `tea.KeyEnter`, `tea.KeyEsc`, `tea.KeyUp`, `tea.KeyDown`, `tea.KeyBackspace`

Tests drive `Update` directly — no terminal, no golden files, and each key is one assertion.

**On the keymap and fzf.** Two fzf editing keys are worth having and are added
here: `ctrl+u` clears the query, `ctrl+w` deletes the last word. fzf's `ctrl+n` /
`ctrl+p` (cursor down / up) are deliberately **not** adopted: `ctrl+n` is already
bound to "open a second pane for this host" in the spec
(`docs/specs/2026-09-09-ssh-picker-design.md:205-208`), and that binding wins because
it is a capability with no other key, while cursor movement already has three
ways to do it (arrows, `ctrl+j` / `ctrl+k`). Adding `ctrl+p` alone would leave an
asymmetric half-pair, so it is left out too. Do not "fix" this toward fzf.

**`Selection.Placement` stays a bare `string` — this was considered and declined.**
A review proposed `type Placement string` plus constants, on the argument that a
`"zoom"`/`"zoomed"` mismatch between this struct and its Task 15 consumer would
compile clean and silently fall through to a default. The argument is sound in
general; it loses here on cost and on redundancy. Cost: `Placement` crosses into
`herdrapi.OpenOpts.Placement`, which is a plain `string`, and comes back in from
a CLI flag in Task 18 — typing it adds conversions in tasks whose code is
already verified as a chain, so the change is not local to this file.
Redundancy: Task 18 already ships `RunConnectValidatesPlacement`, so an
unknown placement is rejected at the boundary where an operator can actually
introduce one. The four literals inside the picker are confined to one file and
asserted by `TestPlacementKeys`. If a third producer of placements ever appears,
revisit this — that is the point at which the type earns its cost.

- [x] **Step 1: Write the failing test**

```go
package picker

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// corpus comes from rank_test.go — same package.
func newTestModel() model {
	return newModel(Options{Hosts: corpus, Theme: theme.Default(), ShowPreview: true})
}

func press(m model, k tea.KeyPressMsg) model {
	next, _ := m.Update(k)
	return next.(model)
}

func typeRunes(m model, s string) model {
	for _, r := range s {
		m = press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func ctrl(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

// pressCmd is press, but it also hands back the Cmd Update returned instead of
// discarding it — tests that care whether a key quits the program need the
// actual Cmd, not just the model's quitting field.
func pressCmd(m model, k tea.KeyPressMsg) (model, tea.Cmd) {
	next, cmd := m.Update(k)
	return next.(model), cmd
}

// assertQuit fails unless cmd, when run, produces tea.QuitMsg. A key that only
// sets quitting=true without returning tea.Quit would leave bubbletea's own
// runtime loop running forever — quitting is a UI flag, tea.Quit is what
// actually stops the program.
func assertQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd = nil, want tea.Quit")
	}
	if msg := cmd(); !isQuit(msg) {
		t.Fatalf("cmd produced %T, want tea.QuitMsg", msg)
	}
}

func isQuit(msg tea.Msg) bool {
	_, ok := msg.(tea.QuitMsg)
	return ok
}

func TestTypingFiltersAndResetsCursor(t *testing.T) {
	m := newTestModel()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.cursor)
	}

	m = typeRunes(m, "dev")
	if m.query != "dev" {
		t.Fatalf("query = %q, want dev", m.query)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after refiltering", m.cursor)
	}
	if len(m.view) != 4 {
		t.Errorf("view = %v, want 4 matches", aliases(m.view))
	}
}

func TestBackspaceTrimsQuery(t *testing.T) {
	m := typeRunes(newTestModel(), "dev")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "de" {
		t.Fatalf("query = %q, want de", m.query)
	}
	// "de" matches the same 4 hosts as "dev" (devbox and dev both still match),
	// so a bare count would stay 4 even if backspace forgot to refilter. Only
	// the order tells them apart: "de" drops dev's exact-match tier, so devbox
	// (still an alias-prefix match) sorts ahead of it.
	if want := []string{"devbox", "dev", "nixos-dev", "prod-web"}; !equal(aliases(m.view), want) {
		t.Fatalf("view = %v, want %v", aliases(m.view), want)
	}
	// Backspace on an empty query is a no-op, not a crash.
	m.query = ""
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
}

func TestCursorClampsAtBothEnds(t *testing.T) {
	m := newTestModel()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 at the top", m.cursor)
	}
	for range corpus {
		m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.cursor != len(corpus)-1 {
		t.Errorf("cursor = %d, want %d at the bottom", m.cursor, len(corpus)-1)
	}
	m = press(m, ctrl('k'))
	if m.cursor != len(corpus)-2 {
		t.Errorf("ctrl+k did not move the cursor up: %d", m.cursor)
	}
	m = press(m, ctrl('j'))
	if m.cursor != len(corpus)-1 {
		t.Errorf("ctrl+j did not move the cursor down: %d", m.cursor)
	}
}

func TestPlacementKeys(t *testing.T) {
	tests := []struct {
		name      string
		key       tea.KeyPressMsg
		placement string
		forceNew  bool
	}{
		{"enter splits", tea.KeyPressMsg{Code: tea.KeyEnter}, "split", false},
		{"ctrl+t opens a tab", ctrl('t'), "tab", false},
		{"ctrl+z zooms", ctrl('z'), "zoomed", false},
		{"ctrl+n forces a new split", ctrl('n'), "split", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := typeRunes(newTestModel(), "nixos")
			m, cmd := pressCmd(m, tc.key)
			if m.chosen == nil {
				t.Fatal("chosen = nil, want a selection")
			}
			if m.chosen.Host.Alias != "nixos-dev" {
				t.Errorf("alias = %q, want nixos-dev", m.chosen.Host.Alias)
			}
			if m.chosen.Placement != tc.placement {
				t.Errorf("placement = %q, want %q", m.chosen.Placement, tc.placement)
			}
			if m.chosen.ForceNew != tc.forceNew {
				t.Errorf("forceNew = %v, want %v", m.chosen.ForceNew, tc.forceNew)
			}
			assertQuit(t, cmd)
		})
	}
}

func TestSelectingWithNoMatchesIsIgnored(t *testing.T) {
	m := typeRunes(newTestModel(), "zzzz")
	m, cmd := pressCmd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chosen != nil {
		t.Fatalf("chosen = %+v, want nil when nothing matches", m.chosen)
	}
	if cmd != nil {
		t.Fatal("cmd != nil, want no Cmd (and no quit) when nothing matches")
	}
}

func TestQuitKeysLeaveNoSelection(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, ctrl('c')} {
		m, cmd := pressCmd(newTestModel(), k)
		if m.chosen != nil {
			t.Errorf("chosen = %+v after quit key, want nil", m.chosen)
		}
		if !m.quitting {
			t.Error("quitting = false, want true")
		}
		assertQuit(t, cmd)
	}
}

// TestNonQuitKeysReturnNilCmd is the missing direction from the test above:
// every test in this file that presses a non-quit key uses press, which
// discards the Cmd, so a key that wrongly quit on every keystroke would still
// pass the rest of the suite. This asserts cmd == nil directly for keys that
// neither select nor quit.
func TestNonQuitKeysReturnNilCmd(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"printable rune", tea.KeyPressMsg{Code: 'd', Text: "d"}},
		{"non-quit special key", tea.KeyPressMsg{Code: tea.KeyUp}},
		{"unmapped ctrl chord", ctrl('x')},
		{"mapped non-quit ctrl chord", ctrl('o')},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, cmd := pressCmd(newTestModel(), tc.key)
			if cmd != nil {
				t.Fatalf("cmd = %v, want nil for %s", cmd, tc.name)
			}
		})
	}
}

func TestCtrlUClearsTheQuery(t *testing.T) {
	// "nixos" matches exactly one host, so the cursor is clamped to 0 before
	// ctrl+u even runs — a broken reset would pass trivially. "dev" matches
	// several, so moving Down first gives the reset something to prove.
	m := typeRunes(newTestModel(), "dev")
	if len(m.view) < 2 {
		t.Fatalf("test setup needs a query matching at least 2 hosts, got %v", aliases(m.view))
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor == 0 {
		t.Fatal("cursor did not move off 0 before ctrl+u, so the reset below proves nothing")
	}

	m = press(m, ctrl('u'))
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
	if len(m.view) != len(corpus) {
		t.Errorf("view = %v, want the whole list back", aliases(m.view))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestCtrlWDeletesTheLastWord(t *testing.T) {
	m := typeRunes(newTestModel(), "nixos-dev")
	m = press(m, ctrl('w'))
	if m.query != "nixos-" {
		t.Fatalf("query = %q, want \"nixos-\" — the separator stays, as in fzf", m.query)
	}
	if want := []string{"nixos-dev"}; !equal(aliases(m.view), want) {
		t.Errorf("view = %v, want %v", aliases(m.view), want)
	}
	// A second press eats the separator and the word in front of it.
	m = press(m, ctrl('w'))
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
	// "nixos-" also matches only nixos-dev, so this transition (1 match -> the
	// whole corpus) is the one that actually proves refilter ran — the "nixos-"
	// checkpoint above would look identical whether or not it did.
	if len(m.view) != len(corpus) {
		t.Errorf("view = %v, want the whole list back", aliases(m.view))
	}
	// On an already-empty query it is a no-op, not a panic.
	m = press(m, ctrl('w'))
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
}

func TestCtrlOTogglesPreview(t *testing.T) {
	m := newTestModel()
	if !m.preview {
		t.Fatal("preview = false, want the configured default of true")
	}
	m = press(m, ctrl('o'))
	if m.preview {
		t.Error("preview = true, want false after toggle")
	}
	m = press(m, ctrl('o'))
	if !m.preview {
		t.Error("preview = false, want true after a second toggle")
	}
}

func TestProbeResultsMarkHostsUp(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(probeMsg{Alias: "nixos-dev", Up: true})
	m = next.(model)
	if !m.up["nixos-dev"] {
		t.Error("nixos-dev not marked up")
	}
	if !m.probed["nixos-dev"] {
		t.Error("nixos-dev not marked probed")
	}
	if m.probed["alpha"] {
		t.Error("alpha marked probed without a result")
	}
}

func TestWindowSizeIsRecorded(t *testing.T) {
	next, _ := newTestModel().Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m := next.(model)
	if m.width != 100 || m.height != 40 {
		t.Fatalf("size = %dx%d, want 100x40", m.width, m.height)
	}
}

func TestProbeChannelDrainsWithoutBlocking(t *testing.T) {
	ch := make(chan probe.Result, 1)
	ch <- probe.Result{Alias: "alpha", Up: true}
	close(ch)

	m := newModel(Options{Hosts: corpus, Theme: theme.Default(), Probes: ch})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned no command, want a probe wait")
	}

	// Run the Cmd Init actually handed back, rather than hand-constructing the
	// message we expect — that would pass even if waitProbe never read ch.
	msg := cmd()
	pm, ok := msg.(probeMsg)
	if !ok {
		t.Fatalf("Init's cmd produced %T, want probeMsg", msg)
	}
	if pm != (probeMsg{Alias: "alpha", Up: true}) {
		t.Fatalf("probeMsg = %+v, want {Alias:alpha Up:true}", pm)
	}

	next, cmd := m.Update(pm)
	m = next.(model)
	if !m.up["alpha"] || !m.probed["alpha"] {
		t.Fatal("probeMsg did not mark alpha up and probed")
	}
	if cmd == nil {
		t.Fatal("Update did not re-arm the probe wait")
	}

	// The channel is now empty and closed, so running the re-armed Cmd for
	// real — not just asserting a hand-built probeClosedMsg{} — is what proves
	// waitProbe actually notices the close instead of blocking forever.
	msg = cmd()
	if _, ok := msg.(probeClosedMsg); !ok {
		t.Fatalf("re-armed cmd produced %T, want probeClosedMsg now the channel is closed", msg)
	}

	next, _ = m.Update(probeClosedMsg{})
	if next.(model).chosen != nil {
		t.Error("probeClosedMsg produced a selection")
	}
}

// TestQueryEditingIsRuneSafe uses a host of its own rather than adding a
// multi-byte alias to the shared corpus in rank_test.go — that corpus backs
// count assertions elsewhere (e.g. len(m.view) == len(corpus)) that a new
// fixture would silently change.
func TestQueryEditingIsRuneSafe(t *testing.T) {
	hosts := []sshconfig.Host{{Alias: "café-dëv", HostName: "10.0.0.1", Port: "22"}}
	m := newModel(Options{Hosts: hosts, Theme: theme.Default()})

	m = typeRunes(m, "café-dëv")
	if m.query != "café-dëv" {
		t.Fatalf("query = %q, want café-dëv", m.query)
	}

	// v is one byte, so dropping it proves nothing about rune-vs-byte slicing —
	// a buggy byte-slice backspace would look identical here.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "café-dë" {
		t.Fatalf("query = %q, want café-dë", m.query)
	}

	// ë is two bytes. A byte-slice backspace would drop only ë's second byte,
	// leaving a mangled "café-d\xc3" instead of the whole rune gone.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "café-d" {
		t.Fatalf("query = %q, want café-d — backspace must drop the whole rune ë, not one byte of it", m.query)
	}

	m = press(m, ctrl('w'))
	if m.query != "café-" {
		t.Fatalf("query = %q, want café-", m.query)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/picker/ -run TestTyping -count=1`
Expected: FAIL — `undefined: newModel`

- [x] **Step 3: Implement the model**

```go
package picker

import (
	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// Selection is what the operator picked and how they want it opened.
type Selection struct {
	Host      sshconfig.Host
	Placement string // split | tab | zoomed
	ForceNew  bool
}

// Options configures one picker run.
type Options struct {
	Hosts       []sshconfig.Host
	Theme       theme.Theme
	ShowPreview bool
	// OpenPanes maps alias → pane id for sessions already running, so the
	// picker can mark them and reuse them.
	OpenPanes map[string]string
	Probes    <-chan probe.Result
	Warnings  []string
}

type probeMsg probe.Result

type probeClosedMsg struct{}

type model struct {
	opts  Options
	query string
	// view holds Matches, not Hosts, because Task 14 highlights the runes that
	// matched and only Rank knows which those were.
	view     []Match
	cursor   int
	preview  bool
	up       map[string]bool
	probed   map[string]bool
	width    int
	height   int
	chosen   *Selection
	quitting bool
}

func newModel(o Options) model {
	return model{
		opts:    o,
		view:    Rank(o.Hosts, ""),
		preview: o.ShowPreview,
		up:      map[string]bool{},
		probed:  map[string]bool{},
	}
}

// waitProbe reads one probe result and re-arms itself, turning the probe
// channel into a stream of messages.
func waitProbe(ch <-chan probe.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return probeClosedMsg{}
		}
		return probeMsg(r)
	}
}

func (m model) Init() tea.Cmd {
	if m.opts.Probes == nil {
		return nil
	}
	return waitProbe(m.opts.Probes)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case probeMsg:
		// probed and up are maps, so these writes are visible through every
		// copy of the model that shares them. Deliberate: bubbletea holds
		// exactly one model and discards the predecessor on each Update, so
		// there is no observer of the older copies. Do not snapshot a model
		// and expect its probe state to stay frozen.
		m.probed[msg.Alias] = true
		m.up[msg.Alias] = msg.Up
		return m, waitProbe(m.opts.Probes)
	case probeClosedMsg:
		// Explicitly a no-op: the trailing return below would handle this
		// identically. The arm exists so that "probes finished" reads as a
		// message the model expects rather than one it silently ignores, and
		// so there is somewhere obvious to hang behavior if it ever needs
		// any. Deleting it changes nothing today — no test can catch that,
		// which is why this comment is here instead of a test.
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ctrl chords are dispatched before printable text. On this stack the two
	// orderings are in fact equivalent: ultraviolet clears Key.Text whenever
	// Mod is stronger than ModShift, and the special codes in the second
	// switch below (esc, enter, up, down, backspace) are non-printable and
	// never carry Text either. So this order is not currently load-bearing —
	// but keep it. It costs nothing, and the alternative relies on a library
	// invariant we do not control. What it is not is evidence that ctrl+t can
	// arrive as Text "t": it cannot, and a guard written on that assumption
	// would be guarding nothing.
	if k.Mod&tea.ModCtrl != 0 {
		return m.handleCtrl(k)
	}

	switch k.Code {
	case tea.KeyEsc:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEnter:
		return m.choose("split", false)
	case tea.KeyDown:
		return m.moveCursor(1), nil
	case tea.KeyUp:
		return m.moveCursor(-1), nil
	case tea.KeyBackspace:
		if m.query != "" {
			r := []rune(m.query)
			m.query = string(r[:len(r)-1])
			m = m.refilter()
		}
		return m, nil
	}

	if k.Text != "" {
		m.query += k.Text
		m = m.refilter()
	}
	return m, nil
}

// handleCtrl handles every ctrl-chord key. Split out of handleKey to keep each
// function under the project's line limit; behavior is unchanged.
func (m model) handleCtrl(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Code {
	case 'c':
		m.quitting = true
		return m, tea.Quit
	case 't':
		return m.choose("tab", false)
	case 'z':
		return m.choose("zoomed", false)
	case 'n':
		return m.choose("split", true)
	case 'o':
		m.preview = !m.preview
		return m, nil
	case 'j':
		return m.moveCursor(1), nil
	case 'k':
		return m.moveCursor(-1), nil
	case 'u':
		m.query = ""
		return m.refilter(), nil
	case 'w':
		m.query = deleteWord(m.query)
		return m.refilter(), nil
	}
	return m, nil
}

func (m model) refilter() model {
	m.view = Rank(m.opts.Hosts, m.query)
	m.cursor = 0
	return m
}

// deleteWord trims the last word off the query: first any trailing separators,
// then the run of non-separators before them. It reuses the scorer's separator
// set so "word" means the same thing while typing as it does while matching —
// ctrl+w on "nixos-dev" leaves "nixos-", which is still a useful query.
//
// Scan runes, not bytes. A byte scan would work today, but only because every
// separator in isSeparator is ASCII and no byte of a multi-byte UTF-8 rune can
// equal an ASCII byte — so it happens to stop exactly where a rune scan does.
// That equivalence is a property of the separator set, not of this function,
// and it ends the moment isSeparator gains a non-ASCII member, at which point
// a byte scan starts cutting runes in half. The conversion is what makes this
// correct independently of that set; do not remove it as a redundant
// allocation.
func deleteWord(q string) string {
	r := []rune(q)
	i := len(r)
	for i > 0 && isSeparator(r[i-1]) {
		i--
	}
	for i > 0 && !isSeparator(r[i-1]) {
		i--
	}
	return string(r[:i])
}

func (m model) moveCursor(delta int) model {
	next := m.cursor + delta
	if next < 0 || next >= len(m.view) {
		return m
	}
	m.cursor = next
	return m
}

func (m model) choose(placement string, forceNew bool) (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.view) {
		return m, nil
	}
	m.chosen = &Selection{Host: m.view[m.cursor].Host, Placement: placement, ForceNew: forceNew}
	m.quitting = true
	return m, tea.Quit
}

// View is replaced by the real renderer in Task 14. It exists here only because
// Update returns tea.Model, which forces model to satisfy the whole interface —
// including View — at every return site in this file. Task 14 deletes this stub
// and implements View in view.go; leaving both would be a redeclaration.
func (m model) View() tea.View { return tea.View{} }
```

**The `View` stub is required, not optional.** `tea.Model` in bubbletea v2.0.9 is `Init() Cmd; Update(Msg) (Model, Cmd); View() View`. Because `Update`, `handleKey`, and `handleCtrl` declare a `tea.Model` return type, Go demands the full interface at every `return m, nil` site in this file — not later, when the model reaches `tea.NewProgram`. Without the stub, `go build ./internal/picker/` fails with ten-plus `model does not implement tea.Model (missing method View)` errors starting at `model.go:82`, and not one test in this task can run. Task 13's tests never call `View`, so the stub cannot mask a rendering bug; it only lets the keymap tests execute.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/picker/ -v -count=1`
Expected: PASS — 13 model tests plus Task 12's 14 rank tests.

- [x] **Step 5: Commit**

```bash
git add internal/picker/
git commit -m "feat(picker): add bubbletea model with placement keymap" \
  -- internal/picker/
```

---

### Task 14: picker — rendering and Run

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `internal/picker/view.go`
- Modify: `internal/picker/model.go` — delete the `View` stub only (Step 3)
- Test: `internal/picker/view_test.go`

- [x] **Step 1: Write the failing test**

```go
package picker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// renderRaw sizes the model, then reads the rendered text with its styling
// intact. tea.View is a struct with a Content field — it has no String method.
func renderRaw(m model) string {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	return next.(model).View().Content
}

// render is renderRaw without the escapes, for assertions about content.
func render(m model) string {
	return stripANSI(renderRaw(m))
}

// requireStyling skips a styling assertion in an environment where lipgloss
// emits no escapes at all. There, every style renders to bare text and the
// highlight is unobservable by construction — a failure would say nothing about
// the code. The content assertions still run.
func requireStyling(t *testing.T, s lipgloss.Style) {
	t.Helper()
	if s.Render("x") == "x" {
		t.Skip("lipgloss emitted no styling here; highlighting is unobservable")
	}
}

func TestViewListsHostsAndQuery(t *testing.T) {
	out := render(typeRunes(newTestModel(), "dev"))
	for _, want := range []string{"dev", "devbox", "nixos-dev"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "alpha") {
		t.Errorf("view still shows a host that does not match %q:\n%s", "dev", out)
	}
}

func TestViewShowsOpenMarker(t *testing.T) {
	m := newModel(Options{
		Hosts:     corpus,
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"nixos-dev": "w5:pB"},
	})
	if out := render(m); !strings.Contains(out, openMarker) {
		t.Errorf("view missing the open-session marker %q:\n%s", openMarker, out)
	}
}

func TestViewMarkersDistinguishOpenUpAndDown(t *testing.T) {
	m := newModel(Options{
		Hosts:     corpus,
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"alpha": "w5:pB"},
	})
	m.probed["nixos-dev"], m.up["nixos-dev"] = true, true
	m.probed["devbox"], m.up["devbox"] = true, false

	out := render(m)
	for _, marker := range []string{openMarker, upMarker, downMarker} {
		if !strings.Contains(out, marker) {
			t.Errorf("view missing marker %q:\n%s", marker, out)
		}
	}
}

func TestViewMarksProxyJumpHostsSkipped(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "jumped", HostName: "10.9.9.9", Port: "22", ProxyJump: "bastion"}},
		Theme: theme.Default(),
	})
	out := render(m)
	if !strings.Contains(out, skipMarker) {
		t.Errorf("view missing the not-probed marker %q:\n%s", skipMarker, out)
	}
	if !strings.Contains(out, "via bastion") {
		t.Errorf("view should show the jump host instead of a direct address:\n%s", out)
	}
}

func TestViewOpenMarkerWinsOverReachability(t *testing.T) {
	m := newModel(Options{
		Hosts:     []sshconfig.Host{{Alias: "alpha", HostName: "10.0.0.1", Port: "22"}},
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"alpha": "w5:pB"},
	})
	m.probed["alpha"], m.up["alpha"] = true, false

	out := render(m)
	if !strings.Contains(out, openMarker) {
		t.Errorf("view missing %q:\n%s", openMarker, out)
	}
	if strings.Contains(out, downMarker) {
		t.Errorf("down marker shadowed the open marker:\n%s", out)
	}
}

func TestViewShowsPreviewForCursorHost(t *testing.T) {
	m := typeRunes(newTestModel(), "nixos")
	out := render(m)
	// "HostName " is the preview's label. Asserting on the hostname alone would
	// pass with no preview at all, because the host row renders the bare
	// hostname in its detail column.
	if !strings.Contains(out, "HostName 192.0.2.10") {
		t.Errorf("preview missing the resolved hostname:\n%s", out)
	}
}

func TestViewHidesPreviewWhenToggledOff(t *testing.T) {
	m := press(typeRunes(newTestModel(), "nixos"), ctrl('o'))
	// Assert the preview's label is gone, not the hostname: the row keeps
	// rendering the hostname whether the preview is up or not, so asserting the
	// hostname is absent is a test that can never pass.
	if out := render(m); strings.Contains(out, "HostName ") {
		t.Errorf("preview still rendered after toggle:\n%s", out)
	}
}

func TestViewOmitsThePortSuffixWhenPortIsUnset(t *testing.T) {
	// sshconfig.Parse always resolves Port, but Host is an exported struct that
	// anything can build directly. An unset Port must read as "no port shown",
	// not as a bare ":" hanging off the hostname.
	m := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "bare", HostName: "10.0.0.5"}},
		Theme: theme.Default(),
	})
	if out := render(m); strings.Contains(out, "10.0.0.5:") {
		t.Errorf("rendered a bare port separator:\n%s", out)
	}
}

func TestViewEmptyStateExplainsItself(t *testing.T) {
	out := render(typeRunes(newTestModel(), "zzzz"))
	if !strings.Contains(out, "no hosts match") {
		t.Errorf("view missing an empty-state message:\n%s", out)
	}
}

func TestViewNoConfigStateExplainsItself(t *testing.T) {
	out := render(newModel(Options{Theme: theme.Default()}))
	if !strings.Contains(out, "no ~/.ssh/config — nothing to pick") {
		t.Errorf("view missing a no-config message:\n%s", out)
	}
}

func TestViewShowsSourceProvenance(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{
			Alias: "inc", HostName: "10.0.0.1", Port: "22",
			SourceFile: "/Users/operator/.config/colima/ssh_config", SourceLine: 4,
		}},
		Theme:       theme.Default(),
		ShowPreview: true,
	})
	if out := render(m); !strings.Contains(out, "source /Users/operator/.config/colima/ssh_config:4") {
		t.Errorf("preview missing source provenance:\n%s", out)
	}
}

func TestViewShowsWarningCount(t *testing.T) {
	m := newModel(Options{
		Hosts:    []sshconfig.Host{{Alias: "a", HostName: "a", Port: "22"}},
		Theme:    theme.Default(),
		Warnings: []string{"config:3: include unreadable: nope"},
	})
	if out := render(m); !strings.Contains(out, "1 config warning") {
		t.Errorf("view missing the warning count:\n%s", out)
	}
}

func TestViewHighlightsMatchedRunes(t *testing.T) {
	// These assertions build the expected styles with lipgloss rather than
	// hardcoding escape bytes, so they test the behavior and not the encoding.
	// View() builds its accent style with Bold(true); match that here.
	th := theme.Default()
	raw := renderRaw(typeRunes(newModel(Options{Hosts: corpus, Theme: th}), "dev"))

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(th.Accent)).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(th.Text))
	requireStyling(t, accent)

	// devbox matched on its first three runes and is not the cursor row, so
	// "dev" carries the accent while "box" keeps the plain text color.
	if want := accent.Render("dev"); !strings.Contains(raw, want) {
		t.Errorf("matched runes are not accented; missing %q in:\n%q", want, raw)
	}
	if want := text.Render("box"); !strings.Contains(raw, want) {
		t.Errorf("unmatched runes lost the text color; missing %q in:\n%q", want, raw)
	}
	if bare := accent.Render("devbox"); strings.Contains(raw, bare) {
		t.Errorf("the whole alias was accented, so the highlight distinguishes nothing:\n%q", raw)
	}
}

func TestViewHighlightsHostNameOnAHostNameMatch(t *testing.T) {
	// prod-web matches only through its HostName "dev.example.com". Accenting
	// the alias would point the operator at the column that did not match.
	th := theme.Default()
	raw := renderRaw(typeRunes(newModel(Options{Hosts: corpus, Theme: th}), "example"))

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(th.Accent)).Bold(true)
	requireStyling(t, accent)

	if want := accent.Render("example"); !strings.Contains(raw, want) {
		t.Errorf("the hostname match is not accented; missing %q in:\n%q", want, raw)
	}
	if bad := accent.Render("prod-web"); strings.Contains(raw, bad) {
		t.Errorf("the alias was accented for a hostname-only match:\n%q", raw)
	}
}

func TestViewShowsKeyHints(t *testing.T) {
	out := render(newTestModel())
	for _, hint := range []string{"enter", "^t", "^z", "^u"} {
		if !strings.Contains(out, hint) {
			t.Errorf("view missing the %q hint:\n%s", hint, out)
		}
	}
}
```

Add the ANSI stripper as a test helper in the same file:

```go
// stripANSI removes styling so assertions test content, not colors.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/picker/ -run TestView -count=1`
Expected: FAIL — `m.View undefined`

- [x] **Step 3: Implement the view**

**First delete the `View` stub Task 13 left at the bottom of `internal/picker/model.go`** — the four-line comment block and `func (m model) View() tea.View { return tea.View{} }`. The real `View` below lands in `view.go`, in the same package, so leaving the stub in place is a `method redeclared` compile error. Nothing else in `model.go` changes.

```go
package picker

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Markers, per the spec. "a pane is already connected" and "the host answers on
// 22" are two different facts and get two different glyphs; conflating them
// would make the reuse affordance unreadable.
const (
	openMarker  = "▪" // a session pane exists (accent)
	upMarker    = "●" // TCP answered (green)
	downMarker  = "○" // no answer
	skipMarker  = "~" // ProxyJump, deliberately not probed
	blankMarker = " " // not probed yet
	maxRows     = 12
)

func (m model) View() tea.View {
	t := m.opts.Theme
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Accent)).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Text))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	upStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Up))

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", accent.Render("ssh"), text.Render(m.query+"▏"))

	switch {
	case len(m.opts.Hosts) == 0:
		b.WriteString(muted.Render("  no ~/.ssh/config — nothing to pick") + "\n")
	case len(m.view) == 0:
		b.WriteString(muted.Render("  no hosts match") + "\n")
	default:
		b.WriteString(m.renderRows(accent, text, muted, upStyle))
	}

	if m.preview && m.cursor < len(m.view) {
		b.WriteString(m.renderPreview(muted, text))
	}

	b.WriteString(muted.Render("  enter split · ^t tab · ^z zoom · ^n new · ^o preview · ^u clear · esc close") + "\n")
	if n := len(m.opts.Warnings); n > 0 {
		b.WriteString(muted.Render(fmt.Sprintf("  %d config warning(s)", n)) + "\n")
	}

	// Leave AltScreen and MouseMode at their zero values: the pane is already an
	// overlay, and the picker is keyboard-only.
	return tea.NewView(b.String())
}

// highlight renders s with the runes at pos in the hit style and everything else
// in base. Runs of same-styled runes are batched into one Render call, so the
// output carries one escape pair per run rather than one per rune.
//
// pos holds rune indices, so s is converted once and indexed as runes. Using
// byte offsets here would slice multi-byte runes in half.
func highlight(s string, pos []int, base, hit lipgloss.Style) string {
	if len(pos) == 0 {
		return base.Render(s)
	}
	matched := make(map[int]bool, len(pos))
	for _, p := range pos {
		matched[p] = true
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); {
		j := i
		for j < len(rs) && matched[j] == matched[i] {
			j++
		}
		style := base
		if matched[i] {
			style = hit
		}
		b.WriteString(style.Render(string(rs[i:j])))
		i = j
	}
	return b.String()
}

// window returns the visible slice of rows and the cursor's offset inside it,
// scrolling only when the cursor would fall outside.
func (m model) window() ([]Match, int) {
	if len(m.view) <= maxRows {
		return m.view, m.cursor
	}
	start := m.cursor - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(m.view) {
		start = len(m.view) - maxRows
	}
	return m.view[start : start+maxRows], m.cursor - start
}

func (m model) renderRows(accent, text, muted, upStyle lipgloss.Style) string {
	rows, cursor := m.window()
	var b strings.Builder
	for i, row := range rows {
		h := row.Host
		marker := blankMarker
		style := muted
		switch {
		case h.ProxyJump != "":
			marker = skipMarker
		case m.probed[h.Alias] && m.up[h.Alias]:
			marker, style = upMarker, upStyle
		case m.probed[h.Alias]:
			marker = downMarker
		}
		// "open" wins over reachability: it is the marker that changes what
		// enter does.
		if _, open := m.opts.OpenPanes[h.Alias]; open {
			marker, style = openMarker, accent
		}

		base := text
		pointer := "  "
		if i == cursor {
			// The cursor row goes bold rather than fully accented. The accent
			// color now means "this rune matched the query", so it cannot also
			// mean "this is the cursor" without swallowing the highlight.
			pointer = accent.Render("▸ ")
			base = text.Bold(true)
		}
		alias := highlight(h.Alias, row.AliasPos, base, accent)

		// The detail column is assembled from already-styled pieces rather than
		// styled at the end, so the hostname's highlight positions stay aligned
		// with the hostname itself when a user or port is prepended.
		detail := highlight(h.HostName, row.HostNamePos, muted, accent)
		if h.User != "" {
			detail = muted.Render(h.User+"@") + detail
		}
		// Both halves matter. Parse defaults Port to "22", so the second clause
		// hides the port that every host has; the first covers a Host built
		// directly rather than parsed, where Port is "" and a lone ":" would
		// otherwise trail the hostname.
		if h.Port != "" && h.Port != "22" {
			detail += muted.Render(":" + h.Port)
		}
		if h.ProxyJump != "" {
			// A jump host replaces the address outright: the address is not what
			// the connection actually reaches.
			detail = muted.Render("via " + h.ProxyJump)
		}
		fmt.Fprintf(&b, "%s%s %s  %s\n", pointer, style.Render(marker), alias, detail)
	}
	if len(m.view) > len(rows) {
		fmt.Fprintf(&b, "%s\n", muted.Render(fmt.Sprintf("  … %d more", len(m.view)-len(rows))))
	}
	return b.String()
}

func (m model) renderPreview(muted, text lipgloss.Style) string {
	h := m.view[m.cursor].Host
	lines := []string{"HostName " + h.HostName, "Port " + h.Port}
	if h.User != "" {
		lines = append(lines, "User "+h.User)
	}
	if h.IdentityFile != "" {
		lines = append(lines, "IdentityFile "+h.IdentityFile)
	}
	if h.ProxyJump != "" {
		lines = append(lines, "ProxyJump "+h.ProxyJump)
	}
	if h.SourceFile != "" {
		// Provenance matters as soon as Include is in play: "which file did this
		// host actually come from" is otherwise unanswerable from the picker.
		lines = append(lines, fmt.Sprintf("source %s:%d", h.SourceFile, h.SourceLine))
	}
	var b strings.Builder
	b.WriteString(muted.Render("  ─────") + "\n")
	for _, l := range lines {
		b.WriteString("  " + text.Render(l) + "\n")
	}
	return b.String()
}
```

- [x] **Step 4: Add `Run`**

Append to `internal/picker/view.go`:

```go
// Run shows the picker and blocks until the operator selects or quits. The
// second return value is false when they quit without choosing.
func Run(o Options) (Selection, bool, error) {
	p := tea.NewProgram(newModel(o))
	final, err := p.Run()
	if err != nil {
		return Selection{}, false, err
	}
	m, ok := final.(model)
	if !ok || m.chosen == nil {
		return Selection{}, false, nil
	}
	return *m.chosen, true, nil
}
```

Note: no `tea.WithAltScreen` — the pane is already an overlay, so a second alt-screen switch just adds a flash.

- [x] **Step 5: Run test to verify it passes**

Run: `go test ./internal/picker/ -v -count=1`
Expected: PASS. `TestViewShowsPreviewForCursorHost` requires `corpus`'s `nixos-dev` HostName of `192.0.2.10` from Task 12, and its `Port: "22"` — a corpus host with no Port renders a bare trailing `":"`.

**The two preview tests must assert on the preview's `HostName ` label, not on the hostname.** The host row renders the bare hostname in its detail column, so the hostname is on screen whether the preview is up or not. Asserting `Contains(out, "192.0.2.10")` for the shown case passes even with `renderPreview` deleted, and asserting `!Contains(out, "192.0.2.10")` for the hidden case can never pass at all. Verified both ways by compiling Tasks 12-14 together: with the hostname assertions, neutering `renderPreview` to `_ = m.renderPreview(...)` left the suite green and the toggle test failed unconditionally. With the label assertions, that mutation fails `TestViewShowsPreviewForCursorHost` and dropping the `m.preview &&` guard fails `TestViewHidesPreviewWhenToggledOff` — each mutation killed by exactly its own test. Do not "simplify" these back to the bare hostname.

**This PASS is not the end of the picker's rendering work; Tasks 14b and 14c finish it.** What passes here is a renderer that never reads `m.height`. `maxRows` is a constant and `Update` records the reported height without anything consuming it, so at 30 hosts with the preview up `View` draws an 18-line frame at height 8, at height 10, at height 24 and at height 200 alike. Because the picker runs inline rather than in an alternate screen, a frame taller than the pane is not clipped by the terminal — it scrolls the pane, so the visible failure is a smear through the operator's scrollback rather than a truncated list. Task 14b derives the row budget from the reported height and measures the chrome; Task 14c lets the preview yield its lines when the pane is too short to hold both it and the host list. Neither is optional, and this green suite is not evidence against either: the plan's own success criterion here certifies a frame that overflows any pane shorter than 18 lines. Do not record the picker's rendering as done at this commit.

- [x] **Step 6: Verify it builds and vet is clean**

Run: `go vet ./...`
Expected: no output

- [x] **Step 7: Commit**

The picker's rendering is not finished at this commit. Continue into Task 14b and Task 14c, which fix the height overflow described in Step 5, before treating the picker as complete.

```bash
git add internal/picker/
git commit -m "feat(picker): render host list, status markers, and preview" \
  -- internal/picker/
```

---

### Task 14b: picker — size the row budget to the reported terminal height

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Modify: `internal/picker/view.go` — measure the chrome, derive the row budget from `m.height`
- Test: `internal/picker/view_test.go` — height helpers, a tall-preview fixture, sizing and discard tests

Task 14 ends on a green suite and a renderer that ignores the terminal. `maxRows = 12` is a constant and `m.height` is recorded by `Update` and then never read, so `View` draws the same frame in every pane. Measured at 30 hosts with the preview up: 18 lines at height 8, 18 at height 10, 18 at height 24, 18 at height 200.

The consequence is worse than a clipped list. The picker runs inline, not in an alternate screen — `Run` omits `tea.WithAltScreen` and `View` leaves `AltScreen` and `MouseMode` at their zero values — so a frame taller than the pane is not truncated by the terminal, it scrolls the pane it was drawn into. The failure is a smear across the operator's scrollback, not a tidy truncation, which is why "it looked fine in my full-screen terminal" is not evidence about this bug.

The fix derives the row budget from `m.height`. It has to _measure_ the chrome rather than write it down as a constant, because the preview's height varies with how many fields the cursor host happens to set: five chrome lines at the two-field minimum, nine with all six populated. Hand-counting the fixed chrome gives seven; the measured figure is five, and that two-line gap is the whole defect in miniature.

The `maxRows` ceiling stays. In any pane with room to spare the budget saturates at the ceiling and the output is byte-identical to Task 14's, so this is a fix for short panes and nothing else moves. `maxRows` also keeps its second job as the `height == 0` fallback, so the first frame — drawn before any `WindowSizeMsg` arrives — is the frame a roomy pane would draw rather than an empty list.

> **Correction (`ed23d73`): the ceiling did not stay.** The paragraph above is
> the historical record of Task 14b's decision and is left as written. What it
> got wrong is the premise, not the arithmetic: it treated the ceiling as free
> because "in any pane with room to spare the output is byte-identical to Task
> 14's" — which is true, and is the problem. The picker does not choose its own
> size. herdr sizes the popup from `width`/`height` on the keybinding and hands
> the plugin the pane that results, so a ceiling under that pane does not keep
> the box small, it leaves it empty. At the operator's `60%` binding two thirds
> of the popup rendered as void, and the operator said so. The ceiling is gone;
> the constant survives as `fallbackRows`, which is only the second job the
> paragraph describes — the `height == 0` first frame. Nothing else in Task 14b
> changes: the chrome is still measured rather than written down, and the floor
> is still one host row.

Note on width: `m.width` stays written and deliberately unread. Pane width is not part of this fix, and the spec's `### Layout` section states that pane width is deliberately unused — so this is the spec's position, not a departure from it. Deviation 7, which recorded it as a departure, is retired for that reason.

- [x] **Step 1: Add the height-aware test helpers**

`internal/picker/view_test.go` — the import block gains `fmt`, for the generated aliases in the new fixture. Replace the existing import block with:

```go
import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)
```

Then replace `renderRaw` and its doc comment with the expanded comment and three new helpers. `renderRaw` pins a height of 30, which cannot express the short-pane case at all, so the sizing tests need `renderAt`. `lineCount` uses `strings.Count(s, "\n")` with no `+1`: every line `View` emits is newline-terminated, so the count of newlines _is_ the number of lines the terminal is asked to draw. `len(strings.Split(...))` is one greater and reports 19 for a frame that is really 18 — an off-by-one that reads as confident arithmetic.

```go
// renderRaw sizes the model, then reads the rendered text with its styling
// intact. tea.View is a struct with a Content field — it has no String method.
//
// Height 30 is a pane with room to spare, so every test using this helper sees
// the full maxRows ceiling and none of them are height-sensitive. Tests about
// short panes must use renderAt, which takes the height explicitly.
//
// Two discards here, both discharged by tests rather than left implicit: the
// tea.Cmd from Update (see TestWindowSizeMsgReturnsNoCmd) and every tea.View
// field except Content (see TestViewLeavesAltScreenAndMouseOff).
func renderRaw(m model) string {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	return next.(model).View().Content
}

// renderAt renders at an explicit terminal height. renderRaw's fixed 30 rows
// cannot express the short-pane case, which is the whole point of sizing.
func renderAt(m model, height int) string {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: height})
	return next.(model).View().Content
}

// lineCount counts rendered lines. Every line View emits is newline-terminated,
// so this is the count of lines the terminal is asked to draw — deliberately not
// len(strings.Split(...)), which is one greater because of the empty element
// after the trailing newline.
func lineCount(s string) int { return strings.Count(s, "\n") }

// manyHosts builds n hosts, more than any row budget under test, so the list is
// always long enough to be truncated.
//
// What this fixture holds constant, and why: every field except Alias and
// HostName is identical, and User/IdentityFile/ProxyJump/SourceFile are all
// unset. That pins the preview at its two-field minimum, which fixes the chrome
// at a known 5 lines and makes the arithmetic in these assertions checkable by
// hand. The height tests are about the row *count*, so the preview's own
// variable height is a confound here — TestViewRowBudgetShrinksForATallerPreview
// varies it on purpose instead.
func manyHosts(n int) []sshconfig.Host {
	out := make([]sshconfig.Host, n)
	for i := range out {
		out[i] = sshconfig.Host{
			Alias:    fmt.Sprintf("host%02d", i),
			HostName: fmt.Sprintf("10.0.0.%d", i+1),
			Port:     "22",
		}
	}
	return out
}
```

- [x] **Step 2: Add the tall-preview fixture and the five sizing tests**

Append to `internal/picker/view_test.go`. `manyHosts` pins the preview at its two-field minimum; `tallPreviewHosts` populates all six so the chrome is nine, and the two fixtures together are what stop the chrome arithmetic from being fitted to a single shape.

```go
// tallPreviewHosts is manyHosts with every optional field set on the host the
// cursor starts on, so the preview renders its maximum six fields instead of
// the minimum two. Only host00 is populated: the preview shows the cursor host,
// so it is the only one whose fields affect the chrome.
func tallPreviewHosts(n int) []sshconfig.Host {
	hosts := manyHosts(n)
	hosts[0].User = "root"
	hosts[0].IdentityFile = "/keys/id_ed25519"
	hosts[0].ProxyJump = "bastion"
	hosts[0].SourceFile = "/Users/operator/.ssh/config"
	hosts[0].SourceLine = 12
	return hosts
}

// TestChromeLinesTracksThePreviewHeight pins the chrome arithmetic directly.
// The end-to-end fit tests below would pass with a hardcoded constant in any
// terminal tall enough for the ceiling to bind instead, so the computation
// needs its own assertion. The numbers are hand-checkable: query header + key
// hints, plus one line per extra element.
func TestChromeLinesTracksThePreviewHeight(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		name string
		opts Options
		want int
	}{
		{"preview off", Options{Hosts: manyHosts(3), Theme: th}, 2},
		{"minimal preview", Options{Hosts: manyHosts(3), Theme: th, ShowPreview: true}, 5},
		{"six-field preview", Options{Hosts: tallPreviewHosts(3), Theme: th, ShowPreview: true}, 9},
		{
			"warning line",
			Options{Hosts: manyHosts(3), Theme: th, Warnings: []string{"config:3: bad"}},
			3,
		},
	} {
		if got := newModel(tc.opts).chromeLines(); got != tc.want {
			t.Errorf("%s: chromeLines() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestViewFitsTheReportedTerminalHeight is the regression test for the defect:
// the picker is a floating overlay, so a frame taller than the pane is a
// clipped or corrupted frame, not a scroll.
func TestViewFitsTheReportedTerminalHeight(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		name    string
		hosts   []sshconfig.Host
		preview bool
		heights []int
	}{
		{"preview off", manyHosts(30), false, []int{5, 8, 10, 12, 24, 200}},
		{"minimal preview", manyHosts(30), true, []int{8, 10, 12, 24, 200}},
		// Chrome is 9 here, so 11 is the shortest pane that can hold one row
		// plus the overflow notice. Below that the one-row floor wins by
		// design and the frame is allowed to exceed the height.
		{"six-field preview", tallPreviewHosts(30), true, []int{11, 12, 16, 24, 200}},
	} {
		m := newModel(Options{Hosts: tc.hosts, Theme: th, ShowPreview: tc.preview})
		for _, h := range tc.heights {
			if got := lineCount(renderAt(m, h)); got > h {
				t.Errorf("%s: height %d rendered %d lines, overflows by %d:\n%s",
					tc.name, h, got, got-h, stripANSI(renderAt(m, h)))
			}
		}
	}
}

// TestViewShowsMoreRowsInATallerPane is the other half of the fit assertion.
// Fitting alone is satisfied by always rendering a single row, which would be a
// different bug; the output has to actually track the height.
func TestViewShowsMoreRowsInATallerPane(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default(), ShowPreview: true})
	short, tall := lineCount(renderAt(m, 10)), lineCount(renderAt(m, 20))
	if short >= tall {
		t.Errorf("10-line pane rendered %d lines and 20-line pane %d; "+
			"the row count is not tracking the height", short, tall)
	}
}

// TestViewInATallPaneMatchesTheUnsizedFallback pins both the maxRows ceiling and
// the height == 0 fallback in one assertion, without a magic line count. It is
// what makes this change a fix for short panes only: given room to spare, the
// output is byte-identical to what the picker drew before it consulted height
// at all.
func TestViewInATallPaneMatchesTheUnsizedFallback(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default(), ShowPreview: true})
	unsized := m.View().Content // no WindowSizeMsg yet, so height is 0
	if tall := renderAt(m, 200); tall != unsized {
		t.Errorf("a 200-line pane and the unsized first frame differ\ntall:\n%s\nunsized:\n%s",
			stripANSI(tall), stripANSI(unsized))
	}
	if !strings.Contains(stripANSI(unsized), "… 18 more") {
		t.Errorf("unsized fallback did not render maxRows rows:\n%s", stripANSI(unsized))
	}
}

// TestViewKeepsARowInAPaneTooShortForChrome covers the floor. In a pane this
// short the frame cannot fit, and showing the operator zero hosts would be the
// worse failure, so one row survives.
func TestViewKeepsARowInAPaneTooShortForChrome(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default(), ShowPreview: true})
	if out := stripANSI(renderAt(m, 3)); !strings.Contains(out, "host00") {
		t.Errorf("no host row survived a 3-line pane; the one-row floor is gone:\n%s", out)
	}
}
```

- [x] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/picker/ -count=1`
Expected: a build failure, not an assertion failure — the tests name a method that does not exist yet.

```
# github.com/purehate/herdr-plugin-ssh/internal/picker [github.com/purehate/herdr-plugin-ssh/internal/picker.test]
internal/picker/view_test.go:324:31: newModel(tc.opts).chromeLines undefined (type model has no field or method chromeLines)
FAIL	github.com/purehate/herdr-plugin-ssh/internal/picker [build failed]
FAIL
```

Run the whole package, never `go test -run TestViewFitsTheReportedTerminalHeight`. `-run` takes an unanchored regex, and a run that selects nothing still exits 0 and prints `ok ... [no tests to run]` — that silence is not a pass.

- [x] **Step 4: Measure the chrome instead of assuming it**

`internal/picker/view.go` — the import block gains `sshconfig`, because the preview's field list is now computed from a `sshconfig.Host` in a helper rather than inline in `renderPreview`:

```go
import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)
```

Then replace the marker comment and `const` block with the same constants plus four new methods. `showPreview` is the single predicate for "is the preview on screen", so the code that measures the preview and the code that draws it cannot disagree. `previewFields` returns the preview's content, one line per populated field, and `chromeLines` counts what `renderPreview` will draw by calling the same function. `visibleRows` subtracts the chrome from `m.height`, reserves one line for the `… N more` notice when the list will overflow, then clamps: floor 1, ceiling `maxRows`.

The floor is a deliberate trade, not an oversight. In a pane too short for the chrome alone, a picker showing zero hosts is worse than one that overflows by a line, so the floor wins over the fit and the comment says so.

```go
// Markers, per the spec. "a pane is already connected" and "the host answers on
// 22" are two different facts and get two different glyphs; conflating them
// would make the reuse affordance unreadable.
const (
	openMarker  = "▪" // a session pane exists (accent)
	upMarker    = "●" // TCP answered (green)
	downMarker  = "○" // no answer
	skipMarker  = "~" // ProxyJump, deliberately not probed
	blankMarker = " " // not probed yet
	// maxRows is the ceiling on host rows, not the count. A short pane gets
	// fewer — see visibleRows. It stays a ceiling so the picker remains the
	// floating box the spec calls it rather than growing into a full-screen
	// list in a tall terminal.
	maxRows = 12
)

// showPreview reports whether View will draw the preview block. The cursor
// bound is load-bearing: renderPreview indexes m.view[m.cursor].
func (m model) showPreview() bool { return m.preview && m.cursor < len(m.view) }

// previewFields is the preview's content, one "Key value" line per populated
// field. chromeLines counts these and renderPreview draws them, so the row
// budget cannot disagree with what actually reaches the screen — the preview's
// height varies with how many fields the cursor host happens to set, which is
// why the chrome is computed here instead of written down as a constant.
func previewFields(h sshconfig.Host) []string {
	lines := []string{"HostName " + h.HostName, "Port " + h.Port}
	if h.User != "" {
		lines = append(lines, "User "+h.User)
	}
	if h.IdentityFile != "" {
		lines = append(lines, "IdentityFile "+h.IdentityFile)
	}
	if h.ProxyJump != "" {
		lines = append(lines, "ProxyJump "+h.ProxyJump)
	}
	if h.SourceFile != "" {
		// Provenance matters as soon as Include is in play: "which file did this
		// host actually come from" is otherwise unanswerable from the picker.
		lines = append(lines, fmt.Sprintf("source %s:%d", h.SourceFile, h.SourceLine))
	}
	return lines
}

// chromeLines counts every line View draws that is not a host row: the query
// header, the key hints, the optional warning count, and the preview block.
//
// The "… N more" overflow notice is deliberately not counted here. Whether it
// appears depends on the row budget this function is used to compute, so
// visibleRows accounts for it instead and this stays a function of the model
// alone.
func (m model) chromeLines() int {
	n := 2 // query header + key hints
	if len(m.opts.Warnings) > 0 {
		n++
	}
	if m.showPreview() {
		n += 1 + len(previewFields(m.view[m.cursor].Host)) // separator + fields
	}
	return n
}

// visibleRows is how many host rows fit in the terminal height the last
// WindowSizeMsg reported, once chrome has taken its share. The picker is a
// floating overlay, so overflow is not a scroll — it is a clipped frame.
func (m model) visibleRows() int {
	if m.height <= 0 {
		// No WindowSizeMsg has arrived yet. Fall back to the ceiling rather
		// than rendering an empty list on the first frame.
		return maxRows
	}
	n := m.height - m.chromeLines()
	if len(m.view) > n {
		// Truncating costs a line for the "… N more" notice, so it comes out
		// of the row budget instead of overflowing past the frame.
		n--
	}
	if n > maxRows {
		n = maxRows
	}
	if n < 1 {
		// A picker showing zero hosts is worse than one that overflows, so the
		// floor wins over the fit. In a pane this short the frame can still
		// exceed the height by a line; that is the deliberate trade.
		n = 1
	}
	return n
}
```

- [x] **Step 5: Spend the budget in View, window, and renderPreview**

Three edits, all in `internal/picker/view.go`. In `View`, the preview guard becomes the shared predicate so the drawn preview and the measured preview are the same decision:

```go
	if m.showPreview() {
		b.WriteString(m.renderPreview(muted, text))
	}
```

`window` takes the budget as a parameter instead of reading `maxRows` directly — it is the function that decides how many rows exist, so it is where the budget has to land:

```go
// window returns the visible slice of rows and the cursor's offset inside it,
// scrolling only when the cursor would fall outside.
func (m model) window() ([]Match, int) {
	rows := m.visibleRows()
	if len(m.view) <= rows {
		return m.view, m.cursor
	}
	start := m.cursor - rows/2
	if start < 0 {
		start = 0
	}
	if start+rows > len(m.view) {
		start = len(m.view) - rows
	}
	return m.view[start : start+rows], m.cursor - start
}
```

And `renderPreview` builds its lines from `previewFields` rather than rebuilding the list, which is what makes `chromeLines` a measurement rather than a second opinion:

```go
func (m model) renderPreview(muted, text lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(muted.Render("  ─────") + "\n")
	for _, l := range previewFields(m.view[m.cursor].Host) {
		b.WriteString("  " + text.Render(l) + "\n")
	}
	return b.String()
}
```

- [x] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/picker/ -count=1`
Expected: `ok  	github.com/purehate/herdr-plugin-ssh/internal/picker`

The rendered frame now tracks the pane. Measured at 30 hosts with the preview up, before and after:

| Reported height | Lines drawn before | Lines drawn after |
| --------------- | ------------------ | ----------------- |
| 8               | 18                 | 8                 |
| 10              | 18                 | 10                |
| 24              | 18                 | 18                |
| 200             | 18                 | 18                |

Heights 24 and 200 are unchanged, which is the `maxRows` ceiling doing its job: panes with room render exactly what Task 14 rendered.

Fault localization, measured by mutating `view.go` in a clean export of this commit and running the full package suite each time — reverted and diffed byte-identical between mutants:

| Mutation                                                                           | Tests that fail                                                                  |
| ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `visibleRows`: `if m.height <= 0` → `>= 0`, so the height is never consulted       | `TestViewFitsTheReportedTerminalHeight`, `TestViewShowsMoreRowsInATallerPane`    |
| `chromeLines`: drop the preview term, so the chrome is assumed instead of measured | `TestChromeLinesTracksThePreviewHeight`, `TestViewFitsTheReportedTerminalHeight` |
| `visibleRows`: delete the `if n > maxRows { n = maxRows }` ceiling                 | `TestViewInATallPaneMatchesTheUnsizedFallback`                                   |

Each mutation is killed by the test written for it, and the ceiling mutation is caught by exactly one test — that is the signal these tests exist to give. Two further mutations were run and behaved the same way: deleting the overflow-notice reserve fails only `TestViewFitsTheReportedTerminalHeight`, and turning the floor's `n = 1` into `n = 0` fails only `TestViewKeepsARowInAPaneTooShortForChrome`.

- [x] **Step 7: Discharge the render discards and close the fixture hole**

This step is not part of the sizing fix and is easy to skip, which is why it is its own step. Two discards in the render path are currently unexplained, and one fixture gap hides a whole column.

`renderRaw` throws away the `tea.Cmd` from `Update` and every `tea.View` field except `Content`. `TestNonQuitKeysReturnNilCmd` covers `KeyPressMsg` only, so nothing yet stops a model from returning a stray command on `WindowSizeMsg`, and nothing asserts the picker stays out of the alternate screen — which is the exact property that makes an over-tall frame smear rather than clip. Both get a test.

The fixture gap: every host fixture in this package sets `Port: "22"` and no `User`, so the detail column's user and non-default-port rendering has never been exercised by any test. That is not a cosmetic hole — it is the same class of blind spot as the chrome miscount, a fixture agreeing with the code about a shape no real config produces. Append:

```go
// TestWindowSizeMsgReturnsNoCmd discharges the tea.Cmd that renderRaw and
// renderAt drop. Nothing else in the package asserts the Cmd for a resize —
// TestNonQuitKeysReturnNilCmd covers KeyPressMsg only — so without this a model
// that returned tea.Quit on every resize would pass the whole suite.
func TestWindowSizeMsgReturnsNoCmd(t *testing.T) {
	if _, cmd := newTestModel().Update(tea.WindowSizeMsg{Width: 90, Height: 30}); cmd != nil {
		t.Errorf("Update(WindowSizeMsg) returned %T, want nil", cmd())
	}
}

// TestViewRendersUserAndNonDefaultPortInTheDetailColumn varies the two fields
// every other fixture in this package holds constant. Every host in corpus,
// manyHosts and tallPreviewHosts has Port "22", and only tallPreviewHosts sets
// User at all — and nothing asserted it. So both detail-column branches did no
// work under test: deleting either the ":"+Port suffix or the User+"@" prefix
// left the whole suite green. A real config sets both, and the detail column is
// where the operator confirms what they are about to connect to.
//
// Port "2222" rather than something like "220" on purpose: it shares a prefix
// with the "22" default, so a prefix comparison in place of the equality check
// would render nothing here and fail.
func TestViewRendersUserAndNonDefaultPortInTheDetailColumn(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "gw", HostName: "10.0.0.3", User: "root", Port: "2222"}},
		Theme: theme.Default(),
	})
	out := render(m)
	if !strings.Contains(out, "root@10.0.0.3") {
		t.Errorf("detail column missing the user prefix:\n%s", out)
	}
	if !strings.Contains(out, "10.0.0.3:2222") {
		t.Errorf("detail column missing the non-default port suffix:\n%s", out)
	}
}

// TestViewLeavesAltScreenAndMouseOff discharges the other discard in the render
// helpers: they read tea.View.Content and ignore its eleven other fields.
// View's comment claims AltScreen and MouseMode are deliberately zero, and Run
// omits tea.WithAltScreen to match. Setting either would flash or grab the
// terminal underneath what is meant to be a keyboard-only overlay.
//
// The nine remaining fields — OnMouse, Cursor, Background/ForegroundColor,
// WindowTitle, ProgressBar, ReportFocus, DisableBracketedPasteMode and
// KeyboardEnhancements — get no assertion deliberately. View reaches the
// terminal through tea.NewView and one Content string and never names them, so
// a test on those would assert the absence of code rather than any behavior.
// Add one the moment View starts setting one.
func TestViewLeavesAltScreenAndMouseOff(t *testing.T) {
	next, _ := newTestModel().Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	v := next.(model).View()
	if v.AltScreen {
		t.Error("AltScreen is set; the picker is an overlay, not a full-screen app")
	}
	if v.MouseMode != 0 {
		t.Errorf("MouseMode = %d, want 0; the picker is keyboard-only", v.MouseMode)
	}
}
```

- [x] **Step 8: Verify the full suite, vet, and gofmt**

```bash
go test ./... -count=1
go vet ./...
gofmt -l internal/ cmd/
```

Expected: tests pass, `go vet` prints nothing, `gofmt -l` prints nothing. `go test`, `go vet` and `gofmt -l` are the gates here; `go build ./...` is not, and what it does depends on which tree you are standing in. Executing this plan in order, `cmd/herdr-ssh` does not exist until Task 15, so the build succeeds and says nothing about the picker. At `de00747`, the commit this task reconstructs, `cmd/herdr-ssh` already existed without `func main()`, so there the same command exits 1 with `runtime.main_main·f: function main is undeclared in the main package`. Both readings were measured on clean exports rather than inferred from the standing rule, which describes plan order only.

This task adds 8 tests to `internal/picker` — 5 in Step 2 and 3 in Step 7. Count them, do not infer them:

```bash
go test ./internal/picker/ -list '.*' -count=1 | grep -c '^Test'
```

Measured at this commit the command prints 55. Treat the delta of 8 as this task's contract and the absolute as informational: the absolute also counts tests from sibling commits that are not part of this task, so it will differ if you arrive here by a different route. Measure before and after rather than trusting either number.

- [x] **Step 9: Commit**

```bash
git add internal/picker/
git commit -m "fix(picker): size the row budget to the reported terminal height" \
  -- internal/picker/
```

---

### Task 14c: picker — let the preview yield its lines in a short pane

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Modify: `internal/picker/view.go` — split the chrome, let the preview shed fields and disappear
- Test: `internal/picker/view_test.go` — a parsed fixture, and the fit test swept instead of sampled

Task 14b sized the row budget and the budget is correct. What it did not do is let the _preview_ give anything back. The preview's height is an input to the chrome and the chrome comes off the top; when the pane is too short for chrome plus rows, the row budget hits its floor of 1 and the frame still overflows, because the preview is holding lines the host list needs.

Two facts about `sshconfig.Parse` are the reason this survived Task 14b's tests. `internal/sshconfig/sshconfig.go:408` defaults `HostName` to the alias and `:410` defaults `Port` to `"22"`, and the parser also sets `SourceFile`/`SourceLine` provenance on every host. So a **parsed** host and a hand-built `sshconfig.Host{...}` literal are not the same shape: a parsed preview never has fewer than three fields, while `manyHosts` has two. Every fixture in the package agreed with the code about a preview height that no real config ever renders. A real host with `User` and `IdentityFile` set overflowed a 9-line pane by one line, and no test saw it.

The second symptom is worse to diagnose than the first. The chrome depends on the _cursor_ host's field count, so on a parsed corpus where field counts alternate it was 9 lines on one row and 10 on the next. The overflow appeared while arrowing down the list and was not reproducible from the pane size alone — the same height fit one row and overflowed the next.

The fix makes the preview the elastic element. `fixedChrome` is the part that cannot shrink; `previewLines` is granted whatever is left and sheds fields from the bottom — provenance first, since it is the least load-bearing line — and returns 0 rather than draw a separator with nothing under it. Lines the preview declines go back to the host list. This is what the spec's `### Layout` section already commits to: the preview is the only optional element, so in a pane too short for both it yields, and where it cannot fit at all `^o` does nothing.

**No backstop clamp.** Do not add a final "truncate the finished frame to `m.height`" pass, and do not add one later when a new fixture overflows. A clamp would satisfy the invariant test while hiding the regression the test exists to catch: the frame would fit and the layout would still be wrong, with content silently dropped off the bottom and no test able to tell. Every chrome shape must render exactly `max(height, minFrame)` by construction. This is the first "fix" a reader reaches for; it is prohibited.

- [x] **Step 1: Widen the test imports and correct the manyHosts contract**

`internal/picker/view_test.go` — the import block gains `os` and `path/filepath` for the fixture that writes a config and parses it. Replace the import block with:

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)
```

Then replace `manyHosts`' doc comment. Its closing sentence points at `TestViewRowBudgetShrinksForATallerPreview`, which is not the name of any test in this package; the test that varies the preview height is `TestChromeLinesTracksThePreviewHeight`. The replacement fixes the reference and, more importantly, writes down the parsed-versus-hand-built asymmetry at the fixture that embodies it, so the next reader does not have to rediscover it:

```go
// manyHosts builds n hosts, more than any row budget under test, so the list is
// always long enough to be truncated.
//
// What this fixture holds constant, and why: every field except Alias and
// HostName is identical, and User/IdentityFile/ProxyJump/SourceFile are all
// unset. That pins the preview at its two-field minimum, which fixes the chrome
// at a known 5 lines and makes the arithmetic in these assertions checkable by
// hand. The height tests are about the row *count*, so the preview's own
// variable height is a confound here — TestChromeLinesTracksThePreviewHeight
// varies it on purpose instead.
//
// Two fields is also a shape sshconfig.Parse cannot produce, since it resolves
// Port and records provenance on every host. That is the confound this fixture
// buys and parsedCorpus pays back: a hand-built Host understates the chrome, so
// tests written only against this fixture measured a preview shorter than any
// real one.
```

- [x] **Step 2: Replace the sampled fit test with a swept invariant**

Delete `TestViewFitsTheReportedTerminalHeight` and its three-line doc comment entirely — the whole function, from `// TestViewFitsTheReportedTerminalHeight is the regression test for the defect:` through its closing brace. Do not keep it alongside the new test. It is superseded, not complemented: it samples heights, and sampling is precisely how it passed while a real config overflowed.

The invariant is **swept over heights 1 through 24, not sampled**, and that is load-bearing rather than thoroughness for its own sake. The boundary where the frame stops fitting is chrome + 2, and the chrome varies with the fixture and with the cursor. So heights sampled from one fixture's chrome step straight over the overflow in another's — which is exactly what happened: `{11, 12, 16, 24, 200}` was chosen against a 9-line chrome and never evaluated height 9 or 10 on a fixture whose chrome was 10.

Two of the four new tests attack the two symptoms separately. `TestViewFitsAtEveryCursorPositionInAShortPane` holds the height fixed and walks the cursor across hosts with different field counts, which is the "overflowed while arrowing" case. `TestViewNeverExceedsTheReportedHeight` drives one case through `sshconfig.Parse` so at least one fixture has the shape a real config produces. Both fail on the pre-fix code.

In place of the deleted function, add:

```go
// parsedCorpus writes a config and runs it through sshconfig.Parse, so at least
// one fixture here has the shape a real config produces rather than the shape a
// convenient literal has. Parse resolves Port and sets SourceFile on every host,
// so a parsed preview never has fewer than three fields; manyHosts has two.
// That gap is not cosmetic — it understated the chrome by enough to hide an
// overflow at height 9 from both the fix and its review, because every fixture
// in the package agreed with the code about a preview height no real config ever
// renders.
//
// Odd-numbered hosts set User and IdentityFile, so field counts alternate 3, 5,
// 3, 5 down the list. That variation is the fixture's whole point: the chrome
// depends on the *cursor* host's field count, so a fixture where every host had
// the same number of fields could not distinguish a height that fits one row
// from a height that fits the next.
//
// Warnings are asserted empty rather than discarded: a warning would add a
// fixedChrome line and silently shift every number computed from this fixture.
func parsedCorpus(t *testing.T, n int) []sshconfig.Host {
	t.Helper()
	var cfg strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&cfg, "Host host%02d\n  HostName 10.0.0.%d\n", i, i+1)
		if i%2 == 1 {
			cfg.WriteString("  User root\n  IdentityFile /keys/id_ed25519\n")
		}
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(cfg.String()), 0o600); err != nil {
		t.Fatalf("writing the fixture config: %v", err)
	}
	hosts, warnings, err := sshconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse(%s): %v", path, err)
	}
	if len(warnings) > 0 {
		t.Fatalf("fixture config produced warnings %v; they would change the chrome", warnings)
	}
	if len(hosts) != n {
		t.Fatalf("Parse returned %d hosts, want %d", len(hosts), n)
	}
	if a, b := len(previewFields(hosts[0])), len(previewFields(hosts[1])); a == b {
		t.Fatalf("hosts 0 and 1 both have %d preview fields; "+
			"this fixture exists to vary that", a)
	}
	return hosts
}

// minFrame is the frame that cannot shrink: fixedChrome (query header + key
// hints), one host row, and the overflow notice. The preview yields all the way
// to nothing, but those four lines have nowhere left to go, so below height 4
// the frame stops shrinking and stays put rather than growing. Showing the
// operator zero hosts, or hiding the keys that dismiss the picker, would both be
// worse than one line of overflow in a pane this small.
//
// It is a constant rather than a call to fixedChrome because every case below
// has more than one host and no warnings. A fixture with warnings would need 5.
const minFrame = 4

// TestViewNeverExceedsTheReportedHeight is the invariant, swept rather than
// sampled: at no height does the frame render more lines than the pane has. The
// sweep matters more than any single case, because the boundary is not a fixed
// height — it is chrome + 2, and the chrome varies with the cursor host. Sampled
// heights chosen from one fixture's chrome therefore step straight over the
// overflow in another's, which is how the last round of this test passed while
// a real config overflowed.
//
// The parsed case is the one that would have caught that. It is here so the
// sweep is anchored to a config Parse actually produced, not to three literals
// that happen to agree with the code.
func TestViewNeverExceedsTheReportedHeight(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		name    string
		hosts   []sshconfig.Host
		preview bool
	}{
		{"preview off", manyHosts(30), false},
		{"minimal preview", manyHosts(30), true},
		{"six-field preview", tallPreviewHosts(30), true},
		{"parsed config", parsedCorpus(t, 30), true},
	} {
		m := newModel(Options{Hosts: tc.hosts, Theme: th, ShowPreview: tc.preview})
		for h := 1; h <= 24; h++ {
			want := h
			if want < minFrame {
				want = minFrame
			}
			if got := lineCount(renderAt(m, h)); got > want {
				t.Errorf("%s: height %d rendered %d lines, want <= %d:\n%s",
					tc.name, h, got, want, stripANSI(renderAt(m, h)))
			}
		}
	}
}

// TestViewFitsAtEveryCursorPositionInAShortPane holds the pane height fixed and
// walks the cursor, which is the case a height sweep cannot reach: the chrome is
// a function of the cursor host, so one height is simultaneously fitting and
// overflowing depending on which row the operator has arrowed to.
//
// Height 9 is the discriminating value for this fixture. Before the preview
// learned to yield, a 9-line pane rendered 9 lines on host00 (3 preview fields)
// and 10 on host01 (5), so the frame overflowed on every other keypress while
// scrolling and was not reproducible from the pane size alone. Both are in the
// sweep below; the neighbouring heights are there so a fix that merely special-
// cased 9 would still fail.
func TestViewFitsAtEveryCursorPositionInAShortPane(t *testing.T) {
	hosts := parsedCorpus(t, 12)
	for _, h := range []int{7, 8, 9, 10, 12} {
		m := newModel(Options{Hosts: hosts, Theme: theme.Default(), ShowPreview: true})
		for i := range hosts {
			cursor := m.view[m.cursor].Host
			if got := lineCount(renderAt(m, h)); got > h {
				t.Errorf("height %d, cursor at row %d on %s (%d preview fields): "+
					"rendered %d lines, overflows by %d:\n%s",
					h, i, cursor.Alias, len(previewFields(cursor)), got, got-h,
					stripANSI(renderAt(m, h)))
			}
			m = press(m, ctrl('j'))
		}
	}
}

// TestViewPreviewShedsFieldsBeforeItDisappears pins the shape of the yield, not
// just the fit. A clamp that truncated the finished frame would satisfy the
// height invariant too — but it would cut from the bottom, taking the key hints
// and leaving the preview whole. These assertions distinguish the two: the
// preview must lose its own trailing fields first, in declaration order, so
// provenance goes before the hostname and the hints never go at all.
func TestViewPreviewShedsFieldsBeforeItDisappears(t *testing.T) {
	m := newModel(Options{Hosts: parsedCorpus(t, 30), Theme: theme.Default(), ShowPreview: true})
	if n := len(previewFields(m.view[m.cursor].Host)); n != 3 {
		t.Fatalf("cursor host has %d preview fields, want 3; "+
			"the heights below are computed from that", n)
	}
	for _, tc := range []struct {
		height  int
		present []string
		absent  []string
	}{
		{8, []string{"HostName ", "Port ", "source "}, nil},
		{7, []string{"HostName ", "Port "}, []string{"source "}},
		{6, []string{"HostName "}, []string{"Port ", "source "}},
		// One line short of a separator plus a field, so the block goes rather
		// than drawing a divider with nothing under it. The host row stays.
		{5, []string{"host00", "esc close"}, []string{"HostName ", "─────"}},
	} {
		out := stripANSI(renderAt(m, tc.height))
		for _, want := range tc.present {
			if !strings.Contains(out, want) {
				t.Errorf("height %d: %q missing:\n%s", tc.height, want, out)
			}
		}
		for _, unwanted := range tc.absent {
			if strings.Contains(out, unwanted) {
				t.Errorf("height %d: %q should have been shed:\n%s", tc.height, unwanted, out)
			}
		}
	}
}

// TestViewSpendsTheYieldedPreviewLinesOnHostRows is the other half of the yield,
// the way TestViewShowsMoreRowsInATallerPane is the other half of the fit. Every
// assertion above is satisfied by a preview that gives its lines up to nothing:
// the frame would fit, and the preview would be correctly absent, while the pane
// sat one row short of full. The lines have to actually reach the host list.
//
// Height 5 is the shortest pane where the preview is gone and there is still
// slack to spend: fixedChrome 2 plus the notice leaves 2 rows, so host01 is
// present exactly when the yielded line was reused rather than dropped.
func TestViewSpendsTheYieldedPreviewLinesOnHostRows(t *testing.T) {
	m := newModel(Options{Hosts: parsedCorpus(t, 30), Theme: theme.Default(), ShowPreview: true})
	out := stripANSI(renderAt(m, 5))
	if got := lineCount(out); got != 5 {
		t.Errorf("a 5-line pane rendered %d lines; the pane is not being filled:\n%s", got, out)
	}
	for _, want := range []string{"host00", "host01"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from a 5-line pane with the preview shed:\n%s", want, out)
		}
	}
}
```

- [x] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/picker/ -count=1`
Expected: four failures. These are assertion failures, not a build failure — unlike Task 14b, these tests name no new production symbol, so they compile against the current code and fail on its behavior.

```
--- FAIL: TestViewNeverExceedsTheReportedHeight (0.01s)
--- FAIL: TestViewFitsAtEveryCursorPositionInAShortPane (0.00s)
--- FAIL: TestViewPreviewShedsFieldsBeforeItDisappears (0.00s)
--- FAIL: TestViewSpendsTheYieldedPreviewLinesOnHostRows (0.00s)
FAIL
FAIL	github.com/purehate/herdr-plugin-ssh/internal/picker	0.246s
FAIL
```

- [x] **Step 4: Let previewFields report what it costs**

`internal/picker/view.go` — replace `previewFields` and its doc comment. The field list is unchanged; the doc comment now records that the caller may take a prefix of it, which is the contract the budget relies on:

```go
// previewFields is the preview's content, one "Key value" line per populated
// field. previewLines budgets these and renderPreview draws them, so the row
// budget cannot disagree with what actually reaches the screen — the preview's
// height varies with how many fields the cursor host happens to set, which is
// why the chrome is computed here instead of written down as a constant.
//
// Note that sshconfig.Parse always resolves Port and sets SourceFile, so a
// parsed host has at least three fields here, never two. A hand-built Host in
// a test can have two, which understates the chrome — that discrepancy hid a
// real overflow at height 9 from two review passes.
func previewFields(h sshconfig.Host) []string {
	lines := []string{"HostName " + h.HostName, "Port " + h.Port}
	if h.User != "" {
		lines = append(lines, "User "+h.User)
	}
	if h.IdentityFile != "" {
		lines = append(lines, "IdentityFile "+h.IdentityFile)
	}
	if h.ProxyJump != "" {
		lines = append(lines, "ProxyJump "+h.ProxyJump)
	}
	if h.SourceFile != "" {
		// Provenance matters as soon as Include is in play: "which file did this
		// host actually come from" is otherwise unanswerable from the picker.
		lines = append(lines, fmt.Sprintf("source %s:%d", h.SourceFile, h.SourceLine))
	}
	return lines
}
```

- [x] **Step 5: Split the chrome and let the preview yield**

Replace `chromeLines` and `visibleRows` with five methods. `fixedChrome` is the chrome that cannot shrink — query header, key hints, and the optional warning line. `noticeReserve` is the `… N more` line, pulled out of `visibleRows` into its own method so the preview's budget can account for it too. `previewLines` is the new elastic element: it computes what the preview wants, grants it whatever the pane has left after the fixed chrome and a minimum host row, and returns 0 when there is not even room for a separator plus one field. `chromeLines` becomes the sum of the fixed part and the _granted_ preview height, not the wanted one — budgeting the wanted height is the bug this task exists to fix.

```go
// fixedChrome counts the lines View draws whatever the height is: the query
// header, the key hints, and the warning count when there is one. These do not
// yield. The header is the operator's own typing echoed back, and the hints are
// the only discoverability the picker has.
func (m model) fixedChrome() int {
	n := 2 // query header + key hints
	if len(m.opts.Warnings) > 0 {
		n++
	}
	return n
}

// noticeReserve is the line the "… N more" notice needs once the list is
// truncated.
//
// It reserves on len(m.view) > 1 rather than on the exact truncation condition
// (len(m.view) > rows), because rows is what this feeds into and the two would
// be mutually recursive. The cost is at most one over-reserved line, and only
// for a list short enough to fit entirely in a pane tight enough for the
// preview to be shedding fields. Never under-reserves: a single-host list
// cannot be truncated at all.
func (m model) noticeReserve() int {
	if len(m.view) > 1 {
		return 1
	}
	return 0
}

// previewLines is how many lines the preview gets: a separator plus as many
// fields as fit, or none.
//
// The preview is what yields when the pane is too short, for two reasons: it is
// the only chrome whose height varies — it grows with the cursor host's field
// count, which is what made the old overflow depend on which row the cursor was
// on — and it is the only one the operator can already dismiss with ^o.
//
// Below the threshold ^o becomes a no-op. That is a deliberate trade rather
// than a bug: in a pane this short the preview cannot fit whatever the toggle
// says, and the host list is the part worth keeping.
func (m model) previewLines() int {
	if !m.showPreview() {
		return 0
	}
	full := 1 + len(previewFields(m.view[m.cursor].Host)) // separator + fields
	if m.height <= 0 {
		// No WindowSizeMsg yet. Assume room, matching visibleRows' fallback, so
		// the first frame is the one a roomy pane would draw.
		return full
	}
	// What is left after the lines that never yield, including one host row.
	budget := m.height - m.fixedChrome() - 1 - m.noticeReserve()
	if budget < 2 {
		// Not even a separator plus one field. Drop the block rather than draw
		// a divider with nothing under it.
		return 0
	}
	if full > budget {
		return budget
	}
	return full
}

// chromeLines is every line that is not a host row and not the overflow notice.
// The notice is excluded because whether it appears depends on the row budget
// this feeds, which is what noticeReserve exists to break.
func (m model) chromeLines() int { return m.fixedChrome() + m.previewLines() }

// visibleRows is how many host rows fit in the terminal height the last
// WindowSizeMsg reported, once chrome has taken its share. The picker is a
// floating overlay, so an oversized frame is not a scroll the operator can use.
func (m model) visibleRows() int {
	if m.height <= 0 {
		// No WindowSizeMsg has arrived yet. Fall back to the ceiling rather
		// than rendering an empty list on the first frame.
		return maxRows
	}
	n := m.height - m.chromeLines() - m.noticeReserve()
	if n > maxRows {
		n = maxRows
	}
	if n < 1 {
		// A picker showing zero hosts is worse than one that overflows, so the
		// floor wins over the fit. fixedChrome + one row + the notice is the
		// irreducible frame; below that height the frame stops shrinking and
		// stays at it rather than growing.
		n = 1
	}
	return n
}
```

- [x] **Step 6: Draw only the preview lines the budget granted**

Replace `renderPreview` and its doc comment. It now takes the prefix of `previewFields` that `previewLines` paid for, so the drawn preview and the budgeted preview cannot diverge:

```go
// renderPreview draws the separator plus the fields previewLines budgeted, in
// declaration order — so a short pane sheds provenance before it sheds the
// hostname. An empty string is the "did not fit" case, which View writes
// harmlessly.
//
// The guard reads n < 2 rather than n < 1 even though n == 1 is unreachable:
// previewFields always yields at least HostName and Port, so a non-zero
// previewLines is at least 2. The stricter bound is not testable — no input
// reaches it — and weakening it to n < 1 changes nothing today. It stays
// because the two differ if that contract ever slips: n < 2 degrades to no
// block, n < 1 degrades to a divider with nothing under it. Only n == 0 must be
// caught at all, since [:n-1] would panic on it.
func (m model) renderPreview(muted, text lipgloss.Style) string {
	n := m.previewLines()
	if n < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString(muted.Render("  ─────") + "\n")
	for _, l := range previewFields(m.view[m.cursor].Host)[:n-1] {
		b.WriteString("  " + text.Render(l) + "\n")
	}
	return b.String()
}
```

- [x] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/picker/ -count=1`
Expected: `ok  	github.com/purehate/herdr-plugin-ssh/internal/picker`

Fault localization, measured by mutating `view.go` in a clean export of this commit and running the full package suite each time — reverted and diffed byte-identical between mutants:

| Mutation                                                                        | Tests that fail                                                                                                                                                                            |
| ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `previewLines`: `if m.height <= 0` → `>= 0`, so the preview never yields        | `TestViewFitsAtEveryCursorPositionInAShortPane`, `TestViewNeverExceedsTheReportedHeight`, `TestViewPreviewShedsFieldsBeforeItDisappears`, `TestViewSpendsTheYieldedPreviewLinesOnHostRows` |
| `chromeLines`: budget the preview's wanted height instead of its granted height | `TestViewSpendsTheYieldedPreviewLinesOnHostRows`                                                                                                                                           |
| `renderPreview`: drop the `[:n-1]` prefix, so the draw ignores the budget       | `TestViewFitsAtEveryCursorPositionInAShortPane`, `TestViewNeverExceedsTheReportedHeight`, `TestViewPreviewShedsFieldsBeforeItDisappears`                                                   |

The first mutation reintroduces the exact defect this task fixes and all four new tests catch it. The second is caught by exactly the test written for it. A fourth mutation was run: relaxing `previewLines`' `if budget < 2` guard to `< 1` fails only `TestViewSpendsTheYieldedPreviewLinesOnHostRows`, which is the test that asserts yielded lines reach the host list.

- [x] **Step 8: Verify the full suite, vet, and gofmt**

```bash
go test ./... -count=1
go vet ./...
gofmt -l internal/ cmd/
```

Expected: tests pass, `go vet` prints nothing, `gofmt -l` prints nothing. `go build ./...` is not a gate here either, for the reason given at Task 14b Step 5: in plan order `cmd/herdr-ssh` does not exist yet and the build succeeds, while at `68d1845`, the commit this task reconstructs, it exits 1 on the missing `func main()`. Measured both ways.

This task adds 4 tests and deletes 1, a net of 3. Count them, do not infer them:

```bash
go test ./internal/picker/ -list '.*' -count=1 | grep -c '^Test'
```

Measured across this commit: 55 before, 58 after. Any later task that states an absolute `internal/picker` test count was written before 14b and 14c existed and needs to be re-measured with the command above rather than adjusted by arithmetic.

- [x] **Step 9: Commit**

```bash
git add internal/picker/
git commit -m "fix(picker): let the preview yield its lines in a short pane" \
  -- internal/picker/
```

---

### Task 15: cmd — caller context handoff

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `cmd/herdr-ssh/caller.go`
- Test: `cmd/herdr-ssh/caller_test.go`

Background: the overlay pane is not the pane the operator was working in, so it cannot know where to put the split. The `open-picker` action runs in the caller's context and writes that context to `$HERDR_PLUGIN_STATE_DIR/caller.json`; the overlay reads it back.

- [x] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadCaller(t *testing.T) {
	dir := t.TempDir()
	want := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := writeCaller(dir, want); err != nil {
		t.Fatalf("writeCaller: %v", err)
	}
	if got := readCaller(dir); got != want {
		t.Fatalf("readCaller = %+v, want %+v", got, want)
	}

	info, err := os.Stat(filepath.Join(dir, "caller.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestReadCallerDegradesGracefully(t *testing.T) {
	if got := readCaller(t.TempDir()); got != (caller{}) {
		t.Errorf("readCaller on a missing file = %+v, want zero value", got)
	}
	if got := readCaller(""); got != (caller{}) {
		t.Errorf("readCaller(\"\") = %+v, want zero value", got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "caller.json"), []byte("{{{"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readCaller(dir); got != (caller{}) {
		t.Errorf("readCaller on malformed JSON = %+v, want zero value", got)
	}
}

func TestCurrentCallerReadsEnv(t *testing.T) {
	t.Setenv("HERDR_PANE_ID", "w9:p1")
	t.Setenv("HERDR_TAB_ID", "w9:t1")
	t.Setenv("HERDR_WORKSPACE_ID", "w9")

	want := caller{PaneID: "w9:p1", TabID: "w9:t1", WorkspaceID: "w9"}
	if got := currentCaller(); got != want {
		t.Fatalf("currentCaller = %+v, want %+v", got, want)
	}
}

func TestWriteCallerRejectsEmptyDir(t *testing.T) {
	if err := writeCaller("", caller{PaneID: "w5:pA"}); err == nil {
		t.Fatal("err = nil, want an error for an empty state dir")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -count=1`
Expected: FAIL — `undefined: caller`

No `-run` filter here, unlike the other red-phase steps. `-run` takes an
_unanchored_ regex over the full test name, and none of the four tests above
_contains_ the substring `TestCaller` — they are `TestWriteAndReadCaller`,
`TestReadCallerDegradesGracefully`, `TestCurrentCallerReadsEnv`, and
`TestWriteCallerRejectsEmptyDir`. A `-run TestCaller` would have selected
nothing and printed `ok ... [no tests to run]` at exit 0. It would still have
looked like a pass of this step today, because the package does not compile
until Step 3 and the build error fails the command whatever the filter says —
so the filter's uselessness would only surface later, to whoever re-ran the
command against a package that builds.

- [x] **Step 3: Implement the handoff**

```go
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const callerFile = "caller.json"

// caller is the pane the operator triggered the picker from. The overlay needs
// it to know where to place a split.
type caller struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

// currentCaller reads the herdr context this process was launched with.
func currentCaller() caller {
	return caller{
		PaneID:      os.Getenv("HERDR_PANE_ID"),
		TabID:       os.Getenv("HERDR_TAB_ID"),
		WorkspaceID: os.Getenv("HERDR_WORKSPACE_ID"),
	}
}

func writeCaller(stateDir string, c caller) error {
	if stateDir == "" {
		return errors.New("HERDR_PLUGIN_STATE_DIR is not set")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, callerFile), raw, 0o600)
}

// readCaller returns the zero value when the file is missing or unusable. A
// missing caller costs the operator a default placement, not the picker.
func readCaller(stateDir string) caller {
	if stateDir == "" {
		return caller{}
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, callerFile))
	if err != nil {
		return caller{}
	}
	var c caller
	if err := json.Unmarshal(raw, &c); err != nil {
		return caller{}
	}
	return c
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/herdr-ssh/ -v -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): pass caller pane context to the overlay via state dir" \
  -- cmd/herdr-ssh/
```

---

### Task 16: cmd — session verb

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `cmd/herdr-ssh/session.go`
- Test: `cmd/herdr-ssh/session_test.go`

Background: the session pane renames itself before `exec`ing ssh, because `plugin pane open` does not hand the new pane's id back to the caller. After `syscall.Exec` this process is gone and ssh owns the pty — no wrapper, no extra shell, and `^d` closes the pane the way the operator expects.

- [x] **Step 1: Write the failing test**

```go
package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
)

func TestSessionArgvPutsFlagsBeforeDestination(t *testing.T) {
	// ssh parses `ssh [options] destination [command]`. Flags after the
	// destination become a remote command, so order is a correctness issue.
	got := sessionArgv([]string{"-o", "ConnectTimeout=5"}, "nixos-dev")
	want := "ssh -o ConnectTimeout=5 nixos-dev"
	if strings.Join(got, " ") != want {
		t.Fatalf("argv = %v, want %q", got, want)
	}
}

func TestSessionArgvWithoutFlags(t *testing.T) {
	if got := sessionArgv(nil, "web1"); strings.Join(got, " ") != "ssh web1" {
		t.Fatalf("argv = %v", got)
	}
}

func TestSessionLabel(t *testing.T) {
	if got := sessionLabel("nixos-dev"); got != "ssh:nixos-dev" {
		t.Fatalf("sessionLabel = %q, want ssh:nixos-dev", got)
	}
}

func TestPrepareSessionRenamesOwnPane(t *testing.T) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}}

	argv, err := prepareSession(api, pluginconfig.Defaults(), "nixos-dev", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if strings.Join(argv, " ") != "ssh nixos-dev" {
		t.Errorf("argv = %v", argv)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "pane rename w5:pC ssh:nixos-dev" {
		t.Errorf("calls = %v", calls)
	}
}

func TestPrepareSessionRequiresATarget(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) { return nil, nil }}
	if _, err := prepareSession(api, pluginconfig.Defaults(), "", "w5:pC"); err == nil {
		t.Fatal("err = nil, want an error for a missing target")
	}
}

func TestPrepareSessionToleratesRenameFailure(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) {
		return []byte("no such pane"), errRenameTest
	}}
	// A failed rename costs pane reuse, not the connection. Connect anyway.
	argv, err := prepareSession(api, pluginconfig.Defaults(), "web1", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if strings.Join(argv, " ") != "ssh web1" {
		t.Fatalf("argv = %v", argv)
	}
}
```

Add the sentinel at the top of the test file:

```go
var errRenameTest = errors.New("rename failed")
```

`"errors"` is already in the import block above for this reason — the sentinel is the only thing in the file that needs it, so it is easy to drop when copying. `connect_test.go` in Task 17 reuses this sentinel; it is package-scoped, so do not redeclare it there.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run Session -count=1`
Expected: FAIL — `undefined: sessionArgv`

`-run Session`, not `-run TestSession`. The selector is an unanchored regex and the narrower form selects only 3 of this package's session tests where the wider form selects 8 (measured at `0ff9e4d`). It happens not to matter for _this_ step, because an `undefined:` build failure fails the package regardless of what the selector picks — but the same line gets copied into steps where it does matter, and a selector that quietly under-selects is indistinguishable from a passing run.

- [x] **Step 3: Implement the session verb**

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
)

const labelPrefix = "ssh:"

func sessionLabel(alias string) string { return labelPrefix + alias }

// sessionArgv builds ssh's argv. Configured flags go before the destination;
// anything after it would be sent to the remote shell as a command.
func sessionArgv(sshArgs []string, alias string) []string {
	argv := make([]string, 0, len(sshArgs)+2)
	argv = append(argv, "ssh")
	argv = append(argv, sshArgs...)
	return append(argv, alias)
}

// prepareSession labels this pane so the picker can find it again, then returns
// the argv to exec. A rename failure is logged and ignored: losing pane reuse
// is much cheaper than losing the connection the operator asked for.
func prepareSession(api herdrapi.Client, cfg pluginconfig.Config, alias, paneID string) ([]string, error) {
	if alias == "" {
		return nil, errors.New("HERDR_SSH_TARGET is not set")
	}
	if paneID != "" {
		if err := api.PaneRename(paneID, sessionLabel(alias)); err != nil {
			fmt.Fprintf(os.Stderr, "herdr-ssh: could not label pane: %v\n", err)
		}
	}
	return sessionArgv(cfg.SSHArgs, alias), nil
}

// runSession replaces this process with ssh.
func runSession() error {
	// Load always returns a usable Config, with only the rejected keys reset, so
	// report the error and keep the Config. Replacing it with Defaults() here
	// would undo a valid `probe = false` because of an unrelated typo.
	cfg, err := pluginconfig.LoadDir(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: %v — ignoring the rejected keys\n", err)
	}

	argv, err := prepareSession(herdrapi.New(), cfg, os.Getenv("HERDR_SSH_TARGET"), os.Getenv("HERDR_PANE_ID"))
	if err != nil {
		return err
	}

	bin, err := exec.LookPath("ssh")
	if err != nil {
		// Returning here would exit immediately and take the pane with it,
		// before the operator can read why. Show the command and hold.
		fmt.Printf("herdr-ssh: cannot run %v\n%v\n\npress enter to close\n", argv, err)
		fmt.Fscanln(os.Stdin)
		return fmt.Errorf("ssh not found on PATH: %w", err)
	}
	// Exec, so ssh owns the pty: no wrapper process, and ^d closes the pane.
	return syscall.Exec(bin, argv, os.Environ())
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/herdr-ssh/ -v -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): add session verb that labels its pane and execs ssh" \
  -- cmd/herdr-ssh/
```

- [x] **Step 6: Cover the `ssh_args` and pane-id paths `prepareSession` branches on**

`prepareSession` is the only production caller of `sessionArgv`, and every other fixture in this file uses `Defaults()`, whose `SSHArgs` is nil — so the configured-args branch has no assertion reaching it, and neither does the pane-id branch. Two tests close both. Append to `cmd/herdr-ssh/session_test.go`:

```go
func TestPrepareSessionPassesConfiguredSSHArgs(t *testing.T) {
	// prepareSession is the only production caller of sessionArgv, and every
	// other fixture in this file uses Defaults(), whose SSHArgs is nil. Without
	// a populated one, prepareSession could drop the operator's ssh_args
	// entirely and the whole file would stay green.
	cfg := pluginconfig.Defaults()
	cfg.SSHArgs = []string{"-o", "ConnectTimeout=5"}

	api := herdrapi.Client{Run: func([]string) ([]byte, error) {
		return []byte(`{"id":1,"result":{}}`), nil
	}}
	argv, err := prepareSession(api, cfg, "nixos-dev", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if got, want := strings.Join(argv, " "), "ssh -o ConnectTimeout=5 nixos-dev"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestPrepareSessionRenamesNothingItShouldNot(t *testing.T) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}}

	// Outside herdr there is no HERDR_PANE_ID, so there is no pane to label.
	// Renaming anyway would send `pane rename "" ssh:nixos-dev`.
	if _, err := prepareSession(api, pluginconfig.Defaults(), "nixos-dev", ""); err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("calls = %v, want none without a pane id", calls)
	}

	// A missing target must be rejected before anything is renamed: a pane left
	// labeled "ssh:" outlives the error and would be reused by the next connect.
	if _, err := prepareSession(api, pluginconfig.Defaults(), "", "w5:pC"); err == nil {
		t.Fatal("err = nil, want an error for a missing target")
	}
	if len(calls) != 0 {
		t.Errorf("calls = %v, want none for a missing target", calls)
	}
}
```

- [x] **Step 7: Run the tests to verify they pass**

Run: `go test ./cmd/herdr-ssh/ -run Session -count=1`
Expected: PASS. Measured at `0ff9e4d` this selector picks 8 tests in the package; `-run TestSession` picks only 3 and reaches neither test added here, which is the under-selection the standing rule on `-count=1` warns about.

- [x] **Step 8: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "test(cmd): cover the ssh_args and pane-id paths prepareSession branches on" \
  -- cmd/herdr-ssh/
```

---

### Task 17: cmd — reuse or open a session pane

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `cmd/herdr-ssh/connect.go`
- Test: `cmd/herdr-ssh/connect_test.go`

- [x] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

const openPanesJSON = `{"id":1,"result":{"panes":[
  {"pane_id":"w5:pA","tab_id":"w5:t1","workspace_id":"w5","label":null},
  {"pane_id":"w8:pQ","tab_id":"w8:t3","workspace_id":"w8","label":"ssh:nixos-dev"}
]}}`

func fakeAPI(out string) (herdrapi.Client, *[][]string) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), nil
	}}
	return api, &calls
}

func joined(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

var devHost = sshconfig.Host{Alias: "nixos-dev", HostName: "192.0.2.10", Port: "22"}

func TestPerformSelectionOpensASplit(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split"}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	if err := performSelection(api, cfg, sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}

	got := joined(*calls)
	if len(got) != 1 {
		t.Fatalf("calls = %v, want exactly one open", got)
	}
	want := "plugin pane open --plugin purehate.herdr-ssh --entrypoint session " +
		"--placement split --target-pane w5:pA --direction right " +
		"--env HERDR_SSH_TARGET=nixos-dev --focus"
	if got[0] != want {
		t.Fatalf("argv =\n  %q\nwant\n  %q", got[0], want)
	}
}

func TestPerformSelectionReusesAnExistingPane(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split"}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}

	want := []string{
		"pane list",
		"workspace focus w8",
		"tab focus w8:t3",
		"plugin pane focus w8:pQ",
	}
	got := joined(*calls)
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPerformSelectionForceNewSkipsReuse(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split", ForceNew: true}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	for _, c := range joined(*calls) {
		if strings.Contains(c, "pane focus") {
			t.Fatalf("calls = %v, want no reuse when ForceNew is set", joined(*calls))
		}
	}
}

func TestPerformSelectionTabPlacementOmitsDirection(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.Selection{Host: devHost, Placement: "tab"}

	if err := performSelection(api, cfg, sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	got := joined(*calls)[0]
	if strings.Contains(got, "--direction") {
		t.Fatalf("argv = %q, want no --direction for a tab", got)
	}
	// A tab takes a workspace id, not a target pane: herdr rejects the open with
	// "tab plugin panes support workspace_id but not target_pane_id or
	// direction". performSelection passes the caller's pane id unconditionally,
	// so this asserts herdrapi filtered it back out.
	if strings.Contains(got, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane for a tab", got)
	}
	if !strings.Contains(got, "--placement tab") {
		t.Fatalf("argv = %q, want --placement tab", got)
	}
}

func TestPerformSelectionHonorsSplitDirection(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	cfg.SplitDirection = "down"
	sel := picker.Selection{Host: devHost, Placement: "split"}

	if err := performSelection(api, cfg, sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	if got := joined(*calls)[0]; !strings.Contains(got, "--direction down") {
		t.Fatalf("argv = %q, want --direction down", got)
	}
}

func TestPerformSelectionWithoutACallerPaneStillOpens(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.Selection{Host: devHost, Placement: "split"}

	if err := performSelection(api, cfg, sel, caller{}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	if got := joined(*calls)[0]; strings.Contains(got, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane when the caller is unknown", got)
	}
}

func TestPerformSelectionToleratesAFailedPaneList(t *testing.T) {
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		if args[0] == "pane" && args[1] == "list" {
			return []byte("socket gone"), errRenameTest
		}
		return []byte(`{"id":1,"result":{}}`), nil
	}}
	sel := picker.Selection{Host: devHost, Placement: "split"}
	// Reuse is a convenience. If the lookup fails, open a fresh pane.
	if err := performSelection(api, pluginconfig.Defaults(), sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run TestPerformSelection -count=1`
Expected: FAIL — `undefined: performSelection`

- [x] **Step 3: Implement it**

```go
package main

import (
	"fmt"
	"os"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
)

const pluginID = "purehate.herdr-ssh"

// performSelection focuses an existing session for this host when there is one,
// and otherwise opens a new session pane.
func performSelection(api herdrapi.Client, cfg pluginconfig.Config, sel picker.Selection, ctx caller) error {
	if cfg.ReusePanes && !sel.ForceNew {
		if pane, ok := findSession(api, sel.Host.Alias); ok {
			return api.FocusPane(pane, ctx.WorkspaceID, ctx.TabID)
		}
	}
	return api.PluginPaneOpen(herdrapi.OpenOpts{
		Plugin:     pluginID,
		Entrypoint: "session",
		Placement:  sel.Placement,
		TargetPane: ctx.PaneID,
		Direction:  cfg.SplitDirection,
		Env:        map[string]string{"HERDR_SSH_TARGET": sel.Host.Alias},
		Focus:      true,
	})
}

// findSession looks for a pane already labeled for this host. A failed lookup
// is reported and treated as "no existing session".
func findSession(api herdrapi.Client, alias string) (herdrapi.Pane, bool) {
	panes, err := api.PaneList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: could not list panes: %v\n", err)
		return herdrapi.Pane{}, false
	}
	return herdrapi.FindLabeled(panes, sessionLabel(alias))
}

// openSessions maps alias → pane id for every live ssh session, so the picker
// can mark them.
func openSessions(api herdrapi.Client) map[string]string {
	out := map[string]string{}
	panes, err := api.PaneList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: could not list panes: %v\n", err)
		return out
	}
	for _, p := range panes {
		if p.Label == nil {
			continue
		}
		if alias, ok := trimLabel(*p.Label); ok {
			out[alias] = p.PaneID
		}
	}
	return out
}

func trimLabel(label string) (string, bool) {
	if len(label) <= len(labelPrefix) || label[:len(labelPrefix)] != labelPrefix {
		return "", false
	}
	return label[len(labelPrefix):], true
}
```

- [x] **Step 4: Add the `openSessions` test**

Append to `cmd/herdr-ssh/connect_test.go`:

```go
func TestOpenSessionsMapsLabeledPanes(t *testing.T) {
	api, _ := fakeAPI(openPanesJSON)
	got := openSessions(api)
	if len(got) != 1 || got["nixos-dev"] != "w8:pQ" {
		t.Fatalf("openSessions = %v, want nixos-dev → w8:pQ", got)
	}
}

func TestTrimLabel(t *testing.T) {
	if alias, ok := trimLabel("ssh:web1"); !ok || alias != "web1" {
		t.Errorf("trimLabel(ssh:web1) = (%q, %v)", alias, ok)
	}
	for _, label := range []string{"ssh:", "build", "", "sshweb1"} {
		if _, ok := trimLabel(label); ok {
			t.Errorf("trimLabel(%q) matched, want no match", label)
		}
	}
}
```

- [x] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/herdr-ssh/ -v -count=1`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): reuse an existing ssh pane or open a new one" \
  -- cmd/herdr-ssh/
```

---

### Task 18: cmd — host loading and probe targets

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `cmd/herdr-ssh/hosts.go`
- Test: `cmd/herdr-ssh/hosts_test.go`

- [x] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

func writeSSHConfig(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadHostsMergesExtraPathsAndHidesGlobs(t *testing.T) {
	primary := writeSSHConfig(t, "config", "Host nixos-dev\n  HostName 10.0.0.1\n\nHost colima\n  HostName 127.0.0.1\n")
	extra := writeSSHConfig(t, "work", "Host client-jump\n  HostName 10.9.9.9\n\nHost old-box\n  HostName 10.9.9.10\n")

	cfg := pluginconfig.Defaults()
	cfg.ExtraConfigPaths = []string{extra}
	cfg.Hidden = []string{"colima", "*-box"}

	hosts, warns := loadHosts(primary, cfg)
	var got []string
	for _, h := range hosts {
		got = append(got, h.Alias)
	}
	want := "nixos-dev client-jump"
	if strings.Join(got, " ") != want {
		t.Fatalf("aliases = %v, want %q", got, want)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
}

func TestLoadHostsTreatsAMissingPrimaryConfigAsEmpty(t *testing.T) {
	hosts, warns := loadHosts(filepath.Join(t.TempDir(), "absent"), pluginconfig.Defaults())
	if len(hosts) != 0 {
		t.Fatalf("hosts = %v, want none", hosts)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none — a missing config is normal", warns)
	}
}

func TestLoadHostsWarnsOnAMissingExtraPath(t *testing.T) {
	primary := writeSSHConfig(t, "config", "Host a\n")
	missing := filepath.Join(t.TempDir(), "nope")
	cfg := pluginconfig.Defaults()
	cfg.ExtraConfigPaths = []string{missing}

	hosts, warns := loadHosts(primary, cfg)
	if len(hosts) != 1 {
		t.Fatalf("hosts = %v, want the primary host", hosts)
	}
	// An extra path the operator explicitly configured is worth complaining about.
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warns)
	}
	// Exactly once, not Contains. sshconfig already embeds the path in the
	// error, so prefixing it again printed it twice in the footer, and a
	// Contains check is blind to that — it passes at one occurrence and at two.
	// The count is two-sided on purpose: it also fails at zero, which is the
	// case that matters if sshconfig ever stops embedding the path, because
	// then the prefix-less version silently loses which file failed.
	if n := strings.Count(warns[0], missing); n != 1 {
		t.Fatalf("path appears %d times in %q, want once", n, warns[0])
	}
}

func TestLoadHostsDeduplicatesByAlias(t *testing.T) {
	primary := writeSSHConfig(t, "config", "Host dup\n  HostName 10.0.0.1\n")
	extra := writeSSHConfig(t, "extra", "Host dup\n  HostName 10.0.0.2\n")
	cfg := pluginconfig.Defaults()
	cfg.ExtraConfigPaths = []string{extra}

	hosts, _ := loadHosts(primary, cfg)
	if len(hosts) != 1 {
		t.Fatalf("hosts = %+v, want one entry", hosts)
	}
	if hosts[0].HostName != "10.0.0.1" {
		t.Errorf("HostName = %q, want the primary config to win", hosts[0].HostName)
	}
}

func TestTargetsForJoinsHostAndPortAndSkipsProxyJump(t *testing.T) {
	hosts := []sshconfig.Host{
		{Alias: "a", HostName: "10.0.0.1", Port: "22"},
		{Alias: "b", HostName: "10.0.0.2", Port: "2222"},
		{Alias: "c", HostName: "10.0.0.3", Port: "22", ProxyJump: "a"},
	}
	got := targetsFor(hosts)
	if len(got) != 3 {
		t.Fatalf("targets = %+v", got)
	}
	if got[0].Addr != "10.0.0.1:22" || got[1].Addr != "10.0.0.2:2222" {
		t.Errorf("addrs = %q, %q", got[0].Addr, got[1].Addr)
	}
	if got[2].Skip != true {
		t.Error("a ProxyJump host was not skipped — probing it would dial the wrong network")
	}
	if got[0].Skip || got[1].Skip {
		t.Error("direct hosts were skipped")
	}
}

func TestSSHConfigPathUsesHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/fake-home")
	if got := sshConfigPath(); got != "/tmp/fake-home/.ssh/config" {
		t.Fatalf("sshConfigPath = %q", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run 'TestLoadHosts|TestTargetsFor|TestSSHConfigPath' -count=1`
Expected: FAIL — `undefined: loadHosts`

- [x] **Step 3: Implement it**

```go
package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

func sshConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}

// loadHosts parses the primary config plus any extra paths, drops hidden
// aliases, and returns human-readable warnings for the picker footer. The first
// definition of an alias wins, matching ssh's own precedence.
func loadHosts(primary string, cfg pluginconfig.Config) ([]sshconfig.Host, []string) {
	var hosts []sshconfig.Host
	var warnings []string
	seen := map[string]bool{}

	add := func(found []sshconfig.Host) {
		for _, h := range found {
			if seen[h.Alias] {
				continue
			}
			seen[h.Alias] = true
			hosts = append(hosts, h)
		}
	}

	found, warns, err := sshconfig.Parse(primary)
	switch {
	case errors.Is(err, sshconfig.ErrNoConfig):
		// No ssh config is a normal state, not a problem to report.
	case err != nil:
		// No path prefix: err already names the file. ErrNoConfig is wrapped as
		// "%w: %s" with the absolute path, and the only other reachable error
		// here is os.ReadFile's *fs.PathError, which embeds it too — so
		// prefixing printed the file twice ("<p>: read <p>: is a directory").
		// The tests assert the path appears exactly once, which is also what
		// catches the reverse: this is correct only while sshconfig keeps
		// embedding the path, and a bare error would otherwise leave the
		// operator unable to tell which file failed.
		warnings = append(warnings, err.Error())
	default:
		add(found)
	}
	for _, w := range warns {
		warnings = append(warnings, w.String())
	}

	for _, path := range cfg.ExtraConfigPaths {
		found, warns, err := sshconfig.Parse(path)
		if err != nil {
			// Same double-naming as the primary arm; see above. One knock-on
			// worth stating: a *relative* ExtraConfigPaths entry used to print
			// the operator's spelling and then the resolved path. Now only the
			// resolved one survives, which is the more useful half — relative
			// entries resolve against the process CWD, and where we actually
			// looked is the part that explains the failure.
			warnings = append(warnings, err.Error())
			// This continue is unobservable today, and only because of an
			// invariant sshconfig currently holds: Parse never returns warnings
			// alongside an error, since every error return precedes the line
			// loop that generates them and parseIncludes downgrades include
			// failures rather than propagating them. So found is nil and warns
			// is empty on this path. If that ever changes, this continue starts
			// discarding the warnings collected before the failure.
			continue
		}
		add(found)
		for _, w := range warns {
			warnings = append(warnings, w.String())
		}
	}

	return sshconfig.Exclude(hosts, cfg.Hidden), warnings
}

// targetsFor builds probe targets. Hosts behind a ProxyJump are marked Skip: a
// direct dial would test the wrong network and report a false "down".
func targetsFor(hosts []sshconfig.Host) []probe.Target {
	out := make([]probe.Target, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, probe.Target{
			Alias: h.Alias,
			Addr:  net.JoinHostPort(h.HostName, h.Port),
			Skip:  h.ProxyJump != "",
		})
	}
	return out
}
```

**Both warning arms above append `err.Error()` and not a path prefix. This was
corrected after the fact — do not "restore" the prefix.** Both arms originally
wrapped the error as `fmt.Sprintf("%s: %v", path, err)`, which named the same
file twice on the line the operator reads in the picker footer:

```
/tmp/x/nope: ssh config not found: /tmp/x/nope
/tmp/x/config: read /tmp/x/config: is a directory
```

`sshconfig.Parse` already wraps not-found as
`fmt.Errorf("%w: %s", ErrNoConfig, abs)`, and the only other error reachable
from the primary arm is `os.ReadFile`'s `*fs.PathError`, which embeds the path
as well. Every error return in `sshconfig` embeds it except `filepath.Abs`,
which fires only if `os.Getwd` fails, so `err.Error()` is already fully
qualified. A redundant-looking prefix is exactly the kind of thing a later
reader adds back, which is why the reason lives here and not only in `87ecc8a`.
Two knock-ons: `fmt` is now unused in this file and must not be imported, or the
copied block fails to build; and a _relative_ `ExtraConfigPaths` entry used to
print the operator's spelling followed by the resolved path, where now only the
resolved path survives — the more useful half, since relative entries resolve
against the process working directory and where we actually looked is what
explains the failure.

The test change is two-sided on purpose. `strings.Count(warns[0], missing) != 1`
fails at two occurrences, which is today's defect, and also at zero, which is
the case that outlives this commit: dropping the prefix is correct only while
`sshconfig` keeps putting the path in its errors, and if that ever changes the
prefix-less version loses the filename entirely and the operator cannot tell
which config failed. `Contains` is blind to both — it passes at one occurrence
and at two.

**Not claimed by any task: five tests and one fixture row now in
`cmd/herdr-ssh/hosts_test.go`.** Measured against this file, each name occurs
zero times in the plan: `TestLoadHostsWarnsWhenThePrimaryConfigIsUnreadable`,
`TestSSHConfigPathFallsBackWithoutHome`, `TestTargetsForCarriesTheAlias`,
`TestLoadHostsSurfacesPrimaryParseWarnings`,
`TestLoadHostsSurfacesExtraPathParseWarnings`, and the `v6` row plus its
`[::1]:22` assertion inside `TestTargetsForJoinsHostAndPortAndSkipsProxyJump`.
That is the same unclaimed-test shape Task 16 Steps 6 through 8 closed for
`0ff9e4d`. Do not delete them, and do not read the Step 1 fence above as the
finished state of that file — it is that file at this task's own commit.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/herdr-ssh/ -v -count=1`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git status --short cmd/herdr-ssh/
git add cmd/herdr-ssh/hosts.go cmd/herdr-ssh/hosts_test.go
git commit -m "feat(cmd): load hosts from ssh config and build probe targets" \
  -- cmd/herdr-ssh/hosts.go cmd/herdr-ssh/hosts_test.go
```

---

### Task 19: cmd — picker verb, open-picker verb, and dispatch

_Steps ticked from a measurement at `250914b` on a clean `git archive` export: all seven packages `ok`, `go test -race` clean, `go vet` and `gofmt -l .` silent, `staticcheck` and `errcheck` at zero findings, `go.mod` and `go.sum` byte-identical. The ticks are a claim about that commit, not about `HEAD`; re-measure before reading them as current._
**Files:**

- Create: `cmd/herdr-ssh/main.go`
- Test: `cmd/herdr-ssh/main_test.go`

- [x] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
)

func TestRunRejectsUnknownVerbs(t *testing.T) {
	for _, args := range [][]string{{}, {"wat"}, {"plugin"}, {"plugin", "wat"}} {
		err := run(args)
		if err == nil {
			t.Fatalf("run(%v) = nil, want a usage error", args)
		}
		if !strings.Contains(err.Error(), "usage") {
			t.Errorf("run(%v) error = %q, want it to mention usage", args, err)
		}
	}
}

func TestOpenPickerWritesCallerAndOpensTheOverlay(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	t.Setenv("HERDR_PANE_ID", "w5:pA")
	t.Setenv("HERDR_TAB_ID", "w5:t1")
	t.Setenv("HERDR_WORKSPACE_ID", "w5")

	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}}

	if err := openPicker(api); err != nil {
		t.Fatalf("openPicker: %v", err)
	}

	if got := readCaller(dir); got.PaneID != "w5:pA" || got.WorkspaceID != "w5" {
		t.Errorf("caller.json = %+v", got)
	}
	want := "plugin pane open --plugin purehate.herdr-ssh --entrypoint picker --placement overlay --focus"
	if len(calls) != 1 || strings.Join(calls[0], " ") != want {
		t.Fatalf("argv = %v, want %q", joined(calls), want)
	}
}

func TestOpenPickerFailsWithoutAStateDir(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	api := herdrapi.Client{Run: func([]string) ([]byte, error) { return nil, nil }}
	if err := openPicker(api); err == nil {
		t.Fatal("err = nil, want an error when the state dir is unset")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run 'TestRun|TestOpenPicker' -count=1`
Expected: FAIL — `undefined: run`

- [x] **Step 3: Implement dispatch and the two picker verbs**

```go
// Command herdr-ssh is the SSH picker plugin for herdr.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

const usage = `usage: herdr-ssh <picker|session|connect <alias> [--placement split|tab|zoomed]|plugin open-picker>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "picker":
		return runPicker()
	case "session":
		return runSession()
	case "connect":
		if len(args) < 2 {
			return errors.New(usage)
		}
		return runConnect(args[1:])
	case "plugin":
		if len(args) < 2 || args[1] != "open-picker" {
			return errors.New(usage)
		}
		return openPicker(herdrapi.New())
	default:
		return errors.New(usage)
	}
}

// openPicker runs in the caller's pane: it records where the operator was, then
// opens the overlay.
func openPicker(api herdrapi.Client) error {
	if err := writeCaller(os.Getenv("HERDR_PLUGIN_STATE_DIR"), currentCaller()); err != nil {
		return err
	}
	return api.PluginPaneOpen(herdrapi.OpenOpts{
		Plugin:     pluginID,
		Entrypoint: "picker",
		Placement:  "overlay",
		Focus:      true,
	})
}

// runPicker draws the overlay and acts on the operator's choice.
func runPicker() error {
	// Keep the returned Config; only the rejected keys were reset. See runSession.
	cfg, cfgErr := pluginconfig.LoadDir(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	// theme.LoadFile always returns a usable Theme, so th is safe to render with
	// even when themeErr is non-nil. The error is reported, not acted on.
	th, themeErr := theme.LoadFile(os.Getenv("HERDR_CONFIG_PATH"))

	hosts, warnings := loadHosts(sshConfigPath(), cfg)
	// Surface load errors in the footer rather than on stderr. This path has an
	// overlay to render into, and the footer is where the operator is already
	// looking; stderr would survive (no alt-screen switch) but prints above the
	// overlay instead of in it. runSession and runConnect have no picker, so
	// they stay on stderr. Built in a fixed order rather than prepended twice,
	// which would silently reverse them.
	var loadWarnings []string
	if cfgErr != nil {
		loadWarnings = append(loadWarnings, fmt.Sprintf("plugin config: %v — ignoring the rejected keys", cfgErr))
	}
	if themeErr != nil {
		loadWarnings = append(loadWarnings, fmt.Sprintf("theme: %v — using the default palette", themeErr))
	}
	warnings = append(loadWarnings, warnings...)
	api := herdrapi.New()

	opts := picker.Options{
		Hosts:       hosts,
		Theme:       th,
		ShowPreview: cfg.ShowPreview,
		OpenPanes:   openSessions(api),
		Warnings:    warnings,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.Probe && len(hosts) > 0 {
		opts.Probes = probe.Run(ctx, targetsFor(hosts), time.Duration(cfg.ProbeTimeoutMS)*time.Millisecond)
	}

	sel, ok, err := picker.Run(opts)
	if err != nil {
		return err
	}

	self := os.Getenv("HERDR_PANE_ID")
	if !ok {
		closeOverlay(api, self)
		return nil
	}

	if err := performSelection(api, cfg, sel, readCaller(os.Getenv("HERDR_PLUGIN_STATE_DIR"))); err != nil {
		// Hold the overlay open with the error on screen. Closing here would
		// take the only explanation with it.
		fmt.Fprintf(os.Stderr, "\nherdr-ssh: %v\n\npress enter to close\n", err)
		fmt.Fscanln(os.Stdin)
		closeOverlay(api, self)
		return err
	}
	closeOverlay(api, self)
	return nil
}

// closeOverlay dismisses the picker pane. Best effort: if the pane is already
// gone, saying so is noise.
func closeOverlay(api herdrapi.Client, paneID string) {
	if paneID == "" {
		return
	}
	if err := api.PaneClose(paneID); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: could not close the picker pane: %v\n", err)
	}
}

// runConnect opens a session for an alias without the picker, so the plugin is
// scriptable and bindable to a single key for a favorite host.
func runConnect(args []string) error {
	alias := args[0]
	placement := "split"
	for i := 1; i < len(args); i++ {
		if args[i] != "--placement" {
			return fmt.Errorf("%s\nunknown flag %q", usage, args[i])
		}
		if i+1 >= len(args) {
			return errors.New("--placement needs a value: split, tab, or zoomed")
		}
		i++
		switch args[i] {
		case "split", "tab", "zoomed":
			placement = args[i]
		default:
			return fmt.Errorf("placement %q must be split, tab, or zoomed", args[i])
		}
	}

	// Keep the returned Config; only the rejected keys were reset. See runSession.
	cfg, err := pluginconfig.LoadDir(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: %v — ignoring the rejected keys\n", err)
	}
	hosts, _ := loadHosts(sshConfigPath(), cfg)
	for _, h := range hosts {
		if h.Alias == alias {
			sel := picker.Selection{Host: h, Placement: placement}
			return performSelection(herdrapi.New(), cfg, sel, currentCaller())
		}
	}
	return fmt.Errorf("host %q not found in ssh config", alias)
}
```

- [x] **Step 4: Add a `runConnect` test**

Append to `cmd/herdr-ssh/main_test.go`:

```go
func TestRunConnectRejectsAnUnknownAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	err := runConnect([]string{"definitely-not-a-host"})
	if err == nil {
		t.Fatal("err = nil, want a not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %q", err)
	}
}

func TestRunConnectValidatesPlacement(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")

	err := runConnect([]string{"host", "--placement", "sideways"})
	if err == nil || !strings.Contains(err.Error(), "must be split, tab, or zoomed") {
		t.Fatalf("err = %v, want a placement validation error", err)
	}
	if err := runConnect([]string{"host", "--placement"}); err == nil {
		t.Fatal("err = nil, want an error for a value-less --placement")
	}
	if err := runConnect([]string{"host", "--nope"}); err == nil {
		t.Fatal("err = nil, want an error for an unknown flag")
	}
}
```

- [x] **Step 5: Run the full suite**

Run: `go test ./... -count=1 -v`
Expected: PASS across all packages.

Counts are measured here, never carried forward by arithmetic:

```bash
go test ./internal/picker/ -list '.*' -count=1 | grep -c '^Test'
go test ./cmd/herdr-ssh/ -list '.*' -count=1 | grep -c '^Test'
```

`internal/picker` measured **60** at `4a55f84`, and this task does not touch that package, so 60 is its final figure for this plan. Re-measure it anyway: the absolute also counts test-only commits that no task here claims, so it moves without any task changing. The `cmd/herdr-ssh` figure is **not knowable before this task lands** — this task's own test step is what creates `main_test.go` — so measure it after the suite is green and record what you got. Do not write a predicted number into this line; that is how the `42` and `30` it replaces went stale.

**Tasks 14-19 were verified as a chain, not individually.** They were layered onto the real committed code for Tasks 1-13 in a scratch module and built together with `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go test -race ./...` — all clean, with the counts above. Two defects surfaced that no single-task reading would have caught, both from `internal/` changes landing _after_ these tasks were written:

- **The `Load` rename.** `pluginconfig.Load` became `LoadDir` and `theme.Load` became `LoadFile`, so four call sites in Tasks 16 and 19 were `undefined`. `theme.LoadFile` also gained a second return value, which is why Task 19's prelude reports it into the footer rather than just renaming the call. The lesson generalizes: **when a task renames or reshapes an exported symbol, grep this plan for callers before marking it done.** A later task's code is not compiled by anything until its implementer gets there.
- **A missing import.** Task 16's `errRenameTest` sentinel needs `"errors"`, which the test file's import block did not have.
- **`runPicker`'s fence below is stale against `4c02061`; read the tree before typing it.** Task 16's fix landed after Task 19 was written, so the fence re-inlines the hold-the-pane-open pattern instead of calling the `fatalInPane` helper that now exists. Three consequences, all real. The inline copy calls `fmt.Fscanln(os.Stdin)` with an unchecked return, which the `errcheck` linter configured in Task 21's `.golangci.yml` rejects — CI fails on it. And `main` prints the returned error to stderr even though `fatalInPane` has already printed it, so any fatal exit from a pane verb reports the same error twice. Call `fatalInPane` and let `main` stay silent about an error a pane verb has already surfaced. This is the third instance of the pattern this note describes, which is why it is now a standing rule rather than a list of incidents.

- [x] **Step 6: Build the binary**

Run: `go build -o bin/ ./cmd/herdr-ssh && ls bin/`
Expected: `herdr-ssh`

- [x] **Step 7: Verify vet and formatting**

```bash
go vet ./...
gofmt -l .
```

Expected: no output from either

- [x] **Step 8: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): add verb dispatch, picker, and direct connect" \
  -- cmd/herdr-ssh/
```

---

### Task 20: Install, bind the key, and smoke-test live

**Files:**

- Modify: `~/.config/herdr/config.toml`

This is the first end-to-end run. Everything before this was unit-tested; this is where the real herdr socket gets involved.

The verification commands below use `jq` (present on this machine). It is a check-only dependency — the plugin itself shells out to nothing but `herdr` and `ssh`.

**Every read-only command in this task was run during planning and its output is recorded below as the literal expectation.** The two that mutate state — `herdr plugin link .` and `herdr server reload-config` — were deliberately not run, because the operator's herdr session was live. Treat their expected output as unverified.

**Most of this task turns out not to need a live session at all.** `herdrapi.New` reads `HERDR_BIN_PATH` and falls back to `herdr` only when it is unset, so pointing that variable at a stub script — one that logs its argv and answers `pane list` with a fixture envelope — runs the whole connect path on the built binary with no herdr server, no linked plugin, and no `ssh`. With `HOME` pointed at a synthetic config the argv matrix reproduces end to end: `--placement split` sends `--target-pane` and `--direction right`; `tab` sends neither; `zoomed` sends the target and no direction; a caller pane absent from the `pane list` fixture has its id dropped while the direction survives (`livePaneID`); a pane labelled `ssh:<alias>` produces `tab focus` then `plugin pane focus` and no open at all; and with `reuse_panes = false` a live session for the selected alias is correctly ignored, with exactly one `pane list` call made for the live-id check — which is the claim `performSelection`'s comment makes about the non-reuse paths paying for a list only when there is an id worth checking. An alias defined only inside an `Include` fragment resolves the same as one in the root file.

This is worth writing down for two reasons. It narrows what is actually unverified to the part that genuinely needs the socket — that herdr parses `herdr-plugin.toml`, registers the action and both pane entrypoints, and that `ssh` connects inside the pane herdr opens — and it means a regression in placement flags, reuse, or pane-liveness is reproducible on any machine without herdr installed.

Two of the checks written on the way there were wrong, in the way this plan keeps recording. A `connect <absent-alias>` lookup reporting `host not found` proves nothing about parsing: the output is byte-identical with no config file present at all, which is why the fixture-alias cases above are the ones carrying the claim. And a missing `Include` target was expected to raise a warning; `parseIncludes` documents that an absent target is silent on purpose, verified against OpenSSH_10.3p1, because warning there false-positives on an optional tool-managed include. The discriminator for that path is a present-but-unreadable target, not an absent one.

- [x] **Step 1: Link the plugin into herdr** — _done; the `jq -e` check below was re-run against the live socket and returned the expected object exactly._

```bash
cd ~/DEVELOPMENT/herdr-plugin-ssh
herdr plugin link .
```

Expected: herdr reports the plugin as linked. Confirm with:

```bash
herdr plugin list --json --plugin purehate.herdr-ssh |
  jq -e '.result.plugins[0] | {plugin_id, enabled, actions: [.actions[].id], panes: [.panes[].id]}'
```

Expected, exactly:

<!-- prettier-ignore -->
```json
{
  "plugin_id": "purehate.herdr-ssh",
  "enabled": true,
  "actions": ["open-picker"],
  "panes": ["picker", "session"]
}
```

This single command covers what used to be two steps — the plugin loaded, it is enabled, the action registered, and both pane entrypoints registered. Three details make the scoping and the `jq -e` necessary rather than decorative, all verified against the live socket on this machine:

- **`herdr plugin list --json` emits one long single-line JSON object**, so `| rg herdr-ssh` prints the entire ~20KB line rather than a readable match. `--plugin ID` scopes it server-side; the flag is real (`herdr plugin list [--plugin ID] [--json]`) even though it is absent from the top-level `--help`, which does not list a `plugin` command at all.
- **An unknown plugin id returns `{"plugins":[],"type":"plugin_list"}` with exit 0.** The exit code alone cannot distinguish "not linked" from "linked fine", which is why `jq -e` is doing real work here: on an empty array `.result.plugins[0]` is `null` and `jq -e` exits 1. Without it this step passes when the plugin failed to load.
- **`herdr plugin action list | rg open-picker` gives a false positive.** `fullerzz.sesh` — the plugin this design is modeled on, and installed on this machine — registers its own action with the id `open-picker`. The old check matches sesh's entry and reports success when this plugin never loaded. The concatenated form `purehate.herdr-ssh.open-picker` the old expectation looked for does not appear in that output at all: `plugin_id` and `action_id` are separate JSON fields, never joined into one string.

If the object is missing or `enabled` is `false`, the manifest failed to load. Read the whole record for the reason:

```bash
herdr plugin list --json --plugin purehate.herdr-ssh | jq '.result.plugins[0]'
```

`manifest_path` and `plugin_root` in that record confirm herdr resolved the link to the right directory.

- [x] **Step 2: Confirm the config parses before touching it** — _done; `config: ok`._

```bash
herdr config check
```

Expected: `config: ok`

Run this _before_ Step 3 so a pre-existing config problem is not mistaken for one the new keybinding introduced.

- [x] **Step 3: Bind the key** — _done, but **not** with the binding this step originally prescribed. The block below is what is actually in the operator's config and what produces a floating box._

Append to `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+i"
type = "popup"
command = "/Users/operator/DEVELOPMENT/herdr-plugin-ssh/bin/herdr-ssh picker"
width = "60%"
height = "60%"
description = "SSH picker"
```

**Why this and not the `plugin_action` block this step used to prescribe.** That version was written, bound, and reloaded, and it worked — it just opened a _pane_, because the action it invokes opens the `picker` entrypoint at `placement = "overlay"`, and overlay is a full-pane placement, not a floating one. The floating box is a **keybinding type**, `type = "popup"`, and the sizing lives on the binding. See the placement correction in the findings above.

Two consequences worth stating here rather than leaving to be rediscovered:

- `command` is a **shell command**, not a `<plugin_id>.<action_id>` pair, so it names the built binary by absolute path. `bin/herdr-ssh` is gitignored — **rebuild it after any change to the picker or the popup keeps running the old code.** `go build -o bin/herdr-ssh ./cmd/herdr-ssh`.
- A popup is launched as a bare process, so herdr exports **none** of the plugin-pane environment for it. That is what `cbd1249` fixed and why `cmd/herdr-ssh/env.go` exists; see the correction in the findings above before adding any new `HERDR_*` read.

The `plugin_action` entrypoints stay registered and are still reachable — this binding is an addition, not a replacement.

**`prefix+i` is verified free, and `prefix+r` is verified taken.** `herdr --default-config` (374 lines, the authoritative list of built-in bindings — the live config's own header points at it) binds these single letters: `b c e g h j k l n o p q r s v w x z`. `prefix+r` is among them, which is why the picker does not take it. The operator's config additionally binds `d e f m o t y` and several `shift`/`ctrl` combinations. `prefix+i` appears in neither list, leaving it and `prefix+u` as the only free single letters (`prefix` itself is `ctrl+a`). Re-check both lists before substituting a different key.

- [x] **Step 4: Validate and reload the config** — _done; `config check` returns `config: ok` with the popup block in place, and the reload took effect (the binding fires)._

```bash
herdr config check
herdr server reload-config
```

Expected: `config check` prints exactly `config: ok`, then the reload succeeds. Note the command is `herdr server reload-config` — there is no `herdr config reload`; the live config's header documents this pair.

`config check` was run read-only during planning and returns `config: ok` on the current config, so a diagnostic here means the block just added in Step 3 caused it. `reload-config` was deliberately **not** run during planning: it mutates the running server's state, and the operator's session was live. If the new binding does not take effect, `herdr config reset-keys` backs up `config.toml` and strips custom keybindings — that is the recovery path, and it removes the operator's other sixteen bindings too, so read the backup path it prints before relying on it.

- [ ] **Step 5: Smoke-test the popup by hand** — _partly done. Items 1-1k are **confirmed by the operator on screen**. Items 3, 4 and 12 were **measured under a pty** (note after item 13). Items 2 and 5-11 still need the operator at the keyboard: they are the ones that depend on the operator's own `~/.ssh/config` or on herdr placing, focusing and labelling a pane._

Press `prefix+i`. Verify each of these, in order. Items 1a-1k are the frame itself, and they are the ones to look at first: they are what `cbd1249`, `ed23d73`, `cdf54f3` and `2dab313` changed, and a wrong accent here means the config resolution regressed rather than the theme being wrong.

The frame's target is herdr's own **settings dialog**, which is the floating box the operator already recognises on this terminal. It is not reachable exactly: a popup keybinding exposes only `type`, `command`, `width` and `height`, and herdr draws the popup's border in `[ui].accent` with a `popup` label where the settings dialog's is white and unlabelled. Nothing in the config surface reaches that, so 1a stands as written — one border, herdr's. Everything inside it is 1g-1k.

1. A floating box appears listing hosts from `~/.ssh/config`, in the accent color from `[ui].accent` (`#14e21a` on this machine) — **not** the built-in blue `#89b4fa`, which is what an unresolved `HERDR_CONFIG_PATH` silently falls back to
   - 1a. There is exactly **one** border, herdr's own, labelled `popup`. The frame draws none of its own. Two concentric boxes a cell apart is the defect `ed23d73` fixed, and herdr's cannot be turned off — if a second one is back, the frame regrew a border rather than herdr growing one.
   - 1b. The cursor row is a **full-width accent band with a `▸` marker**, running edge to edge with no gaps in it. Dark slots inside the band mean a column separator lost its styling: reverse video only paints cells a style actually renders.
   - 1c. `↵ split` in the footer is an **inverted chip**; the remaining hints are muted
   - 1d. Typing a query underlines the matched characters on the banded row and accents them on every other row
   - 1e. The hostnames form **one column**, not a ragged edge stepping with each alias's length
   - 1f. The list **fills the popup**. Blank space below the last host, with more hosts than rows drawn, means a row ceiling is back — the popup's size is the operator's to set on the keybinding (`width`/`height`), and the plugin's job is to fill whatever it is handed. If the popup is simply taller than the host list, shrink the binding; that is config, not a bug. The binding is `width = 94`, `height = 28` in cells, which was `60%`/`60%`: the plugin fills what it is given, so a percentage of a large terminal is a wall rather than a dialog. **Bare integers, not quoted** — `herdr`'s `PopupSize` is an integer cell count _or_ a percentage string matching `^(100|[1-9][0-9]?)%$`, and nothing else. `width = "94"` is not a smaller number, it is a `TOML parse error at line 311, column 9` that stops the whole config loading. Measured by writing it that way first.
   - 1g. A **blank padding row** opens and closes the frame, so nothing touches herdr's border. Content flush against the border reads as a pane with a line round it rather than as a dialog.
   - 1h. The title `ssh` is **plain foreground, not accent and not bold**. It labels an input; an accent title competes with the cursor band for the eye. Accent in the title means the de-accenting in `2dab313` regressed — this is the one item where the _absence_ of accent is correct, so read it against item 1 rather than with it.
   - 1i. The rule under the title is **inset two columns on both sides**, landing on the same column the title and the nav hints start at. A rule running the full pane width while everything around it is indented is a divider drawn across a dialog rather than the dialog's own.
   - 1j. The footer is **two lines**: `↑↓ select  ^o preview  ^u clear` left-aligned at the frame indent, then the action keys centred under the list with `↵ split` as the chip. One crammed line with `·` separators is the old footer.
   - 1k. Aliases start on the **same column whether or not the cursor is on them** — the `▸` sits in the gutter between the frame indent and the alias, so the list does not shift sideways as the cursor moves through it.
2. The `colima` host from the existing `Include` is present — the include chain resolved
3. Typing filters the list; the cursor snaps back to the top — **measured**, see the pty note below
4. Status markers fill in shortly after the box opens (`●` reachable, `○` not); first paint did not wait on the network — **measured**, see the pty note below
5. `enter` splits the current pane and lands at an ssh prompt for the selected host
6. The popup closed itself after acting
7. The new pane's title reads `ssh:<alias>`
8. `prefix+i` again shows `▪` next to that host
9. Selecting it again focuses the existing pane instead of opening a second one
10. `^n` on that same host does open a second pane
11. `^t` opens a tab, `^z` opens a zoomed pane
12. `^o` toggles the preview, and the preview shows a `source <file>:<line>` line — **measured**, see the pty note below
13. `esc` closes the popup and changes nothing — **half measured**: the picker exits `0` with no error output, but that was outside a popup, so "changes nothing" is still an operator observation

**Items 3, 4 and 12 were driven without the operator, under a pty against a
synthetic `HOME`.** Worth writing down because a Bubble Tea program cannot be
exercised from a pipe: it opens `/dev/tty` and asks the kernel for the window
size by ioctl, so the run needs a real pty with an explicit `TIOCSWINSZ`
(94×28 here, matching the binding) before it will render anything. A synthetic
`HOME` is what keeps this off the operator's real config — `sshConfigPath()`
goes through `os.UserHomeDir()`, which honours `$HOME` on unix — and every
`HERDR_*` variable was stripped from the child so the run could not reach the
live server or pick up the operator's plugin config.

What it showed, against three synthetic hosts: typing `b` narrowed the list to
`bravo` alone with the cursor on it, `^u` restored all three and emptied the
query, `^o` removed the preview block and its `─────` separator, the preview
carried `source …/.ssh/config:4`, the first frame drew no status markers and a
later frame drew `○` on the two probed hosts, and `esc` exited 0.

This covers the parsing, the filtering, the probe's asynchrony and the frame —
everything that does not depend on herdr. It does **not** cover placement,
focus, labelling or teardown, which is why items 5-11 and 13 are still the
operator's.

Then resize the pane deliberately short — roughly 6 to 8 rows — and check three more.
These are the ones unit tests cannot reach: `view_test.go` asserts how many lines
`View` emits, which is the only thing it can see. What the terminal does with those
lines is not observable from Go.

14. The preview sheds fields from the bottom as the pane shrinks — `source` goes first, then `Port`, then the whole block including its `─────` separator — and the key-hints row never disappears. Each line the preview gives up should become another host row, so the box stays full rather than shrinking.
15. `^o` in a pane too short for the preview does nothing, and specifically does not corrupt the frame. This is accepted behavior, not a bug: the preview cannot fit whatever the toggle says. Worth an explicit look because it is the one key that silently no-ops.
16. **Watch for smearing rather than clean truncation at the floor.** Below about 7 rows the frame stops shrinking and is allowed to exceed the pane. (It was 4 before the frame became a dialog and 9 while the frame drew its own border; the title block and the footer cost 5 lines now that the border is herdr's, and the floor is those plus one host row and the overflow notice. `frameChrome` in `view_test.go` is the number to re-derive this from if the frame changes again.) The floor exists because one host row and the key hints are worth more than a perfect fit. But the picker renders **inline, not in the alt screen**, and bubbletea's inline renderer sizes the frame to the content (`cursed_renderer.go` sets `frameArea.Max.Y = content.Height()`) instead of clamping to the terminal. So an over-tall frame may scroll the surrounding pane or leave residue behind after `esc` rather than being cut off at the pane edge. If that happens, the overflow is the trigger but the renderer is the cause — file it against the floor's size, not against the shedding logic, and note whether `esc` leaves the pane clean.
17. Run a `session` verb that **exits non-zero** — point it at a host that refuses the connection — then check `herdr pane list`. Nothing should be left behind: no pane, and no stray `ssh:<alias>` entry. A failed connect that leaks a pane is the defect `f620f4a` fixed.
18. Run one that **exits zero** — connect, then type `exit` at the remote shell — and check `herdr pane list` again. Record whether the pane is reaped or persists. Both are defensible; the plan needs to know which one herdr actually does.
19. If it persists, check whether it **keeps its `ssh:<alias>` label**. A labelled but dead pane is precisely what `performSelection`'s reuse scan matches on, so the next pick would focus a corpse instead of opening a session — the same defect class as `f620f4a`, reached from the other side. None of items 17-19 is covered by a unit test, which is why they are here.

- [ ] **Step 6: Verify the label round-trip from the CLI** — _unticked, and it is **Step 5 item 5 that blocks it**, not the live session: herdr is running and this command works today. Until a host has been picked there is simply no session pane for the label to be on. Run against the live server this session, it returned only `Explorer` labels — which is the correct answer for "no host picked yet", and would be indistinguishable from a labelling defect if run before Step 5. Perform Step 5 first, then this._

```bash
herdr pane list | jq -c '[.result.panes[] | select(.label) | {pane_id, label}]'
```

Expected: an entry whose `label` is `ssh:<alias>` for the host selected in Step 5, e.g. `{"pane_id":"w5:pE","label":"ssh:nixos-dev"}`.

`select(.label)` is the load-bearing part. herdr **omits** the `label` key entirely on an unlabeled pane rather than sending `null` — the same fact `herdrapi.Pane.Label` is a `*string` for — so this filter keeps only the labeled panes instead of erroring on the rest. Run read-only during planning against 22 live panes; it correctly returned only the two labeled ones. `| rg 'ssh:'` would instead dump the whole single-line JSON blob for all 22.

- [x] **Step 7: Commit the plugin config note** — _satisfied by Task 21; nothing to commit here._

  The "needs a live herdr session" reason this step used to carry was copied from Steps 1-6 and was false: this step has no live-session dependency, and its own body says so on the next line. It was the one incorrect entry among the unticked boxes in Task 20 — worth naming, because an unticked box with a plausible-looking reason is harder to spot than one with no reason at all.

The herdr config lives outside this repo, so record the binding in the README instead (Task 21). Nothing to commit here.

If any step failed, fix it and re-run from Step 1. Do not proceed to Task 21 with a failing smoke test.

---

### Task 21: CI, README, and publish

**Files:**

- Create: `.github/workflows/ci.yml`, `.golangci.yml`, `README.md`, `LICENSE`
- Modify: `internal/probe/probe.go` — unchecked `Close` (Step 1), dial seam
  (Step 1b)
- Modify: `internal/probe/probe_test.go` — `defer ln.Close()` (Step 1),
  concurrency test (Step 1b)

- [x] **Step 1: Fix the unchecked `Close` calls errcheck will flag** — _measured at `4844fd3`: both discards are in place, and reverting them yields `2 issues: * errcheck: 2` at `probe.go:89` and `probe_test.go:41`, exit 1; restored, `0 issues.` and exit 0. The mutants ran on an export of `e9ba82d`, which is the same code — `4844fd3` changes only this plan._

Enabling `errcheck` retro-breaks code that landed in Task 11 — this step exists
because the linter was run against the real tree while planning, not because
anything is wrong with Task 11's logic. Two sites, both in `internal/probe`,
both cases where the error genuinely carries no information. Make the discard
explicit at the call site rather than adding `exclude-functions` to
`.golangci.yml`: an exclude silences the whole class repo-wide, including the
places where a `Close` error is the only report you get that a buffered write
never reached the disk.

`internal/probe/probe.go` — the dial has already answered the question:

```go
			conn, err := d.DialContext(ctx, "tcp", t.Addr)
			if err == nil {
				// The dial succeeding is the whole answer; a close error says
				// nothing about reachability.
				_ = conn.Close()
			}
```

`internal/probe/probe_test.go` — `defer ln.Close()` becomes:

```go
	defer func() { _ = ln.Close() }()
```

**The `d.DialContext` line in the first fence is pre-seam, and correct here.** Step 1b is
what replaces it with the injected dial, so at this point in the sequence the fence matches
the tree it was written against, and Step 1b's own staleness flag covers the divergence from
`HEAD`. Only the `_ =` is Step 1's change — do not modernize the surrounding line to match
the current tree, which would make this step unreadable as history without fixing anything.

- [x] **Step 1b: Put the concurrency bound under test** — _measured at `4844fd3`: `maxInFlight` 16 → 1000 is killed, uniquely, by `TestRunBoundsConcurrentDials`. The survivor this step was written to kill no longer survives, which is the claim the step makes and the only one worth checking._

Numbered `1b` rather than renumbering Steps 2 through 7, because the standing
rules section cites "Task 21 Step 5" by number and renaming would break that
reference.

`maxInFlight = 16` at `probe.go:14` is enforced by nothing a test observes. I
raised it to `1000` and ran the full package suite: green. The constant's own
comment says a large ssh config "should not open a hundred sockets at once just
to draw a status dot," and on a pentest workstation that is a file-descriptor
and network-noise property, not a style preference — so a refactor that drops
the semaphore should not be able to pass CI silently. Every other mutant tried
in this package and in `pluginconfig` was killed, including the `<= 0`
probe-timeout boundary; **this was the only survivor at the sha this step was
written against, and it does not survive now.** `dd54c30` added
`TestRunBoundsConcurrentDials` and killed it: re-measured at `e9ba82d`,
`maxInFlight` 16 → 1000 fails that test and no other. The uniqueness is the part
worth keeping, because it names the one test that must not be deleted — but the
survivorship is not, and it is stamped rather than deleted for the same reason
Step 3's stamp was extended rather than moved. A survivor is a claim about a sha,
not about the repo, and a survivor list that outlives its own fix sends the next
reader off to write a test that already exists.

**The bound cannot be observed from outside `Run` as currently written.** A dial
that succeeds is released immediately, so a real listener never sees sixteen
sockets at once, and the only remaining signal is wall-clock batching, which
makes for a flaky CI test. So add the smallest seam that makes it observable —
in `probe.go`, replace the two dial lines with a package-level function value:

**The fences in this step and in Step 1c are stale against `HEAD`; read the tree
before typing them.** They describe the package-level `var dialContext` form as
committed in `dd54c30`. `79d18a9` then replaced it with a `dialFn` named type
passed in as a parameter — `Run` delegates to `run(ctx, targets, timeout,
dialContext)` and `rg '^var ' internal/probe/*.go` now returns nothing — because
a package-level var is shared mutable state that no two tests can fake under
`t.Parallel()`. The stub-swap idiom in both steps (`orig := dialContext;
defer func() { dialContext = orig }()`) therefore no longer compiles against the
current tree. Neither `79d18a9` nor `edd2882` has a step in this task; both are
unauthorized by any task in this plan, the same shape as the gap Task 16 Steps 6
through 8 closed. Left as a flag rather than rewritten, because
`internal/probe/` is owned elsewhere and the steps should be authored against
the tree by whoever owns it.

```go
// dialContext performs one probe dial. It is a variable so a test can observe
// how many dials are in flight at once without opening real sockets. Nothing
// in production reassigns it.
var dialContext = func(ctx context.Context, timeout time.Duration, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp", addr)
}
```

and inside the goroutine in `Run`, the dial becomes:

```go
			conn, err := dialContext(ctx, timeout, t.Addr)
			if err == nil {
				// The dial succeeding is the whole answer; a close error says
				// nothing about reachability.
				_ = conn.Close()
			}
```

Note the `_ = conn.Close()` — that is Step 1's change, already applied by the
time you reach this step. Do not revert it.

This is a production change made for testability, which is worth naming rather
than sliding in: it adds one indirection to a hot-ish path and a package-level
variable that a future author could mistake for configuration. The comment is
there to say it is not. The alternative considered and rejected was a
timing-based test — dial sixteen blackholed TEST-NET-1 addresses and assert the
elapsed time shows batching — which needs no production change but asserts on
wall-clock duration in CI. If you disagree with this trade, say so before
implementing; it is a judgment call, not a derived requirement.

Then in `probe_test.go`:

```go
func TestRunBoundsConcurrentDials(t *testing.T) {
	var mu sync.Mutex
	var cur, peak int
	release := make(chan struct{})

	orig := dialContext
	defer func() { dialContext = orig }()
	dialContext = func(context.Context, time.Duration, string) (net.Conn, error) {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		mu.Unlock()
		<-release // hold the slot so concurrency can accumulate
		mu.Lock()
		cur--
		mu.Unlock()
		// A nil conn is safe: Run only calls Close when err is nil.
		return nil, errors.New("probe: test dial")
	}

	// Fixed, and deliberately NOT derived from maxInFlight. A test written in
	// terms of the constant it is testing scales with its own mutant.
	const nTargets = 64
	targets := make([]Target, 0, nTargets)
	for i := 0; i < nTargets; i++ {
		targets = append(targets, Target{Alias: fmt.Sprintf("h%02d", i), Addr: "192.0.2.1:22"})
	}

	out := Run(context.Background(), targets, time.Second)

	// Wait for the bound to be reached, then give it room to be exceeded. A
	// correct Run pins cur at exactly maxInFlight; an unbounded one runs
	// straight past it to len(targets).
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := cur
		mu.Unlock()
		if got >= maxInFlight || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	close(release)

	n := 0
	for range out {
		n++
	}

	if gotPeak != maxInFlight {
		t.Errorf("peak concurrent dials = %d, want %d", gotPeak, maxInFlight)
	}
	if n != len(targets) {
		t.Errorf("got %d results, want %d", n, len(targets))
	}
}
```

The assertion is `!=`, not `<=`. `<=` would pass if the semaphore were made so
small that the probe crawled, and it would also pass on a build where the dials
never overlapped at all — the second is the same failure class as the rest of
this plan, a check that reports success about something other than what it looks
like it checks. Pinning the exact value fails in both directions.

**`nTargets` is a literal on purpose, and this is the whole trick.** The first
draft of this step said `maxInFlight*4`, which reads naturally and is wrong. A
test written in terms of the constant it is testing scales with its own mutant:
raising `maxInFlight` to `1000` also raised the target count to `4000`, the
semaphore correctly pinned the peak at `1000`, and the assertion
`gotPeak != maxInFlight` compared `1000` against `1000` and passed. The draft
asserted "the semaphore equals the constant," which is true for _every_ value of
the constant, rather than "concurrency is bounded." It was caught by running the
mutant against the draft before this step was committed, not by reading it —
reading it is how it got written.

With `nTargets` fixed at 64 the two mutants that matter both die:

| mutant                     | peak | want | result                  |
| -------------------------- | ---- | ---- | ----------------------- |
| semaphore removed entirely | 64   | 16   | FAIL after 0.05s        |
| `maxInFlight` 16 → 1000    | 64   | 1000 | FAIL after 2.05s        |
| _(unmutated)_              | 16   | 16   | pass, 0.24s, race clean |

The second takes two seconds because the wait loop never sees `cur` reach 1000
and falls through to its deadline. That is a slow _failure_ only; the passing
path returns in a quarter second.

Add `errors`, `fmt`, and `sync` to the test file's imports if they are not
already there. Confirm the test earns its place by re-running both mutants
above, on a scratch copy outside the repo. Revert and `diff` to prove
byte-identical.

- [x] **Step 1c: Pin that `Run` forwards the timeout and address** — _measured at `4844fd3`: three mutants driven through `Run`, each killed by the test named for it — a dropped timeout by `TestRunForwardsItsTimeoutToTheDial`, a dropped context by `TestRunForwardsItsContextToTheDial`, and `t.Addr` replaced by a constant by `TestRunForwardsTimeoutAndAddrToTheDial` among three. Driven through `Run` rather than `run`, so the delegation hop is inside what the mutants cover._

Found by exploratory mutation after Step 1b was already committed, so it is a
step rather than an edit to one. `Run`'s `timeout` parameter was reaching
`net.Dialer` and being observed by nothing: mutating the dial to
`dialContext(ctx, 0, t.Addr)` left the whole package green. That predates the
seam — the same mutant on the pre-seam tree, `net.Dialer{Timeout: 0}`, also
survives — so Step 1b relocated the gap rather than creating it.

**The point of this step is that the seam closes it for free.** Before Step 1b
the timeout vanished inside `net.Dialer` and the only way to observe it was
wall-clock, which is the flaky timing test the plan rejected. After Step 1b it
is an argument to a swappable function, so a fake reads it directly — no
network, no clock, 0.00s. That is the second thing the indirection bought, and
it is worth recording: a production change made for testability is easier to
defend at two tests than at one.

```go
func TestRunForwardsTimeoutAndAddrToTheDial(t *testing.T) {
	var mu sync.Mutex
	got := map[string]time.Duration{}

	orig := dialContext
	defer func() { dialContext = orig }()
	dialContext = func(_ context.Context, timeout time.Duration, addr string) (net.Conn, error) {
		mu.Lock()
		got[addr] = timeout
		mu.Unlock()
		return nil, errors.New("probe: test dial")
	}

	targets := []Target{
		{Alias: "a", Addr: "192.0.2.1:22"},
		{Alias: "b", Addr: "192.0.2.2:2222"},
	}
	for range Run(context.Background(), targets, 7*time.Second) {
	}

	mu.Lock()
	defer mu.Unlock()
	want := map[string]time.Duration{"192.0.2.1:22": 7 * time.Second, "192.0.2.2:2222": 7 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("dialed %d addrs, want %d: %v", len(got), len(want), got)
	}
	for addr, w := range want {
		if g, ok := got[addr]; !ok {
			t.Errorf("addr %q never dialed", addr)
		} else if g != w {
			t.Errorf("addr %q got timeout %v, want %v", addr, g, w)
		}
	}
}
```

Two ports that share a prefix (`:22` and `:2222`) are deliberate, so a
prefix-comparison bug cannot pass.

Measured, full package suite each run, revert diffed byte-identical:

| mutant                               | result                                                |
| ------------------------------------ | ----------------------------------------------------- |
| `dialContext(ctx, 0, t.Addr)`        | FAIL — this test **only**                             |
| `dialContext(ctx, timeout, t.Alias)` | FAIL — this test + `TestRunReportsReachability`       |
| `Skip` check removed                 | FAIL — `TestRunReportsReachability` (already covered) |

**Known limit, and state it in the test's comment rather than overclaiming.**
Because every target gets the same timeout, this pins the addr/timeout
_pairing_ only as strongly as one uniform value allows — a mutant that shuffled
which timeout went to which target would not die. Either give the targets
distinguishable timeouts or narrow the test's stated claim to "every dial
receives `Run`'s timeout and its own address." Do not leave a comment claiming
the stronger property.

**Conditional rule on `probe.Result`: extending it re-opens both delegation
hops.** `Result` carries `Alias` and `Up` and nothing else, and that collapse is
what makes the two tests above work at all. Against a _live_ loopback listener
only a dropped timeout or a dropped context lets the connect succeed, so
`Up: true` is a real signal. Against a closed port, `context.Canceled`,
`i/o timeout` and `connection refused` all collapse to `Up: false` and nothing
discriminates — demonstrated by swapping both tests' addresses to
`127.0.0.1:1`, which returns both `Run`-level mutants to SURVIVED with a green
suite.

The trap is long-fused: **if `Result` ever grows an `Err` field, both hops
silently un-pin unless the tests move with it.** Once errors are
distinguishable a closed port starts discriminating, so both tests begin to look
simplifiable back to a dead address — and would still pass. The change that
makes these tests appear improvable is the same change that guts them.

So the rule: extending `Result` is not done until the two `Run`-level delegation
mutants — `timeout → 0` and `ctx → context.Background()` — have been re-scored
and still die. `Run`'s production doc comment already carries the closed-port
warning; this is the plan-side half, which says what to do when the premise
changes rather than only what not to violate.

Two things this is not. It is **not a defect**: nothing today needs error
discrimination, and `README.md`'s marker table already renders `Up: false`
honestly as "port not reachable" rather than claiming the host is down. The
current code and docs are correct. And it is **not a request to add an `Err`
field** — leave `Result` as it is. Reported by `impl-probe-bound`.

- [x] **Step 2: Write the CI workflow** — _measured at `250914b`._

`.github/workflows/ci.yml`:

<!-- prettier-ignore -->
```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.27"
      - name: Verify formatting
        run: test -z "$(gofmt -l .)"
      - name: Vet
        run: go vet ./...
      - name: Lint
        uses: golangci/golangci-lint-action@v8
        with:
          version: latest
      - name: Test
        run: go test -race ./...
      - name: Build
        run: go build -o bin/ ./cmd/herdr-ssh
```

Add a minimal `.golangci.yml` so the linter's defaults are explicit rather than
version-dependent:

<!-- prettier-ignore -->
```yaml
version: "2"
linters:
  enable:
    - errcheck
    - govet
    - ineffassign
    - staticcheck
    - unused
```

**The action major version and the config `version:` are coupled — do not
"upgrade" one without the other.** This originally said `@v6`, which cannot
read a `version: "2"` config: per the action's own compatibility table,
`v7.0.0` is the first release that "supports golangci-lint v2 only", and
`v8.0.0` requires golangci-lint >= v2.1.0. Pairing `@v6` with the config above
fails in CI and nowhere else, which is the expensive place to find out.

`version: latest` is kept deliberately, but note the tradeoff: a new
golangci-lint release can turn this repo's CI red with no change to this repo.
If that happens, pin the minor (`version: v2.1`) rather than deleting the lint
step.

- [x] **Step 3: Write the README** — _the embedded copy is gone; `README.md` is the artifact and this step no longer restates it. The stamp history that made that necessary is under the pointer below._

`README.md`:

**The README is not reproduced here. Read `README.md` — it is the artifact.**

This block used to carry a byte-for-byte copy, and that copy went stale on
three of the four edits that touched the README: `20f6166` added a
`## Development` section and left it behind, `fc01b5e` re-embedded it,
`4844fd3` re-certified it at 105 lines against 105, and the install section
was rewritten again after publication. A duplicate that has to be re-certified
after every edit is not documentation of the README, it is a second README
that nobody runs `prettier` over and nobody reads.

What the rewrite after publication fixed, both of which mattered because the
repo is public by then:

- **"Once this repo is published, the shorter route works too"** — it is
  published; the conditional was describing a state that no longer held.
- **The one documented binding was `type = "plugin_action"`, which docks.** The
  README's own first line promises "a floating fuzzy picker", and that binding
  does not produce one. Only `type = "popup"` floats. A reader following the
  install section got a docked pane and no way to tell whether the plugin or
  their config was at fault. Both bindings are documented now, each labelled
  with what it actually does, and the popup form carries the bare-integer
  `width`/`height` warning — `width = "94"` is a TOML parse error that stops
  the whole config loading.

The design intent the step was written to capture, which the file still has to
satisfy, is below.

Two things in this README were wrong when written and are corrected above; both were caught by checking it against the committed code and the live herdr rather than by rereading it:

- **The config path was `~/.config/herdr/plugin-config/<id>/`, which does not exist.** The real directory is `~/.config/herdr/plugins/config/<id>/`, confirmed by `herdr plugin config-dir` and by `ls`. This is the worst shape of documentation bug: a user would create the wrong directory, herdr would never read it, and nothing would report an error — their config would silently do nothing. The plugin code is unaffected, since it reads `$HERDR_PLUGIN_CONFIG_DIR` rather than building the path.
- **The keys table omitted `^u` and `^w`**, which Task 13 implements and tests. Check the table against the `k.Mod&tea.ModCtrl` switch in `internal/picker/model.go` before publishing, not against this plan — the code is the source of truth by then.

- [x] **Step 4: Add the license** — _measured at `250914b`._

Write a standard MIT `LICENSE` with `Copyright (c) 2026 purehate`.

- [x] **Step 5: Verify CI passes locally**

```bash
test -z "$(gofmt -l .)" && go vet ./... && go test -race ./... && go build -o bin/ ./cmd/herdr-ssh
```

Expected: `gofmt` and `go vet` silent, then one `ok` line per package from
`go test` — seven of them — and `bin/herdr-ssh` afterward. Not "no output":
`go test` is chatty on success, so silence at that point in the chain means it
short-circuited before running, not that it passed.

CI runs the linters through `golangci-lint`, which is probably not installed
locally. Run the two that actually have something to say about this codebase
directly — `go run pkg@version` ignores this repo's `go.mod`, so it cannot
perturb the pinned dependency set (verify with `git diff --exit-code go.mod go.sum`
afterward if you want to be sure):

```bash
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run github.com/kisielk/errcheck@latest ./...
```

Expected: **a count to triage, not silence.** Clean is the outcome of this step,
not its gate. What governs the count is the exemption table below, so read a
nonzero result against that table and route each site. This step's stated output
has been wrong three times — zero predicted through Task 14, five measured after
Task 19, back to zero at `a62d073` — and the seam below was expected to move it
again.

Both commands float on `@latest`, so the expected output is unpinned even when
the code holds still. That is also a reproducibility hazard for everything
measured below: a future reader running these commands can be handed a different
linter than the one every count in this step was measured with, so the counts
are pinned to a version the command does not pin. The reproducible form of the
check is `golangci-lint` pinned, which is also the one that mirrors CI:

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run
```

**The pass condition for that pinned command is `0 issues` on stdout and exit
0 — both, read positively.** Do not write it as "no output". `golangci-lint`
does not fail open, but a run that linted nothing is also quiet, and absence
does not let a reader tell "clean" from "did not run": a bad config path, a
build failure in a package, or a typo in the module path can all read as calm.
`0 issues.` is the affirmative statement that it ran and found nothing, and the
exit code is what CI actually gates on.

**The two standalone commands have no such line, so do not expect one of them.**
Measured on a clean export, `staticcheck ./...` and `errcheck ./...` each print
zero bytes on stdout _and_ stderr when clean, at `staticcheck` 2026.2.1 (0.8.1)
and `errcheck` v1.20.0. For those two the checkable condition is exit 0 with
zero findings: the exit status is the only thing that separates clean from
did-not-run, because the silence is identical either way. `0 issues.` belongs to
`golangci-lint` alone. Reported by `plan-author-sizing`, whose Verification
Checklist lines are worded that way for this reason — the two sections agree.

Measured, not estimated. Against a clean `git archive` export of the Task 13
tree with Task 14's `view.go` and `Run` fences spliced in and the `View` stub
removed — a dry run of everything through Task 14 — the package built and
`errcheck` reported exactly two findings, `internal/probe/probe.go:53` and
`internal/probe/probe_test.go:15`, the two sites Step 1 fixes. `staticcheck` was
clean.

The same export was then run through `golangci-lint` v2 with the
`.golangci.yml` above, which is the only way to exercise the config itself:

```
2 issues:
* errcheck: 2
```

Same two sites, no others, and the `version: "2"` config parsed — so the `@v8`
pairing is right. Worth noting what this rules out: `view.go` discards an error
on twelve writes into a `strings.Builder` — three `fmt.Fprintf(&b, …)` and nine
`b.WriteString(…)` — so `errcheck` looked certain to flag Task 14 the way it
retro-flagged Task 11. It does not, and the reason needs stating precisely,
because the obvious reading of it is wrong and that reading is what failed.

**The exemption is syntactic, and it is narrower than "writer type".** `errcheck`
ships a `DefaultExcludedSymbols` list whose entries name a function _and_ its
destination — `fmt.Fprintf(*strings.Builder)`, `fmt.Fprintf(*bytes.Buffer)`,
`fmt.Fprintf(os.Stderr)` — next to method entries like
`(*strings.Builder).WriteString`. What it exempts is the `os.Stderr` identifier
**as written at the call site**: not stderr-the-stream, and not
`fmt.Fprint*`-the-function. Measured in an isolated scratch module under
`errcheck` v1.20.0, then confirmed byte-identical under `golangci-lint` v2.13.2
with a `version: "2"` config:

| Call site                                                 | Flagged |
| --------------------------------------------------------- | ------- |
| `fmt.Fprintf(os.Stderr, …)`                               | no      |
| `fmt.Fprintf(&b, …)` where `b` is `strings.Builder`       | no      |
| `b.WriteString(…)`                                        | no      |
| `fmt.Fprintf(os.Stdout, …)`                               | **yes** |
| `fmt.Fprintln(os.Stdout)`                                 | **yes** |
| `fmt.Fprintf(w, …)` where `var w io.Writer = os.Stderr`   | **yes** |
| `fmt.Fprintf(w, …)` where `w` is an `io.Writer` parameter | **yes** |

`os.Stdout` gets no exemption at all, so it is flagged sitting directly beside an
`os.Stderr` line that is not. The builder rows are exempt for a third reason
again — a `*strings.Builder` write cannot fail — which is why `view.go`'s twelve
discarded builder writes are silent under two different entry shapes.

**The exemption dies the moment the writer becomes a variable, even when the
value passed is still `os.Stderr`.** That is the row that predicts instead of
explaining: seaming a diagnostic for testability also exposes it to `errcheck`.
`cmd/herdr-ssh` held seven `fmt.Fprintf(os.Stderr, …)` sites as of `d7591ec` —
two in `connect.go`, three in `main.go`, two in `session.go` — and each becomes a
flagged write the moment it is threaded through an `io.Writer`, so the seam had
to close them in the same change rather than leave them for CI.

Seven is a count taken from the tree, and the two wrong counts are worth keeping
because they were wrong in opposite directions: the seam was originally
authorized over six, and `spec-review-t18`'s table listed eight. Seven includes
`main.go:209`, which arrived in `57efb52` after the authorization was written. A
count copied from an authorization or from another document's table is a claim
about when that text was written, not about the tree.

**Outcome: the seam landed as `9e9dc9b` and produced no new `errcheck`
findings.** That is the intended result rather than a refutation of the rule
above, and the distinction matters because a zero is consistent with both
readings. The evidence that the exemption did die is in the diff, not in the
count: all seven sites moved off the `os.Stderr` identifier onto the threaded
`out`, and all seven are written `_, _ = fmt.Fprintf(out, …)`. Had the exemption
travelled with the value, those discards would have been unnecessary. Findings
here would have meant the rule was right _and_ the commit was incomplete.

The seam's blast radius was nine call sites rather than seven. `cmd/herdr-ssh`
holds ten `fmt.Fprint*` calls before and after; the seven `os.Stderr` sites moved
onto `out` and joined the two — `main.go:41` and `session.go:69` — that `591dcbd`
had already threaded and made explicit. The tenth, `main.go:150`, is still
`fmt.Fprintln(os.Stdout)` on purpose: it terminates the alias printed for the
caller to consume, so it is output rather than diagnostics and does not belong on
a diagnostic writer.

Every `Fprintf` the original Task 14 measurement sampled was a builder write, so
the exemption looked like a property of `fmt.Fprintf`. That is uniform-fixture
blindness inside this plan's own measurement, and it is why the prediction
survived to Task 19 before failing. The three findings that broke it fit the
table exactly: `main.go:41` and `session.go:69` were `fmt.Fprintf(out, …)` on a
seamed writer, and `main.go:150` was `fmt.Fprintln(os.Stdout)`. Not one of them
was `fmt.Fprintf(os.Stderr, …)`.

**The count, measured per commit on clean `git archive` exports** under
`golangci-lint` v2.13.2 with the `.golangci.yml` above:

| Export                 | Issues                        | Exit |
| ---------------------- | ----------------------------- | ---- |
| `35505e3` (`591dcbd^`) | 5 — errcheck 3, staticcheck 2 | 1    |
| `591dcbd`              | 2 — staticcheck 2             | 1    |
| `08c9037`              | 2 — staticcheck 2             | 1    |
| `a62d073`              | 0                             | 0    |
| `9e9dc9b` (the seam)   | 0                             | 0    |
| `687d103`              | 0                             | 0    |

Each delta has exactly one owner. `591dcbd` took it 5 → 2 by making the three
flagged `errcheck` writes explicit, and touched nothing else either linter looks
at. `a62d073` took it 2 → 0 by retiring the comparison behind the `staticcheck`
finding. The seam held at 0.

**That `2` is one finding, not two.** `golangci-lint` counts a finding and its
companion line separately: `SA4023: this comparison is never true` at
`update_cmd_test.go:274`, plus `SA4023(related information)` at `:260` pointing
at the `var none tea.Cmd` that gives the comparison its concrete type, summarized
as `* staticcheck: 2`. The same arithmetic applies to the 5 — five issues, four
findings. Every total in this step is in issues, because that is the unit the
tool prints; say which unit is meant before comparing one of these numbers to a
count of findings.

**Ticked at `687d103`**, where the whole step was measured green: `gofmt` and
`go vet` silent, seven `ok` lines from `go test -race`, `bin/herdr-ssh` built,
and `golangci-lint` v2.13.2 at `0 issues.` with exit 0. `go.mod` and `go.sum`
were byte-identical afterward. That is a statement about `687d103`, not a durable
property of the branch — `impl-cmd-session` still held uncommitted edits under
`cmd/herdr-ssh/` when it was taken, so re-run this step rather than re-reading
this line.

Measure the export, not the working tree. CI lints what was committed, and while
several agents hold uncommitted edits in `cmd/` and `internal/picker/` a
working-tree run reports their in-flight state instead — the first attempt at
this measurement died on a missing `reflect` import that existed only in another
agent's editor.

- [x] **Step 6: Commit** — _ticked because the work landed, and stale because it did not land this way: four commits rather than the two below. The block is preserved rather than corrected — see the note under it._

The probe fix is a separate logical change from the CI/docs addition, so it is a
separate commit:

```bash
git add internal/probe/probe.go internal/probe/probe_test.go
git commit -m "fix(probe): make discarded Close errors explicit" \
  -- internal/probe/probe.go internal/probe/probe_test.go
git add .github/ .golangci.yml README.md LICENSE
git commit -m "docs: add readme, license, and CI workflow" \
  -- .github/ .golangci.yml README.md LICENSE
```

**This step is stale against history and is left as written.** The work landed
as four commits, not two: `1c8f189` for the probe `Close` discards (under the
message `fix(probe): make unchecked Close discards explicit`, not the one
above), then `7933b0f` for `.github/workflows/ci.yml` and `.golangci.yml`,
`e3072dd` for `README.md`, and `e7d646c` for `LICENSE`. The second block is the
reason: bundling a CI contract, a README and a license grant into one `docs:`
commit puts three unrelated changes behind one message, which this plan's own
atomicity rule already rules out. Rewriting the step to match the four commits
would be reconstructing history to fit a document, which is backwards — the road
not taken is more useful visible. Replaying it verbatim cannot reproduce the
tree in any case, because `README.md` and `.github/workflows/ci.yml` have both
been touched again since the step ran — stated without enumerating the commits,
deliberately. The first version of this sentence named `16f3c38` as the one
commit to touch `ci.yml` afterwards, and was made incomplete one commit later by
`da19a81`, which touched the same file. That is the stamp rule at the top of this
file failing on the shortest possible timescale: naming instances reads as
exhaustive and goes stale the moment another one lands, whereas "touched again
since" cannot. Use `git log -- <path>` when the actual list is what you need.

- [x] **Step 7: Publish (operator decision — confirm before running)** — _authorized by the operator and done: the repo is live and public at https://github.com/purehate/herdr-plugin-ssh with the `herdr-plugin` topic set. Both commands below ran clean; `gh repo view` reports `{"isPrivate":false,"repositoryTopics":[{"name":"herdr-plugin"}]}`. Only `refs/heads/main` was pushed — `feat/ssh-picker` stays a local 202-commit archive. **The install-path verification further down is the half still outstanding** (see the note under it)._

Publishing pushes to a public repo. Confirm with the operator first, then:

```bash
gh repo create purehate/herdr-plugin-ssh --public --source=. --push
gh repo edit purehate/herdr-plugin-ssh --add-topic herdr-plugin
```

**Before the push, scan the whole history, not just the worktree.** This repo was
developed against a penetration-testing workstation, its fixtures imitate a real
`~/.ssh/config`, and the operator's screenshots during review carried real
hostnames, real internal and public addresses, and a real username. Making the
repo public makes every reachable commit public with it, and a rewrite after the
fact does not unpublish what was already fetched or indexed. What ran here:
`git log -p --all` (2,450,369 chars) plus the worktree (908,435 chars), grepped
for the specific values seen in review and for the generic classes — public IPv4
excluding RFC1918/RFC5737/loopback/link-local, RFC1918, home paths outside this
project, `.ssh/` filenames, `BEGIN ... PRIVATE KEY`, `AKIA[0-9A-Z]{16}`,
`gh[pousr]_`. Every hit triaged to a synthetic fixture; none of the real values
appeared anywhere. Keep new fixtures on RFC 2606 (`.invalid`, `.example`) and
RFC 5737 (`192.0.2.0/24`) so this stays true.

The `herdr-plugin` topic plus the root `herdr-plugin.toml` is all the index
needs — there is no submission queue. There is no `herdr plugin search`, so
verify the install path directly instead:

```bash
herdr plugin unlink ~/DEVELOPMENT/herdr-plugin-ssh
herdr plugin install purehate/herdr-plugin-ssh --yes
herdr plugin list --json | rg herdr-ssh
```

Expected: the plugin installs from GitHub and appears in `plugin list` with no
error field. Re-run the Task 20 smoke test against the installed copy — the
build step runs on the install host, so a missing `go` toolchain shows up here
and nowhere earlier.

**Not run — this one needs the operator to say go, and it is the only step here
that does.** Everything else in Task 21 either reads state or writes to GitHub.
This sequence mutates the operator's live herdr: `unlink` drops the dev tree
they are still developing in, and `install` replaces it with a GitHub copy under
`~/.config/herdr/plugins/github/`. Two specifics worth knowing before choosing:

- The `prefix+i` popup binding does **not** go through the plugin — it execs
  `/Users/operator/DEVELOPMENT/herdr-plugin-ssh/bin/herdr-ssh` by absolute path. So
  unlinking would not break the picker, and the swap would not exercise the
  binding either. What it does exercise is the `plugin open-picker` action and
  the manifest's `build` step.
- It is reversible — `herdr plugin uninstall purehate.herdr-ssh` then
  `herdr plugin link ~/DEVELOPMENT/herdr-plugin-ssh` puts it back — but only
  one copy of a given `plugin_id` loads at a time, so the dev link is inert for
  as long as the installed copy is present.

The thing it genuinely catches, and nothing earlier does: the manifest's
`go build -o bin/ ./cmd/herdr-ssh` runs on the _install host_, against a
freshly-cloned tree with no `bin/` in it. A toolchain or module problem that the
dev tree papers over surfaces here first.

---

## Verification Checklist

Run after Task 21. Every line must pass before calling this done.

**The epoch for this checklist is `250914b`, and every measurement below was
taken on a clean `git archive <rev> | tar -x -C <scratch>` export of it.** The
per-task tick stamps name that same commit, so the ticks and these gates are one
epoch rather than two. Figures elsewhere in this plan carry their own shas and are
not covered by this one — the point is not that the plan has a single epoch, it is
that no figure in it should be readable as current when it is not. Run on this worktree
instead and the numbers are fiction: several agents hold uncommitted edits, and
a linter reports their in-flight state in the same shape as a real finding.
Measured, running `staticcheck ./...` against the live tree —

```
cmd/herdr-ssh/main_test.go:639:10: undefined: fmt (compile)
exit status 1
```

— which is a peer's half-finished edit presented exactly like a lint finding,
with nothing in the output saying so.

**The two linter gates below use `go run <pkg>@<version>`, and both halves of
that are deliberate.** Neither tool is installed on this machine — `staticcheck`
and `errcheck` are absent from `PATH` and `~/go/bin` does not exist — so the
bare commands exit **127**, and 127 is the outcome no output-based pass
condition can distinguish: it reads as a finding under "grep the output", and as
"not clean" under "zero bytes on both streams", in both cases for a reason that
has nothing to do with this code. The message is the shell's rather than the
tool's, so its length is not even stable across machines (37 and 40 bytes here
against 38 and 41 reported from another shell). **Read the exit code and nothing
else: `0` clean, `1` findings, `127` the tool never ran.** The versions are
pinned for a measured reason rather than tidiness: under Go 1.27 the
then-current standalone staticcheck 2025.1.1 failed with `export data version 4
is greater than maximum supported version 2`, so `@latest` is a moving target
that has already been broken once on this toolchain. The `go run` form needs no
install and does not touch this module's files — `go.mod` and `go.sum` are
byte-identical after both invocations, at `3577905575 1555` and
`3805898435 3298`.

- [x] `go test -race ./...` — exit 0, seven `ok` lines, zero data races, zero failures. Measured at `250914b` on a clean `git archive` export. Read the exit status: `${PIPESTATUS[0]}` is bash syntax and returns empty under zsh, which is one shell idiom away from passing this gate on the strength of seven `ok` lines and no exit code at all.
- [x] `go vet ./...` — exit 0 with no diagnostics. Measured at `250914b` on a clean export.
- [x] `gofmt -l .` — exit 0 and no files listed. Measured at `250914b` on a clean export.
- [x] `go run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./...` — **exit 0, which is the entire pass condition.** Measured at `250914b` on a clean export and re-measured at `d643b6a`: exit 0, zero bytes on stdout _and_ stderr. The silence is real but is not the gate — this tool prints nothing whatsoever when clean, so a run that linted nothing looks identical and only the exit code separates them. (`0 issues.` is golangci-lint's summary line, not this tool's — see Task 21 Step 5, which measures golangci-lint and correctly uses that string.)
- [x] `go run github.com/kisielk/errcheck@v1.20.0 ./...` — **exit 0, which is the entire pass condition.** Measured at `250914b` on a clean export and re-measured at `d643b6a`: exit 0, zero bytes on both streams. `errcheck: 0` here is a _passed prediction_, not merely a clean run — `9e9dc9b` threaded a writer through all seven `os.Stderr` diagnostic sites, which converts them from errcheck-excluded to errcheck-flagged, and landed the explicit `_, _ =` discards in the same commit.
- [x] `go build -o bin/ ./cmd/herdr-ssh` — exit 0 and `bin/herdr-ssh` present. Measured at `250914b` on a clean export.
- [x] `herdr plugin list --json | rg herdr-ssh` — plugin loads with no error field. Measured against the running 0.9.0 server: `{"plugin_id":"purehate.herdr-ssh","name":"SSH Picker","version":"0.1.0","enabled":true,"source_kind":"local","has_error":false}`. `has_error` is `has("error")` rather than a grep for the word — `rg herdr-ssh` matches the whole one-line JSON document for every plugin, so the plugin's own error field and a neighbour's are the same hit. `source_kind` is `local`: this is the linked dev tree, not the GitHub install, which is the separate check in Step 7.
- [x] `prefix+i` opens the picker with real hosts from `~/.ssh/config` — operator-confirmed on screen. **"the overlay" is stale wording:** the binding that ships is `type = "popup"`, which is what renders a floating box; the `overlay` placement in `herdr-plugin.toml` docks and is the other entrypoint. Both exist, and this gate is about the popup.
- [ ] `enter` lands at an ssh prompt in a new split — _unticked: needs the operator at the keyboard. Everything up to the exec is covered by `cmd/herdr-ssh` unit tests; what is not covered is herdr actually placing the split and ssh actually answering._
- [ ] The session pane is labeled `ssh:<alias>` in `herdr pane list` — _unticked: depends on the gate above having been performed. Until a host is picked there is no pane to carry the label, and `pane list` currently reports no `ssh:` label for exactly that reason — absence here is not yet evidence of a defect._
- [ ] Re-picking an open host focuses it; `^n` opens a second pane — _unticked: needs the operator at the keyboard._
- [ ] `^t` and `^z` place a tab and a zoomed pane respectively — _unticked: needs the operator at the keyboard._
- [ ] `esc` closes cleanly with no pane created — _partially measured, and unticked because the halves were measured separately rather than in one pass. "Closes cleanly": driven under a pty, `esc` exits **0** with no error output. "No pane created": a 60s before/after poll of the `pane_id` set across an operator-opened picker held at 22 panes. Neither run observed an `esc` close **inside a live popup**, which is the one thing this gate is for._
- [x] A config with a broken `Include` still lists every other host, and the footer says so. Driven end-to-end under a pty against a synthetic `HOME` — the real binary, a real file on disk, the real footer. All three readable hosts rendered, and the footer carried `…/.ssh/config:1: include unreadable: …/.ssh/conf.d/locked`. **Two corrections to how this gate was written:**
  - _"a warning count" is wrong._ The footer renders the warning **text**; the count was what shipped first and `view.go:564` records why it was replaced ("A count tells the operator that something is wrong and nothing about what"). An operator running this gate as written would look for a number, not find one, and fail correct code.
  - _"a broken `Include`" is ambiguous, and one of its two readings is silent by design._ A **missing** target is deliberately not a warning — `sshconfig.go:474` verified that against OpenSSH_10.3p1, since warning there would false-positive on an optional tool-managed include. The warning case is a **present-but-unreadable** target (mode `000`) or a malformed pattern. Measured both in the same config: the unreadable include on line 1 warned, the missing one on line 2 correctly did not.
