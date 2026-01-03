# Minijob

[![Go Report Card](https://goreportcard.com/badge/github.com/nbigot/minijob)](https://goreportcard.com/report/github.com/nbigot/minijob)
[![license](https://img.shields.io/github/license/nbigot/minijob)](https://github.com/nbigot/minijob/blob/main/LICENSE)


<p align="center">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/content/assets/img/minijob.logo-dark.small.png">
      <source media="(prefers-color-scheme: light)" srcset="docs/content/assets/img/minijob.logo.small.png">
      <img alt="Minijob" title="Minijob" src="docs/content/assets/img/minijob.logo.png">
    </picture>
</p>

## Overview

Minijob is an open-source job queue and task scheduling service.
Minijob is well suited for distributed task processing and background job management.

Minijob is simple and straightforward, it runs on a single server and has minimal dependencies.
Jobs are submitted and retrieved through a simple HTTP API.
It is served by its own HTTP(s) server and stores data efficiently.
Minijob can easily fit in a standalone docker container.
Minijob also provides a complete web API to manage the server.


## Quick install

### Install by compiling source code

#### Download source code

```sh
$ git clone https://github.com/nbigot/minijob.git
```


#### Compile

```sh
$ cd minijob
$ go build -v cmd/minijob/minijob.go
```


Or to set the version variable at build time:

```sh
$ MINIJOB_VERSION="${TAG_VERSION-$(git describe --tags --abbrev=0)}"
$ go build -ldflags="-X 'main.Version=${MINIJOB_VERSION}'" cmd/minijob/minijob.go
```

#### Linter

To run the linter, use:

```sh
$ golangci-lint run
```

#### Configure

Edit the file *config-templates/standard/config/config.yaml*

Pay attention to the directory paths in the config file.


#### Run minijob

```sh
$ ./minijob -config config-templates/standard/config/config.yaml
```


## Minijob quick tips

Let's assume Minijob is running and listening on the tcp port 8080.

Run simple commands:

```sh
$ curl http://localhost:8080
Welcome to minijob!

$ curl http://localhost:8080/livez
ok

$ curl http://localhost:8080/readyz
ok
```


## Run minijob

Compile & run

```sh
$ go build -v cmd/minijob/minijob.go
$ ./minijob -config config-templates/standard/config/config.yaml
```

Or use an in-memory configuration for quick testing:

```sh
$ go run cmd/minijob/minijob.go -config config-templates/standard/config/config.inmemory.yaml
```

Or use a specific configuration if you want to run the loadtest:

```sh
$ go run cmd/minijob/minijob.go -config config-templates/standard/config/config.loadtest.yaml
```

## Run load test

Run:

```sh
$ go build cmd/loadtest/loadtest.go
$ loadtest.exe -url=http://127.0.0.1:8080 -wait=0 -concurrency=2 -iterations=10000
```

Or use multiple arguments like:

```sh
$ go run cmd/loadtest/loadtest.go -url=http://127.0.0.1:8080 -wait=5 -iterations=10
$ go run cmd/loadtest/loadtest.go -url=http://127.0.0.1:8080 -wait=0 -concurrency=4 -iterations=10000
$ go run cmd/loadtest/loadtest.go -url=http://127.0.0.1:8080 -wait=0 -concurrency=4 -iterations=10000 -http1.1 -deletaAllJobsBeforeStart -monkey=0
$ go run cmd/loadtest/loadtest.go -url=http://127.0.0.1:8080 -wait=0 -concurrency=4 -iterations=10000 -http1.1 -deletaAllJobsBeforeStart -monkey=6
```

The `iterations` and `monkey` parameters significantly affect both test duration and performance metrics. Higher iteration counts increase overall test duration while providing more comprehensive performance data. The `monkey` parameter (ranging from 0-4) controls the level of randomized chaos testing, with higher values introducing more aggressive fault simulation that can substantially impact measured performance.


## Profiling

[profiler](http://127.0.0.1:8080/debug/pprof/)

Sample commands:

```sh
$ go tool pprof http://localhost:8080/debug/pprof/heap

$ go tool pprof -http=localhost:8081 http://localhost:8080/debug/pprof/

$ curl -s http://127.0.0.1:8080/debug/pprof/heap > ./heap.out
$ curl -s http://127.0.0.1:8080/debug/pprof/goroutine > ./goroutine.out
$ curl -s http://127.0.0.1:8080/debug/pprof/allocs > ./allocs.out
$ go tool pprof -http=:8081 ./heap.out
$ go tool pprof -http=:8081 ./goroutine.out
$ go tool pprof -http=:8081 ./allocs.out
```


## Contribution guidelines

If you want to contribute to Minijob, be sure to review the [code of conduct](CODE_OF_CONDUCT.md).


## License

This software is licensed under the [MIT](./LICENSE).
