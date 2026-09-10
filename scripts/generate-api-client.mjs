#!/usr/bin/env node
/**
 * Generates `packages/domain/src/api/SurgeApi.ts` — the client's whole view of
 * the Go API — from the OpenAPI document grpc-gateway emits.
 *
 * `proto/*.proto` is the single source of truth. It already generates the Go
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
 * Custom methods — `:cancel`, `:arrive` — are Google's convention, and every one
 * in the proto carries a second binding on a plain path segment beside it.
 *
 * Effect's router reads `/v1/trips/:tripId:cancel` as a parameter literally
 * named `tripId:cancel`, so the client cannot express the `:verb` spelling at
 * all. Both bindings reach the same handler, so the generated client takes the
 * segment one and every `}:verb` path is dropped here — as a rule, so the next
 * custom method needs nothing in this file.
 */
const UNROUTABLE = /\}:[A-Za-z]+$/;

const spec = JSON.parse(fs.readFileSync(specPath, "utf8"));

for (const route of Object.keys(spec.paths)) {
  if (UNROUTABLE.test(route)) delete spec.paths[route];
}

/**
 * Schema names are qualified by tag; method names are not.
 *
 * The generator derives two things from an `operationId`: the endpoint's
 * method name, and the prefix of every schema it emits for that endpoint.
 * Method names live inside their group, so `trips.get` and `simulator.get`
 * coexist happily. Schema names are module exports, so both operations want
 * `Get200` — and the generator renames the loser `Get2002` with a warning,
 * which is how `Trip.ts` came to derive its types from the simulator.
 *
 * So each id is qualified with its tag for generation — `TripsGet200`,
 * `SimulatorGet200` — and the proto's short name is put back on the endpoint
 * afterwards. Operation ids then only have to be unique within a service,
 * which is the only place anyone reading the proto would think to check.
 */
const shortNames = new Map();
for (const operations of Object.values(spec.paths)) {
  for (const operation of Object.values(operations)) {
    const id = operation?.operationId;
    const tag = operation?.tags?.[0];
    if (typeof id !== "string" || typeof tag !== "string") continue;
    const qualified = `${tag}_${id}`;
    shortNames.set(qualified, id);
    operation.operationId = qualified;
  }
}

/**
 * The statuses the gateway's error handler can answer with, each carrying the
 * same `ErrorBody`.
 *
 * The document declares errors as `default`, which is true — every operation can
 * fail — and useless to the generated client: `HttpApiClient` decodes an error
 * body only for the exact statuses an endpoint declares, and the generator maps
 * `default` to 500 alone. A 404 or a 409 arrived as an untyped `StatusCodeError`
 * with the body never read, and a screen could not tell "that fare expired,
 * quote again" from "the server is down".
 *
 * So `default` is spelled out as every status `rest.go` maps a gRPC code to.
 * `rest_test.go` in the gateway reads this array and fails if the two disagree,
 * which is the only reason it is safe to write the list down twice.
 */
const GATEWAY_ERROR_STATUSES = [400, 401, 403, 404, 409, 429, 500, 501, 503, 504];

for (const operations of Object.values(spec.paths)) {
  for (const operation of Object.values(operations)) {
    const fallback = operation?.responses?.default;
    if (fallback === undefined) continue;
    for (const status of GATEWAY_ERROR_STATUSES) {
      operation.responses[String(status)] ??= fallback;
    }
    delete operation.responses.default;
  }
}

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
  { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
);

/** Swaps each `format` for the schema that actually decodes it. */
const applyFormats = (source) => {
  let out = source;
  for (const [format, schema] of Object.entries(FORMATS)) {
    out = out.replaceAll(`Schema.String.annotate({ "format": ${JSON.stringify(format)} })`, schema);
  }
  return out;
};

/**
 * Every exported type becomes the decoded type of the schema beside it.
 *
 * The generator emits each shape twice: a `Schema`, and a `type` alias of what
 * the document says — the wire's view, with ids as plain strings and 64-bit
 * fields as the quoted strings proto3 sends. After the `format` rewrite above,
 * the schemas decode to branded ids and numbers, so those aliases described
 * values nothing in the program ever holds. `Trip["route"]["seconds"]` typed as
 * a string, while every runtime value was a number, is how that showed up.
 *
 * Each alias is one line in the generator's raw output, which is why this runs
 * before dprint reflows them.
 */
const alignTypes = (source) => {
  const schemas = new Set(
    [...source.matchAll(/^export const (\w+) = /gm)].map((match) => match[1]),
  );
  return source.replace(
    /^export type (\w+) = .*$/gm,
    (line, name) => (schemas.has(name) ? `export type ${name} = typeof ${name}.Type` : line),
  );
};

/** The generator's own camelize, for matching the method names it emitted. */
const camelize = (id) => id.replace(/[^A-Za-z0-9]+([A-Za-z0-9])/g, (_, next) => next.toUpperCase());

/** Puts each endpoint's short name back, and fails rather than ship a qualified one. */
const restoreMethodNames = (source) => {
  const byEmitted = new Map(
    [...shortNames].map(([qualified, short]) => [camelize(qualified), short]),
  );
  let out = source.replace(
    /HttpApiEndpoint\.(\w+)\("(\w+)"/g,
    (call, verb, name) =>
      byEmitted.has(name) ? `HttpApiEndpoint.${verb}("${byEmitted.get(name)}"` : call,
  );
  for (const [qualified, short] of shortNames) {
    out = out.replaceAll(`OpenApi.Identifier, "${qualified}")`, `OpenApi.Identifier, "${short}")`);
  }
  const leftover = [...byEmitted.keys()].filter((name) => out.includes(`("${name}"`));
  if (leftover.length > 0) throw new Error(`method names left qualified: ${leftover.join(", ")}`);
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
 * \`proto/*.proto\`, which is the single source of truth for the gRPC service,
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
  + pruneImports(alignTypes(applyFormats(restoreMethodNames(generated)))).replace(
    /^(import \* as Schema from "effect\/Schema"\n)/m,
    `$1import { ${imports} } from "./Primitives.js"\n`,
  );

fs.writeFileSync(target, source);
execFileSync("npx", ["dprint", "fmt", target], { cwd: root, stdio: "inherit" });
console.log(path.relative(root, target));
