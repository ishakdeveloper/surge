import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import react from "@vitejs/plugin-react";
import { nitro } from "nitro/vite";
import * as path from "node:path";
import { defineConfig, loadEnv } from "vite";

const repoRoot = path.resolve(import.meta.dirname, "../..");

export default defineConfig(({ command, mode }) => {
  /**
   * `loadEnv` reads the repo-root `.env`, the same file the API reads, with no
   * prefix filter — so the server-side values below are visible here whatever
   * they are called.
   *
   * `envDir` below points Vite's own loading at that same file, which is what
   * puts the `VITE_`-prefixed ones into the client bundle. Without it Vite
   * looks only inside this package, finds no `.env` at all, and a key the
   * browser needs is simply absent — which each reader treats as "not
   * configured" rather than as an error, so it fails by going quiet.
   */
  const env = loadEnv(mode, repoRoot, "");

  /**
   * This server's own address. The port comes from `WEB_URL` because the API
   * already needs that value for its trusted origins, and two variables that
   * must agree is one more than necessary.
   */
  const port = Number(
    new URL(process.env["WEB_URL"] ?? env["WEB_URL"] ?? "http://localhost:5273").port || 5273,
  );

  if (command === "serve") {
    /**
     * One variable the dev server needs in its own process, and only there.
     *
     * A deployment does not have this problem: `PORT` is whatever the platform
     * assigned that container. `pnpm dev` is one shell and one `.env` for two
     * servers, so it is sorted out here instead.
     *
     * Nitro's plugin takes `PORT` as the dev server's port, and that variable
     * belongs to the API — so left alone both halves of `pnpm dev` bind the same
     * one and whichever loses is invisible.
     */
    process.env["PORT"] = String(port);
  }

  return {
    plugins: [
      // Must come before react(). It owns the route tree generation that the
      // standalone router plugin used to do, so there is only one of them.
      tanstackStart(),
      /**
       * Turns the Start build into a deployable server.
       *
       * Without it `vite build` emits a fetch handler and stops, and hosting is
       * the application's problem. Nitro wraps that handler and writes
       * `.output/server/index.mjs`, which `node` runs directly.
       */
      nitro(),
      tailwindcss(),
      react(),
    ],
    /**
     * Bundle the server build's dependencies, but only when building.
     *
     * Vite externalises them by default, which is wrong for an image — it means
     * shipping the dependency tree to run one bundle — and right for the dev
     * server, which is why this is conditional rather than always on. Inlining
     * them in dev puts React's CommonJS entry through Vite's ESM module runner,
     * and every server-rendered route dies on `module is not defined`.
     */
    ssr: command === "build" ? { noExternal: true } : {},
    /**
     * Mapbox is served as the ES modules it ships, not pre-bundled.
     *
     * It asks for its worker as `new URL("worker.js", import.meta.url)`, which
     * Vite rewrites only while the module is served from its own directory.
     * Pre-bundling folds it into one file elsewhere, and the map then never
     * finishes loading a style and says nothing about it: no error, no tiles, a
     * blank rectangle with Mapbox's own attribution sitting in the corner. The
     * production build resolves the worker itself.
     *
     * This is the `mapbox-gl/esm` entry rather than the package's default one,
     * which is UMD — see the note where the map imports it.
     */
    optimizeDeps: { exclude: ["mapbox-gl/esm"] },
    /**
     * The repository root, so one `.env` serves every surface — and so the
     * `VITE_`-prefixed values in it, the map's token and Stripe's publishable
     * key, reach the browser.
     */
    envDir: repoRoot,
    resolve: {
      /**
       * Source, not build output. The workspace packages expose `src` under a
       * `development` condition; Vite compiles TypeScript, so it wants that in
       * both dev and build rather than requiring the packages be built first.
       */
      conditions: ["development"],
      alias: {
        // Mirrors the `@/*` mapping in tsconfig.app.json and vitest.config.ts.
        "@": path.resolve(import.meta.dirname, "./src"),
      },
    },
    server: {
      port,
      // Fail rather than quietly move if the port is taken; a web server on an
      // unexpected port looks exactly like the API being broken.
      strictPort: true,
    },
  };
});
