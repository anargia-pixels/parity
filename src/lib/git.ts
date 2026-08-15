export type GitResult = {
  exitCode: number;
  stdout: string;
  stderr: string;
};

/**
 * Run git with -C <dir> so the working directory stays put.
 */
export async function git(dir: string, args: string[]): Promise<GitResult> {
  const proc = Bun.spawn(["git", "-C", dir, ...args], {
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
  ]);
  const exitCode = await proc.exited;
  return { exitCode, stdout: stdout.trim(), stderr: stderr.trim() };
}

export async function isGitRepo(dir: string): Promise<boolean> {
  const res = await git(dir, ["rev-parse", "--is-inside-work-tree"]);
  return res.exitCode === 0;
}

export async function hasRemote(dir: string): Promise<boolean> {
  const res = await git(dir, ["remote"]);
  return res.exitCode === 0 && res.stdout.length > 0;
}

/**
 * Return the repo's identity, or null when user.name/user.email are unset.
 */
export async function repoIdentity(dir: string): Promise<{ name: string; email: string } | null> {
  const name = await git(dir, ["config", "--get", "user.name"]);
  const email = await git(dir, ["config", "--get", "user.email"]);
  if (name.exitCode !== 0 || email.exitCode !== 0) {
    return null;
  }
  return { name: name.stdout, email: email.stdout };
}

/**
 * Identity flags to pass to git when the repo has no user.name/user.email, so
 * parity's auto-commits work on fresh machines without touching user config.
 */
export function identityFlags(identity: { name: string; email: string } | null): string[] {
  if (identity) {
    return [];
  }
  return ["-c", "user.name=parity", "-c", "user.email=parity@localhost"];
}
