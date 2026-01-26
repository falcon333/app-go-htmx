package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type ExposureTradeRow struct {
	Strategy  string
	EntryTime time.Time
	ExitTime  time.Time
	PnLAbs    float64
}

type ExposureSummaryRow struct {
	Rank                          int     `json:"rank"`
	StrategyA                     string  `json:"strategy_a"`
	StrategyB                     string  `json:"strategy_b"`
	ExposureOverlapHours          float64 `json:"exposure_overlap_hours"`
	ExposureJaccard               float64 `json:"exposure_jaccard"`
	DDCoincidenceHours            float64 `json:"dd_coincidence_hours"`
	DDSimultaneousExposureHours   float64 `json:"dd_simul_exposure_hours"`
	DDIndependentTimingHours      float64 `json:"dd_independent_hours"`
	DDCoincidenceAttribution      float64 `json:"dd_coincidence_attrib"`
	ExposureAHours                float64 `json:"exposure_a_hours"`
	ExposureBHours                float64 `json:"exposure_b_hours"`
}

type ExposureSummary struct {
	TopByExposureOverlap []ExposureSummaryRow `json:"top_by_exposure_overlap"`
	TopByExposureJaccard []ExposureSummaryRow `json:"top_by_exposure_jaccard"`
	TopByDDCoincidence   []ExposureSummaryRow `json:"top_by_dd_coincidence"`
	TopByDDSimExposure   []ExposureSummaryRow `json:"top_by_dd_sim_exposure"`
}

type ExposureMatrix struct {
	Strategies []string   `json:"strategies"`
	Values     [][]string `json:"values"`
}

type ExposureAnalysis struct {
	ExposureOverlapHours ExposureMatrix `json:"exposure_overlap_hours"`
	ExposureJaccardPct   ExposureMatrix `json:"exposure_jaccard_pct"`
	DDCoincidenceHours   ExposureMatrix `json:"dd_coincidence_hours"`
	DDCoincidenceAttrib  ExposureMatrix `json:"dd_coincidence_attrib"`
	DerivedRiskScore     *ExposureMatrix `json:"derived_risk_score"`
}

type ExposureHeatmapSpec struct {
	JaccardMid float64 `json:"jaccard_mid"`
	JaccardMax float64 `json:"jaccard_max"`
	DdAttribMid float64 `json:"dd_attrib_mid"`
	DdAttribMax float64 `json:"dd_attrib_max"`
	RiskMid float64 `json:"risk_mid"`
	RiskMax float64 `json:"risk_max"`
}

type exposureInterval struct {
	Start int64
	End   int64
}

type exposurePoint struct {
	Time int64
	PnL  float64
}

type exposurePairStat struct {
	a                          string
	b                          string
	exposureHoursA             float64
	exposureHoursB             float64
	overlapExposureHours       float64
	overlapExposureJaccard     float64
	overlapDdHours             float64
	overlapDdAttribPct         float64
	overlapDdDueToExposureHours float64
	overlapDdIndependentHours  float64
}

const (
	hourMs = 3600000.0
)

func buildExposureAnalysis(trades []ExposureTradeRow) (*ExposureSummary, *ExposureAnalysis) {
	if len(trades) == 0 {
		return nil, nil
	}

	byStrategyExposure := buildExposureIntervalsByStrategy(trades)
	strategies := make([]string, 0, len(byStrategyExposure))
	for k := range byStrategyExposure {
		strategies = append(strategies, k)
	}
	sort.Strings(strategies)

	if len(strategies) < 2 {
		return nil, nil
	}

	exposureUnion := make(map[string][]exposureInterval)
	exposureMs := make(map[string]float64)
	for _, s := range strategies {
		merged := mergeIntervals(byStrategyExposure[s])
		exposureUnion[s] = merged
		exposureMs[s] = float64(sumIntervalsMs(merged))
	}

	byStrategyUw := buildUnderwaterIntervalsByStrategy(trades)
	uwUnion := make(map[string][]exposureInterval)
	uwMs := make(map[string]float64)
	for _, s := range strategies {
		merged := mergeIntervals(byStrategyUw[s])
		uwUnion[s] = merged
		uwMs[s] = float64(sumIntervalsMs(merged))
	}

	n := len(strategies)
	overlapHours := initMatrixValues(n, "")
	overlapJaccard := initMatrixValues(n, "")
	ddOverlapHours := initMatrixValues(n, "")
	ddAttrib := initMatrixValues(n, "")

	jaccardNumeric := make([][]float64, n)
	attribNumeric := make([][]float64, n)
	for i := 0; i < n; i++ {
		jaccardNumeric[i] = make([]float64, n)
		attribNumeric[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			jaccardNumeric[i][j] = math.NaN()
			attribNumeric[i][j] = math.NaN()
		}
	}

	pairStats := make([]exposurePairStat, 0)

	for i := 0; i < n; i++ {
		a := strategies[i]
		for j := 0; j < n; j++ {
			b := strategies[j]

			if i == j {
				overlapHours[i][j] = fmtFloat(exposureMs[a]/hourMs, 2)
				overlapJaccard[i][j] = fmtPct(1, 2)
				ddOverlapHours[i][j] = fmtFloat(uwMs[a]/hourMs, 2)
				ddAttrib[i][j] = "—"
				jaccardNumeric[i][j] = 1
				attribNumeric[i][j] = math.NaN()
				continue
			}

			bothExposed := intersectIntervals(exposureUnion[a], exposureUnion[b])
			bothExposedMs := float64(sumIntervalsMs(bothExposed))

			denom := exposureMs[a] + exposureMs[b] - bothExposedMs
			jaccard := 0.0
			if denom > 0 {
				jaccard = bothExposedMs / denom
			}

			overlapHours[i][j] = fmtFloat(bothExposedMs/hourMs, 2)
			overlapJaccard[i][j] = fmtPct(jaccard, 2)
			jaccardNumeric[i][j] = jaccard

			bothUw := intersectIntervals(uwUnion[a], uwUnion[b])
			bothUwMs := float64(sumIntervalsMs(bothUw))
			ddOverlapHours[i][j] = fmtFloat(bothUwMs/hourMs, 2)

			uwWhileBothExposed := intersectIntervals(bothUw, bothExposed)
			uwWhileBothExposedMs := float64(sumIntervalsMs(uwWhileBothExposed))
			attribPct := 0.0
			if bothUwMs > 0 {
				attribPct = uwWhileBothExposedMs / bothUwMs
			}
			if bothUwMs > 0 {
				ddAttrib[i][j] = fmtPct(attribPct, 2)
				attribNumeric[i][j] = attribPct
			} else {
				ddAttrib[i][j] = ""
				attribNumeric[i][j] = math.NaN()
			}

			if j > i {
				pairStats = append(pairStats, exposurePairStat{
					a:                           a,
					b:                           b,
					exposureHoursA:              exposureMs[a] / hourMs,
					exposureHoursB:              exposureMs[b] / hourMs,
					overlapExposureHours:        bothExposedMs / hourMs,
					overlapExposureJaccard:      jaccard,
					overlapDdHours:              bothUwMs / hourMs,
					overlapDdAttribPct:          func() float64 { if bothUwMs > 0 { return attribPct }; return math.NaN() }(),
					overlapDdDueToExposureHours: uwWhileBothExposedMs / hourMs,
					overlapDdIndependentHours:   (bothUwMs - uwWhileBothExposedMs) / hourMs,
				})
			}
		}
	}

	riskMatrix := buildDerivedRiskMatrix(strategies, jaccardNumeric, attribNumeric)

	summary := buildExposureSummary(pairStats)
	analysis := ExposureAnalysis{
		ExposureOverlapHours: ExposureMatrix{Strategies: strategies, Values: overlapHours},
		ExposureJaccardPct:   ExposureMatrix{Strategies: strategies, Values: overlapJaccard},
		DDCoincidenceHours:   ExposureMatrix{Strategies: strategies, Values: ddOverlapHours},
		DDCoincidenceAttrib:  ExposureMatrix{Strategies: strategies, Values: ddAttrib},
		DerivedRiskScore:     &ExposureMatrix{Strategies: strategies, Values: riskMatrix},
	}

	return &summary, &analysis
}

func buildDerivedRiskMatrix(strategies []string, jaccard [][]float64, attrib [][]float64) [][]string {
	n := len(strategies)
	values := initMatrixValues(n, "")
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j {
				values[i][j] = ""
				continue
			}
			jac := jaccard[i][j]
			att := attrib[i][j]
			if !isFinite(jac) || !isFinite(att) {
				values[i][j] = ""
				continue
			}
			values[i][j] = fmtFloat(jac*att, 4)
		}
	}
	return values
}

func buildExposureIntervalsByStrategy(trades []ExposureTradeRow) map[string][]exposureInterval {
	out := make(map[string][]exposureInterval)
	for _, t := range trades {
		strategy := strings.TrimSpace(t.Strategy)
		if strategy == "" {
			continue
		}
		entryMs, okEntry := timeToMillis(t.EntryTime)
		exitMs, okExit := timeToMillis(t.ExitTime)
		if !okEntry || !okExit {
			continue
		}
		if exitMs <= entryMs {
			continue
		}
		out[strategy] = append(out[strategy], exposureInterval{Start: entryMs, End: exitMs})
	}
	return out
}

func buildUnderwaterIntervalsByStrategy(trades []ExposureTradeRow) map[string][]exposureInterval {
	byStrategy := make(map[string][]exposurePoint)
	for _, t := range trades {
		strategy := strings.TrimSpace(t.Strategy)
		if strategy == "" {
			continue
		}
		exitTime := t.ExitTime
		if exitTime.IsZero() {
			exitTime = t.EntryTime
		}
		ms, ok := timeToMillis(exitTime)
		if !ok {
			continue
		}
		if !isFinite(t.PnLAbs) {
			continue
		}
		byStrategy[strategy] = append(byStrategy[strategy], exposurePoint{Time: ms, PnL: t.PnLAbs})
	}

	intervalsByStrat := make(map[string][]exposureInterval)
	for strategy, points := range byStrategy {
		sort.Slice(points, func(i, j int) bool { return points[i].Time < points[j].Time })
		if len(points) < 2 {
			intervalsByStrat[strategy] = []exposureInterval{}
			continue
		}
		equity := 0.0
		peak := 0.0
		underwater := false
		intervals := make([]exposureInterval, 0)
		for i := 0; i < len(points)-1; i++ {
			equity += points[i].PnL
			if equity >= peak {
				peak = equity
				underwater = false
			} else {
				underwater = true
			}

			t0 := points[i].Time
			t1 := points[i+1].Time
			if underwater && t1 > t0 {
				intervals = append(intervals, exposureInterval{Start: t0, End: t1})
			}
		}
		intervalsByStrat[strategy] = intervals
	}
	return intervalsByStrat
}

func mergeIntervals(intervals []exposureInterval) []exposureInterval {
	if len(intervals) == 0 {
		return []exposureInterval{}
	}

	filtered := make([]exposureInterval, 0, len(intervals))
	for _, in := range intervals {
		if in.End > in.Start {
			filtered = append(filtered, in)
		}
	}
	if len(filtered) == 0 {
		return []exposureInterval{}
	}

	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Start < filtered[j].Start })
	merged := []exposureInterval{filtered[0]}
	for i := 1; i < len(filtered); i++ {
		last := &merged[len(merged)-1]
		cur := filtered[i]
		if cur.Start <= last.End {
			if cur.End > last.End {
				last.End = cur.End
			}
		} else {
			merged = append(merged, cur)
		}
	}

	return merged
}

func intersectIntervals(a []exposureInterval, b []exposureInterval) []exposureInterval {
	if len(a) == 0 || len(b) == 0 {
		return []exposureInterval{}
	}

	i := 0
	j := 0
	out := make([]exposureInterval, 0)
	for i < len(a) && j < len(b) {
		start := maxInt64(a[i].Start, b[j].Start)
		end := minInt64(a[i].End, b[j].End)
		if end > start {
			out = append(out, exposureInterval{Start: start, End: end})
		}
		if a[i].End < b[j].End {
			i++
		} else {
			j++
		}
	}
	return out
}

func sumIntervalsMs(intervals []exposureInterval) int64 {
	var sum int64
	for _, in := range intervals {
		if in.End > in.Start {
			sum += in.End - in.Start
		}
	}
	return sum
}

func initMatrixValues(n int, fill string) [][]string {
	values := make([][]string, n)
	for i := 0; i < n; i++ {
		row := make([]string, n)
		for j := 0; j < n; j++ {
			row[j] = fill
		}
		values[i] = row
	}
	return values
}

func buildExposureSummary(pairStats []exposurePairStat) ExposureSummary {
	const topN = 25
	topBy := func(keyFn func(exposurePairStat) float64, filterFn func(exposurePairStat) bool) []ExposureSummaryRow {
		filtered := make([]exposurePairStat, 0)
		for _, p := range pairStats {
			if filterFn == nil || filterFn(p) {
				filtered = append(filtered, p)
			}
		}
		sort.Slice(filtered, func(i, j int) bool { return keyFn(filtered[i]) > keyFn(filtered[j]) })

		if len(filtered) > topN {
			filtered = filtered[:topN]
		}
		rows := make([]ExposureSummaryRow, 0, len(filtered))
		for i, p := range filtered {
			rows = append(rows, ExposureSummaryRow{
				Rank:                        i + 1,
				StrategyA:                   p.a,
				StrategyB:                   p.b,
				ExposureOverlapHours:        p.overlapExposureHours,
				ExposureJaccard:             p.overlapExposureJaccard,
				DDCoincidenceHours:          p.overlapDdHours,
				DDSimultaneousExposureHours: p.overlapDdDueToExposureHours,
				DDIndependentTimingHours:    p.overlapDdIndependentHours,
				DDCoincidenceAttribution:    p.overlapDdAttribPct,
				ExposureAHours:              p.exposureHoursA,
				ExposureBHours:              p.exposureHoursB,
			})
		}
		return rows
	}

	return ExposureSummary{
		TopByExposureOverlap: topBy(func(p exposurePairStat) float64 { return p.overlapExposureHours }, nil),
		TopByExposureJaccard: topBy(func(p exposurePairStat) float64 { return p.overlapExposureJaccard }, nil),
		TopByDDCoincidence:   topBy(func(p exposurePairStat) float64 { return p.overlapDdHours }, nil),
		TopByDDSimExposure:   topBy(func(p exposurePairStat) float64 { return p.overlapDdDueToExposureHours }, func(p exposurePairStat) bool { return p.overlapDdHours > 0 }),
	}
}

func timeToMillis(t time.Time) (int64, bool) {
	if t.IsZero() {
		return 0, false
	}
	return t.UnixNano() / int64(time.Millisecond), true
}

func fmtFloat(v float64, decimals int) string {
	if !isFinite(v) {
		return "—"
	}
	return fmt.Sprintf("%.*f", decimals, v)
}

func fmtPct(v float64, decimals int) string {
	if !isFinite(v) {
		return "—"
	}
	return fmt.Sprintf("%.*f%%", decimals, v*100)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
