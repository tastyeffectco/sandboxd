package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/reaper"
)

func TestRoomForAgent(t *testing.T) {
	old := readMemInfo
	defer func() { readMemInfo = old }()
	free := func(gb float64) {
		readMemInfo = func() (reaper.MemInfo, error) {
			return reaper.MemInfo{Total: 8 << 20, Available: uint64(gb * (1 << 20))}, nil
		}
	}
	free(2) // OpenClaw (3 GB + 1 GB browser) needs about 3 GB
	err := roomForAgent(agentcatalog.OpenClaw)
	if err == nil || !strings.Contains(err.Error(), "Pause another agent") || !strings.Contains(err.Error(), "2.0 GB is free") {
		t.Fatalf("low memory: %v", err)
	}
	if err := roomForAgent(agentcatalog.Hermes); err != nil { // 2 GB limit: 1.5 GB needed
		t.Fatalf("hermes fits: %v", err)
	}
	free(4)
	if err := roomForAgent(agentcatalog.OpenClaw); err != nil {
		t.Fatalf("enough memory: %v", err)
	}
	readMemInfo = func() (reaper.MemInfo, error) { return reaper.MemInfo{}, errors.New("no /proc") }
	if err := roomForAgent(agentcatalog.OpenClaw); err != nil {
		t.Fatalf("unknown memory must not block: %v", err)
	}
	if memoryBytes("512m") != 512<<20 || memoryBytes("3g") != 3<<30 || memoryBytes("x") != 0 {
		t.Fatal("memory sizes")
	}
}
