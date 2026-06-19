---
description: "Build the self-contained mdv binary for the current OS into build/. Pass 'debug' to build an unoptimized debug build."
argument-hint: "[debug]"
agent: "agent"
---

# Build mdv

Build the self-contained `mdv` executable for the **current operating system**
into the `build/` directory.

Determine the mode from the prompt argument:

- If the argument contains **`debug`** → produce a **debug build**: compile with
  symbols and without optimization/inlining, and do not strip. Specifically,
  build the Go binaries with `-gcflags=all="-N -l"` and **omit** the release
  `-s -w` linker flags. A quick debug build of the launcher only is acceptable
  when the GUI is not needed:
  ```sh
  go build -gcflags=all="-N -l" -tags gui_bundled -o build/mdv ./cmd/mdv
  ```
  When the GUI helper is required for the task, run the full pipeline but keep
  debug flags (skip `-s -w`).

- Otherwise (no argument, or anything other than `debug`) → produce the normal
  **release build** by running the project's build script for the current OS:
  - macOS/Linux: `scripts/build.sh`
  - Windows (PowerShell): `pwsh scripts/build.ps1`

Rules and notes:

- Build **only for the current OS** - do not cross-compile other platforms here.
- The output must land in `build/` (`build/mdv`, or `build/mdv.exe` on Windows).
- Filter known-cosmetic macOS linker noise when showing output:
  `... 2>&1 | grep -vE "ld: warning|object file"`.
- After building, confirm the binary exists and, on macOS, verify the launcher
  stayed webview-free: `otool -L build/mdv | grep -i webkit` must print nothing.
- Report the resulting path, the mode used (release/debug), and the version
  string if printed by the build.
