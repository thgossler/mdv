---
description: "Build mdv in debug mode, then run the full Go test suite (unit + end-to-end)."
argument-hint: "(no arguments)"
agent: "agent"
---

# Test mdv

Run a debug build first, then execute all tests.

1. **Build (debug).** Follow the `/build` prompt with the `debug` argument:
   [build.prompt.md](./build.prompt.md). Produce a debug build of `mdv` into
   `build/` for the current OS (Go binaries compiled with `-gcflags=all="-N -l"`
   and without the release `-s -w` strip flags). If the debug build fails, stop
   and report the build errors — do not continue to the tests.

2. **Run all tests.** Execute the complete suite (unit + end-to-end):
   ```sh
   go test ./... 2>&1 | grep -vE "ld: warning|object file"
   ```
   The end-to-end tests in `cmd/mdv` build the binary themselves and exercise
   the real CLI. Do not pass `-short` here — the goal is to run *all* tests.

3. **Report.** Summarize the result: every package should be `ok` (or
   `[no test files]`). Call out any `FAIL`, panic, or warning with the failing
   package and test name. If everything passes, state that the full suite is
   green.
