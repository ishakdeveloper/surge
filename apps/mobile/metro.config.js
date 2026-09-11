const path = require("node:path");
const { getDefaultConfig } = require("expo/metro-config");
const { withNativeWind } = require("nativewind/metro");

const projectRoot = __dirname;
const workspaceRoot = path.resolve(projectRoot, "../..");
const src = path.join(projectRoot, "src");

const config = getDefaultConfig(projectRoot);

const SHARED = /^@surge\/(client|common|domain)\/(.+)$/;
const NODE_MODULES = `${path.sep}node_modules${path.sep}`;

const stripJs = (specifier) => specifier.endsWith(".js") ? specifier.slice(0, -3) : specifier;

/**
 * Resolution the way the compiler sees it.
 *
 * The workspace packages ship TypeScript source and import each other with the
 * `.js` extension TypeScript wants, which Metro does not map back to `.ts`. So
 * `@surge/client/*`, `@surge/common/*` and `@surge/domain/*` go straight to `src` — the same
 * mapping as `paths` in tsconfig.base.json and the aliases in vitest.shared.ts —
 * and a `.js` specifier inside our own source is resolved without its
 * extension, letting Metro find the `.ts` or `.tsx` beside it.
 *
 * Nothing under `node_modules` is touched: a published package that says `.js`
 * means a `.js` file.
 */
config.resolver.resolveRequest = (context, moduleName, platform) => {
  // One React: the app's own. pnpm gives NativeWind no React of its own, so
  // lookup from it climbs to the workspace's hoisted copy — the web app's,
  // a different version — and hooks called through its JSX runtime would run
  // against a React the renderer never set up.
  if (moduleName === "react" || moduleName.startsWith("react/")) {
    return context.resolveRequest(
      { ...context, originModulePath: path.join(projectRoot, "package.json") },
      moduleName,
      platform,
    );
  }

  const shared = SHARED.exec(moduleName);
  if (shared !== null) {
    return context.resolveRequest(
      context,
      path.join(workspaceRoot, "packages", shared[1], "src", stripJs(shared[2])),
      platform,
    );
  }

  if (moduleName.startsWith("@/")) {
    return context.resolveRequest(context, path.join(src, stripJs(moduleName.slice(2))), platform);
  }

  if (moduleName.startsWith(".") && !context.originModulePath.includes(NODE_MODULES)) {
    return context.resolveRequest(context, stripJs(moduleName), platform);
  }

  return context.resolveRequest(context, moduleName, platform);
};

module.exports = withNativeWind(config, { input: "./src/global.css" });
