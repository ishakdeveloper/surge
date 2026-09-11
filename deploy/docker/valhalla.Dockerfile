# surge-valhalla-ams: Valhalla with Noord-Holland's routing tiles already built.
#
# The stock image builds its tiles on first start — a 189MB extract, ten to
# twenty minutes — which is fine once on a laptop with a volume to keep them in,
# and hopeless for anything started fresh: every CI run, every new pod. Built
# here instead, a container serves the moment it starts, and the same image
# serves the end-to-end tests and, later, the cluster.
#
# Geofabrik publishes a new extract daily, so this is rebuilt weekly by
# .github/workflows/valhalla.yml rather than with every commit.
#
#   docker build -f deploy/docker/valhalla.Dockerfile -t surge/valhalla-ams deploy/docker
FROM ghcr.io/gis-ops/docker-valhalla/valhalla@sha256:060da5b92e6024a67f65135c236d918b021d63e73d1808a0d55b7e7cbd17240c

# What the compose file sets for the stock image, fixed here — except
# use_tiles_ignore_pbf, which is True so that at runtime the tiles below are
# served as they are, with no extract left to compare them against.
ENV tile_urls=https://download.geofabrik.de/europe/netherlands/noord-holland-latest.osm.pbf \
    use_tiles_ignore_pbf=True \
    build_admins=True \
    build_time_zones=False \
    build_elevation=False \
    force_rebuild=False \
    server_threads=4

# Build and stop: with serve_tiles False the run script exits once the tiles and
# their tarball exist. Then drop what serving does not read — the extract, and
# the loose tiles the tarball already holds — which halves the image.
RUN serve_tiles=False /valhalla/scripts/run.sh build_tiles \
  && sudo rm -rf /custom_files/*.pbf /custom_files/valhalla_tiles
