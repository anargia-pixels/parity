/**
 * Best-effort desktop notification via notify-send. Only attempts when a
 * graphical session is present; failures are silent.
 */
export function notify(summary: string, body: string): void {
  if (!process.env.DISPLAY && !process.env.WAYLAND_DISPLAY) {
    return;
  }
  const proc = Bun.spawn(
    ["notify-send", "-a", "parity", "-u", "critical", summary, body],
    { stdout: "ignore", stderr: "ignore", stdin: "ignore" },
  );
  proc.unref();
}
