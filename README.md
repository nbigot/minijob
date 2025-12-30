# Readme


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

## Run minijob

```sh
$ go build -v cmd/minijob/minijob.go
$ ./minijob -config config-templates/standard/config/config.yaml
```

Or use a specific configuration if you want to run other setups:

```sh
$ go run cmd/minijob/minijob.go -config config-templates/standard/config/config.yaml
$ go run cmd/minijob/minijob.go -config config-templates/standard/config/config.loadtest.yaml
```

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
