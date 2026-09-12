import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The rule that makes the Expo app cheap instead of a rewrite.
 *
 * `packages/client` and `packages/domain` are compiled by every surface: the
 * web app today, `apps/mobile` later. The moment either imports a platform
 * package — `@effect/platform-browser`, `@effect/platform-node`, `window`,
 * `document` — React Native cannot compile it, and the fix at that point is to
 * rework every call site rather than one import.
 *
 * So they declare what they need (`HttpClient`, `Socket`) as requirements and
 * let each app provide it at the edge. This is the check that keeps it true,
 * and it is deliberately cheap: a lint rule nobody runs is not a constraint.
 */
const BANNED = [
  { pattern: /@effect\/platform-browser/, why: "browser-only; apps/web provides it at the edge" },
  { pattern: /@effect\/platform-node/, why: "Node-only; nothing shared may assume a Node runtime" },
  { pattern: /\bfrom\s+["']node:/, why: "Node built-ins are not available in React Native" },
  // Bare globals only. `response.document.id` is a field on an API response
  // and `started.window.close` would be somebody's property; neither is the
  // DOM. The lookbehind is what tells those apart from a real `document.body`.
  { pattern: /(?<![.\w$])window\./, why: "no DOM globals in shared code" },
  { pattern: /(?<![.\w$])document\./, why: "no DOM globals in shared code" },
  { pattern: /\blocalStorage\b/, why: "storage differs per platform; inject it" },
];

/**
 * Comments are stripped before scanning.
 *
 * Without this the check fails on its own documentation: `AuthToken.ts` names
 * `@effect/platform-browser` in a doc comment explaining why it must never
 * import it, and a naive scan reads that as the violation it is warning about.
 */
const stripComments = (source: string): string =>
  source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");

/**
 * String literals are stripped too, and for the same reason.
 *
 * `api/SurgeApi.ts` is generated, and it carries every description written in
 * `proto/*.proto` as a string. A sentence that ends "...read off the
 * document." is prose about a driver's paperwork, and reading it as a DOM
 * global would make the proto's comments unwritable.
 */
const stripStrings = (source: string): string =>
  source
    .replace(/"(?:[^"\\\n]|\\.)*"/g, "\"\"")
    .replace(/'(?:[^'\\\n]|\\.)*'/g, "''")
    .replace(/`(?:[^`\\]|\\.)*`/g, "``");

const sourceFiles = (root: string): ReadonlyArray<string> => {
  const found: Array<string> = [];

  const walk = (directory: string) => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        walk(full);
      } else if (entry.name.endsWith(".ts") && !entry.name.endsWith(".test.ts")) {
        found.push(full);
      }
    }
  };

  walk(root);
  return found;
};

describe("shared packages stay platform-free", () => {
  const packages = [
    ["@surge/client", path.join(import.meta.dirname, "..", "src")],
    ["@surge/domain", path.join(import.meta.dirname, "..", "..", "domain", "src")],
    // The atoms both apps run: the web app and `apps/mobile` import them as-is.
    ["@surge/common", path.join(import.meta.dirname, "..", "..", "common", "src")],
  ] as const;

  for (const [name, root] of packages) {
    it(`${name} imports no platform-specific module`, () => {
      const offences: Array<string> = [];

      for (const file of sourceFiles(root)) {
        const contents = stripStrings(stripComments(fs.readFileSync(file, "utf8")));

        for (const { pattern, why } of BANNED) {
          if (pattern.test(contents)) {
            offences.push(`${path.relative(root, file)} matches ${pattern} — ${why}`);
          }
        }
      }

      expect(offences).toEqual([]);
    });
  }

  it("finds files at all, so a passing run means something", () => {
    for (const [name, root] of packages) {
      expect(sourceFiles(root).length, `${name} has no source files`).toBeGreaterThan(0);
    }
  });
});
