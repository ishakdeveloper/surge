# The TanStack Start web server.
#
# Built from the repository root, not from this directory:
#
#   docker build -f apps/web/Dockerfile -t surge-web .
#
# That is not a quirk of this file. pnpm resolves a workspace package against
# the root lockfile and every sibling manifest, so the build context has to be
# the whole repo whatever the Dockerfile's own location.
#
# No build arguments. Where the browser finds the gateway and auth is read from
# the environment when a page is served (src/lib/public-config.ts), so one image
# serves every environment and the image that was tested is the one that ships.
#
# This serves SSR and its own static assets. Behind a CDN the asset requests
# should never reach it, but a boilerplate that only works behind one is not
# much of a boilerplate.
#
# It proxies nothing: the browser talks to the API directly, so the two need a
# shared parent domain for the session cookie. See AGENTS.md.

# Debian rather than Alpine: `prepare` runs `effect-tsgo patch`, whose packaged
# Oxlint integration has no musl build and fails the install outright.
FROM node:22-slim AS base
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
RUN corepack enable
WORKDIR /app

FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/auth/package.json apps/auth/
COPY apps/web/package.json apps/web/
COPY packages/client/package.json packages/client/
COPY packages/common/package.json packages/common/
COPY packages/database/package.json packages/database/
COPY packages/domain/package.json packages/domain/
COPY scripts/package.json scripts/
# pnpm applies `patchedDependencies` during install, so the patches have to be
# here before it runs, not with the source after it.
COPY patches/ patches/
RUN pnpm install --frozen-lockfile

FROM deps AS build
COPY . .
RUN pnpm --filter @surge/web build

# Alpine for the same reason as the API image: nothing installs here, and the
# rootfs is 86MB smaller than the Debian base the build needed.
FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=5273

# Nitro turns the Start fetch handler into a server, so there is nothing to host
# it with by hand: `.output` is self-contained, node_modules included.
COPY --from=build /app/apps/web/.output ./apps/web/.output

WORKDIR /app/apps/web
EXPOSE 5273
CMD ["node", ".output/server/index.mjs"]
