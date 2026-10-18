# benchmark

Load generator for the minijob HTTP API, and a script that benchmarks server images in
Docker containers. It measures throughput, latency, and the server's CPU time and memory,
and checks that no job was lost or delivered twice.

## Scenarios

Each scenario starts from an empty server (`DELETE /api/v1/jobs/`).

| Scenario | What one item is | Notes |
|---|---|---|
| `ping` | `GET /ping` | trivial request |
| `create` | `POST /api/v1/job/` (about 130 bytes) | small writes; checks that every created job is stored |
| `pull-batch` | `POST /api/v1/job/pull?numjobs=100` | batch writes: `-batch-jobs` jobs are created first (not measured), then pulled until the queue is empty; checks that every job is pulled exactly once |
| `get` | `GET /api/v1/job/{id}` | reads, on `-preload` jobs |
| `list` | `GET /api/v1/jobs/?page=N&limit=100` | reads (random page) |
| `list-filter` | `GET /api/v1/jobs/?status=queued&topic=tN&sort=priority_desc&limit=20` | reads with filters |
| `cycle` | create, pull (until the job is queued), succeed | the life of a job; latency is per cycle; checks that every cycle's job ends succeeded |

Timed scenarios run `-concurrency` connections in a closed loop (one request at a time per
connection) for `-warmup` seconds, then are measured for `-duration` seconds.

## Running the tool

Start a server (for example with `config-templates/standard/config/config.loadtest.yaml`),
then:

```sh
go run ./cmd/benchmark -url http://127.0.0.1:8080 -json result.json
go run ./cmd/benchmark report run1.json run2.json run3.json
```

Options: `-url`, `-scenarios` (comma-separated or `all`), `-concurrency` (32), `-duration`
(10), `-warmup` (2), `-preload` (10000), `-batch-jobs` (100000), `-batch-size` (100),
`-pid` (server process id, Linux: CPU time per request and memory from `/proc`), `-label`,
`-wait` (seconds to wait for `/ping`), `-json`. The exit status is 1 when a consistency
check fails.

Result file: one object per run with, for each scenario, requests, errors, requests/s,
items/s, latency (mean, p50, p90, p99, max in ms, nearest-rank percentiles over every
sample), HTTP statuses, server CPU µs per request, server RSS, load generator CPU and the
consistency check. `report` prints the median and the min–max of several result files as
Markdown tables.

## Benchmark in Docker

```sh
cmd/benchmark/run.sh                                         # 3 runs, every storage
RUNS=1 DURATION=5 STORAGES=inmemory cmd/benchmark/run.sh     # quick check
IMAGES="before=minijob:before after=minijob:after" cmd/benchmark/run.sh   # compare images
```

- By default the image of the current checkout is built (`Dockerfile`) and labeled
  `current`; `IMAGES` benchmarks existing images instead (any image with the server at
  `/app/minijob`).
- Storages: `inmemory` (load test template), `file` (same, with the jobs file in the
  container, written every 30 s), `redis` (Redis 7.4 without persistence).
- The server is pinned to CPUs 0–1 and the load generator to CPUs 2–3 (`SERVER_CPUS`,
  `CLIENT_CPUS`); every server uses `config-templates/standard/config/config.loadtest.yaml`
  with only the storage settings changed. The order of the images rotates between runs.
- Results go to `cmd/benchmark/results/<date>/` (not versioned): one JSON file per run,
  `report.md` (median and min–max over runs) and `environment.txt` (machine, Docker, image
  ids).

## Measurement limits

- One machine: the load generator, Redis and the server share the same memory bus and
  caches, and under Docker Desktop they run in a virtual machine.
- Redis shares the load generator's CPUs.
- When the load generator's CPU is close to 200% (2 CPUs), it is the limit, not the server.
- CPU time comes from `/proc/<pid>/stat` with a 10 ms resolution, averaged over the window.
- `pull-batch` is not timed: it ends when the queue is empty, so a fast server is measured
  over a shorter time (less than a second for 100,000 jobs) and its figures vary more.
- Closed loop: the latencies are those of a server under full load (no coordinated
  omission correction); they are not the latencies of a lightly loaded server.
