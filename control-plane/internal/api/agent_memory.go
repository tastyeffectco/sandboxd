package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/reaper"
)

// An agent started without enough free memory pushes the whole engine over
// (on Windows, WSL stops and every app and agent with it). sandboxd refuses
// to start one when free memory is clearly too low, and says what to do.

// readMemInfo is the engine's memory (a test replaces it).
var readMemInfo = reaper.ReadMemInfo

// agentRoomShare: an agent soon uses this share of its memory limit (OpenClaw
// sat at 2.3 GB of 3 GB right after starting).
const agentRoomShare = 0.75

func roomForAgent(pkg agentcatalog.Package) error {
	limit := memoryBytes(pkg.Memory)
	if pkg.Browser {
		limit += 1 << 30 // its browser
	}
	mi, err := readMemInfo()
	if err != nil || limit == 0 {
		return nil // unknown: do not stand in the way
	}
	need := uint64(float64(limit) * agentRoomShare)
	if free := mi.AvailableBytes(); free < need {
		return fmt.Errorf("Not enough free memory to run %s now: it needs about %s and %s is free. "+
			"Pause another agent or close some programs, then try again.", pkg.Label, gigabytes(need), gigabytes(free))
	}
	return nil
}

// memoryBytes reads a Docker memory size such as "3g" or "512m".
func memoryBytes(v string) uint64 {
	v = strings.ToLower(strings.TrimSpace(v))
	mult := uint64(1)
	switch {
	case strings.HasSuffix(v, "g"):
		mult, v = 1<<30, strings.TrimSuffix(v, "g")
	case strings.HasSuffix(v, "m"):
		mult, v = 1<<20, strings.TrimSuffix(v, "m")
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return uint64(n * float64(mult))
}

func gigabytes(b uint64) string { return fmt.Sprintf("%.1f GB", float64(b)/(1<<30)) }
