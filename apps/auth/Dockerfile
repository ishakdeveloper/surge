# The Effect API.
#
# Built from the repository root, not from this directory:
#
#   docker build -f apps/auth/Dockerfile -t surge-auth .
#
# That is not a quirk of this file. pnpm resolves a workspace package against
# the root lockfile and every sibling manifest, so the build context has to be
# the whole repo whatever the Dockerfile's own location. Living next to the app
# it builds is for the reader; the context is for pnpm.
#
# This image also carries the migration runner. Migrations are applied by a
# script and never at boot — two instances starting together would both migrate —
# so it is a separate command rather than an entrypoint:
#
#   docker run --rm -e DATABASE_URL=... surge-auth \
#     node packages/database/build/bundle/migrate.js

# Debian rather than Alpine: `prepare` runs `effect-tsgo patch`, whose packaged
# Oxlint integration has no musl build and fails the install outright.
FROM node:22-slim AS base
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
RUN corepack enable
WORKDIR /app

# Manifests first, so a source-only change does not reinstall the world.
FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/auth/package.json apps/auth/
COPY apps/web/package.json apps/web/
COPY packages/database/package.json packages/database/
COPY packages/domain/package.json packages/domain/
COPY scripts/package.json scripts/
# pnpm applies `patchedDependencies` during install, so the patches have to be
# here before it runs, not with the source after it.
COPY patches/ patches/
# `prepare` patches TypeScript and oxlint with the Effect language service, which
# the build needs and which pnpm runs as part of install.
RUN pnpm install --frozen-lockfile

FROM deps AS build
COPY . .
RUN pnpm --filter @surge/domain --filter @surge/database --filter @surge/auth build

# Alpine, not the Debian base the build used. `effect-tsgo` has no musl build so
# the install cannot run here — but nothing installs at runtime, it only runs
# node, and Alpine's rootfs is 86MB smaller.
FROM node:22-alpine AS runtime

# This stage only ever runs `node` on a bundle. The image's own npm, yarn and
# corepack are never used, and they carry most of its known CVEs, so they go;
# the OS packages take whatever fixes Alpine has published since the base was
# built. Once a rebuilt base includes them, the upgrade does nothing.
RUN apk upgrade --no-cache \
  && rm -rf /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/corepack /opt/yarn-* \
    /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack /usr/local/bin/yarn /usr/local/bin/yarnpkg

WORKDIR /app
ENV NODE_ENV=production
# Platforms assign a port; the server reads PORT and falls back to this.
ENV PORT=3000

# Two bundles and the .sql files, and nothing else. No node_modules: both entry
# points carry the code they actually reach, which is ~9MB out of a 250MB
# dependency tree — the rest being other entry points, type declarations, source
# maps, and CJS copies of everything.
COPY --from=build /app/apps/auth/build/bundle ./apps/auth/build/bundle
COPY --from=build /app/packages/database/build/bundle ./packages/database/build/bundle

EXPOSE 3000
# /health is liveness and /ready is readiness; a container check wants the former.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD node -e "fetch('http://localhost:'+(process.env.PORT??3000)+'/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "apps/auth/build/bundle/main.js"]
