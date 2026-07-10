package score

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pshima/sre-field-tests/internal/instance"
)

// InstanceResult pairs an instance's metadata with its grade.
type InstanceResult struct {
	Meta   *instance.Metadata
	Result *Result
}

// ReadResult loads score.json from an instance directory.
func ReadResult(dir string) (*Result, error) {
	data, err := os.ReadFile(filepath.Join(dir, instance.ScoreFile))
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// LoadResults reads every graded instance under resultsRoot. Directories without
// a score.json are skipped (not yet graded).
func LoadResults(resultsRoot string) ([]InstanceResult, error) {
	entries, err := os.ReadDir(resultsRoot)
	if err != nil {
		return nil, err
	}
	var out []InstanceResult
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(resultsRoot, e.Name())
		meta, err := instance.ReadMetadata(dir)
		if err != nil {
			continue
		}
		res, err := ReadResult(dir)
		if err != nil {
			continue // not graded yet
		}
		out = append(out, InstanceResult{Meta: meta, Result: res})
	}
	return out, nil
}

// Aggregate groups instances by (scenario, model, harness) and computes the
// scorecard statistics for each group. Following the eval-error-bars guidance:
// mean with SE via CLT, pass@1 and pass^k reliability, and a median MTTR (since
// incident durations are heavy-tailed).
func AggregateResults(results []InstanceResult) []Aggregate {
	type key struct{ scenario, model, harness string }
	groups := map[key][]InstanceResult{}
	for _, ir := range results {
		k := key{ir.Meta.Scenario, ir.Meta.Model, ir.Meta.Harness}
		groups[k] = append(groups[k], ir)
	}

	var aggs []Aggregate
	for k, g := range groups {
		a := Aggregate{Scenario: k.scenario, Model: k.model, Harness: k.harness, N: len(g)}
		var composites, diag, rem, comm []float64
		var mttrs []float64
		var tokens, costs []float64
		fullCount, safetyViol := 0, 0
		for _, ir := range g {
			r := ir.Result
			composites = append(composites, r.Composite)
			diag = append(diag, r.Diagnosis)
			rem = append(rem, r.Remediation)
			comm = append(comm, r.Communication)
			if r.Verdict == VerdictFull {
				fullCount++
			}
			if len(r.SafetyViolations) > 0 {
				safetyViol++
			}
			if r.MTTRSeconds != nil {
				mttrs = append(mttrs, *r.MTTRSeconds)
			}
			// Cost is only counted for instances that reported usage, so keyless
			// reference rows don't drag a real model's mean toward zero.
			if u := ir.Meta.Usage; u != nil && u.TotalTokens > 0 {
				tokens = append(tokens, float64(u.TotalTokens))
				costs = append(costs, u.CostUSD)
			}
		}
		a.CompositeMean, a.CompositeSE = meanSE(composites)
		a.DiagnosisMean, _ = meanSE(diag)
		a.RemediationMean, _ = meanSE(rem)
		a.CommunicationMean, _ = meanSE(comm)
		a.PassAtK = float64(fullCount) / float64(a.N)
		// pass^k: probability all k seeds resolve. With the group as the k
		// trials, this is 1 iff every instance fully resolved.
		if fullCount == a.N {
			a.PassHatK = 1
		}
		a.SafetyViolationRate = float64(safetyViol) / float64(a.N)
		if len(mttrs) > 0 {
			m := median(mttrs)
			a.MTTRMedianSeconds = &m
		}
		a.TokensMean, _ = meanSE(tokens)
		a.CostUSDMean, _ = meanSE(costs)
		aggs = append(aggs, a)
	}
	// Stable, useful ordering: by scenario, then composite descending.
	sort.Slice(aggs, func(i, j int) bool {
		if aggs[i].Scenario != aggs[j].Scenario {
			return aggs[i].Scenario < aggs[j].Scenario
		}
		return aggs[i].CompositeMean > aggs[j].CompositeMean
	})
	return aggs
}

// Scorecard renders aggregates as a Markdown table — the prototype "SRE score"
// row plus the per-dimension sub-table.
func Scorecard(aggs []Aggregate) string {
	var b strings.Builder
	b.WriteString("# SRE Field Tests — Scorecard\n\n")
	b.WriteString("| Scenario | Model | Harness | N | SRE score (±SE) | pass@1 | pass^k | Diag | Remed | MTTR (med) | Safety viol. | Tokens | $/inc |\n")
	b.WriteString("|---|---|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|\n")
	for _, a := range aggs {
		mttr := "—"
		if a.MTTRMedianSeconds != nil {
			mttr = fmt.Sprintf("%.0fs", *a.MTTRMedianSeconds)
		}
		// Cost columns stay blank for keyless rows (reference/reflex baselines)
		// that reported no usage, rather than showing a misleading 0.
		tokens, cost := "—", "—"
		if a.TokensMean > 0 {
			tokens = fmt.Sprintf("%.0f", a.TokensMean)
			cost = fmt.Sprintf("$%.4f", a.CostUSDMean)
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %d | %.2f ±%.2f | %.0f%% | %.0f%% | %.2f | %.2f | %s | %.0f%% | %s | %s |\n",
			a.Scenario, a.Model, a.Harness, a.N,
			a.CompositeMean, a.CompositeSE, a.PassAtK*100, a.PassHatK*100,
			a.DiagnosisMean, a.RemediationMean, mttr, a.SafetyViolationRate*100, tokens, cost))
	}
	b.WriteString("\n_SRE score is the composite of diagnosis, remediation, and (when scored) communication, ")
	b.WriteString("after the safety penalty. pass^k is the probability all k seeds resolve. ")
	b.WriteString("Single-scenario results have wide confidence intervals — see docs/scoring.md._\n")
	return b.String()
}

// --- stats helpers -----------------------------------------------------------

func meanSE(xs []float64) (mean, se float64) {
	n := len(xs)
	if n == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean = sum / float64(n)
	if n == 1 {
		return mean, 0
	}
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	variance := ss / float64(n-1) // sample variance
	return mean, math.Sqrt(variance / float64(n))
}

func median(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}
