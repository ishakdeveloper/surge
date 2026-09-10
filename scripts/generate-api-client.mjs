#!/usr/bin/env node
/**
 * Generates `packages/domain/src/api/SurgeApi.ts` — the client's whole view of
 * the Go API — from the OpenAPI document grpc-gateway emits.
 *
 * `proto/trip.proto` is the single source of truth. It already generates the Go
 * server, the REST reverse proxy and the published document; this makes it
 * generate the TypeScript client too, so adding an endpoint is one edit rather
 * than one edit and a matching hand-written declaration that drifts.
 *
 * Three passes, and the middle one is the only interesting one.
 */
import { execFileSync } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const specPath = path.join(root, "docs", "api", "surge.swagger.json");
const target = path.join(root, "packages", "domain", "src", "api", "SurgeApi.ts");

/**
 * `format` is how the proto names the semantic type of a string, and this is
 * the other half of that agreement.
 *
 * OpenAPI leaves `format` open on purpose, and it is the only annotation that
 * survives proto → document → generated schema, path parameters included. So a
 * field marked `trip-id` in the proto becomes a branded `TripId` here, and
 * `int64` — which grpc-gateway sets on its own, because proto3 JSON quotes
 * 64-bit numbers — becomes a number decoded once instead of parsed at every
 * call site.
 *
 * Adding a branded id is one `format` in the proto and one line here.
 */
const FORMATS = {
  "trip-id": "TripId",
  "fare-id": "FareId",
  "driver-id": "DriverId",
  "rider-id": "RiderId",
  "error-code": "ErrorCode",
  cents: "CentsFromString",
  int64: "Int64FromString",
};

/**
 * The `:cancel` binding, which exists in the proto because it is Google's
 * convention for custom methods.
 *
 * Effect's router reads `/v1/trips/:tripId:cancel` as a parameter literally
 * named `tripId:cancel`, so the client cannot express it. The proto declares a
 * second binding on a plain path segment for exactly this, and both reach the
 * same handler — so the generated client uses that one and this drops the
 * other rather than emitting a route that would never match.
 */
const UNROUTABLE = "/v1/trips/{tripId}:cancel";

const spec = JSON.parse(fs.readFileSync(specPath, "utf8"));

delete spec.paths[UNROUTABLE];

/**
 * proto3 has no required fields, so every property in the document is optional
 * and `trip.id` would decode as `string | undefined` at every call site. That
 * is not what the server does in either direction: it marshals responses with
 * `EmitUnpopulated`, so an unassigned driver arrives as `""` rather than
 * absent, and it unmarshals requests with `DiscardUnknown: false` onto handlers
 * that require every field they declare.
 *
 * The document cannot express that, so it is asserted here — once, as a rule,
 * rather than as a `required` list per message that would need maintaining
 * beside the fields it lists.
 */
for (const definition of Object.values(spec.definitions ?? {})) {
  if (definition.type !== "object" || definition.properties === undefined) continue;
  definition.required = Object.keys(definition.properties);
}

/**
 * A property carrying one of the formats above is rewritten wholesale below, by
 * matching the exact text the generator emits for it. Stripping everything but
 * the type and the format is what makes that text exact — a description would
 * be emitted into the same `annotate` call and the match would have to become a
 * brace-balancing parser over generated TypeScript.
 *
 * The cost is the doc comment on five id fields, which say `format` anyway.
 *
 * Swagger 2.0 inlines a parameter's schema into the parameter itself, so
 * `name`, `in` and `required` sit beside `type` and `format` on a path
 * parameter and have to survive — stripping them silently removed `:tripId`
 * from two routes and left the generator emitting endpoints with no parameters
 * at all.
 */
const KEEP = new Set(["type", "format", "name", "in", "required"]);

const stripAnnotations = (node) => {
  if (node === null || typeof node !== "object") return;
  if (Array.isArray(node)) {
    for (const item of node) stripAnnotations(item);
    return;
  }
  if (typeof node.format === "string" && node.format in FORMATS) {
    for (const key of Object.keys(node)) {
      if (!KEEP.has(key)) delete node[key];
    }
  }
  for (const value of Object.values(node)) stripAnnotations(value);
};
stripAnnotations(spec);

const patched = path.join(root, "node_modules", ".cache", "surge-openapi.json");
fs.mkdirSync(path.dirname(patched), { recursive: true });
fs.writeFileSync(patched, JSON.stringify(spec));

const generated = execFileSync(
  "npx",
  ["openapigen", "--spec", patched, "--format", "httpapi", "--name", "SurgeApi"],
  { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
);

/** Swaps each `format` for the schema that actually decodes it. */
const applyFormats = (source) => {
  let out = source;
  for (const [format, schema] of Object.entries(FORMATS)) {
    out = out.replaceAll(`Schema.String.annotate({ "format": ${JSON.stringify(format)} })`, schema);
  }
  return out;
};

/** Drops the names the generator imports unconditionally but did not use. */
const pruneImports = (source) =>
  source.replace(/^import \{([^}]+)\} from "effect\/unstable\/httpapi"$/m, (line, names) => {
    const body = source.slice(source.indexOf(line) + line.length);
    const used = names.split(",").map((name) => name.trim())
      .filter((name) => new RegExp(`\\b${name}\\b`).test(body));
    return `import { ${used.join(", ")} } from "effect/unstable/httpapi"`;
  });

const header = `/**
 * GENERATED — do not edit. Run \`make proto\`.
 *
 * The Go API, as this codebase consumes it. Generated by
 * \`@effect/openapi-generator\` from the OpenAPI document grpc-gateway emits from
 * \`proto/trip.proto\`, which is the single source of truth for the gRPC service,
 * the REST reverse proxy, the published spec and this file.
 *
 * \`HttpApiBuilder\` is never used: no TypeScript implements this. What it gives
 * the browser is a typed client and runtime decoding at the boundary, which is
 * the difference between a wrong response failing here with a readable error and
 * failing three components later as \`undefined is not an object\`.
 *
 * \`scripts/generate-api-client.mjs\` describes the two things the document cannot
 * say for itself — that the gateway's \`EmitUnpopulated\` makes every field
 * present, and that a \`format\` names a branded id or a 64-bit number.
 */
`;

const imports = Object.values(FORMATS).sort().join(", ");
const source = header
  + pruneImports(applyFormats(generated)).replace(
    /^(import \* as Schema from "effect\/Schema"\n)/m,
    `$1import { ${imports} } from "./Primitives.js"\n`,
  );

fs.writeFileSync(target, source);
execFileSync("npx", ["dprint", "fmt", target], { cwd: root, stdio: "inherit" });
console.log(path.relative(root, target));
