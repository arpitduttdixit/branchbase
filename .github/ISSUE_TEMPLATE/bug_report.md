---
name: "Bug Report"
about: Create a report to help us improve BranchBase
title: "[BUG]: <short description>"
labels: ["bug", "triage"]
assignees: ""
---

## Describe the Bug

A clear and concise description of what the bug is.

## Environment & Diagnostics

Run `branchbase doctor` in your repository and paste the output:

```text
Paste output of `branchbase doctor` here
```

If `branchbase doctor` cannot be run, specify:
- **BranchBase Version:** (e.g. `v0.4.0` or git commit hash)
- **Database Engine:** (e.g. PostgreSQL 16, MySQL 8.0, SQLite 3.45, MongoDB 7)
- **Host OS:** (e.g. Ubuntu 24.04, macOS 14 Sonoma, Windows 11)
- **Go Version:** (if compiling from source, e.g. `go version go1.26.0`)

## Steps to Reproduce

1. Initialize repository with: `branchbase init`
2. Start proxy with: `branchbase proxy`
3. Switch branch with: `git checkout -b <branch-name>`
4. Connect application and run query: `...`
5. See error: `...`

## Expected Behavior

A clear and concise description of what you expected to happen.

## Relevant Logs or Screenshots

```text
Paste terminal output or error messages here
```
