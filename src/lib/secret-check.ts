import { git } from "./git";

export type SecretFinding = {
  file: string;
  pattern: string;
};

const SECRET_PATTERNS: { name: string; regex: RegExp }[] = [
  { name: "openai api key", regex: /\bsk-[A-Za-z0-9]{20,}/ },
  { name: "github token", regex: /\bgh[pousr]_[A-Za-z0-9]{30,}/ },
  { name: "slack token", regex: /\bxox[baprs]-[A-Za-z0-9-]{10,}/ },
  { name: "aws access key", regex: /\bAKIA[0-9A-Z]{16}\b/ },
  { name: "private key", regex: /-----BEGIN [A-Z ]*PRIVATE KEY-----/ },
  {
    name: "api key assignment",
    regex:
      /\b(api[_-]?key|api[_-]?token|access[_-]?token|auth[_-]?token|refresh[_-]?token|client[_-]?secret|secret[_-]?key|password|passwd)\s*[:=]\s*["']?[A-Za-z0-9+/_.=-]{12,}["']?/i,
  },
];

/**
 * Scan added lines of a staged diff for obvious secrets.
 */
export function findSecretsInDiff(diff: string): string[] {
  const patterns: string[] = [];
  for (const line of diff.split("\n")) {
    if (!line.startsWith("+") || line.startsWith("+++")) {
      continue;
    }
    for (const { name, regex } of SECRET_PATTERNS) {
      if (regex.test(line)) {
        patterns.push(name);
      }
    }
  }
  return patterns;
}

/**
 * List staged files, skipping binary files.
 */
export async function stagedFiles(dir: string): Promise<string[]> {
  const names = await git(dir, ["diff", "--cached", "--name-only"]);
  if (names.exitCode !== 0 || names.stdout === "") {
    return [];
  }
  const numstat = await git(dir, ["diff", "--cached", "--numstat"]);
  const binary = new Set<string>();
  if (numstat.exitCode === 0) {
    for (const line of numstat.stdout.split("\n")) {
      const parts = line.split("\t");
      if (parts.length >= 3 && parts[0] === "-" && parts[1] === "-") {
        const file = parts[2];
        if (file !== undefined) {
          binary.add(file);
        }
      }
    }
  }
  return names.stdout.split("\n").filter((f) => !binary.has(f));
}

export async function checkStagedForSecrets(dir: string): Promise<SecretFinding[]> {
  const files = await stagedFiles(dir);
  const findings: SecretFinding[] = [];
  for (const file of files) {
    const diff = await git(dir, ["diff", "--cached", "--", file]);
    if (diff.exitCode !== 0) {
      continue;
    }
    for (const pattern of findSecretsInDiff(diff.stdout)) {
      findings.push({ file, pattern });
    }
  }
  return findings;
}
