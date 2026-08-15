import { openSync } from "node:fs";
import { ensureStateDir, loadConfig, logFilePath, pidFilePath, type ParityConfig } from "./lib/config";
import { isPidAlive } from "./lib/lock";
import { isTty } from "./lib/progress";
import { readState, writeState } from "./lib/state";
import { tryCatch, tryCatchSync } from "./lib/try-catch";
import { daemonPid, runWatcher } from "./watch";
import { outcomeMessage, syncAll, syncEntry, timestamp } from "./sync";

const HELP = `parity — sync config folders between machines via git

usage:
  parity watch [--foreground]
      Sync all folders once at startup, then watch them for changes
      (3s debounce) and sync automatically. Detaches into the
      background by default; --foreground keeps it attached (use with
      systemd).

  parity sync [label] [--allow-secrets]
      Sync all configured folders, or just one label.
      Commits are skipped when a change matches obvious secret
      patterns unless --allow-secrets is given.

  parity stop
      Stop a running watcher.

  parity status
      Show watcher status and the last sync result per label.

  parity list
      List the configured folders being watched.

options:
  --config <path>   Use a specific config file.
  --help            Show this help.

default config location:
  compiled binary: ~/.config/parity/parity.toml
  dev (bun run):  ./parity.toml

each [label] in the config maps one local git folder (with a remote)
to a unique name shared between machines:
  [opencode]
  local_dir = "$HOME/.config/opencode"
`;

type ParsedArgs = {
  subcommand: string | null;
  configPath?: string;
  flags: Set<string>;
  positionals: string[];
};

function parseArgs(argv: string[]): ParsedArgs {
  const result: ParsedArgs = { subcommand: null, flags: new Set(), positionals: [] };
  let i = 0;
  const first = argv[0];
  if (first !== undefined && !first.startsWith("-")) {
    result.subcommand = first;
    i = 1;
  }
  for (; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === undefined) {
      continue;
    }
    if (arg === "--config") {
      const value = argv[++i];
      if (!value) {
        throw new Error("--config requires a path");
      }
      result.configPath = value;
    } else if (arg.startsWith("-")) {
      result.flags.add(arg);
    } else {
      result.positionals.push(arg);
    }
  }
  return result;
}

async function main(): Promise<void> {
  const parsed = await tryCatchSync(() => parseArgs(process.argv.slice(2)));
  if (parsed.error) {
    fail(parsed.error);
  }
  const args = parsed.data;

  if (args.subcommand === null || args.flags.has("--help") || args.flags.has("-h") || args.subcommand === "help") {
    console.log(HELP);
    process.exit(0);
  }

  const dispatched = await tryCatch(dispatch(args));
  if (dispatched.error) {
    fail(dispatched.error);
  }
}

async function dispatch(args: ParsedArgs): Promise<void> {
  switch (args.subcommand) {
    case "watch":
      await runWatchCommand(await loadConfig(args.configPath), args);
      break;
    case "sync":
      await runSyncCommand(await loadConfig(args.configPath), args);
      break;
    case "stop":
      await runStopCommand();
      break;
    case "status":
      await runStatusCommand();
      break;
    case "list":
      await runListCommand(await loadConfig(args.configPath));
      break;
    default:
      console.error(`unknown command: ${args.subcommand}\n\n${HELP}`);
      process.exit(1);
  }
}

async function runWatchCommand(config: ParityConfig, args: ParsedArgs): Promise<void> {
  if (!args.flags.has("--foreground")) {
    ensureStateDir();
    const logFd = openSync(logFilePath(), "a");
    // argv[1] is the script path in dev and the embedded module path when
    // compiled, so the real arguments always start at index 2.
    const childArgs = [...process.argv.slice(2), "--foreground"];
    const child = Bun.spawn([process.execPath, ...childArgs], {
      detached: true,
      stdio: ["ignore", logFd, logFd],
    });
    child.unref();
    console.log("parity watch started in the background");
    process.exit(0);
  }
  await runWatcher(config, pidFilePath());
}

async function runSyncCommand(config: ParityConfig, args: ParsedArgs): Promise<void> {
  const allowSecrets = args.flags.has("--allow-secrets");
  const options = { allowSecrets, progress: await isTty() };
  const label = args.positionals[0];

  if (label) {
    const entry = config.entries.find((e) => e.label === label);
    if (!entry) {
      fail(new Error(`no entry labeled "${label}" in ${config.path}`));
    }
    const outcome = await syncEntry(entry, options);
    await writeState({
      [label]: {
        lastSync: timestamp(),
        result: outcome.status === "skipped" ? "skipped" : outcome.status,
        message: outcomeMessage(outcome),
      },
    });
    printOutcome(label, outcome);
    process.exit(outcome.status === "error" ? 1 : 0);
  }

  const outcomes = await syncAll(config.entries, options);
  for (const [entryLabel, outcome] of outcomes) {
    printOutcome(entryLabel, outcome);
  }
  const failed = [...outcomes.values()].some((o) => o.status === "error");
  process.exit(failed ? 1 : 0);
}

async function runListCommand(config: ParityConfig): Promise<void> {
  for (const entry of config.entries) {
    console.log(`${entry.label.padEnd(16)} ${entry.localDir}`);
  }
}

function printOutcome(label: string, outcome: Awaited<ReturnType<typeof syncEntry>>): void {
  if (outcome.status === "ok") {
    console.log(`[${label}] synced`);
  } else if (outcome.status === "skipped") {
    if (outcome.reason === "secrets") {
      console.error(`[${label}] skipped: ${outcome.message}`);
    } else {
      console.log(`[${label}] nothing to commit`);
    }
  } else {
    console.error(`[${label}] error: ${outcome.message}`);
  }
}

async function runStopCommand(): Promise<void> {
  const pidPath = pidFilePath();
  const pid = await daemonPid(pidPath);
  if (pid === null) {
    console.log("parity watch is not running");
    process.exit(0);
  }
  process.kill(pid, "SIGTERM");
  const deadline = Date.now() + 5000;
  while (Date.now() < deadline) {
    if (!(await isPidAlive(pid))) {
      console.log(`parity watch stopped (pid ${pid})`);
      process.exit(0);
    }
    await Bun.sleep(100);
  }
  fail(new Error(`watcher (pid ${pid}) did not stop in time`));
}

async function runStatusCommand(): Promise<void> {
  const pid = await daemonPid(pidFilePath());
  console.log(pid !== null ? `parity: running (pid ${pid})` : "parity: not running");
  const state = await readState();
  if (state && Object.keys(state.entries).length > 0) {
    console.log();
    for (const [label, entryState] of Object.entries(state.entries)) {
      const message = entryState.message ? ` — ${entryState.message}` : "";
      console.log(`  ${label.padEnd(16)} ${entryState.result.padEnd(8)} ${entryState.lastSync}${message}`);
    }
  }
  process.exit(pid !== null ? 0 : 1);
}

function fail(error: Error): never {
  console.error(`parity: ${error.message}`);
  process.exit(1);
}

await main();
