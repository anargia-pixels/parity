import { existsSync, mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { tryCatchSync } from "./try-catch";

export type Entry = {
  label: string;
  localDir: string;
};

export type ParityConfig = {
  path: string;
  entries: Entry[];
};

const LABEL_PATTERN = /^[A-Za-z0-9_-]+$/;

export const isCompiled = Bun.main.startsWith("/$bunfs/");

export function homeDir(): string {
  return process.env.HOME || homedir();
}

export function configDir(): string {
  return process.env.XDG_CONFIG_HOME || join(homeDir(), ".config");
}

/**
 * Base dir for the config file, pid file, log, state, and locks.
 * PARITY_STATE_DIR overrides it for tests.
 */
export function parityDir(): string {
  return process.env.PARITY_STATE_DIR || join(configDir(), "parity");
}

/**
 * Expand environment variables and `~` in a path: `$VAR`, `${VAR}`, `~/...`.
 */
export function expandPath(raw: string): string {
  return raw.replace(/\$\{(\w+)\}|\$(\w+)|^~/g, (match, braced, plain, offset) => {
    if (match === "~" && offset === 0) {
      return homeDir();
    }
    const name = braced ?? plain;
    const value = process.env[name];
    if (value === undefined) {
      throw new Error(`environment variable ${name} is not set (in "${raw}")`);
    }
    return value;
  });
}

/**
 * Parse TOML text into entries. Top-level tables are labels, each requiring a
 * string `local_dir`.
 */
export async function parseToml(text: string, source: string): Promise<Entry[]> {
  const parsed = await tryCatchSync<unknown>(() => Bun.TOML.parse(text));
  if (parsed.error) {
    throw new Error(`could not parse ${source}: ${parsed.error.message}`);
  }
  if (parsed.data === null || typeof parsed.data !== "object" || Array.isArray(parsed.data)) {
    throw new Error(`${source}: expected tables like [label] with local_dir`);
  }
  const entries: Entry[] = [];
  for (const [label, rawTable] of Object.entries(parsed.data)) {
    if (rawTable === null || typeof rawTable !== "object" || Array.isArray(rawTable)) {
      throw new Error(`${source}: [${label}] must be a table with a local_dir string`);
    }
    const localDir = rawTable["local_dir"];
    if (typeof localDir !== "string" || localDir.trim() === "") {
      throw new Error(`${source}: [${label}] is missing a local_dir string`);
    }
    if (!LABEL_PATTERN.test(label)) {
      throw new Error(`${source}: label "${label}" must match ${LABEL_PATTERN}`);
    }
    entries.push({ label, localDir: expandPath(localDir) });
  }
  if (entries.length === 0) {
    throw new Error(`${source}: no entries defined`);
  }
  return entries;
}

/**
 * Locate parity.toml: --config flag wins, otherwise the compiled binary reads
 * $XDG_CONFIG_HOME/parity/parity.toml and the dev version reads ./parity.toml.
 */
export function discoverConfigPath(flagPath?: string): string {
  if (flagPath) {
    if (!existsSync(flagPath)) {
      throw new Error(`config file not found: ${flagPath}`);
    }
    return flagPath;
  }
  const path = isCompiled ? join(parityDir(), "parity.toml") : join(process.cwd(), "parity.toml");
  if (!existsSync(path)) {
    throw new Error(
      `config file not found: ${path}\n` +
        (isCompiled
          ? "create it, or pass --config <path>. See parity-example.toml"
          : "create it next to the project, or pass --config <path>. See parity-example.toml"),
    );
  }
  return path;
}

export async function loadConfig(flagPath?: string): Promise<ParityConfig> {
  const path = discoverConfigPath(flagPath);
  const text = await Bun.file(path).text();
  return { path, entries: await parseToml(text, path) };
}

export function ensureStateDir(): string {
  const dir = parityDir();
  mkdirSync(dir, { recursive: true });
  return dir;
}

export function pidFilePath(): string {
  return join(parityDir(), "parity.pid");
}

export function logFilePath(): string {
  return join(parityDir(), "parity.log");
}

export function stateFilePath(): string {
  return join(parityDir(), "state.json");
}
