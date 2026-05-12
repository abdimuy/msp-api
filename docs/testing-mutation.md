# Mutation testing (gremlins)

We use [`go-gremlins/gremlins`](https://github.com/go-gremlins/gremlins) to
measure how well our tests detect changes in production code. Gremlins
applies small, mechanical edits ("mutants") to the source — flipping `>`
to `<`, replacing `true` with `false`, removing self-assignments, etc. —
and re-runs the test suite for each mutant. If the tests still pass, the
mutant **survived**, meaning the tests did not exercise that line/branch
strongly enough.

Mutation testing complements line coverage: 100% line coverage with 40%
mutation kill rate means the tests touch every line but assert almost
nothing about them.

## When to run

Mutation testing is **slow** (each mutant runs the full test package). It
is not part of the lefthook hooks or the default `make test`.

Run it:

- On demand, when working on `internal/auth/domain` or `internal/auth/app`
  (the modules where false-negative tests are most costly).
- Before merging a refactor that significantly changes branching logic.
- Periodically (e.g. monthly) on critical modules to catch test rot.

## Installation

Gremlins is not vendored. Install once:

```bash
go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
```

Verify:

```bash
gremlins --version
```

## How to run

```bash
# Both auth/domain and auth/app
make test-mutation

# Just one of the two (much faster)
make test-mutation-domain
make test-mutation-app
```

Config lives in `.gremlins.yaml` at the repo root.

## Interpreting output

For each mutated file, gremlins prints one line per mutant:

```
KILLED   internal/auth/domain/user.go:42:18    CONDITIONALS_NEGATION
LIVED    internal/auth/domain/user.go:88:7     INVERT_NEGATIVES
NOT_VIABLE internal/auth/domain/user.go:91:11  ARITHMETIC_BASE
TIMED_OUT internal/auth/domain/user.go:120:4   CONDITIONALS_BOUNDARY
```

- **KILLED** — at least one test failed. Good.
- **LIVED** — all tests still passed. The mutant survived → write a test
  that distinguishes the mutated code from the original.
- **NOT_VIABLE** — the mutated code didn't compile. Doesn't count.
- **TIMED_OUT** — counted as killed (the mutation likely introduced an
  infinite loop, which the timeout caught).

The run finishes with an **efficacy score**:

```
Efficacy: 82.3% (162 killed / 197 viable mutants)
```

Our threshold is **75% efficacy** on `internal/auth/domain` and
`internal/auth/app`. Below that, the run exits non-zero.

## Current thresholds

| Package                  | Efficacy threshold |
|--------------------------|--------------------|
| `internal/auth/domain`   | 75%                |
| `internal/auth/app`      | 75%                |
| everything else          | not enforced       |

Raise these as the modules mature. Add additional packages to
`Makefile` (new `test-mutation-<module>` target) once they reach
parity with auth.

## Known limitations

- **Slow.** A full domain run can take several minutes; app even longer.
  Use `--tags` / package selection to scope down while iterating.
- **Equivalent mutants.** Some mutants are semantically identical to the
  original code and can never be killed (e.g. `i++` vs. `i += 1`). These
  bias the score down. If a specific mutator produces too many on this
  codebase, disable it in `.gremlins.yaml`.
- **Requires a clean working tree.** Gremlins rewrites files on disk
  during the run. Commit or stash changes first; the run restores files
  on completion, but a crashed run can leave mutated source behind.
- **No DB access.** Don't run on integration-test packages — gremlins
  doesn't manage docker/Postgres state, and the suite would explode.
  Hence the `**/testutil/**` and `**/fbtestutil/**` excludes.
- **Not in pre-push.** We deliberately keep this out of lefthook so the
  push hook stays fast. Run manually before high-stakes merges.
