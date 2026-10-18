package main

// Result file format: one object per run. Field names and units are part of the format, so
// result files of different versions of the tool can be compared with `report`.

type runResult struct {
	Tool string `json:"tool"`
	// Language of the load generator that wrote the file ("go").
	Implementation string           `json:"implementation"`
	ToolVersion    string           `json:"tool_version"`
	Label          string           `json:"label"`
	URL            string           `json:"url"`
	Started        string           `json:"started"`
	Concurrency    int              `json:"concurrency"`
	DurationS      int              `json:"duration_s"`
	WarmupS        int              `json:"warmup_s"`
	Server         serverInfo       `json:"server"`
	Scenarios      []scenarioResult `json:"scenarios"`
}

type serverInfo struct {
	Version string `json:"version"`
	PID     int    `json:"pid"`
	// Resident memory before the first scenario (kB), when the process is visible.
	IdleRSSKB *uint64 `json:"idle_rss_kb"`
	// Highest resident memory of the process at the end (VmHWM, kB).
	PeakRSSKB *uint64 `json:"peak_rss_kb"`
}

type scenarioResult struct {
	Name string `json:"name"`
	// HTTP requests sent during the measured window.
	Requests uint64 `json:"requests"`
	// Requests that failed (connection error or unexpected status).
	Errors       uint64  `json:"errors"`
	ElapsedS     float64 `json:"elapsed_s"`
	RequestsPerS float64 `json:"requests_per_s"`
	// Scenario units: jobs for `pull-batch`, cycles for `cycle`, requests otherwise.
	Items     uint64  `json:"items"`
	ItemsPerS float64 `json:"items_per_s"`
	// Latency of one item (one request, or one whole cycle for `cycle`).
	LatencyMS             latency           `json:"latency_ms"`
	Statuses              map[string]uint64 `json:"statuses"`
	ServerCPUUsPerRequest *float64          `json:"server_cpu_us_per_request"`
	ServerRSSKB           *uint64           `json:"server_rss_kb"`
	// CPU used by the load generator itself, in % of one CPU (Linux): close to the number
	// of CPUs it may use means the load generator, not the server, was the limit.
	ClientCPUPercent *float64 `json:"client_cpu_percent"`
	Check            *check   `json:"check"`
}

type latency struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

type check struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}
