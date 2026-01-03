#!/bin/bash
#PROGRAM_VERSION="v1.0.0"
PROGRAM_VERSION="${TAG_VERSION-$(git describe --tags --abbrev=0)}"
go build -ldflags="-X 'main.Version=${PROGRAM_VERSION}'" cmd/minijob/minijob.go
