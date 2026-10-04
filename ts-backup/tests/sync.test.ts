import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { syncEntry } from "../src/sync";

type Ran = {
  exitCode: number;
  stdout: string;
  stderr: string;
};

const GIT_ENV = {
  GIT_AUTHOR_NAME: "test",
  GIT_AUTHOR_EMAIL: "test@example.com",
  GIT_COMMITTER_NAME: "test",
  GIT_COMMITTER_EMAIL: "test@example.com",
  GIT_CONFIG_GLOBAL: "/dev/null",
};

const testEnv = { ...process.env, ...GIT_ENV };

function gitRun(dir: string, args: string[]): Ran {
  const proc = Bun.spawnSync(["git", "-C", dir, ...args], {
    env: testEnv,
    stdout: "pipe",
    stderr: "pipe",
  });
  return {
    exitCode: proc.exitCode,
    stdout: proc.stdout.toString().trim(),
    stderr: proc.stderr.toString().trim(),
  };
}

/** Create a bare remote and a clone; both paths are returned. */
function setupRepos(base: string, name: string): { bare: string; clone: string } {
  const bare = join(base, `${name}.git`);
  const clone = join(base, name);
  gitRun(base, ["init", "--bare", `${name}.git`]);
  gitRun(base, ["clone", bare, clone]);
  return { bare, clone };
}

let stateDir: string;
let testBase: string;
let plainDirs: string[] = [];

beforeAll(() => {
  stateDir = mkdtempSync(join(tmpdir(), "parity-state-"));
  testBase = mkdtempSync(join(tmpdir(), "parity-tests-"));
  process.env.PARITY_STATE_DIR = stateDir;
});

afterAll(() => {
  rmSync(stateDir, { recursive: true, force: true });
  rmSync(testBase, { recursive: true, force: true });
  for (const path of plainDirs) {
    rmSync(path, { recursive: true, force: true });
  }
});

describe("syncEntry", () => {
  test("commits and pushes local changes", async () => {
    const { bare, clone } = setupRepos(testBase, "happy");
    
    Bun.write(join(clone, "a.txt"), "hello");
    const outcome = await syncEntry({ label: "happy", localDir: clone }, { allowSecrets: false, progress: false });
    expect(outcome.status).toBe("ok");
    expect(gitRun(bare, ["log", "--oneline"]).stdout).toContain("sync");
    expect(gitRun(bare, ["show", "HEAD:a.txt"]).stdout).toBe("hello");
  });

  test("skips silently when nothing to commit", async () => {
    const { bare, clone } = setupRepos(testBase, "empty");
    
    const outcome = await syncEntry({ label: "empty", localDir: clone }, { allowSecrets: false, progress: false });
    expect(outcome).toEqual({ status: "skipped", reason: "empty", files: [] });
    expect(gitRun(bare, ["log", "--oneline"]).stdout).toBe("");
  });

  test("skips when secrets are staged, commits with --allow-secrets", async () => {
    const { bare, clone } = setupRepos(testBase, "secrets");
    
    Bun.write(join(clone, "config.toml"), 'api_key = "abcdefghijklmnopqr"\n');
    const blocked = await syncEntry({ label: "secrets", localDir: clone }, { allowSecrets: false, progress: false });
    expect(blocked.status).toBe("skipped");
    if (blocked.status === "skipped") {
      expect(blocked.reason).toBe("secrets");
      expect(blocked.files).toContain("config.toml");
    }
    expect(gitRun(bare, ["log", "--oneline"]).stdout).toBe("");
    const allowed = await syncEntry({ label: "secrets", localDir: clone }, { allowSecrets: true, progress: false });
    expect(allowed.status).toBe("ok");
    expect(gitRun(bare, ["log", "--oneline"]).stdout).toContain("sync");
  });

  test("stashes dirty work, pulls, and pops cleanly", async () => {
    const { bare, clone: cloneA } = setupRepos(testBase, "stash-happy-a");
    const cloneB = join(testBase, "stash-happy-b");
    gitRun(testBase, ["clone", bare, "stash-happy-b"]);
    

    const lines = ["one", "two", "three", "four", "five"];
    Bun.write(join(cloneA, "f.txt"), lines.join("\n"));
    gitRun(cloneA, ["add", "f.txt"]);
    gitRun(cloneA, ["commit", "-m", "base"]);
    gitRun(cloneA, ["push"]);

    gitRun(cloneB, ["pull"]);
    const localLines = [...lines];
    localLines[0] = "ONE";
    Bun.write(join(cloneB, "f.txt"), localLines.join("\n"));

    const remoteLines = [...lines];
    remoteLines[4] = "FIVE";
    Bun.write(join(cloneA, "f.txt"), remoteLines.join("\n"));
    gitRun(cloneA, ["add", "f.txt"]);
    gitRun(cloneA, ["commit", "-m", "remote change"]);
    gitRun(cloneA, ["push"]);

    const outcome = await syncEntry({ label: "stash-happy", localDir: cloneB }, { allowSecrets: false, progress: false });
    expect(outcome.status).toBe("ok");
    expect(gitRun(cloneB, ["stash", "list"]).stdout).toBe("");
    const merged = Bun.file(join(cloneB, "f.txt"));
    const text = await merged.text();
    expect(text).toContain("ONE");
    expect(text).toContain("FIVE");
    expect(gitRun(bare, ["log", "--oneline"]).stdout).toContain("sync");
  });

  test("keeps the stash when popping conflicts", async () => {
    const { bare, clone: cloneA } = setupRepos(testBase, "conflict-a");
    const cloneB = join(testBase, "conflict-b");
    gitRun(testBase, ["clone", bare, "conflict-b"]);

    Bun.write(join(cloneA, "f.txt"), "original\n");
    gitRun(cloneA, ["add", "f.txt"]);
    gitRun(cloneA, ["commit", "-m", "base"]);
    gitRun(cloneA, ["push"]);

    gitRun(cloneB, ["pull"]);
    Bun.write(join(cloneB, "f.txt"), "local version\n");

    Bun.write(join(cloneA, "f.txt"), "remote version\n");
    gitRun(cloneA, ["add", "f.txt"]);
    gitRun(cloneA, ["commit", "-m", "remote change"]);
    gitRun(cloneA, ["push"]);

    const outcome = await syncEntry({ label: "conflict", localDir: cloneB }, { allowSecrets: false, progress: false });
    expect(outcome.status).toBe("error");
    if (outcome.status === "error") {
      expect(outcome.message).toContain("stash preserved");
    }
    expect(gitRun(cloneB, ["stash", "list"]).stdout).toContain("parity autostash");
  });

  test("errors on non-repo and missing remote", async () => {
    const plain = mkdtempSync(join(tmpdir(), "parity-plain-"));
    plainDirs.push(plain);
    
    const outcome = await syncEntry({ label: "plain", localDir: plain }, { allowSecrets: false, progress: false });
    expect(outcome.status).toBe("error");
    if (outcome.status === "error") {
      expect(outcome.message).toContain("not a git repository");
    }

    gitRun(plain, ["init"]);
    const noRemote = await syncEntry({ label: "plain", localDir: plain }, { allowSecrets: false, progress: false });
    expect(noRemote.status).toBe("error");
    if (noRemote.status === "error") {
      expect(noRemote.message).toContain("no git remote");
    }
  });
});
