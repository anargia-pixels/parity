import { existsSync, mkdirSync, openSync, readFileSync, unlinkSync, writeSync } from "node:fs";
import { join } from "node:path";
import { tryCatchSync } from "./try-catch";

export async function isPidAlive(pid: number): Promise<boolean> {
  const probe = await tryCatchSync(() => {
    process.kill(pid, 0);
    return true;
  });
  return probe.error === null;
}

/**
 * Cross-process lock via a lock file created with O_EXCL. Used so the watch
 * daemon and a manual `parity sync` never interleave git operations on the
 * same entry. Stale locks (dead owner, corrupt file) are cleared first.
 */
export async function acquireLock(stateDir: string, label: string): Promise<boolean> {
  const dir = join(stateDir, "locks");
  mkdirSync(dir, { recursive: true });
  const path = join(dir, `${label}.lock`);
  if (!(await isStale(path))) {
    return false;
  }
  await tryCatchSync(() => existsSync(path) && unlinkSync(path));
  const opened = await tryCatchSync(() => {
    const fd = openSync(path, "wx");
    writeSync(fd, JSON.stringify({ pid: process.pid, at: Date.now() }));
  });
  return opened.error === null;
}

export async function releaseLock(stateDir: string, label: string): Promise<void> {
  const path = join(stateDir, "locks", `${label}.lock`);
  if (!existsSync(path)) {
    return;
  }
  await tryCatchSync(() => unlinkSync(path));
}

export async function isLocked(stateDir: string, label: string): Promise<boolean> {
  const path = join(stateDir, "locks", `${label}.lock`);
  if (!existsSync(path)) {
    return false;
  }
  return !(await isStale(path));
}

/** A lock is stale when its owner process is gone or the file is corrupt. */
async function isStale(path: string): Promise<boolean> {
  if (!existsSync(path)) {
    return true;
  }
  const read = await tryCatchSync<string>(() => readFileSync(path, "utf8"));
  if (read.error) {
    return false;
  }
  const parsed = await tryCatchSync<{ pid?: number }>(() => JSON.parse(read.data));
  if (parsed.error || typeof parsed.data.pid !== "number") {
    return true;
  }
  return !(await isPidAlive(parsed.data.pid));
}
