---
description: "Verify mdv builds cleanly, all tests pass without errors or warnings, and LICENSE.md is unchanged."
argument-hint: "(no arguments)"
agent: "agent"
---

# Ensure quality

Run the project's quality gate and report a clear pass/fail summary. Do **not**
make code changes unless the user explicitly asks you to fix what you find -
first just verify and report.

Perform these checks in order and capture the output of each:

1. **Formatting** - `gofmt -l $(git ls-files '*.go')` must print nothing.
   Any listed file is a failure.
2. **Vet** - `go vet ./...` must pass with no findings.
3. **Build without errors or warnings** - run `go build ./...`. It must exit 0.
   Treat any compiler error or non-cosmetic warning as a failure. The macOS
   linker may emit "object file ... built for newer macOS" lines - these are
   known-cosmetic and may be ignored (filter with
   `grep -vE "ld: warning|object file"`).
4. **Tests without errors or warnings** - run
   `go test ./... 2>&1 | grep -vE "ld: warning|object file"`. Every package must
   be `ok` (or `[no test files]`). Any `FAIL`, panic, or test warning is a
   failure.
5. **LICENSE.md is unchanged** - run `git status --porcelain LICENSE.md` and
   `git diff --stat -- LICENSE.md`. If `LICENSE.md` shows any modification, that
   is a **hard failure**; report it prominently.

Finally, print a concise summary table: each check with ✅ pass / ❌ fail and a
one-line note. If everything passes, state that the quality gate is green. If
anything fails, list exactly what failed and where, but do not auto-fix unless
asked.
