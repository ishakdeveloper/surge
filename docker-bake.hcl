# Every image the system is made of, in one place.
#
#   docker buildx bake              all nine, tagged surge/<name>:dev
#   docker buildx bake gateway      one
#   TAG=ci docker buildx bake       what CI's end-to-end job builds
#
# The end-to-end job builds from here, reading the GitHub Actions cache the
# images job fills, so it tests the same artifacts rather than a second recipe.

variable "TAG" {
  default = "dev"
}

# `gha` in CI, to reuse the layers the images job cached per image.
variable "CACHE" {
  default = ""
}

function "cache_from" {
  params = [name]
  result = CACHE == "gha" ? ["type=gha,scope=${name}"] : []
}

group "default" {
  targets = ["service", "migrate", "auth", "web"]
}

target "go" {
  context    = "."
  dockerfile = "deploy/docker/go.Dockerfile"
}

target "service" {
  name       = svc
  matrix     = { svc = ["gateway", "ingest", "matcher", "trip", "payments", "simulator"] }
  inherits   = ["go"]
  args       = { PACKAGE = "services/${svc}/cmd" }
  tags       = ["surge/${svc}:${TAG}"]
  cache-from = cache_from(svc)
}

target "migrate" {
  inherits   = ["go"]
  args       = { PACKAGE = "tools/migrate" }
  tags       = ["surge/migrate:${TAG}"]
  cache-from = cache_from("migrate")
}

target "auth" {
  context    = "."
  dockerfile = "apps/auth/Dockerfile"
  tags       = ["surge/auth:${TAG}"]
  cache-from = cache_from("auth")
}

target "web" {
  context    = "."
  dockerfile = "apps/web/Dockerfile"
  tags       = ["surge/web:${TAG}"]
  cache-from = cache_from("web")
}
