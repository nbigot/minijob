#!/bin/bash
PROGRAM_VERSION="v1.0.0"
go build -ldflags="-X 'main.Version=${PROGRAM_VERSION}'" cmd/minijob/minijob.go
