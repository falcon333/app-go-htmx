package main

import (
	"math"
	"sort"
	"strings"
	"time"
)

type SummaryTradeRow struct {
	Strategy string
	Pair     string
	Dt       time.Time
	AcctPct  float64
	Balance  float64
	PnLAbs   float64
}

type ScoreWeights struct {
	R2         float64
	Net        float64
	DD         float64
	PF         float64
	Expectancy float64
}

type StrategySummaryRow struct {
	Strategy       string  `json:"strategy"`
	Trades         int     `json:"trades"`
	NetGain        float64 `json:"net_gain"`
	CAGR           float64 `json:"cagr"`
	MaxDrawdown    float64 `json:"max_drawdown"`
	WinRate        float64 `json:"win_rate"`
	AvgReturn      float64 `json:"avg_return"`
	RiskReward     float64 `json:"risk_reward"`
	ProfitFactor   float64 `json:"profit_factor"`
	UlcerIndex     float64 `json:"ulcer_index"`
	EquityR2       float64 `json:"equity_r2"`
	TimeUnderWater float64 `json:"time_under_water"`
	Scorecard      float64 `json:"scorecard"`
	SleepWell      float64 `json:"sleep_well"`
	AvgDDDays      float64 `json:"avg_dd_days"`
	RecoveryScore  float64 `json:"recovery_score"`
	Tier           string  `json:"tier"`
	Allocation     float64 `json:"allocation"`
}

type PairSummaryRow struct {
	Pair           string  `json:"pair"`
	Trades         int     `json:"trades"`
	NetGain        float64 `json:"net_gain"`
	CAGR           float64 `json:"cagr"`
	MaxDrawdown    float64 `json:"max_drawdown"`
	WinRate        float64 `json:"win_rate"`
	AvgReturn      float64 `json:"avg_return"`
	RiskReward     float64 `json:"risk_reward"`
	ProfitFactor   float64 `json:"profit_factor"`
	UlcerIndex     float64 `json:"ulcer_index"`
	EquityR2       float64 `json:"equity_r2"`
	TimeUnderWater float64 `json:"time_under_water"`
	Scorecard      float64 `json:"scorecard"`
	SleepWell      float64 `json:"sleep_well"`
}

type SummaryDebugStats struct {
	TotalTrades           int      `json:"total_trades"`
	DistinctStrategies    int      `json:"distinct_strategies"`
	DistinctPairs         int      `json:"distinct_pairs"`
	InvalidExitTime       int      `json:"invalid_exit_time"`
	InvalidBalance        int      `json:"invalid_balance"`
	EmptyStrategy         int      `json:"empty_strategy"`
	InvalidExitSamples    []string `json:"invalid_exit_samples"`
	InvalidBalanceSamples []string `json:"invalid_balance_samples"`
	EmptyStrategySamples  []string `json:"empty_strategy_samples"`
}

func defaultScoreWeights() ScoreWeights {
	return ScoreWeights{
		R2:         1.0,
		Net:        1.0,
		DD:         1.0,
		PF:         1.0,
		Expectancy: 1.0,
	}
}

type summaryEntry struct {
	dt     time.Time
	pct    float64
	pnlAbs float64
}

func buildStrategySummary(trades []SummaryTradeRow, startingBalance float64, weights ScoreWeights) []StrategySummaryRow {
	if len(trades) == 0 {
		return []StrategySummaryRow{}
	}

	initBal := startingBalance
	if !isFinite(initBal) {
		initBal = 0
	}

	byStrat := make(map[string][]summaryEntry)
	for _, t := range trades {
		name := strings.TrimSpace(t.Strategy)
		if name == "" {
			name = "Unlabeled"
		}
		if t.Dt.IsZero() {
			continue
		}
		if !isFinite(t.PnLAbs) {
			continue
		}
		pct := t.AcctPct
		if !isFinite(pct) {
			pct = math.NaN()
		}
		byStrat[name] = append(byStrat[name], summaryEntry{dt: t.Dt, pct: pct, pnlAbs: t.PnLAbs})
	}

	rows := make([]StrategySummaryRow, 0, len(byStrat))
	for name, arr := range byStrat {
		sort.Slice(arr, func(i, j int) bool { return arr[i].dt.Before(arr[j].dt) })

		pcts := make([]float64, 0, len(arr))
		dates := make([]time.Time, 0, len(arr))
		pnls := make([]float64, 0, len(arr))

		for _, item := range arr {
			if item.dt.IsZero() {
				continue
			}
			if !isFinite(item.pct) && !isFinite(item.pnlAbs) {
				continue
			}
			dates = append(dates, item.dt)
			pcts = append(pcts, item.pct)
			pnls = append(pnls, item.pnlAbs)
		}

		if len(pcts) == 0 || len(dates) == 0 {
			continue
		}

		gp := 0.0
		gl := 0.0
		totalPnl := 0.0
		for _, v := range pnls {
			if !isFinite(v) {
				continue
			}
			totalPnl += v
			if v > 0 {
				gp += v
			} else if v < 0 {
				gl += math.Abs(v)
			}
		}
		pf := 0.0
		if gl > 0 {
			pf = gp / gl
		}

		b2 := initBal
		peakB := b2
		minDD := 0.0
		ddPos := make([]float64, 0, len(pcts))
		eqSeries := make([]float64, 0, len(pcts))
		underCnt := 0

		ddDurations := make([]float64, 0)
		var currentDDStart *time.Time
		peakDate := dates[0]

		for i := 0; i < len(pcts); i++ {
			pk := pcts[i]
			xk := pnls[i]

			if isFinite(pk) {
				b2 += b2 * pk
			} else if isFinite(xk) {
				b2 += xk
				prevB := math.Max(b2-xk, 1)
				pk = xk / prevB
				pcts[i] = pk
			} else {
				continue
			}

			eqSeries = append(eqSeries, b2)
			currDate := dates[i]

			if b2 > peakB {
				if currentDDStart != nil {
					ddDurations = append(ddDurations, currDate.Sub(*currentDDStart).Hours()/24)
					currentDDStart = nil
				}
				peakB = b2
				peakDate = currDate
			} else {
				if currentDDStart == nil {
					tmp := peakDate
					currentDDStart = &tmp
				}
			}

			dd2 := 0.0
			if peakB != 0 {
				dd2 = (b2 - peakB) / peakB
			}
			if dd2 < minDD {
				minDD = dd2
			}
			ddPos = append(ddPos, math.Abs(dd2))
			if b2 < peakB {
				underCnt++
			}
		}

		if currentDDStart != nil && len(dates) > 0 {
			lastD := dates[len(dates)-1]
			ddDurations = append(ddDurations, lastD.Sub(*currentDDStart).Hours()/24)
		}

		ulcer := UlcerIndexFromDrawdowns(ddPos)
		r2 := R2OfSeries(eqSeries)
		tuw := 0.0
		if len(eqSeries) > 0 {
			tuw = float64(underCnt) / float64(len(eqSeries))
		}

		validPcts := make([]float64, 0, len(pcts))
		for _, v := range pcts {
			if isFinite(v) {
				validPcts = append(validPcts, v)
			}
		}
		tradesCount := len(validPcts)
		if tradesCount == 0 {
			continue
		}

		net := 0.0
		if initBal > 0 {
			net = (b2 / initBal) - 1
		}

		yrs := math.Max(dates[len(dates)-1].Sub(dates[0]).Hours()/24/365.25, 0)
		cagr := 0.0
		if yrs > 0 && initBal > 0 {
			cagr = math.Pow(b2/initBal, 1/yrs) - 1
		}

		winRate := 0.0
		sumPct := 0.0
		posCount := 0
		gains := make([]float64, 0)
		losses := make([]float64, 0)
		for _, v := range validPcts {
			sumPct += v
			if v > 0 {
				posCount++
				gains = append(gains, v)
			} else if v < 0 {
				losses = append(losses, math.Abs(v))
			}
		}
		winRate = float64(posCount) / float64(tradesCount)
		avgReturn := sumPct / float64(tradesCount)

		ag := avgFloat64(gains)
		al := avgFloat64(losses)
		rr := 0.0
		if al > 0 {
			rr = ag / al
		}

		avgDDDays := avgFloat64(ddDurations)
		recScore := CalculateRecoveryScore(net*100, math.Abs(minDD)*100, avgDDDays)
		sleepWell := CalculateSleepWellScore(cagr, r2, ulcer)
		expectancy := totalPnl / float64(tradesCount)
		scorecard := CalculateStrategyScorecard(r2, net, math.Abs(minDD), pf, expectancy, weights)

		rows = append(rows, StrategySummaryRow{
			Strategy:       name,
			Trades:         tradesCount,
			NetGain:        normalizeFloat(net),
			CAGR:           normalizeFloat(cagr),
			MaxDrawdown:    normalizeFloat(minDD),
			WinRate:        normalizeFloat(winRate),
			AvgReturn:      normalizeFloat(avgReturn),
			RiskReward:     normalizeFloat(rr),
			ProfitFactor:   normalizeFloat(pf),
			UlcerIndex:     normalizeFloat(ulcer),
			EquityR2:       normalizeFloat(r2),
			TimeUnderWater: normalizeFloat(tuw),
			Scorecard:      normalizeFloat(scorecard),
			SleepWell:      normalizeFloat(sleepWell),
			AvgDDDays:      normalizeFloat(avgDDDays),
			RecoveryScore:  normalizeFloat(recScore),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Scorecard > rows[j].Scorecard
	})

	totalCount := len(rows)
	topCutoffIndex := int(math.Floor(float64(totalCount) * 0.20))
	bottomCutoffIndex := int(math.Floor(float64(totalCount) * 0.80))

	topGroupCount := topCutoffIndex
	bottomGroupCount := totalCount - bottomCutoffIndex
	midGroupCount := totalCount - topGroupCount - bottomGroupCount

	topAllocationPerStrat := 0.0
	if topGroupCount > 0 {
		topAllocationPerStrat = 0.40 / float64(topGroupCount)
	}
	midAllocationPerStrat := 0.0
	if midGroupCount > 0 {
		midAllocationPerStrat = 0.60 / float64(midGroupCount)
	}

	for i := range rows {
		if i < topCutoffIndex {
			rows[i].Tier = "Elite (Top 20%)"
			rows[i].Allocation = topAllocationPerStrat
		} else if i >= bottomCutoffIndex {
			rows[i].Tier = "Kill (Bottom 20%)"
			rows[i].Allocation = 0
		} else {
			rows[i].Tier = "Mid-Pack"
			rows[i].Allocation = midAllocationPerStrat
		}
	}

	return rows
}

func buildPairSummary(trades []SummaryTradeRow, startingBalance float64, weights ScoreWeights) []PairSummaryRow {
	if len(trades) == 0 {
		return []PairSummaryRow{}
	}

	initBal := startingBalance
	if !isFinite(initBal) {
		initBal = 0
	}

	byPair := make(map[string][]summaryEntry)
	for _, t := range trades {
		name := strings.TrimSpace(strings.ToUpper(t.Pair))
		if name == "" {
			name = "Unlabeled"
		}
		if t.Dt.IsZero() {
			continue
		}
		if !isFinite(t.PnLAbs) {
			continue
		}
		pct := t.AcctPct
		if !isFinite(pct) {
			pct = math.NaN()
		}
		byPair[name] = append(byPair[name], summaryEntry{dt: t.Dt, pct: pct, pnlAbs: t.PnLAbs})
	}

	rows := make([]PairSummaryRow, 0, len(byPair))
	for name, arr := range byPair {
		sort.Slice(arr, func(i, j int) bool { return arr[i].dt.Before(arr[j].dt) })

		pcts := make([]float64, 0, len(arr))
		dates := make([]time.Time, 0, len(arr))
		pnls := make([]float64, 0, len(arr))

		for _, item := range arr {
			if item.dt.IsZero() {
				continue
			}
			if !isFinite(item.pct) && !isFinite(item.pnlAbs) {
				continue
			}
			dates = append(dates, item.dt)
			pcts = append(pcts, item.pct)
			pnls = append(pnls, item.pnlAbs)
		}

		if len(pcts) == 0 || len(dates) == 0 {
			continue
		}

		gp := 0.0
		gl := 0.0
		totalPnl := 0.0
		for _, v := range pnls {
			if !isFinite(v) {
				continue
			}
			totalPnl += v
			if v > 0 {
				gp += v
			} else if v < 0 {
				gl += math.Abs(v)
			}
		}
		pf := 0.0
		if gl > 0 {
			pf = gp / gl
		}

		b2 := initBal
		peakB := b2
		minDD := 0.0
		ddPos := make([]float64, 0, len(pcts))
		eqSeries := make([]float64, 0, len(pcts))
		underCnt := 0

		for i := 0; i < len(pcts); i++ {
			pk := pcts[i]
			xk := pnls[i]
			if isFinite(pk) {
				b2 += b2 * pk
			} else if isFinite(xk) {
				b2 += xk
				prevB := math.Max(b2-xk, 1)
				pk = xk / prevB
				pcts[i] = pk
			} else {
				continue
			}
			eqSeries = append(eqSeries, b2)
			if b2 > peakB {
				peakB = b2
			}
			dd2 := 0.0
			if peakB != 0 {
				dd2 = (b2 - peakB) / peakB
			}
			if dd2 < minDD {
				minDD = dd2
			}
			ddPos = append(ddPos, math.Abs(dd2))
			if b2 < peakB {
				underCnt++
			}
		}

		ulcer := UlcerIndexFromDrawdowns(ddPos)
		r2 := R2OfSeries(eqSeries)
		tuw := 0.0
		if len(eqSeries) > 0 {
			tuw = float64(underCnt) / float64(len(eqSeries))
		}

		validPcts := make([]float64, 0, len(pcts))
		for _, v := range pcts {
			if isFinite(v) {
				validPcts = append(validPcts, v)
			}
		}
		tradesCount := len(validPcts)
		if tradesCount == 0 {
			continue
		}

		net := 0.0
		if initBal > 0 {
			net = (b2 / initBal) - 1
		}

		yrs := math.Max(dates[len(dates)-1].Sub(dates[0]).Hours()/24/365.25, 0)
		cagr := 0.0
		if yrs > 0 && initBal > 0 {
			cagr = math.Pow(b2/initBal, 1/yrs) - 1
		}

		winRate := 0.0
		sumPct := 0.0
		posCount := 0
		gains := make([]float64, 0)
		losses := make([]float64, 0)
		for _, v := range validPcts {
			sumPct += v
			if v > 0 {
				posCount++
				gains = append(gains, v)
			} else if v < 0 {
				losses = append(losses, math.Abs(v))
			}
		}
		winRate = float64(posCount) / float64(tradesCount)
		avgReturn := sumPct / float64(tradesCount)

		ag := avgFloat64(gains)
		al := avgFloat64(losses)
		rr := 0.0
		if al > 0 {
			rr = ag / al
		}

		expectancy := totalPnl / float64(tradesCount)
		scorecard := CalculateStrategyScorecard(r2, net, math.Abs(minDD), pf, expectancy, weights)
		sleepWell := CalculateSleepWellScore(cagr, r2, ulcer)

		rows = append(rows, PairSummaryRow{
			Pair:           name,
			Trades:         tradesCount,
			NetGain:        normalizeFloat(net),
			CAGR:           normalizeFloat(cagr),
			MaxDrawdown:    normalizeFloat(minDD),
			WinRate:        normalizeFloat(winRate),
			AvgReturn:      normalizeFloat(avgReturn),
			RiskReward:     normalizeFloat(rr),
			ProfitFactor:   normalizeFloat(pf),
			UlcerIndex:     normalizeFloat(ulcer),
			EquityR2:       normalizeFloat(r2),
			TimeUnderWater: normalizeFloat(tuw),
			Scorecard:      normalizeFloat(scorecard),
			SleepWell:      normalizeFloat(sleepWell),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Scorecard > rows[j].Scorecard
	})

	return rows
}

func UlcerIndexFromDrawdowns(dds []float64) float64 {
	sumSq := 0.0
	n := 0
	for _, d := range dds {
		if !isFinite(d) {
			continue
		}
		sumSq += d * d
		n++
	}
	if n == 0 {
		return 0
	}
	return math.Sqrt(sumSq / float64(n))
}

func R2OfSeries(ys []float64) float64 {
	if len(ys) < 2 {
		return 0
	}

	n := 0.0
	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumXX := 0.0
	sumYY := 0.0

	for i := 0; i < len(ys); i++ {
		y := ys[i]
		if !isFinite(y) {
			continue
		}
		x := float64(n)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
		sumYY += y * y
		n++
	}

	if n < 2 {
		return 0
	}

	num := (n*sumXY - sumX*sumY)
	den := math.Sqrt((n*sumXX - sumX*sumX) * (n*sumYY - sumY*sumY))
	if den == 0 {
		return 0
	}
	r := num / den
	return r * r
}

func TimeUnderWaterFromEquity(eq []float64) float64 {
	if len(eq) == 0 {
		return 0
	}

	peak := math.Inf(-1)
	under := 0
	n := 0

	for _, v := range eq {
		if !isFinite(v) {
			continue
		}
		n++
		if v >= peak {
			peak = v
		} else {
			under++
		}
	}

	if n == 0 {
		return 0
	}
	return float64(under) / float64(n)
}

func CalculateRecoveryScore(netPct, maxDDPct, avgDDDays float64) float64 {
	s := 0.0
	s += math.Max(-50, math.Min(50, netPct)) * 0.6
	s += math.Max(0, 30-maxDDPct) * 1.2
	s += math.Max(0, 30-avgDDDays) * 0.8
	if s < 0 {
		return 0
	}
	return s
}

func CalculateSleepWellScore(cagr, r2, ulcer float64) float64 {
	s := 0.0
	s += (cagr * 100) * 1.0
	s += r2 * 50
	s += math.Max(0, 25-(ulcer*100)) * 1.0
	if s < 0 {
		return 0
	}
	return s
}

func CalculateStrategyScorecard(r2, net, maxDdAbs, pf, expectancy float64, weights ScoreWeights) float64 {
	w := weights
	if w.R2 == 0 && w.Net == 0 && w.DD == 0 && w.PF == 0 && w.Expectancy == 0 {
		w = defaultScoreWeights()
	}

	pfN := pf
	if !isFinite(pfN) {
		pfN = 0
	}

	score := 0.0
	score += (r2 * 100) * w.R2
	score += (net * 100) * w.Net
	score += math.Max(0, 25-(maxDdAbs*100)) * w.DD
	score += math.Min(10, pfN) * 10 * w.PF
	score += (expectancy / 10) * w.Expectancy

	if score < 0 {
		return 0
	}
	return score
}

func avgFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	count := 0
	for _, v := range values {
		if !isFinite(v) {
			continue
		}
		sum += v
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func normalizeFloat(v float64) float64 {
	if !isFinite(v) {
		return 0
	}
	return v
}
