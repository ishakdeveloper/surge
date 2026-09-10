# Tilt drives the Kubernetes loop.
#
# It is deliberately NOT the primary development loop. The fastest way to work
# on this system is `make dev-*`, which runs the Go services straight on the
# host against the compose stack — a change is a one-second compile and a
# restart, and a 16GB machine is not asked to run two copies of Redpanda.
#
# This exists because "it runs on my laptop as six processes" is not the same
# claim as "it deploys", and the difference is where a surprising number of
# problems live: a config value that was only ever an environment variable, a
# service that assumed localhost, an image that will not build because a
# dependency needs cgo.
#
#   tilt up
#
# Guard rail: this refuses to run against anything but a local cluster. The
# kubectl context on this machine has been a production GKE cluster, and
# `tilt up` against that would be a very bad afternoon.
allow_k8s_contexts([])
context = k8s_context()
local_clusters = ('orbstack', 'docker-desktop', 'rancher-desktop', 'minikube')
if context not in local_clusters and not context.startswith('kind-'):
    fail('refusing to run against %r — switch to a local cluster first' % context)

# One kustomize overlay rather than a hand-listed set of files, so `tilt up` and
# `kubectl apply -k deploy/k8s/development` deploy exactly the same thing.
# Anything else drifts, and the drift is discovered when the manifest nobody
# tilted turns out to be broken.
k8s_yaml(kustomize('deploy/k8s/development'))

# Infrastructure. Long startup budgets because Valhalla builds routing tiles
# from a 189MB extract on first boot.
k8s_resource('postgres', labels=['infra'], port_forwards='55433:5432')
k8s_resource('redis', labels=['infra'], port_forwards='56380:6379')
k8s_resource('redpanda', labels=['infra'], port_forwards='19092:9092')
k8s_resource('valhalla', labels=['infra'], port_forwards='8002:8002')
k8s_resource('jaeger', labels=['infra'], port_forwards=['16686:16686', '4318:4318'])

# Migrations as jobs, before anything that reads the database. Two owners, two
# jobs: better-auth's tables and the trip tables.
k8s_resource('migrate', labels=['infra'], resource_deps=['postgres'])
k8s_resource('migrate-auth', labels=['infra'], resource_deps=['postgres'])

# Authentication. Its own image because it is TypeScript, and its own rollout
# because nothing in a request path depends on it being up.
docker_build(
    'surge/auth',
    context='.',
    dockerfile='apps/auth/Dockerfile',
    only=['apps/auth/', 'packages/', 'package.json', 'pnpm-lock.yaml',
          'pnpm-workspace.yaml', 'tsconfig.base.json'],
)
k8s_resource('auth', labels=['services'], port_forwards='3200:3200',
             resource_deps=['migrate-auth'])

# The Go services.
#
# Each image is a full multi-stage build rather than the usual Tilt trick of
# compiling on the host and live-updating the binary in. That trick needs a
# host-compiled Linux binary, and h3-go is a cgo binding to the H3 C library —
# so a macOS host cannot produce one without a cross toolchain, and
# CGO_ENABLED=0 fails outright. The Dockerfiles mount the module and build
# caches instead, which keeps a rebuild to a few seconds.
services = [
    ('gateway',   ['redpanda', 'auth'],                          ['8100:8100', '9104:9104']),
    ('ingest',    ['redpanda'],                                  ['8102:8102', '9102:9102']),
    ('matcher',   ['redpanda'],                                  ['9103:9103']),
    ('trip',      ['redpanda', 'postgres', 'valhalla', 'migrate'], ['8110:8110', '9105:9105']),
    ('simulator', ['gateway', 'valhalla'],                       ['8101:8101', '9101:9101']),
]

for name, deps, ports in services:
    docker_build(
        'surge/%s' % name,
        context='.',
        dockerfile='deploy/docker/%s.Dockerfile' % name,
        # Only the Go tree and the Dockerfiles matter; without this, editing a
        # TypeScript file rebuilds every Go image.
        only=['backend/', 'deploy/docker/'],
    )
    k8s_resource(name, labels=['services'], port_forwards=ports, resource_deps=deps)
