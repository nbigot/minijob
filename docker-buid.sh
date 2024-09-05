#!/bin/bash
TAG_VERSION="1.0.0"
PROGRAM_VERSION="v${TAG_VERSION}"
docker build --build-arg PROGRAM_VERSION=${PROGRAM_VERSION} -t nbigot/minijob:${TAG_VERSION} .
