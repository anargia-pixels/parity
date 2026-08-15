import { describe, expect, test } from "bun:test";
import { findSecretsInDiff } from "../src/lib/secret-check";

describe("findSecretsInDiff", () => {
  test("detects common secret patterns on added lines", () => {
    const diff = [
      "diff --git a/config.toml b/config.toml",
      "--- a/config.toml",
      "+++ b/config.toml",
      "+api_key = \"abcdefghijklmnopqrstuvwxyz123\"",
      "+  sk-1234567890abcdefghijklmnop",
    ].join("\n");
    const patterns = findSecretsInDiff(diff);
    expect(patterns).toContain("api key assignment");
    expect(patterns).toContain("openai api key");
  });

  test("ignores unchanged context lines", () => {
    const diff = [" context line", "- removed line", "+ a plain added line"].join("\n");
    expect(findSecretsInDiff(diff)).toEqual([]);
  });

  test("detects private key blocks", () => {
    const diff = "+-----BEGIN OPENSSH PRIVATE KEY-----";
    expect(findSecretsInDiff(diff)).toContain("private key");
  });

  test("ignores the +++ header line", () => {
    const diff = "+++ b/key.txt";
    expect(findSecretsInDiff(diff)).toEqual([]);
  });
});
