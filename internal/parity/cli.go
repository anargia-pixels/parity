package parity

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"
)

// version is set at build time by scripts/build-release.sh.
var version = "dev"

const help = `parity — sync config folders between machines via GitHub repositories

usage:
  parity watch [--foreground]
      Sync all folders once, then sync local changes after a 3s debounce.
      Runs in the background by default; use --foreground with systemd.
      Run parity sync on other machines to receive remote changes.

  parity sync [label] [--allow-secrets]
      Pull, commit, and push all folders, or just one label.
      Skip commits with obvious secrets unless --allow-secrets is given.

  parity stop
      Stop the background watcher.

  parity status
      Show watcher status and the last sync result for each label.

  parity list
      List configured folders.

options:
  --config <path>   Use a specific TOML config file.
  --help, -h        Show this help.
  --version         Print the parity version.

default config: $XDG_CONFIG_HOME/parity/parity.toml
                (usually ~/.config/parity/parity.toml)

Each label maps a local Git folder to its configured GitHub remote:
  [opencode]
  local_dir = "$HOME/.config/opencode"

Select files before your first sync. In each folder's .gitignore, put
* first, then ! exceptions for the files you want to sync:
  *
  !.gitignore
  !config.toml

For a nested file, also include its parent directories:
  !themes/
  !themes/colors.toml

Only include files you have checked for credentials. The last matching
rule wins. .gitignore does not affect files already tracked by Git;
use git rm --cached -- <file> to stop tracking one (keeps the local file).
`

// credentialsHelp follows a sync stopped by missing Git credentials.
const credentialsHelp = `parity uses your Git credentials and never prompts for them.
Set up access to the remote, then run parity sync again:
  SSH:   load your key with ssh-add, and run ssh -T git@github.com once
         to trust the host
  HTTPS: run gh auth setup-git, or configure a Git credential helper
`

type arguments struct {
	command      string
	configPath   string
	positionals  []string
	help         bool
	version      bool
	foreground   bool
	allowSecrets bool
}

func parseArgs(argv []string) (arguments, error) {
	var args arguments
	positionalOnly := false
	for index := 0; index < len(argv); index++ {
		arg := argv[index]
		if !positionalOnly {
			switch {
			case arg == "--":
				positionalOnly = true
				continue
			case arg == "--config":
				index++
				if index >= len(argv) || argv[index] == "" || strings.HasPrefix(argv[index], "-") {
					return args, fmt.Errorf("--config requires a path")
				}
				args.configPath = argv[index]
				continue
			case strings.HasPrefix(arg, "--config="):
				args.configPath = strings.TrimPrefix(arg, "--config=")
				if args.configPath == "" {
					return args, fmt.Errorf("--config requires a path")
				}
				continue
			case arg == "--help" || arg == "-h":
				args.help = true
				continue
			case arg == "--version":
				args.version = true
				continue
			case arg == "--foreground":
				args.foreground = true
				continue
			case arg == "--allow-secrets":
				args.allowSecrets = true
				continue
			case strings.HasPrefix(arg, "-"):
				return args, fmt.Errorf("unknown option: %s", arg)
			}
		}
		if args.command == "" {
			args.command = arg
		} else {
			args.positionals = append(args.positionals, arg)
		}
	}
	return args, nil
}

// Run executes one CLI command and returns its process exit code.
func Run(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	args, err := parseArgs(argv)
	if err != nil {
		printError(stderr, err)
		return 1
	}
	if args.version || args.command == "version" {
		fmt.Fprintf(stdout, "parity %s\n", version)
		return 0
	}
	if args.command == "" || args.command == "help" || args.help {
		fmt.Fprint(stdout, help)
		return 0
	}
	stateDir, err := parityDir()
	if err != nil {
		printError(stderr, err)
		return 1
	}
	code, err := dispatch(ctx, args, stateDir, stdout, stderr)
	if err != nil {
		signalReady(err)
		printError(stderr, err)
		return 1
	}
	return code
}

func printError(stderr io.Writer, err error) {
	if isJournal(stderr) {
		fmt.Fprintf(stderr, "<3>parity: %s\n", err)
		return
	}
	fmt.Fprintf(stderr, "%s %s\n", paint(stderr, red, "parity:"), err)
}

func dispatch(ctx context.Context, args arguments, stateDir string, stdout, stderr io.Writer) (int, error) {
	if len(args.positionals) > 0 && (args.command != "sync" || len(args.positionals) > 1) {
		return 1, fmt.Errorf("unexpected arguments: %s", strings.Join(args.positionals, " "))
	}
	switch args.command {
	case "stop":
		return 0, stopWatcher(ctx, stateDir, stdout)
	case "status":
		return showStatus(stateDir, stdout)
	case "watch", "sync", "list":
	default:
		return 1, fmt.Errorf("unknown command: %s\n\n%s", args.command, help)
	}
	cfg, err := loadConfig(args.configPath, stateDir)
	if err != nil {
		return 1, err
	}
	switch args.command {
	case "watch":
		if !args.foreground {
			return 0, startDaemon(ctx, cfg.path, stateDir, stdout)
		}
		return 0, runWatcher(ctx, cfg, stateDir, stdout, stderr)
	case "list":
		for _, item := range cfg.entries {
			fmt.Fprintf(stdout, "%-16s %s\n", item.label, item.localDir)
		}
		return 0, nil
	default:
		return runSyncCommand(ctx, cfg, args, stateDir, stdout, stderr)
	}
}

func runSyncCommand(ctx context.Context, cfg config, args arguments, stateDir string, stdout, stderr io.Writer) (int, error) {
	entries := cfg.entries
	if len(args.positionals) == 1 {
		label := args.positionals[0]
		index := slices.IndexFunc(cfg.entries, func(item entry) bool { return item.label == label })
		if index < 0 {
			return 1, fmt.Errorf("no entry labeled %q in %s", label, cfg.path)
		}
		entries = cfg.entries[index : index+1]
	}
	started := time.Now()
	fmt.Fprintf(stdout, "Syncing %d %s\n\n", len(entries), pluralFolder(len(entries)))
	report := func(result entryOutcome) {
		printOutcome(result.label, result.outcome, stdout, stderr)
	}
	outcomes, err := syncAll(ctx, entries, syncOptions{stateDir: stateDir, allowSecrets: args.allowSecrets, stopOnAuth: true, progress: stdout}, report)
	code := 0
	for _, result := range outcomes {
		if result.outcome.status == statusError {
			code = 1
		}
	}
	printSyncSummary(stdout, outcomes, time.Since(started))
	if len(outcomes) > 0 && outcomes[len(outcomes)-1].outcome.reason == reasonAuth {
		if remaining := len(entries) - len(outcomes); remaining > 0 {
			stopped := fmt.Sprintf("Stopped before syncing %d more %s.", remaining, pluralFolder(remaining))
			fmt.Fprint(stderr, "\n"+paint(stderr, red, stopped))
		}
		fmt.Fprint(stderr, "\n"+paint(stderr, yellow, credentialsHelp))
	}
	return code, err
}

func pluralFolder(count int) string {
	if count == 1 {
		return "folder"
	}
	return "folders"
}

func printOutcome(label string, outcome syncOutcome, stdout, stderr io.Writer) {
	switch outcome.status {
	case statusOK:
		fmt.Fprintf(stdout, "  %s %s — synced\n\n", paint(stdout, green, "✓"), label)
	case statusSkipped:
		if outcome.reason == reasonSecrets {
			heading := fmt.Sprintf("! %s — blocked by possible secrets; commit and push skipped", label)
			fmt.Fprintf(stderr, "  %s\n%s\n\n", paint(stderr, yellow, heading), outcome.message)
		} else {
			fmt.Fprintf(stdout, "  %s %s — up to date\n\n", paint(stdout, green, "✓"), label)
		}
	default:
		heading := fmt.Sprintf("✗ %s — sync failed", label)
		fmt.Fprintf(stderr, "  %s\n    %s\n\n", paint(stderr, red, heading), outcome.message)
	}
}

func printSyncSummary(stdout io.Writer, outcomes []entryOutcome, elapsed time.Duration) {
	var synced, unchanged, blocked, failed int
	for _, result := range outcomes {
		switch {
		case result.outcome.status == statusOK:
			synced++
		case result.outcome.reason == reasonEmpty:
			unchanged++
		case result.outcome.reason == reasonSecrets:
			blocked++
		default:
			failed++
		}
	}
	var counts []string
	for _, count := range []struct {
		number int
		label  string
		color  string
	}{
		{synced, "synced", green}, {unchanged, "up to date", ""}, {blocked, "blocked by secrets", yellow}, {failed, "failed", red},
	} {
		if count.number == 0 {
			continue
		}
		text := fmt.Sprintf("%d %s", count.number, count.label)
		if count.color != "" {
			text = paint(stdout, count.color, text)
		}
		counts = append(counts, text)
	}
	fmt.Fprintf(stdout, "Finished in %.1fs · %s\n", elapsed.Seconds(), strings.Join(counts, " · "))
}

func showStatus(stateDir string, stdout io.Writer) (int, error) {
	pid := daemonPID(stateDir)
	code := 1
	if pid != 0 {
		fmt.Fprintf(stdout, "parity: running (pid %d)\n", pid)
		code = 0
	} else {
		fmt.Fprintln(stdout, "parity: not running")
	}
	state, err := readState(stateDir)
	if err != nil {
		return 1, err
	}
	labels := slices.Sorted(maps.Keys(state.Entries))
	if len(labels) > 0 {
		fmt.Fprintln(stdout)
	}
	for _, label := range labels {
		item := state.Entries[label]
		message := ""
		if item.Message != "" {
			message = " — " + item.Message
		}
		fmt.Fprintf(stdout, "  %-16s %-8s %s%s\n", label, item.Result, item.LastSync, message)
	}
	return code, nil
}
