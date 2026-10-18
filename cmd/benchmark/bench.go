package main

// Scenarios. Each one starts from an empty server (`DELETE /api/v1/jobs/`), so results do
// not depend on the order or on what ran before.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var scenarioNames = []string{"ping", "create", "pull-batch", "get", "list", "list-filter", "cycle"}

// rng is xorshift64*: cheap, deterministic per worker.
type rng struct{ s uint64 }

func newRng(seed uint64) *rng {
	return &rng{seed*0x9E3779B97F4A7C15 | 1}
}

func (r *rng) below(n uint64) uint64 {
	r.s ^= r.s >> 12
	r.s ^= r.s << 25
	r.s ^= r.s >> 27
	return r.s * 0x2545F4914F6CDD1D % max(n, 1)
}

// conn is one HTTP/1.1 keep-alive connection, used by one worker.
type conn struct {
	base   string
	client *http.Client
}

func newConn(base string) *conn {
	return &conn{base: base, client: &http.Client{Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		MaxConnsPerHost:     1,
		DisableCompression:  true,
	}}}
}

func (c *conn) close() {
	c.client.CloseIdleConnections()
}

// send sends one request and returns the status and the body; status 0 when the request
// failed.
func (c *conn) send(method, path, body string) (int, []byte) {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return 0, nil
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return 0, nil
	}
	return resp.StatusCode, b
}

type op int

const (
	opPing op = iota
	opCreate
	opGet
	opList
	opListFilter
	opCycle
)

// outcome of one item (one request, or one create/pull/succeed cycle).
type outcome struct {
	status   int // last HTTP status, 0 when the connection failed
	ok       bool
	requests uint64
}

type workerOut struct {
	samples  samples
	requests uint64
	errors   uint64
	items    uint64
	statuses map[string]uint64
	okTotal  uint64 // successful items including the warm-up (for the consistency checks)
}

func newWorkerOut() *workerOut {
	return &workerOut{statuses: map[string]uint64{}}
}

func statusKey(s int) string {
	if s == 0 {
		return "error"
	}
	return strconv.Itoa(s)
}

func jobID(body []byte) (string, bool) {
	var v struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if json.Unmarshal(body, &v) != nil || v.Job.ID == "" {
		return "", false
	}
	return v.Job.ID, true
}

// pulledIDs returns the ids of the jobs of a pull answer.
func pulledIDs(body []byte) []string {
	var v struct {
		Jobs []struct {
			ID string `json:"id"`
		} `json:"jobs"`
	}
	_ = json.Unmarshal(body, &v)
	ids := make([]string, 0, len(v.Jobs))
	for _, j := range v.Jobs {
		if j.ID != "" {
			ids = append(ids, j.ID)
		}
	}
	return ids
}

func runOp(o op, opts *options, c *conn, worker int, seq uint64, r *rng, ids []string) outcome {
	one := func(status int) outcome { return outcome{status: status, ok: status == 200, requests: 1} }
	switch o {
	case opPing:
		s, _ := c.send("GET", "/ping", "")
		return one(s)
	case opCreate:
		body := fmt.Sprintf(`{"topic":"bench","priority":%d,"properties":{"worker":%d,"seq":%d,"data":"0123456789abcdef0123456789abcdef"}}`,
			seq%10, worker, seq)
		s, _ := c.send("POST", "/api/v1/job/", body)
		return one(s)
	case opGet:
		s, _ := c.send("GET", "/api/v1/job/"+ids[r.below(uint64(len(ids)))], "")
		return one(s)
	case opList:
		pages := uint64(max(opts.preload/100, 1))
		s, _ := c.send("GET", fmt.Sprintf("/api/v1/jobs/?page=%d&limit=100", 1+r.below(pages)), "")
		return one(s)
	case opListFilter:
		s, _ := c.send("GET", fmt.Sprintf("/api/v1/jobs/?status=queued&topic=t%d&sort=priority_desc&limit=20", r.below(10)), "")
		return one(s)
	default: // opCycle
		topic := fmt.Sprintf("cycle-%d", worker)
		s, _ := c.send("POST", "/api/v1/job/", fmt.Sprintf(`{"topic":"%s","properties":{"seq":%d}}`, topic, seq))
		requests := uint64(1)
		if s != 200 {
			return outcome{status: s, requests: requests}
		}
		// The job is queued by the server's scheduler; pull until it is there.
		pull := "/api/v1/job/pull?topic=" + topic + "&visibilitytimeout=60"
		id := ""
		for range 1000 {
			s, b := c.send("POST", pull, "")
			requests++
			if s == 404 {
				continue
			}
			if s != 200 {
				return outcome{status: s, requests: requests}
			}
			if ids := pulledIDs(b); len(ids) > 0 {
				id = ids[0]
			}
			break
		}
		if id == "" {
			return outcome{status: 404, requests: requests}
		}
		s, _ = c.send("POST", "/api/v1/job/"+id+"/succeed", "")
		return outcome{status: s, ok: s == 200, requests: requests + 1}
	}
}

func cpuPerRequest(p0, p1 *procStat, requests uint64) *float64 {
	if p0 == nil || p1 == nil || requests == 0 || p1.cpuTicks < p0.cpuTicks {
		return nil
	}
	us := float64(p1.cpuTicks-p0.cpuTicks) * 1e6 / ticksPerS
	v := round3(us / float64(requests))
	return &v
}

func finish(name string, all *workerOut, elapsed float64, p0, p1 *procStat, c0, c1 *uint64) scenarioResult {
	r := scenarioResult{
		Name:                  name,
		Requests:              all.requests,
		Errors:                all.errors,
		ElapsedS:              round3(elapsed),
		RequestsPerS:          round3(float64(all.requests) / elapsed),
		Items:                 all.items,
		ItemsPerS:             round3(float64(all.items) / elapsed),
		LatencyMS:             all.samples.summary(),
		Statuses:              all.statuses,
		ServerCPUUsPerRequest: cpuPerRequest(p0, p1, all.requests),
		ClientCPUPercent:      cpuPercent(c0, c1, elapsed),
	}
	if p1 != nil {
		r.ServerRSSKB = &p1.rssKB
	}
	return r
}

func merge(outs []*workerOut) *workerOut {
	all := newWorkerOut()
	for _, w := range outs {
		all.samples.merge(w.samples)
		all.requests += w.requests
		all.errors += w.errors
		all.items += w.items
		all.okTotal += w.okTotal
		for k, v := range w.statuses {
			all.statuses[k] += v
		}
	}
	return all
}

// timed runs a closed loop: `concurrency` workers, warm-up then a measured window of
// `duration`. It also returns the number of successful items, warm-up included.
func timed(opts *options, name string, o op, ids []string) (scenarioResult, uint64) {
	start := time.Now()
	recStart := start.Add(time.Duration(opts.warmup) * time.Second)
	end := recStart.Add(time.Duration(opts.duration) * time.Second)
	outs := make([]*workerOut, opts.concurrency)
	var wg sync.WaitGroup
	for w := range opts.concurrency {
		wg.Go(func() {
			c := newConn(opts.url)
			defer c.close()
			r := newRng(uint64(w) + 1)
			out := newWorkerOut()
			for seq := uint64(0); ; seq++ {
				t := time.Now()
				if !t.Before(end) {
					break
				}
				res := runOp(o, opts, c, w, seq, r, ids)
				if res.ok {
					out.okTotal++
				}
				if !t.Before(recStart) {
					out.samples.push(time.Since(t))
					out.requests += res.requests
					if res.ok {
						out.items++
					} else {
						out.errors++
					}
					out.statuses[statusKey(res.status)]++
				}
			}
			outs[w] = out
		})
	}
	time.Sleep(time.Until(recStart))
	p0, c0 := readProcStat(opts.pid), selfCPUTicks()
	time.Sleep(time.Until(end))
	p1, c1 := readProcStat(opts.pid), selfCPUTicks()
	wg.Wait()
	all := merge(outs)
	return finish(name, all, float64(opts.duration), p0, p1, c0, c1), all.okTotal
}

// total returns the number of jobs matching a `GET /api/v1/jobs/` query.
func total(c *conn, query string) (uint64, bool) {
	s, b := c.send("GET", "/api/v1/jobs/?"+query+"&limit=1", "")
	if s != 200 {
		return 0, false
	}
	var v struct {
		Total *uint64 `json:"total"`
	}
	if json.Unmarshal(b, &v) != nil || v.Total == nil {
		return 0, false
	}
	return *v.Total, true
}

func totalText(n uint64, ok bool) string {
	if !ok {
		return "unknown"
	}
	return strconv.FormatUint(n, 10)
}

func waitTotal(c *conn, query string, expected uint64) bool {
	deadline := time.Now().Add(60 * time.Second)
	for {
		if n, ok := total(c, query); ok && n == expected {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func reset(opts *options) error {
	c := newConn(opts.url)
	defer c.close()
	if s, _ := c.send("DELETE", "/api/v1/jobs/", ""); s != 200 {
		return fmt.Errorf("DELETE /api/v1/jobs/ answered %d", s)
	}
	if !waitTotal(c, "", 0) {
		return errors.New("jobs remain after DELETE /api/v1/jobs/")
	}
	return nil
}

// preload creates n jobs (not measured) and returns their ids, in creation order per worker.
func preload(opts *options, n int, topic func(int) string) ([]string, error) {
	ids := make([][]string, opts.concurrency)
	errs := make([]error, opts.concurrency)
	var wg sync.WaitGroup
	for w := range opts.concurrency {
		wg.Go(func() {
			c := newConn(opts.url)
			defer c.close()
			for i := w; i < n; i += opts.concurrency {
				body := fmt.Sprintf(`{"topic":"%s","priority":%d,"properties":{"seq":%d}}`, topic(i), i%10, i)
				s, b := c.send("POST", "/api/v1/job/", body)
				id, ok := jobID(b)
				if s != 200 || !ok {
					errs[w] = fmt.Errorf("preload: create answered %d", s)
					return
				}
				ids[w] = append(ids[w], id)
			}
		})
	}
	wg.Wait()
	all := make([]string, 0, n)
	for w := range ids {
		if errs[w] != nil {
			return nil, errs[w]
		}
		all = append(all, ids[w]...)
	}
	return all, nil
}

func pullBatch(opts *options) (scenarioResult, error) {
	ids, err := preload(opts, opts.batchJobs, func(int) string { return "batch" })
	if err != nil {
		return scenarioResult{}, err
	}
	c := newConn(opts.url)
	defer c.close()
	if !waitTotal(c, "status=queued&topic=batch", uint64(len(ids))) {
		return scenarioResult{}, errors.New("pull-batch: the preloaded jobs were not all queued after 60 s")
	}
	p0, c0 := readProcStat(opts.pid), selfCPUTicks()
	start := time.Now()
	outs := make([]*workerOut, opts.concurrency)
	pulled := make([][]string, opts.concurrency)
	var wg sync.WaitGroup
	for w := range opts.concurrency {
		wg.Go(func() {
			c := newConn(opts.url)
			defer c.close()
			out := newWorkerOut()
			path := fmt.Sprintf("/api/v1/job/pull?topic=batch&numjobs=%d&visibilitytimeout=300", opts.batchSize)
			failures := 0
		loop:
			for {
				t := time.Now()
				s, b := c.send("POST", path, "")
				out.samples.push(time.Since(t))
				out.requests++
				out.statuses[statusKey(s)]++
				switch s {
				case 200:
					got := pulledIDs(b)
					pulled[w] = append(pulled[w], got...)
					out.items += uint64(len(got))
				case 404: // queue empty: this worker is done
					break loop
				default:
					out.errors++
					failures++
					if failures >= 10 {
						break loop
					}
				}
			}
			outs[w] = out
		})
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	p1, c1 := readProcStat(opts.pid), selfCPUTicks()
	r := finish("pull-batch", merge(outs), elapsed, p0, p1, c0, c1)
	// Every preloaded job must be pulled exactly once.
	expected := make(map[string]bool, len(ids))
	for _, id := range ids {
		expected[id] = true
	}
	seen := make(map[string]bool, len(ids))
	count, duplicates, unknown := 0, 0, 0
	for _, p := range pulled {
		for _, id := range p {
			count++
			switch {
			case seen[id]:
				duplicates++
			case !expected[id]:
				seen[id] = true
				unknown++
			default:
				seen[id] = true
			}
		}
	}
	missing := 0
	for id := range expected {
		if !seen[id] {
			missing++
		}
	}
	r.Check = &check{
		OK: duplicates == 0 && missing == 0 && unknown == 0,
		Detail: fmt.Sprintf("%d jobs created, %d pulled, %d duplicates, %d missing, %d unknown",
			len(ids), count, duplicates, missing, unknown),
	}
	return r, nil
}

func serverVersion(opts *options) string {
	c := newConn(opts.url)
	defer c.close()
	_, b := c.send("GET", "/api/v1/system/info", "")
	var v struct {
		SystemInfo struct {
			Version string `json:"version"`
		} `json:"systemInfo"`
	}
	_ = json.Unmarshal(b, &v)
	return v.SystemInfo.Version
}

func printLine(r scenarioResult) {
	cpu := "       - µs/req"
	if r.ServerCPUUsPerRequest != nil {
		cpu = fmt.Sprintf("%8.1f µs/req", *r.ServerCPUUsPerRequest)
	}
	chk := ""
	if r.Check != nil {
		state := "OK"
		if !r.Check.OK {
			state = "FAILED"
		}
		chk = "  check " + state + ": " + r.Check.Detail
	}
	fmt.Printf("%-12s %10.1f req/s %10.1f items/s  p50 %8.3f ms  p99 %8.3f ms  cpu %s  errors %d%s\n",
		r.Name, r.RequestsPerS, r.ItemsPerS, r.LatencyMS.P50, r.LatencyMS.P99, cpu, r.Errors, chk)
}

func runBenchmark(opts *options) (*runResult, error) {
	c := newConn(opts.url)
	defer c.close()
	deadline := time.Now().Add(time.Duration(opts.wait) * time.Second)
	s, _ := c.send("GET", "/ping", "")
	for s != 200 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		s, _ = c.send("GET", "/ping", "")
	}
	if s != 200 {
		return nil, fmt.Errorf("%s/ping answered %s", opts.url, statusKey(s))
	}
	version := serverVersion(opts)
	if err := reset(opts); err != nil {
		return nil, err
	}
	result := &runResult{
		Tool:           "minijob-bench",
		Implementation: "go",
		ToolVersion:    toolVersion,
		Label:          opts.label,
		URL:            opts.url,
		Started:        time.Now().UTC().Format(time.RFC3339),
		Concurrency:    opts.concurrency,
		DurationS:      opts.duration,
		WarmupS:        opts.warmup,
		Server:         serverInfo{Version: version, PID: opts.pid},
		Scenarios:      []scenarioResult{},
	}
	if idle := readProcStat(opts.pid); idle != nil {
		result.Server.IdleRSSKB = &idle.rssKB
	}
	var readIDs []string // jobs preloaded for the read scenarios, nil when there are none
	for _, name := range opts.scenarios {
		reads := name == "get" || name == "list" || name == "list-filter"
		if !reads && readIDs != nil {
			readIDs = nil
			if err := reset(opts); err != nil {
				return nil, err
			}
		}
		var r scenarioResult
		switch name {
		case "ping":
			r, _ = timed(opts, name, opPing, nil)
		case "create":
			var created uint64
			r, created = timed(opts, name, opCreate, nil)
			stored, ok := total(c, "")
			r.Check = &check{
				OK:     ok && stored == created,
				Detail: fmt.Sprintf("%d jobs created, %s stored", created, totalText(stored, ok)),
			}
			if err := reset(opts); err != nil {
				return nil, err
			}
		case "pull-batch":
			var err error
			if r, err = pullBatch(opts); err != nil {
				return nil, err
			}
			if err := reset(opts); err != nil {
				return nil, err
			}
		case "get", "list", "list-filter":
			if readIDs == nil {
				ids, err := preload(opts, opts.preload, func(i int) string { return "t" + strconv.Itoa(i%10) })
				if err != nil {
					return nil, err
				}
				if !waitTotal(c, "status=queued", uint64(len(ids))) {
					return nil, errors.New("read preload: jobs not all queued after 60 s")
				}
				readIDs = ids
			}
			o := map[string]op{"get": opGet, "list": opList, "list-filter": opListFilter}[name]
			r, _ = timed(opts, name, o, readIDs)
		case "cycle":
			var cycles uint64
			r, cycles = timed(opts, name, opCycle, nil)
			done, ok := total(c, "status=succeeded")
			r.Check = &check{
				OK:     ok && done == cycles,
				Detail: fmt.Sprintf("%d cycles completed, %s jobs succeeded", cycles, totalText(done, ok)),
			}
			if err := reset(opts); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown scenario %s", name)
		}
		printLine(r)
		result.Scenarios = append(result.Scenarios, r)
	}
	if readIDs != nil {
		if err := reset(opts); err != nil {
			return nil, err
		}
	}
	if p := readProcStat(opts.pid); p != nil {
		result.Server.PeakRSSKB = &p.hwmKB
	}
	return result, nil
}
