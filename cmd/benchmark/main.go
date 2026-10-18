// Command benchmark is a load generator for the minijob HTTP API: it runs scenarios against a
// server, measures throughput, latency and the server's CPU and memory, checks that no job
// was lost, and compares several result files (see README.md).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

const toolVersion = "1.0.0"

const usage = `Usage:
  benchmark [options]             run the scenarios against a server
  benchmark report FILE.json...   median and spread of several result files (Markdown)

Options:
  -url string          server base URL (default "http://127.0.0.1:8080")
  -scenarios string    comma-separated list or "all" (default "all"):
                       ping, create, pull-batch, get, list, list-filter, cycle
  -concurrency int     connections, one request at a time each (default 32)
  -duration int        measured seconds per timed scenario (default 10)
  -warmup int          unmeasured seconds before each timed scenario (default 2)
  -preload int         jobs created before the read scenarios (default 10000)
  -batch-jobs int      jobs created for pull-batch (default 100000)
  -batch-size int      numjobs of each pull in pull-batch (default 100)
  -pid int             server process id, to read its CPU time and memory in /proc (Linux)
  -label string        name of this run in reports (default: the URL)
  -wait int            seconds to wait for the server to answer /ping (default 0)
  -json string         write the result to this file
`

type options struct {
	url         string
	scenarios   []string
	concurrency int
	duration    int
	warmup      int
	preload     int
	batchJobs   int
	batchSize   int
	pid         int
	label       string
	wait        int
	json        string
}

func parseArgs(args []string) (*options, error) {
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := &options{}
	var scenarios string
	fs.StringVar(&o.url, "url", "http://127.0.0.1:8080", "")
	fs.StringVar(&scenarios, "scenarios", "all", "")
	fs.IntVar(&o.concurrency, "concurrency", 32, "")
	fs.IntVar(&o.duration, "duration", 10, "")
	fs.IntVar(&o.warmup, "warmup", 2, "")
	fs.IntVar(&o.preload, "preload", 10000, "")
	fs.IntVar(&o.batchJobs, "batch-jobs", 100000, "")
	fs.IntVar(&o.batchSize, "batch-size", 100, "")
	fs.IntVar(&o.pid, "pid", 0, "")
	fs.StringVar(&o.label, "label", "", "")
	fs.IntVar(&o.wait, "wait", 0, "")
	fs.StringVar(&o.json, "json", "", "")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	for _, f := range []struct {
		name  string
		value int
	}{
		{"concurrency", o.concurrency}, {"duration", o.duration}, {"warmup", o.warmup},
		{"preload", o.preload}, {"batch-jobs", o.batchJobs}, {"batch-size", o.batchSize},
		{"pid", o.pid}, {"wait", o.wait},
	} {
		if f.value < 0 {
			return nil, fmt.Errorf("invalid value \"%d\" for flag -%s", f.value, f.name)
		}
	}
	o.concurrency = max(o.concurrency, 1)
	o.duration = max(o.duration, 1)
	o.preload = max(o.preload, 1)
	o.batchJobs = max(o.batchJobs, 1)
	o.batchSize = min(max(o.batchSize, 1), 100)
	o.url = strings.TrimRight(o.url, "/")
	if !strings.HasPrefix(o.url, "http://") {
		return nil, errors.New("only http:// URLs are supported")
	}
	if scenarios == "all" {
		o.scenarios = slices.Clone(scenarioNames)
	} else {
		for _, s := range strings.Split(scenarios, ",") {
			s = strings.TrimSpace(s)
			if !slices.Contains(scenarioNames, s) {
				return nil, fmt.Errorf("unknown scenario %q", s)
			}
			o.scenarios = append(o.scenarios, s)
		}
	}
	if o.label == "" {
		o.label = o.url
	}
	return o, nil
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "report" {
		text, err := report(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(text)
		return 0
	}
	o, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stderr, usage)
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	fmt.Printf("minijob-bench (go) %s -> %s, concurrency %d, %d s per scenario after %d s warm-up\n",
		toolVersion, o.url, o.concurrency, o.duration, o.warmup)
	result, err := runBenchmark(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if o.json != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err == nil {
			err = os.WriteFile(o.json, append(data, '\n'), 0o644)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot write %s: %v\n", o.json, err)
			return 1
		}
	}
	for _, s := range result.Scenarios {
		if s.Check != nil && !s.Check.OK {
			fmt.Fprintln(os.Stderr, "a consistency check failed")
			return 1
		}
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:]))
}
