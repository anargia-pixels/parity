import { existsSync } from "node:fs";
import { hostname } from "node:os";
import type { Entry } from "./lib/config";
import { parityDir } from "./lib/config";
import { git, hasRemote, identityFlags, isGitRepo, repoIdentity } from "./lib/git";
import { acquireLock, releaseLock } from "./lib/lock";
import { checkStagedForSecrets } from "./lib/secret-check";
import type { EntryState } from "./lib/state";
import { writeState } from "./lib/state";

export type SyncOutcome =
  | { status: "ok" }
  | { status: "skipped"; reason: "empty" | "secrets"; files: string[]; message?: string }
  | { status: "error"; message: string };

export type SyncOptions = {
  allowSecrets: boolean;
};

/** `YYYY-MM-DD HH:MM:SS` local time. */
export function timestamp(date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  );
}

/**
 * Sync one entry: pull (stashing dirty work if needed), stage, commit (skipped
 * when empty or secrets found), push. The stash is preserved when popping it
 * conflicts, so nothing is lost.
 */
export async function syncEntry(entry: Entry, options: SyncOptions): Promise<SyncOutcome> {
  const { label, localDir: dir } = entry;

  if (!existsSync(dir)) {
    return { status: "error", message: `directory does not exist: ${dir}` };
  }
  if (!(await isGitRepo(dir))) {
    return { status: "error", message: `not a git repository: ${dir}` };
  }
  if (!(await hasRemote(dir))) {
    return { status: "error", message: `no git remote configured in: ${dir}` };
  }
  if (!(await acquireLock(parityDir(), label))) {
    return { status: "error", message: `sync already in progress for ${label}` };
  }

  try {
    return await doSync(entry, options);
  } finally {
    await releaseLock(parityDir(), label);
  }
}

async function doSync(entry: Entry, options: SyncOptions): Promise<SyncOutcome> {
  const { label, localDir: dir } = entry;

  const pull = await git(dir, ["pull"]);
  if (pull.exitCode !== 0 && !hasNoUpstream(pull.stderr)) {
    const restored = await stashPullPop(dir, label, pull);
    if (restored !== null) {
      return { status: "error", message: restored };
    }
  }

  const add = await git(dir, ["add", "-A", "."]);
  if (add.exitCode !== 0) {
    return { status: "error", message: `git add failed: ${add.stderr}` };
  }

  const staged = await git(dir, ["diff", "--cached", "--quiet"]);
  if (staged.exitCode === 0) {
    return { status: "skipped", reason: "empty", files: [] };
  }

  const secrets = await checkStagedForSecrets(dir);
  if (secrets.length > 0 && !options.allowSecrets) {
    const files = [...new Set(secrets.map((s) => s.file))];
    const detail = secrets.map((s) => `  ${s.file} (${s.pattern})`).join("\n");
    return {
      status: "skipped",
      reason: "secrets",
      files,
      message: `possible secrets staged:\n${detail}`,
    };
  }

  const identity = await repoIdentity(dir);
  const message = `sync ${timestamp()} ${hostname()}`;
  const commit = await git(dir, [...identityFlags(identity), "commit", "-m", message]);
  if (commit.exitCode !== 0) {
    return { status: "error", message: `git commit failed: ${commit.stderr}` };
  }

  const push = await git(dir, ["push"]);
  if (push.exitCode !== 0) {
    return { status: "error", message: `git push failed: ${push.stderr}` };
  }

  return { status: "ok" };
}

/**
 * A failed pull is harmless when the remote branch has no commits yet or the
 * branch has no upstream — there is simply nothing to merge.
 */
function hasNoUpstream(stderr: string): boolean {
  return /no such ref was fetched|no tracking information/i.test(stderr);
}

/**
 * Recover from a failed pull caused by uncommitted local changes: stash,
 * pull, pop. Returns null on success or an error message. On pop conflict the
 * stash is kept so nothing is lost.
 */
async function stashPullPop(dir: string, label: string, firstPull: { stderr: string }): Promise<string | null> {
  const status = await git(dir, ["status", "--porcelain"]);
  if (status.exitCode !== 0) {
    return `git pull failed and status check failed: ${status.stderr}`;
  }
  if (status.stdout === "") {
    return `git pull failed: ${firstPull.stderr}`;
  }

  const stash = await git(dir, ["stash", "push", "--include-untracked", "-m", `parity autostash ${label}`]);
  if (stash.exitCode !== 0) {
    return `git pull failed and could not stash local changes: ${stash.stderr}`;
  }

  const pull = await git(dir, ["pull"]);
  if (pull.exitCode !== 0) {
    const pop = await git(dir, ["stash", "pop"]);
    if (pop.exitCode !== 0) {
      return `git pull failed and stash pop also failed — stash preserved: ${pop.stderr}`;
    }
    return `git pull failed (local changes restored): ${pull.stderr}`;
  }

  const pop = await git(dir, ["stash", "pop"]);
  if (pop.exitCode !== 0) {
    return (
      `merge conflict after git pull — stash preserved (resolve with ` +
      `"git stash pop" in ${dir}): ${pop.stderr}`
    );
  }
  return null;
}

/**
 * One-line summary of a sync outcome for state files and logs.
 */
export function outcomeMessage(outcome: SyncOutcome): string {
  if (outcome.status === "skipped") {
    return outcome.reason;
  }
  if (outcome.status === "error") {
    return outcome.message;
  }
  return "synced";
}

export async function syncAll(
  entries: Entry[],
  options: SyncOptions,
): Promise<Map<string, SyncOutcome>> {
  const outcomes = new Map<string, SyncOutcome>();
  const updates: Record<string, EntryState> = {};
  for (const entry of entries) {
    const outcome = await syncEntry(entry, options);
    outcomes.set(entry.label, outcome);
    updates[entry.label] = {
      lastSync: timestamp(),
      result: outcome.status === "skipped" ? "skipped" : outcome.status,
      message: outcomeMessage(outcome),
    };
  }
  await writeState(updates);
  return outcomes;
}
