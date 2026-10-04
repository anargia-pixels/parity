import { existsSync } from "node:fs";
import { stateFilePath } from "./config";
import { tryCatch } from "./try-catch";

export type EntryState = {
  lastSync: string;
  result: "ok" | "skipped" | "error";
  message?: string;
};

export type StateFile = {
  updated: string;
  entries: Record<string, EntryState>;
};

export async function readState(): Promise<StateFile | null> {
  const path = stateFilePath();
  if (!existsSync(path)) {
    return null;
  }
  const parsed = await tryCatch<StateFile>(
    Bun.file(path)
      .text()
      .then((text) => JSON.parse(text)),
  );
  return parsed.error ? null : parsed.data;
}

/**
 * Merge per-entry updates into the state file for `parity status`.
 */
export async function writeState(updates: Record<string, EntryState>): Promise<void> {
  const previous = await readState();
  const state: StateFile = {
    updated: new Date().toISOString(),
    entries: { ...previous?.entries, ...updates },
  };
  Bun.write(stateFilePath(), JSON.stringify(state, null, 2));
}
