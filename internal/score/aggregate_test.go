package score

import (
	"testing"

	"github.com/pshima/sre-field-tests/internal/instance"
)

func mkIR(model string, seed int, composite float64, verdict Verdict, mttr *float64, viol []string) InstanceResult {
	return InstanceResult{
		Meta:   &instance.Metadata{Scenario: "oom-killed", Model: model, Harness: "neutral-go", Seed: seed},
		Result: &Result{Composite: composite, Verdict: verdict, MTTRSeconds: mttr, SafetyViolations: viol, Diagnosis: composite, Remediation: composite},
	}
}

func TestAggregatePassHatKAndStats(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	results := []InstanceResult{
		// model A: 3 seeds, all full -> pass^k = 1
		mkIR("A", 1, 1.0, VerdictFull, f(5), nil),
		mkIR("A", 2, 1.0, VerdictFull, f(7), nil),
		mkIR("A", 3, 0.9, VerdictFull, f(9), nil),
		// model B: 2 seeds, one full one none -> pass@1=0.5, pass^k=0
		mkIR("B", 1, 0.8, VerdictFull, f(20), nil),
		mkIR("B", 2, 0.0, VerdictNone, nil, []string{"killed-neighbor"}),
	}
	aggs := AggregateResults(results)
	byModel := map[string]Aggregate{}
	for _, a := range aggs {
		byModel[a.Model] = a
	}

	a := byModel["A"]
	if a.N != 3 || a.PassAtK != 1 || a.PassHatK != 1 {
		t.Errorf("A: N=%d passAt1=%.2f passHatK=%.2f, want 3/1/1", a.N, a.PassAtK, a.PassHatK)
	}
	if a.MTTRMedianSeconds == nil || *a.MTTRMedianSeconds != 7 {
		t.Errorf("A: median MTTR = %v, want 7 (median of 5,7,9)", a.MTTRMedianSeconds)
	}
	if a.CompositeSE <= 0 {
		t.Errorf("A: expected a positive SE for N=3, got %.3f", a.CompositeSE)
	}

	b := byModel["B"]
	if b.PassAtK != 0.5 || b.PassHatK != 0 {
		t.Errorf("B: passAt1=%.2f passHatK=%.2f, want 0.5/0", b.PassAtK, b.PassHatK)
	}
	if b.SafetyViolationRate != 0.5 {
		t.Errorf("B: safety viol rate = %.2f, want 0.5", b.SafetyViolationRate)
	}

	// Scorecard renders without panicking and includes a header + both models.
	sc := Scorecard(aggs)
	if len(sc) == 0 || !contains2(sc, "SRE score") || !contains2(sc, "| oom-killed | A |") {
		t.Errorf("scorecard missing expected content:\n%s", sc)
	}
}

func contains2(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
