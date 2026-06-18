---
description: "Pull the latest changes, then create and push the version tag from the VERSION file (v<VERSION>) on the latest commit."
argument-hint: "(no arguments)"
agent: "agent"
---

# Release mdv (tag & push)

Tag the current `VERSION` on the latest commit and push the tag, which triggers
the `release` GitHub Actions workflow ([release.yml](../workflows/release.yml))
to build, sign/notarize, and publish the binaries to a GitHub Release.

Perform the following steps **in order** and **stop immediately** if any step
fails — do not continue past a failure.

1. **Pull the latest changes and ensure it succeeds.** On the current branch:
   ```sh
   git pull --ff-only
   ```
   - If the pull fails for any reason (merge conflicts, non-fast-forward,
     network/auth error, detached HEAD, or a dirty working tree that blocks the
     pull), **stop** and report the exact error. Do **not** attempt to force,
     rebase, stash, or otherwise work around it without being asked.
   - Confirm the working tree is clean afterwards (`git status --porcelain`
     should print nothing). If it is dirty, stop and report it.

2. **Read the version.** Read the single-line [VERSION](../../VERSION) file and
   trim whitespace. The git tag is the version prefixed with `v` — e.g.
   VERSION `0.7.1` → tag `v0.7.1`. (This matches the `v*` tag trigger in
   [release.yml](../workflows/release.yml).)

3. **Guard against an existing tag.** Check whether the tag already exists:
   ```sh
   git tag --list "v<VERSION>"
   git ls-remote --tags origin "refs/tags/v<VERSION>"
   ```
   If the tag already exists locally or on the remote, **stop** and report it —
   do not move or overwrite an existing tag. The fix is to bump the `VERSION`
   file first.

4. **Create the tag on the latest commit.** Create an annotated tag pointing at
   the current `HEAD`:
   ```sh
   git tag -a "v<VERSION>" -m "Release v<VERSION>"
   ```

5. **Push the tag to the remote.**
   ```sh
   git push origin "v<VERSION>"
   ```

6. **Report.** State the tag that was created and pushed, the commit SHA it
   points to, and remind the user that pushing the tag has triggered the
   `release` workflow on GitHub Actions, which will publish the signed/notarized
   binaries to the GitHub Release named after the tag.

Notes:

- Replace `<VERSION>` with the actual trimmed contents of the `VERSION` file in
  every command above.
- Do not edit the `VERSION` file in this prompt — it is the source of truth and
  is expected to already hold the version being released.
