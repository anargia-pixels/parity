# parity

Sync config folders between machines via git, automatically.

Each folder you want to sync is its own git repository with a remote. Parity
watches those folders for changes, debounces for 3 seconds, and runs a sync
operation: pull, commit, push. The other machine pulls the same repos by
running `parity sync` (there is no way to receive change events over the
network, so the receiving side syncs manually or on a schedule).

Built with Bun. No runtime dependencies.

## Sync behavior

- `git pull` first. If local uncommitted changes block it, parity stashes
  them, pulls, and pops the stash back. On a merge conflict the stash is
  preserved and an error is reported.
- `git add .` and commit with message `sync <YYYY-MM-DD HH:MM:SS> <hostname>`.
  Skipped silently when there is nothing to commit.
- Before pushing, staged changes are checked against common secret patterns
  (API keys, tokens, private keys). If any match, the commit and push are
  skipped and a warning is shown. `parity sync --allow-secrets` overrides.
- A failed pull or push prints an error to stderr (and exits non-zero for
  `parity sync`). The watcher additionally sends a desktop notification
  (`notify-send`) when a graphical session is available.

## Setup

1. Install:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/Doctorthe113/parity/main/install.sh | bash
   ```

   This downloads the compiled binary for your architecture (x86_64 or
   arm64) from the repo's `release/` folder into `~/.local/bin` (override
   with `PARITY_INSTALL_DIR`). Binaries are rebuilt automatically on every
   push to main by GitHub Actions. Make sure `~/.local/bin` is on your PATH.

2. Create `~/.config/parity/parity.toml` (copy `parity-example.toml`).
   Each `[label]` table maps one local git folder to a unique name shared
   between machines; `$VAR`, `${VAR}`, and `~` are expanded in paths:

   ```toml
   [opencode]
   local_dir = "$HOME/.config/opencode"

   [ghostty]
   local_dir = "$HOME/.config/ghostty"
   ```

3. Make sure each folder is a git repo with a remote already configured.

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
parity --config <path> ...  # use a specific config file
```

## Running at boot

A systemd user unit example is in `systemd/parity.service.example`:

```sh
mkdir -p ~/.config/systemd/user
cp systemd/parity.service.example ~/.config/systemd/user/parity.service
systemctl --user daemon-reload
systemctl --user enable --now parity
```

## Development

```sh
bun run src/main.ts --help   # dev builds read ./parity.toml
bun test
bun run build                # compile to ./parity
bun run build:release        # compile both archs into release/
```

`release/` is kept up to date by the `Build release binaries` workflow on
every push to main — no need to commit binaries by hand.

Runtime files (pid, log, state, locks) live in `~/.config/parity/`, or in
`$PARITY_STATE_DIR` when set.

## License

[MIT](LICENSE)
