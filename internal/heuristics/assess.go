package heuristics

import (
	"os"
	"strconv"
	"strings"
)

// Threshold defaults, overridable through environment variables (the demo
// needs tuning).
const (
	defaultJaccardMin        = 0.70
	defaultChurnMinGross     = 100.0
	defaultChurnMaxEff       = 0.15
	defaultChurnMinSnapshots = 10
)

// Score penalties. TOOL_CALL_TRAP arrives in Phase 3 with a -30 penalty; the
// slot below is reserved so Assess already has a place for it.
const (
	penaltyErrorLoop    = 50
	penaltyOscillation  = 35
	penaltyToolCallTrap = 30 // Phase 3: reserved, not yet wired
)

func jaccardMin() float64    { return envFloat("VAULT_JACCARD_MIN", defaultJaccardMin) }
func churnMinGross() float64 { return envFloat("VAULT_CHURN_MIN_GROSS", defaultChurnMinGross) }
func churnMaxEff() float64   { return envFloat("VAULT_CHURN_MAX_EFF", defaultChurnMaxEff) }
func churnMinSnapshots() int { return envInt("VAULT_CHURN_MIN_SNAPSHOTS", defaultChurnMinSnapshots) }

func envFloat(name string, def float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Metrics carries the raw numbers behind the health score.
type Metrics struct {
	Similarities  []float64 `json:"similarities"`
	Net           float64   `json:"net"`
	Gross         float64   `json:"gross"`
	Efficiency    float64   `json:"efficiency"`
	ActivityCount int       `json:"activity_count"`
}

// Health is the aggregated context-rot assessment returned by Assess and
// check_context_health.
type Health struct {
	Score          int     `json:"score"`
	Status         string  `json:"status"`
	Flags          []Flag  `json:"flags"`
	Reason         string  `json:"reason"`
	Recommendation string  `json:"recommendation"`
	Metrics        Metrics `json:"metrics"`
}

// Assess aggregates H1 and H2 into a Health verdict. workspace is where git
// churn/snapshot commands run; trees are the ordered tree snapshots (entry
// "tree" fields plus the latest snapshot).
func Assess(workspace string, entries []Entry, trees []string) Health {
	h := Health{
		Score:          100,
		Flags:          []Flag{},
		Recommendation: "continue",
		Metrics: Metrics{
			Similarities:  []float64{},
			ActivityCount: len(entries),
		},
	}

	if f := DetectErrorLoop(entries); f != nil {
		h.Flags = append(h.Flags, *f)
		h.Score -= penaltyErrorLoop
		if ev, ok := f.Evidence.(LoopEvidence); ok {
			h.Metrics.Similarities = ev.Similarities
		}
	}

	net, gross, snapshots := Churn(workspace, trees)
	h.Metrics.Net = round2(net)
	h.Metrics.Gross = round2(gross)
	h.Metrics.Efficiency = round2(churnEfficiency(net, gross))
	if f := DetectOscillation(net, gross, snapshots); f != nil {
		h.Flags = append(h.Flags, *f)
		h.Score -= penaltyOscillation
	}

	// Phase 3: TOOL_CALL_TRAP detection subtracts penaltyToolCallTrap here.

	if h.Score < 0 {
		h.Score = 0
	}
	if h.Score > 100 {
		h.Score = 100
	}

	h.Status = statusFor(h.Score)
	h.Reason = reasonFor(h.Flags)
	if len(h.Flags) > 0 {
		h.Recommendation = "call create_handoff and start a fresh task"
	}
	return h
}

func statusFor(score int) string {
	switch {
	case score >= 70:
		return "healthy"
	case score >= 40:
		return "degraded"
	default:
		return "critical"
	}
}

func reasonFor(flags []Flag) string {
	if len(flags) == 0 {
		return "no context-rot signals detected."
	}
	names := make([]string, 0, len(flags))
	for _, f := range flags {
		names = append(names, f.Name)
	}
	return "context rot detected: " + strings.Join(names, ", ") + "."
}
