#!/usr/bin/env node
/**
 * Generates the reference `HttpApi` that `packages/domain/test/api/ApiContract.test.ts`
 * diffs the hand-written contract against.
 *
 * `@effect/openapi-generator` reads the Swagger 2.0 document grpc-gateway emits
 * from `proto/trip.proto`, so the reference is derived from the same source of
 * truth the Go gateway serves — which is what makes the diff mean anything.
 *
 * Two things happen after the generator runs. Its import list is fixed rather
 * than computed, so the names it did not use are pruned; without that the file
 * fails `tsc` under `noUnusedLocals` and a generated file that does not compile
 * is a generated file nobody regenerates. And the output is one very long line
 * per schema, so dprint formats it — a committed generated file has to have a
 * stable shape or every regeneration is a diff of the whole thing.
 */
import { execFileSync } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const spec = path.join(root, "docs", "api", "surge.swagger.json");
const target = path.join(root, "packages", "domain", "test", "api", "generated.ts");

const header = `/**
 * GENERATED — do not edit. \`make proto\` regenerates it.
 *
 * \`@effect/openapi-generator\` reading the Swagger 2.0 document grpc-gateway
 * emits from \`proto/trip.proto\`. It is deliberately not the client:
 * \`src/api/SurgeApi.ts\` is, and it is hand-written because this output cannot
 * express the three things that make a client worth having — proto3 leaves
 * every field optional in Swagger, ids arrive as bare strings rather than
 * branded, and \`int64\` stays the quoted string it is on the wire.
 *
 * What it is for is \`ApiContract.test.ts\`, which diffs the two. A path, method,
 * parameter or field that moves in the proto appears here on the next
 * \`make proto\`, and the test fails until the hand-written contract catches up.
 */
`;

const generated = execFileSync(
  "npx",
  ["openapigen", "--spec", spec, "--format", "httpapi", "--name", "SurgeApi"],
  { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
);

/** Drops the names the generator imported but did not use. */
const pruneImports = (source) =>
  source.replace(
    /^import \{([^}]+)\} from "effect\/unstable\/httpapi"$/m,
    (line, names) => {
      const body = source.slice(source.indexOf(line) + line.length);
      const used = names
        .split(",")
        .map((name) => name.trim())
        .filter((name) => new RegExp(`\\b${name}\\b`).test(body));
      return `import { ${used.join(", ")} } from "effect/unstable/httpapi"`;
    },
  );

fs.writeFileSync(target, header + pruneImports(generated));
execFileSync("npx", ["dprint", "fmt", target], { cwd: root, stdio: "inherit" });
console.log(path.relative(root, target));
