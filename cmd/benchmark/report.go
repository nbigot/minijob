package main

// `benchmark report <files...>`: median and spread (min–max) over runs, grouped by label
// then scenario, as Markdown tables.

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

func median(v []float64) float64 {
	slices.Sort(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// cell formats `median (min–max)`, or the median alone for a single run.
func cell(v []float64, decimals int) string {
	if len(v) == 0 {
		return "-"
	}
	f := func(x float64) string { return strconv.FormatFloat(x, 'f', decimals, 64) }
	m := median(v)
	lo, hi := v[0], v[len(v)-1]
	if len(v) == 1 || lo == hi {
		return f(m)
	}
	return f(m) + " (" + f(lo) + "–" + f(hi) + ")"
}

func report(files []string) (string, error) {
	runs := make([]runResult, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		var r runResult
		if err := json.Unmarshal(data, &r); err != nil {
			return "", fmt.Errorf("%s: %w", f, err)
		}
		runs = append(runs, r)
	}
	// Labels and scenarios in order of first appearance.
	var labels, scenarios []string
	for _, r := range runs {
		if !slices.Contains(labels, r.Label) {
			labels = append(labels, r.Label)
		}
		for _, s := range r.Scenarios {
			if !slices.Contains(scenarios, s.Name) {
				scenarios = append(scenarios, s.Name)
			}
		}
	}
	var out strings.Builder
	out.WriteString("Median over runs, (min–max) when they differ. Latency in ms, server CPU in µs per request.\n\n")
	for _, sc := range scenarios {
		fmt.Fprintf(&out, "### %s\n\n", sc)
		out.WriteString("| label | runs | req/s | items/s | p50 | p99 | server CPU | load gen CPU % | errors | check |\n")
		out.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
		for _, label := range labels {
			var rs []scenarioResult
			for _, r := range runs {
				if r.Label != label {
					continue
				}
				if i := slices.IndexFunc(r.Scenarios, func(s scenarioResult) bool { return s.Name == sc }); i >= 0 {
					rs = append(rs, r.Scenarios[i])
				}
			}
			if len(rs) == 0 {
				continue
			}
			col := func(f func(s scenarioResult) *float64) []float64 {
				var v []float64
				for _, s := range rs {
					if p := f(s); p != nil {
						v = append(v, *p)
					}
				}
				return v
			}
			val := func(v float64) *float64 { return &v }
			var errs uint64
			chk := "-"
			for _, s := range rs {
				errs += s.Errors
				if s.Check != nil {
					if chk == "-" {
						chk = "OK"
					}
					if !s.Check.OK && !strings.HasPrefix(chk, "FAILED") {
						chk = "FAILED: " + s.Check.Detail
					}
				}
			}
			fmt.Fprintf(&out, "| %s | %d | %s | %s | %s | %s | %s | %s | %d | %s |\n",
				label, len(rs),
				cell(col(func(s scenarioResult) *float64 { return val(s.RequestsPerS) }), 0),
				cell(col(func(s scenarioResult) *float64 { return val(s.ItemsPerS) }), 0),
				cell(col(func(s scenarioResult) *float64 { return val(s.LatencyMS.P50) }), 3),
				cell(col(func(s scenarioResult) *float64 { return val(s.LatencyMS.P99) }), 3),
				cell(col(func(s scenarioResult) *float64 { return s.ServerCPUUsPerRequest }), 1),
				cell(col(func(s scenarioResult) *float64 { return s.ClientCPUPercent }), 0),
				errs, chk)
		}
		out.WriteString("\n")
	}
	out.WriteString("### Server memory (MiB)\n\n")
	out.WriteString("| label | runs | idle RSS | peak RSS |\n")
	out.WriteString("|---|---|---|---|\n")
	for _, label := range labels {
		var n int
		var idle, peak []float64
		for _, r := range runs {
			if r.Label != label {
				continue
			}
			n++
			if r.Server.IdleRSSKB != nil {
				idle = append(idle, float64(*r.Server.IdleRSSKB)/1024)
			}
			if r.Server.PeakRSSKB != nil {
				peak = append(peak, float64(*r.Server.PeakRSSKB)/1024)
			}
		}
		fmt.Fprintf(&out, "| %s | %d | %s | %s |\n", label, n, cell(idle, 1), cell(peak, 1))
	}
	return out.String(), nil
}
