import { watch } from "node:fs";
import { existsSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import type { ParityConfig } from "./lib/config";
import { parityDir } from "./lib/config";
import { isLocked, isPidAlive } from "./lib/lock";
import { notify } from "./lib/notify";
import { tryCatchSync } from "./lib/try-catch";
import { syncAll, syncEntry, timestamp, type SyncOutcome } from "./sync";

const DEBOUNCE_MS = 3000;

export async function daemonPid(pidPath: string): Promise<number | null> {
  if (!existsSync(pidPath)) {
    return null;
  }
  const pid = Number.parseInt(readFileSync(pidPath, "utf8"), 10);
  if (!Number.isFinite(pid) || !(await isPidAlive(pid))) {
    return null;
  }
  return pid;
}

/**
 * Run the watcher in the foreground: write the pid file, do an initial sync,
 * watch every entry (excluding .git), debounce 3s, then sync on quiet.
 */
export async function runWatcher(config: ParityConfig, pidPath: string): Promise<void> {
  if (existsSync(pidPath)) {
    const existing = await daemonPid(pidPath);
    if (existing !== null) {
      throw new Error(`parity watch is already running (pid ${existing})`);
    }
    unlinkSync(pidPath);
  }
  writeFileSync(pidPath, String(process.pid));

  const timers = new Map<string, Timer>();
  const watchers: { close: () => void }[] = [];
  let stopping = false;

  const stop = async () => {
    if (stopping) {
      return;
    }
    stopping = true;
    for (const timer of timers.values()) {
      clearTimeout(timer);
    }
    for (const watcher of watchers) {
      watcher.close();
    }
    await cleanupPid(pidPath);
    process.exit(0);
  };

  process.on("SIGTERM", () => void stop());
  process.on("SIGINT", () => void stop());

  const report = (label: string, outcome: SyncOutcome): void => {
    if (outcome.status === "ok") {
      log(`[${label}] synced`);
    } else if (outcome.status === "skipped") {
      if (outcome.reason === "secrets") {
        const message = `possible secrets in ${outcome.files.join(", ")} — commit and push skipped`;
        log(`[${label}] ${message}`);
        notify(`parity: ${label}`, message);
      } else {
        log(`[${label}] nothing to commit`);
      }
    } else {
      log(`[${label}] error: ${outcome.message}`);
      notify(`parity: ${label} sync failed`, outcome.message);
    }
  };

  const runSync = async (label: string) => {
    const entry = config.entries.find((e) => e.label === label);
    if (!entry || (await isLocked(parityDir(), label))) {
      return;
    }
    log(`[${label}] syncing`);
    report(label, await syncEntry(entry, { allowSecrets: false, progress: false }));
  };

  const schedule = (label: string) => {
    const existing = timers.get(label);
    if (existing) {
      clearTimeout(existing);
    }
    timers.set(
      label,
      setTimeout(() => {
        timers.delete(label);
        if (!stopping) {
          void runSync(label);
        }
      }, DEBOUNCE_MS),
    );
  };

  const startWatching = async () => {
    for (const entry of config.entries) {
      if (!existsSync(entry.localDir)) {
        log(`[${entry.label}] directory does not exist: ${entry.localDir}`);
        continue;
      }
      const started = await tryCatchSync(() =>
        watch(entry.localDir, { recursive: true }, (_event, filename) => {
          if (typeof filename !== "string" || filename.includes(".git")) {
            return;
          }
          schedule(entry.label);
        }),
      );
      if (started.error) {
        log(`[${entry.label}] failed to watch ${entry.localDir}: ${started.error.message}`);
        continue;
      }
      watchers.push(started.data);
      log(`[${entry.label}] watching ${entry.localDir}`);
    }
  };

  const initialSync = async () => {
    log("initial sync");
    const outcomes = await syncAll(config.entries, { allowSecrets: false, progress: false });
    for (const [label, outcome] of outcomes) {
      report(label, outcome);
    }
  };

  await initialSync();
  await startWatching();
}

async function cleanupPid(pidPath: string): Promise<void> {
  if (!existsSync(pidPath)) {
    return;
  }
  await tryCatchSync(() => unlinkSync(pidPath));
}

function log(message: string): void {
  console.log(`${timestamp()} ${message}`);
}
