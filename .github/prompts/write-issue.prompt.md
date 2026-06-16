---
description: "Draft a GitHub issue for mdv, check for duplicates first, then open the prefilled new-issue form in the browser."
argument-hint: "[short description of the bug or feature]"
agent: "agent"
---

# Write a GitHub issue

Help the user file a high-quality issue for the `mdv` repository
(`thgossler/mdv`) without creating duplicates.

## 1. Get the description

- If the prompt was invoked **with an argument**, treat that text as the initial
  issue description.
- If **no argument** was provided, ask the user to describe the issue (what
  happened or what they want), and wait for their answer before continuing.

## 2. Search for similar existing issues (best effort)

Before drafting, look for closely related or duplicate issues. Use whatever is
available, in order of preference:

- GitHub CLI if installed:
  ```sh
  gh issue list --repo thgossler/mdv --state all --search "<key terms>" --limit 20
  ```
- Otherwise, any available GitHub search tool/MCP for the repo.
- If neither is available, note that the duplicate check was skipped and
  continue.

Derive 2–4 concise key terms from the description for the search. If you find
issues that look **closely related or very similar**, stop and show them to the
user (number, title, state, URL). Ask whether they want to comment on an
existing one instead of opening a new issue. Do **not** open the browser in this
case unless the user confirms it is genuinely different.

## 3. Draft the issue

If no closely related issue exists (or the user confirms theirs is distinct),
compose a clear issue:

- **Title**: concise and specific.
- **Body** (Markdown): infer bug vs. feature from the description.
  - Bug: steps to reproduce, expected vs. actual behavior, run mode
    (GUI/TUI/console), OS, and `mdv --version` if known (ask only if it matters
    and is unknown).
  - Feature: the problem being solved, who benefits, and how it fits mdv's
    "minimal, self-contained, always-starts" goals.

Show the drafted title and body to the user.

## 4. Open the prefilled new-issue form in the OS browser

Build a prefilled URL and open it in the default browser so the user can review
and submit:

```
https://github.com/thgossler/mdv/issues/new?title=<url-encoded-title>&body=<url-encoded-body>
```

URL-encode both values. Open it with the OS-appropriate command:

- macOS: `open "<url>"`
- Linux: `xdg-open "<url>"`
- Windows: `start "" "<url>"`

Confirm to the user that the form was opened (or print the URL if opening
failed) and remind them the issue is **not** submitted until they click *Submit*
on GitHub.
