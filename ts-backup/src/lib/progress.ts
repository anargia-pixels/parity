import { fstatSync } from "node:fs";
import { tryCatchSync } from "./try-catch";

const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
const BAR_WIDTH = 20;

export type ProgressBar = {
  setStep(index: number): void;
  finish(): void;
};

/** True when stdout is a terminal (not piped or redirected). */
export async function isTty(): Promise<boolean> {
  const stat = await tryCatchSync(() => fstatSync(1));
  return stat.error === null && stat.data.isCharacterDevice();
}

/**
 * One-line progress bar with a spinner, rendered on stdout. Steps are
 * discrete phases of a sync; the bar fills as each one completes.
 */
export function createProgressBar(steps: string[]): ProgressBar {
  let frame = 0;
  let stepIndex = 0;
  const timer = setInterval(() => {
    frame++;
    render();
  }, 80);

  const render = () => {
    const fraction = steps.length === 0 ? 1 : stepIndex / steps.length;
    const filled = Math.round(fraction * BAR_WIDTH);
    const bar = "=".repeat(filled) + " ".repeat(BAR_WIDTH - filled);
    const percent = Math.round(fraction * 100);
    const label = steps[stepIndex] ?? "";
    const spinner = SPINNER_FRAMES[frame % SPINNER_FRAMES.length];
    process.stdout.write(`\r[${bar}] ${String(percent).padStart(3)}% ${label} ${spinner}`);
  };

  render();

  return {
    setStep(index: number) {
      stepIndex = index;
      render();
    },
    finish() {
      clearInterval(timer);
      process.stdout.write("\r\x1b[2K");
    },
  };
}
