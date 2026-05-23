# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Workflow

When working on an epic or issue:

1. **Check issue details**: `bd show <issue-id>`
2. **Create feature branch**: `git checkout -b <branch-name>`
3. **Claim the work**: `bd update <issue-id> --claim`
4. **For each child task**:
   - Show task: `bd show <task-id>`
   - Claim task: `bd update <task-id> --claim`
   - Implement the changes
   - Commit with conventional commit (no body): `git commit -m "feat: description"`
   - Close task: `bd close <task-id>`
5. **Run tests**: `go test ./...` or `make test`
6. **Close epic**: `bd close <epic-id>`
7. **Push everything**:
   ```bash
   git pull --rebase origin master
   bd dolt push
   git push -u origin <branch-name>
   ```
8. **Create PR**: `gh pr create` with concise description

### Commit Message Rules

- Use conventional commits: `feat:`, `fix:`, `test:`, `docs:`, `chore:`
- **NO commit bodies** - subject line only
- **DO NOT mention beads** in commit messages
- Keep messages concise and descriptive

Examples:
- ✅ `feat: add config validation for required fields`
- ✅ `test: add unit tests for config loading`
- ❌ `feat: add config validation\n\nCloses mailtagger-u8n.4`
- ❌ `feat: implement task from beads issue`

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->


## Build & Test

```bash
go test ./...              # Run all tests
go test -v -race ./...     # Run tests with race detector
make test                  # If Makefile exists
```

## Architecture Overview

Mailtagger is a lightweight self-hosted AI Gmail labeler written in Go. It polls Gmail, classifies messages with an LLM, and applies labels.

**Structure:**
- `cmd/mailtagger/` - CLI entry point with cobra commands
- `internal/config/` - Configuration loading and validation
- `internal/` - Internal packages (to be implemented)

## Conventions & Patterns

- Use Go 1.22+ features
- Follow standard Go project layout
- Configuration via YAML with `${ENV}` expansion
- Conventional commits for all changes
- Comprehensive unit tests for new code
- Work on feature branches, create PRs for review
