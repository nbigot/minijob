#!/bin/bash
# Image tag: the latest git tag without its "v", unless TAG_VERSION is set.
TAG_VERSION="${TAG_VERSION-$(git describe --tags --abbrev=0 | sed 's/^v//')}"
PROGRAM_VERSION="v${TAG_VERSION}"
docker build --build-arg PROGRAM_VERSION=${PROGRAM_VERSION} -t nbigot/minijob:${TAG_VERSION} .
