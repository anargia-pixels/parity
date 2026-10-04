import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

let stateDir: string;
let testBase: string;
let child: { kill: () => void; exited: Promise<number> } | null = null;

const GIT_ENV = {
  GIT_AUTHOR_NAME: "test",
  GIT_AUTHOR_EMAIL: "test@example.com",
  GIT_COMMITTER_NAME: "test",
  GIT_COMMITTER_EMAIL: "test@example.com",
  GIT_CONFIG_GLOBAL: "/dev/null",
};

beforeAll(() => {
  stateDir = mkdtempSync(join(tmpdir(), "parity-watch-state-"));
  testBase = mkdtempSync(join(tmpdir(), "parity-watch-tests-"));
});

afterAll(() => {
  if (child) {
    child.kill();
  }
  rmSync(stateDir, { recursive: true, force: true });
  rmSync(testBase, { recursive: true, force: true });
});

function gitRun(dir: string, args: string[]) {
  const proc = Bun.spawnSync(["git", "-C", dir, ...args], {
    env: { ...process.env, ...GIT_ENV },
    stdout: "pipe",
    stderr: "pipe",
  });
  return proc.stdout.toString().trim();
}

async function waitFor(check: () => boolean, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (check()) {
      return;
    }
    await Bun.sleep(100);
  }
  throw new Error("condition not met in time");
}

describe("parity watch (daemon)", () => {
  test("initial sync, debounced sync on change, clean SIGTERM stop", async () => {
    const bare = join(testBase, "watch.git");
    const clone = join(testBase, "watch");
    gitRun(testBase, ["init", "--bare", "watch.git"]);
    gitRun(testBase, ["clone", bare, clone]);

    Bun.write(join(clone, "initial.txt"), "boot");
    gitRun(clone, ["add", "initial.txt"]);
    gitRun(clone, ["commit", "-m", "initial"]);
    gitRun(clone, ["push"]);

    const configPath = join(testBase, "parity.toml");
    Bun.write(configPath, `[watch]\nlocal_dir = ${JSON.stringify(clone)}\n`);

    const env = {
      ...process.env,
      ...GIT_ENV,
      PARITY_STATE_DIR: stateDir,
    };
    const proc = Bun.spawn(
      ["bun", "src/main.ts", "watch", "--foreground", "--config", configPath],
      { env, stdout: "pipe", stderr: "pipe" },
    );
    child = { kill: () => proc.kill("SIGTERM"), exited: proc.exited };

    // The daemon writes its pid file at startup.
    const pidPath = join(stateDir, "parity.pid");
    await waitFor(() => existsSync(pidPath), 5000);

    // A file change syncs after the 3s debounce.
    Bun.write(join(clone, "changed.txt"), "edited");
    await waitFor(() => gitRun(bare, ["log", "--oneline"]).includes("sync"), 20000);

    // The file content reached the remote.
    expect(gitRun(bare, ["show", "HEAD:changed.txt"])).toBe("edited");

    // SIGTERM removes the pid file and exits cleanly.
    proc.kill("SIGTERM");
    const exitCode = await proc.exited;
    expect(exitCode).toBe(0);
    await waitFor(() => !existsSync(pidPath), 5000);
    child = null;
  }, 60000);
});
