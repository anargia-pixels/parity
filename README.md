# parity

Sync selected config files across machines through GitHub repositories.

Each config folder is a Git repository with a GitHub remote. Parity pulls
remote changes, commits selected local changes, and pushes them back. It uses
your existing Git authentication, including SSH keys or a credential helper.

Run `parity watch` to sync local edits after a three-second pause. On another
machine, run `parity sync` to receive the changes, or schedule that command.
The watcher does not poll GitHub for remote changes.

Built with Go. The binary requires Git; Bun and Node.js are not required.

## Sync behavior

- `git pull` first. If local uncommitted changes block it, parity stashes
  them, pulls, and pops the stash back. On a merge conflict the stash is
  preserved and an error is reported.
- Stage additions, edits, and deletions with `git add -A .`, respecting each
  folder's `.gitignore`. Commit with message
  `sync <YYYY-MM-DD HH:MM:SS> <hostname>` when there are staged changes.
- Push pending commits even when there is nothing new to commit, so a failed
  push can be retried with `parity sync`.
- Before pushing, staged changes are checked against common secret patterns
  (API keys, tokens, private keys). If any match, the commit and push are
  skipped and a warning is shown. `parity sync --allow-secrets` overrides.
- A failed pull or push prints an error to stderr (and exits non-zero for
  `parity sync`). The watcher additionally sends a desktop notification
  (`notify-send`) when a graphical session is available.
- Git runs with your existing credentials (SSH keys, `ssh-agent`, credential
  helpers, `gh auth setup-git`) and never prompts. If a remote needs
  credentials Git cannot supply, `parity sync` stops, skips the remaining
  folders, and explains how to set up access.

## Setup

1. Install:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/Doctorthe113/parity/main/install.sh | bash
   ```

   This detects Linux or macOS and downloads the gzip-compressed binary for
   your architecture (x86_64 or arm64) from the repo's `release/` folder into
   `~/.local/bin` (override with `PARITY_INSTALL_DIR`). Binaries are rebuilt
   automatically on every push to main by GitHub Actions. Make sure
   `~/.local/bin` is on your PATH.

   Run the same command to update. The installer shows the installed and
   new versions, the download size and progress, and exits early when you are
   already up to date (set `PARITY_FORCE=1` to reinstall). It replaces the
   binary safely while a watcher is running and restarts the `parity` systemd
   user service if it is active. `parity --version` prints the installed
   version, named after the source commit (for example `2026.10.04-f930957`).

2. **Select the files to sync before running Parity.** In each config folder,
   create a `.gitignore` that ignores everything by default, then explicitly
   includes the files you have checked for credentials:

   ```gitignore
   *
   !.gitignore
   !config.toml
   !keybindings.json
   ```

   Keep `*` first: the last matching rule wins. Putting `*` after the `!`
   entries would ignore the selected files too. For nested files, include
   each parent directory as well:

   ```gitignore
   *
   !.gitignore
   !themes/
   !themes/colors.toml
   ```

   This includes `themes/colors.toml` while other files in `themes/` remain
   ignored. Review changes with `git status --short` before the first sync.
   Parity's secret checks are an extra check; they cannot detect every secret.

   `.gitignore` does not affect files that Git already tracks. To stop tracking
   a credential file while keeping the local copy:

   ```sh
   git rm --cached -- credentials.json
   ```

   Commit that removal. If a credential was already pushed, revoke it: removing
   the file from tracking does not remove it from repository history. See the
   [Git ignore rules](https://git-scm.com/docs/gitignore).

3. Give each folder its own GitHub repository, preferably private. A clone
   already has a remote and a tracking branch. For a new local folder, create
   an empty GitHub repository, then run these commands in the config folder
   with your repository URL and selected file names:

   ```sh
   git init -b master
   git remote add origin git@github.com:YOUR-USER/my-app-config.git
   git add .gitignore config.toml
   git commit -m "Add selected config"
   git push --set-upstream origin master
   ```

   On the other machine, clone that repository into the corresponding config
   folder. Parity also works with other Git remotes.

4. Create `~/.config/parity/parity.toml` (copy `parity-example.toml`).
   Each `[label]` table maps one local git folder to a unique name shared
   between machines; `$VAR`, `${VAR}`, and `~` are expanded in paths:

   ```toml
   [opencode]
   local_dir = "$HOME/.config/opencode"

   [ghostty]
   local_dir = "$HOME/.config/ghostty"
   ```

   Use the same labels on each machine and each machine's own local paths.
   `$XDG_CONFIG_HOME/parity/parity.toml` is used when `XDG_CONFIG_HOME` is set.

## Commands

```sh
parity watch            # sync once, then watch for changes (detaches, logs to
                        # ~/.config/parity/parity.log)
parity watch --foreground   # stay attached (for systemd)
parity sync             # sync all configured folders
parity sync opencode    # sync one label
parity sync --allow-secrets
parity stop             # stop a running watcher
parity status           # watcher state and last sync result per label
parity list             # configured labels and local folders
parity --config <path> ...  # use a specific config file
```

`parity sync` shows the folder number and current phase: pulling, checking
selected files, committing, and pushing. A terminal shows a spinner on the
current line; redirected output records each phase as plain text. Each folder
then shows its result, followed by a summary:

"Synced" means changes were received, committed, or pushed. "Up to date" means
there were no changes to receive or publish.

```text
Syncing 2 folders

  ✓ opencode — synced

  ✓ ghostty — up to date

Finished in 0.8s · 1 synced · 1 up to date
```

Errors and secret warnings go to stderr. A sync error exits with code 1;
secret-blocked folders keep the existing skipped behavior and exit with code 0.

## Running at boot

A systemd user unit example is in `systemd/parity.service.example`:

```sh
mkdir -p ~/.config/systemd/user
cp systemd/parity.service.example ~/.config/systemd/user/parity.service
systemctl --user daemon-reload
systemctl --user enable --now parity
```

The watcher logs to the systemd journal. Errors, including missing Git
credentials, are logged with error priority:

```sh
journalctl --user -u parity -f        # follow all watcher output
journalctl --user -u parity -p err    # show only errors
```

## Development

Install Go 1.24 or newer and Git. Repository instructions are in
[agents.md](agents.md).

```sh
go run . --help
go run . --config ./parity.toml list
go test ./...
go test -race ./...
go vet ./...
go build -o parity .
sh scripts/build-release.sh  # Linux and macOS x64 and arm64 binaries, gzipped, plus release/version
```

Development and compiled binaries use the same default config location. Pass
`--config ./parity.toml` to use a config in the project directory.

The tests use temporary local Git remotes and cover config validation, secret
checks, stash recovery, file selection, locks, status, progress, and watcher
startup and shutdown. They do not need GitHub access.

`release/` is kept up to date by the `Build release binaries` workflow on
every push to main — no need to commit binaries by hand.

Runtime files (pid, log, state, locks) live in `~/.config/parity/`, or in
`$PARITY_STATE_DIR` when set.

The original TypeScript app, tests, build configuration, and documentation are
preserved in `ts-backup/`. Dependencies and compiled binaries are excluded.

## License

[MIT](LICENSE)
