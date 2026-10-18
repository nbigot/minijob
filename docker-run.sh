#!/bin/bash
# Runs the image built by docker-buid.sh: the latest git tag without its "v", unless
# TAG_VERSION is set.
TAG_VERSION="${TAG_VERSION-$(git describe --tags --abbrev=0 | sed 's/^v//')}"
docker run --rm --name minijob -it -p 8080:8080 nbigot/minijob:${TAG_VERSION}
