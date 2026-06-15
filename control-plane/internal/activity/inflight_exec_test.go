package activity

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/sandboxd/control-plane/internal/metrics"
)

func inflightGauge(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	if err := metrics.InflightExec.Write(&m); err != nil {
		t.Fatalf("read gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

// An unbalanced Exit (no matching Enter) must not drive the exported gauge
// negative — Dec() fires only when the map counter actually decrements.
func TestInflightExecGaugeStaysBalanced(t *testing.T) {
	ie := NewInflightExec()
	base := inflightGauge(t)
	ie.Enter("a")
	if d := inflightGauge(t) - base; d != 1 {
		t.Fatalf("after Enter: gauge delta=%v, want 1", d)
	}
	ie.Exit("a")
	if d := inflightGauge(t) - base; d != 0 {
		t.Fatalf("after balanced Exit: gauge delta=%v, want 0", d)
	}
	ie.Exit("a") // unbalanced
	if d := inflightGauge(t) - base; d != 0 {
		t.Fatalf("after unbalanced Exit: gauge delta=%v, want 0 (regression: went negative)", d)
	}
}
