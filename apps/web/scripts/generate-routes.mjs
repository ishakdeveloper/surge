#!/usr/bin/env node
/**
 * Regenerates `src/routeTree.gen.ts` without starting the dev server.
 *
 * The router plugin does this on `vite dev` and `vite build`, which is fine while
 * someone is running the app and useless for everything else: a new route file
 * does not type-check until the tree knows about it — `<Link to>` is typed
 * against it — so `pnpm check` fails on a route nobody has opened in a browser
 * yet.
 *
 * Two things are taken from the installed packages rather than written here,
 * because both would drift if copied:
 *
 * - The generator is resolved *through the router plugin*, so it is the exact
 *   version the dev server runs. The store holds more than one, and two versions
 *   can disagree about formatting.
 * - The footer — the `declare module '@tanstack/react-start'` block that
 *   registers the router with Start — comes from Start's own
 *   `buildRouteTreeFileFooterFromConfig`. Start adds it on top of the plain generator, and
 *   leaving it out produced a tree that type-checked differently from the one
 *   the dev server writes.
 *
 * Usage: `pnpm routes` writes the real tree. `pnpm routes <path>` writes
 * somewhere else, which is how this was checked against the committed file.
 */
import { createRequire } from "node:module";
import * as path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(import.meta.url);

const resolveFrom = (from, name) =>
  path.dirname(require.resolve(`${name}/package.json`, { paths: [from] }));

const routerPlugin = resolveFrom(root, "@tanstack/router-plugin");
const generator = resolveFrom(routerPlugin, "@tanstack/router-generator");
const start = resolveFrom(root, "@tanstack/react-start");
const startCore = resolveFrom(start, "@tanstack/start-plugin-core");

const { Generator, getConfig } = await import(
  pathToFileURL(path.join(generator, "dist", "esm", "index.js")).href
);
const { buildRouteTreeFileFooterFromConfig } = await import(
  pathToFileURL(path.join(startCore, "dist", "esm", "start-router-plugin", "route-tree-footer.js"))
    .href
);

const output = process.argv[2] ?? "./src/routeTree.gen.ts";
const generatedRouteTreePath = path.resolve(root, output);

const config = getConfig(
  {
    routesDirectory: "./src/routes",
    generatedRouteTree: output,
    target: "react",
    // Start's own function, fed the two facts it reads from Start's config:
    // where the router lives, and that this app has no `start.ts`. It is the
    // exported entry point rather than the helper it wraps, so an internal
    // rename in Start does not break this.
    routeTreeFileFooter: buildRouteTreeFileFooterFromConfig({
      generatedRouteTreePath,
      corePluginOpts: { framework: "react" },
      getConfig: () => ({
        startConfig: { router: { routeTreeFileFooter: undefined } },
        resolvedStartConfig: {
          routerFilePath: path.join(root, "src", "router.tsx"),
          startFilePath: undefined,
        },
      }),
    }),
  },
  root,
);

await new Generator({ config, root }).run();
console.log(path.relative(process.cwd(), generatedRouteTreePath));
