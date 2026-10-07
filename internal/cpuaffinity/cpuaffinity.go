// Package cpuaffinity keeps Crush and everything it starts (agent CLIs,
// sub-agents, background jobs, dev servers) on a subset of CPUs so the rest
// stay free for the desktop. Child processes inherit the CPU set, so moving
// Crush itself once covers everything it starts later.
package cpuaffinity

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/config"
)

// ApplyOptions moves Crush onto the configured CPUs when
// options.agent_cpus_enabled is on. It only logs failures: running on every
// CPU is a safe fallback.
func ApplyOptions(o *config.Options) {
	if o == nil || !o.AgentCPUsEnabled || !Supported() {
		return
	}
	list, err := Apply(o.AgentCPUs)
	if err != nil {
		slog.Warn("Failed to move Crush onto agent CPUs", "cpus", o.AgentCPUs, "error", err)
		return
	}
	slog.Info("Crush runs on agent CPUs", "cpus", list)
}

// ParseList parses a Linux CPU list such as "0-5,12-17" into sorted,
// unique CPU numbers.
func ParseList(s string) ([]int, error) {
	var cpus []int
	for part := range strings.SplitSeq(strings.TrimSpace(s), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		first, last, isRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(strings.TrimSpace(first))
		if err != nil || start < 0 {
			return nil, fmt.Errorf("invalid CPU %q in %q", part, s)
		}
		end := start
		if isRange {
			end, err = strconv.Atoi(strings.TrimSpace(last))
			if err != nil || end < start {
				return nil, fmt.Errorf("invalid CPU range %q in %q", part, s)
			}
		}
		for cpu := start; cpu <= end; cpu++ {
			cpus = append(cpus, cpu)
		}
	}
	if len(cpus) == 0 {
		return nil, fmt.Errorf("empty CPU list %q", s)
	}
	slices.Sort(cpus)
	return slices.Compact(cpus), nil
}

// FormatList writes CPU numbers in Linux CPU list form, such as
// "6-11,18-23".
func FormatList(cpus []int) string {
	cpus = slices.Clone(cpus)
	slices.Sort(cpus)
	cpus = slices.Compact(cpus)
	var parts []string
	for i := 0; i < len(cpus); {
		j := i
		for j+1 < len(cpus) && cpus[j+1] == cpus[j]+1 {
			j++
		}
		if i == j {
			parts = append(parts, strconv.Itoa(cpus[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", cpus[i], cpus[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}
