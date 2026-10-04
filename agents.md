# Repository instructions

Parity lets users sync selected config files across machines through GitHub
repositories. Preserve the existing Git remote and authentication workflow.
The watcher syncs local changes; users receive remote changes with `parity sync`.

## Workflow

- Read the README, applicable instructions, configuration, and nearby tests
  before editing. Check `git status` and preserve unrelated changes.
- Use the `fff` MCP tools for file search.
- Make the smallest coherent change. Keep CLI output clear about the current
  folder, sync phase, and result.
- Use Go for the active application. Format changed Go files with `gofmt` and
  run `go test ./...` and `go vet ./...` before completing a change.
- Keep `ts-backup/` as an unchanged archive of the TypeScript application.
  It excludes dependencies and compiled binaries.

## File names

- All new or renamed file names MUST use lowercase kebab-case: words separated
  by hyphens, such as `secret-check.go` and `build-release.sh`.
- Go requires `_test.go` and platform suffixes such as `_linux.go`. These
  suffixes are allowed; the name before the suffix MUST use kebab-case.
- Keep tool-required and standard names such as `go.mod`, `go.sum`, `README.md`,
  `LICENSE`, and `.gitignore`. The archived files in `ts-backup/` keep their
  original names. Keep these instructions in `agents.md`.

## Git and config safety

- Keep the existing branch convention. Initialize new repositories with
  `git init -b master`.
- Document a `.gitignore` allowlist: ignore everything with `*` first, then
  explicitly include files with `!`. Keep staged secret checks enabled by
  default, and preserve stashed changes when a pull or restore fails.
