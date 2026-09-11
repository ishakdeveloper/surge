#!/usr/bin/env node
/**
 * Which images a change touches, so CI builds and scans those and nothing else.
 *
 * Detection only decides what CI bothers to build. What actually rolls is
 * decided by the digest: an image whose inputs did not change builds to the
 * same bytes and leaves its Deployment alone. So being too cautious here costs
 * a cache hit, and missing something is caught by the digest moving anyway.
 *
 * Go: a service is affected when a file changes in any package it imports —
 * `go list -deps` knows the graph exactly — or when the module or the
 * Dockerfile does. TypeScript: when a file changes in the app or in a
 * workspace package it depends on, or in what every install reads.
 *
 * Usage: node scripts/affected.mjs <base>
 *
 * Prints a JSON array of image names, and writes `images=<array>` to
 * $GITHUB_OUTPUT when that is set. A base that is missing, all zeros (a new
 * branch) or not an ancestor (a force push) means everything.
 */
import { execFileSync } from "node:child_process";
import { appendFileSync, readdirSync, readFileSync } from "node:fs";
import * as path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const backend = path.join(root, "backend");

const run = (command, args, cwd = root) => execFileSync(command, args, { cwd, encoding: "utf8" });

const goImages = {
  gateway: "services/gateway/cmd",
  ingest: "services/ingest/cmd",
  matcher: "services/matcher/cmd",
  trip: "services/trip/cmd",
  payments: "services/payments/cmd",
  simulator: "services/simulator/cmd",
  migrate: "tools/migrate",
};

const nodeImages = { auth: "@surge/auth", web: "@surge/web" };

/** Files every Go image is built from, whatever it imports. */
const goEverything = ["backend/go.mod", "backend/go.sum", "deploy/docker/go.Dockerfile"];

/** Files every install reads, so every TypeScript image depends on them. */
const nodeEverything = [
  "package.json",
  "pnpm-lock.yaml",
  "pnpm-workspace.yaml",
  "tsconfig.base.json",
  "patches/",
];

const changedSince = (base) => {
  if (base === undefined || base === "" || /^0+$/.test(base)) return undefined;
  try {
    return run("git", ["diff", "--name-only", `${base}...HEAD`]).split("\n").filter(Boolean);
  } catch {
    return undefined;
  }
};

/** Repository-relative directories of the module's own packages `pkg` imports. */
const goDependencies = (pkg) =>
  new Set(
    run("go", ["list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}", `./${pkg}`], backend)
      .split("\n")
      .filter((dir) => dir.startsWith(backend))
      .map((dir) => path.relative(root, dir)),
  );

/** Every workspace package, by name: its directory and its workspace dependencies. */
const workspace = () => {
  const packages = new Map();
  for (const group of ["apps", "packages"]) {
    for (const entry of readdirSync(path.join(root, group), { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      const dir = path.join(group, entry.name);
      let manifest;
      try {
        manifest = JSON.parse(readFileSync(path.join(root, dir, "package.json"), "utf8"));
      } catch {
        continue;
      }
      const all = { ...manifest.dependencies, ...manifest.devDependencies };
      const local = Object.entries(all)
        .filter(([, version]) => String(version).startsWith("workspace:"))
        .map(([name]) => name);
      packages.set(manifest.name, { dir, local });
    }
  }
  return packages;
};

/** The directories `name` is built from: its own and, transitively, its workspace dependencies'. */
const nodeDependencies = (packages, name, seen = new Set()) => {
  const found = packages.get(name);
  if (found === undefined || seen.has(name)) return seen;
  seen.add(name);
  for (const dependency of found.local) nodeDependencies(packages, dependency, seen);
  return seen;
};

const affected = (changed) => {
  if (changed === undefined) return [...Object.keys(goImages), ...Object.keys(nodeImages)];

  const images = [];

  const anyGo = changed.some((file) => goEverything.includes(file));
  for (const [image, pkg] of Object.entries(goImages)) {
    const dependencies = goDependencies(pkg);
    if (anyGo || changed.some((file) => dependencies.has(path.dirname(file)))) images.push(image);
  }

  const packages = workspace();
  const anyNode = changed.some((file) =>
    nodeEverything.some((shared) =>
      shared.endsWith("/") ? file.startsWith(shared) : file === shared
    )
  );
  for (const [image, name] of Object.entries(nodeImages)) {
    const dirs = [...nodeDependencies(packages, name)].map((dependency) =>
      packages.get(dependency).dir
    );
    if (anyNode || changed.some((file) => dirs.some((dir) => file.startsWith(`${dir}/`)))) {
      images.push(image);
    }
  }

  return images;
};

const images = JSON.stringify(affected(changedSince(process.argv[2])));
console.log(images);
if (process.env["GITHUB_OUTPUT"] !== undefined) {
  appendFileSync(process.env["GITHUB_OUTPUT"], `images=${images}\n`);
}
