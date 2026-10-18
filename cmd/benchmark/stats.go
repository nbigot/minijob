package main

import (
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// samples holds every latency of a scenario in microseconds; percentiles by nearest rank.
type samples struct {
	us []uint32
}

func (s *samples) push(d time.Duration) {
	s.us = append(s.us, uint32(min(d.Microseconds(), math.MaxUint32)))
}

func (s *samples) merge(other samples) {
	s.us = append(s.us, other.us...)
}

func (s *samples) summary() latency {
	n := len(s.us)
	if n == 0 {
		return latency{}
	}
	slices.Sort(s.us)
	rank := func(p float64) float64 {
		i := int(math.Ceil(p / 100 * float64(n)))
		return float64(s.us[min(max(i, 1), n)-1]) / 1000
	}
	var sum uint64
	for _, v := range s.us {
		sum += uint64(v)
	}
	return latency{
		Mean: round3(float64(sum) / float64(n) / 1000),
		P50:  round3(rank(50)),
		P90:  round3(rank(90)),
		P99:  round3(rank(99)),
		Max:  round3(float64(s.us[n-1]) / 1000),
	}
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

// procStat is the CPU time and memory of the server process, from /proc/<pid> (Linux only).
type procStat struct {
	cpuTicks uint64 // utime + stime in clock ticks
	rssKB    uint64
	hwmKB    uint64
}

// ticksPerS is the number of clock ticks per second (`getconf CLK_TCK`), 100 on every
// mainstream Linux.
const ticksPerS = 100.0

// cpuTicks reads utime + stime from a /proc/<pid>/stat file.
func cpuTicks(path string) (uint64, bool) {
	stat, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	// The command name may contain spaces: fields start after the last ')'.
	i := strings.LastIndexByte(string(stat), ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(string(stat[i+1:]))
	if len(f) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseUint(f[11], 10, 64)
	stime, err2 := strconv.ParseUint(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return utime + stime, true
}

// selfCPUTicks returns the CPU ticks used by this process (Linux).
func selfCPUTicks() *uint64 {
	t, ok := cpuTicks("/proc/self/stat")
	if !ok {
		return nil
	}
	return &t
}

// cpuPercent returns the % of one CPU used between two readings of selfCPUTicks.
func cpuPercent(t0, t1 *uint64, elapsedS float64) *float64 {
	if t0 == nil || t1 == nil || *t1 < *t0 {
		return nil
	}
	v := round3(float64(*t1-*t0) / ticksPerS / elapsedS * 100)
	return &v
}

func readProcStat(pid int) *procStat {
	if pid == 0 {
		return nil
	}
	ticks, ok := cpuTicks("/proc/" + strconv.Itoa(pid) + "/stat")
	if !ok {
		return nil
	}
	status, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return nil
	}
	kb := func(key string) (uint64, bool) {
		for _, l := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(l, key) {
				f := strings.Fields(l)
				if len(f) < 2 {
					return 0, false
				}
				v, err := strconv.ParseUint(f[1], 10, 64)
				return v, err == nil
			}
		}
		return 0, false
	}
	rss, ok1 := kb("VmRSS:")
	hwm, ok2 := kb("VmHWM:")
	if !ok1 || !ok2 {
		return nil
	}
	return &procStat{cpuTicks: ticks, rssKB: rss, hwmKB: hwm}
}
