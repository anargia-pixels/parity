import { describe, expect, test } from "bun:test";
import { expandPath, parseToml } from "../src/lib/config";

describe("expandPath", () => {
  test("expands $VAR and ${VAR}", () => {
    process.env.PARITY_TEST_DIR = "/tmp/parity-test";
    expect(expandPath("$PARITY_TEST_DIR/a")).toBe("/tmp/parity-test/a");
    expect(expandPath("${PARITY_TEST_DIR}/b")).toBe("/tmp/parity-test/b");
  });

  test("expands leading ~", () => {
    expect(expandPath("~/foo").startsWith("/")).toBe(true);
    expect(expandPath("~/foo").endsWith("/foo")).toBe(true);
  });

  test("throws on unset variable", () => {
    expect(() => expandPath("$PARITY_UNSET_VAR_XYZ")).toThrow("not set");
  });
});

describe("parseToml", () => {
  test("parses label tables with env expansion", async () => {
    process.env.PARITY_TEST_DIR = "/tmp/parity-test";
    const entries = await parseToml('[opencode]\nlocal_dir = "$PARITY_TEST_DIR/opencode"\n', "test.toml");
    expect(entries).toEqual([{ label: "opencode", localDir: "/tmp/parity-test/opencode" }]);
  });

  test("rejects missing local_dir", async () => {
    await expect(parseToml("[opencode]\nfoo = 1\n", "test.toml")).rejects.toThrow("missing a local_dir");
  });

  test("rejects invalid labels", async () => {
    await expect(parseToml('["bad label"]\nlocal_dir = "/x"\n', "test.toml")).rejects.toThrow("must match");
  });

  test("rejects empty config", async () => {
    await expect(parseToml("", "test.toml")).rejects.toThrow("no entries");
  });

  test("rejects invalid toml", async () => {
    await expect(parseToml("a = 1\na = 2\n", "test.toml")).rejects.toThrow("could not parse");
  });
});
