# Tilt drives the Kubernetes loop.
#
# It is deliberately NOT the primary development loop. The fastest way to work
# on this system is `make dev-*`, which runs the Go services straight on the
# host against the compose stack — a change is a one-second compile and a
# restart, and the 16GB machine is not asked to run two copies of Redpanda.
#
# This exists because "it runs on my laptop as five processes" is not the same
# claim as "it deploys", and the difference is where a surprising number of
# problems live: images that will not build, config that was only ever an
# environment variable, and services that assumed localhost.
#
#   tilt up
#
# Guard rail: this refuses to run against anything but a local cluster. The
# kubectl context on this machine has previously been a production GKE cluster,
# and `tilt up` against that would be a very bad afternoon.
allow_k8s_contexts([])
k8s_context = k8s_context()
if k8s_context not in ('orbstack', 'docker-desktop', 'rancher-desktop', 'minikube', 'kind-surge'):
    fail('refusing to run against %r — switch to a local cluster first' % k8s_context)

# Namespace, config and infrastructure first: everything else names them.
k8s_yaml([
    'deploy/k8s/00-namespace.yaml',
    'deploy/k8s/10-config.yaml',
    'deploy/k8s/20-infra.yaml',
])

# Secrets are not committed. Create them once:
#   kubectl -n surge create secret generic surge-secrets \
#     --from-literal=SIM_TOKEN_SECRET="$(openssl rand -base64 32)" \
#     --from-literal=SIM_ENABLED=true
if os.path.exists('deploy/k8s/11-secrets.yaml'):
    k8s_yaml('deploy/k8s/11-secrets.yaml')

# Infrastructure: grouped so the services list stays readable, and given long
# startup budgets because Valhalla builds tiles from a 189MB extract on first
# boot.
k8s_resource('postgres', labels=['infra'], port_forwards='55433:5432')
k8s_resource('redis', labels=['infra'], port_forwards='56380:6379')
k8s_resource('redpanda', labels=['infra'], port_forwards='19092:9092')
k8s_resource('valhalla', labels=['infra'], port_forwards='8002:8002')
k8s_resource('jaeger', labels=['infra'], port_forwards=['16686:16686', '4318:4318'])

# Authentication. Its own image because it is TypeScript, and its own rollout
# because nothing in a request path depends on it being up.
docker_build('surge/auth', context='.', dockerfile='apps/auth/Dockerfile',
             only=['apps/auth/', 'packages/', 'package.json', 'pnpm-lock.yaml',
                   'pnpm-workspace.yaml', 'tsconfig.base.json'])
k8s_yaml('deploy/k8s/25-auth.yaml')
k8s_resource('auth', labels=['services'], port_forwards='3200:3200', resource_deps=['migrate-auth'])

# Migrations as a job, run before anything that reads the database.
k8s_yaml(['deploy/k8s/21-migrate-job.yaml', 'deploy/k8s/26-migrate-auth-job.yaml'])
k8s_resource('migrate', labels=['infra'], resource_deps=['postgres'])
k8s_resource('migrate-auth', labels=['infra'], resource_deps=['postgres'])

# The services.
#
# Each image is a full multi-stage build rather than the usual Tilt trick of
# compiling on the host and live-updating the binary in. That trick needs a
# host-compiled Linux binary, and h3-go is a cgo binding to the H3 C library —
# so a macOS host cannot produce one without a cross toolchain, and
# CGO_ENABLED=0 fails outright. The Dockerfiles mount the module and build
# caches instead, which keeps an incremental rebuild to a few seconds.
services = {
    'gateway':   {'deps': ['redpanda', 'auth'], 'ports': ['8100:8100', '9104:9104']},
    'ingest':    {'deps': ['redpanda'], 'ports': ['8102:8102', '9102:9102']},
    'matcher':   {'deps': ['redpanda'], 'ports': ['9103:9103']},
    'trip':      {'deps': ['redpanda', 'postgres', 'valhalla', 'migrate'], 'ports': ['8110:8110', '9105:9105']},
    'simulator': {'deps': ['gateway', 'valhalla'], 'ports': ['8101:8101', '9101:9101']},
}

for name, spec in services.items():
    docker_build(
        'surge/%s' % name,
        context='.',
        dockerfile='deploy/docker/%s.Dockerfile' % name,
        # Only the Go tree and the Dockerfile matter; without this, editing a
        # TypeScript file rebuilds every Go image.
        only=['backend/', 'deploy/docker/'],
    )
    k8s_yaml('deploy/k8s/%s' % {
        'gateway': '30-gateway.yaml',
        'ingest': '31-ingest.yaml',
        'matcher': '32-matcher.yaml',
        'trip': '33-trip.yaml',
        'simulator': '40-simulator.yaml',
    }[name])
    k8s_resource(name, labels=['services'], port_forwards=spec['ports'], resource_deps=spec['deps'])
