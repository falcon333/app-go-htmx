package main

import (
	"math"
	"sort"
	"strings"
	"time"
)

type DrawdownContributor struct {
	Strategy string
	PnL      float64
	Share    float64
}

type DrawdownEvent struct {
	Start        string
	Trough       string
	End          string
	DepthPct     float64
	DepthUsd     float64
	DurationDays float64
	RecoveryDays float64
	Open         bool
	Contributors []DrawdownContributor
}

func buildDrawdownEvents(trades []SummaryTradeRow, startingCapital float64, minDepthPct float64, maxEvents int) []DrawdownEvent {
	if len(trades) == 0 || maxEvents <= 0 {
		return []DrawdownEvent{}
	}

	rows := make([]SummaryTradeRow, 0, len(trades))
	for _, t := range trades {
		if t.Dt.IsZero() || !isFinite(t.PnLAbs) {
			continue
		}
		rows = append(rows, t)
	}
	if len(rows) == 0 {
		return []DrawdownEvent{}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Dt.Before(rows[j].Dt) })

	equity := startingCapital
	if !isFinite(equity) {
		equity = 0
	}

	peakEquity := equity
	peakTime := rows[0].Dt

	inDD := false
	startEquity := peakEquity
	startTime := peakTime
	troughEquity := peakEquity
	troughTime := peakTime
	contrib := map[string]float64{}
	events := make([]DrawdownEvent, 0)

	appendEvent := func(endTime *time.Time, open bool) {
		if !inDD {
			return
		}
		depthUsd := troughEquity - startEquity
		depthPct := 0.0
		if startEquity != 0 {
			depthPct = depthUsd / startEquity
		}
		if math.Abs(depthPct) < minDepthPct {
			inDD = false
			return
		}

		durationDays := 0.0
		if !startTime.IsZero() && !troughTime.IsZero() {
			durationDays = troughTime.Sub(startTime).Hours() / 24
			if durationDays < 0 {
				durationDays = 0
			}
		}

		recoveryDays := 0.0
		endLabel := "Open"
		if endTime != nil {
			recoveryDays = endTime.Sub(startTime).Hours() / 24
			if recoveryDays < 0 {
				recoveryDays = 0
			}
			endLabel = formatTradeDate(*endTime)
		}

		event := DrawdownEvent{
			Start:        formatTradeDate(startTime),
			Trough:       formatTradeDate(troughTime),
			End:          endLabel,
			DepthPct:     depthPct,
			DepthUsd:     depthUsd,
			DurationDays: durationDays,
			RecoveryDays: recoveryDays,
			Open:         open,
			Contributors: buildDrawdownContributors(contrib, depthUsd, 3),
		}
		inDD = false
		contrib = map[string]float64{}
		startEquity = peakEquity
		startTime = peakTime
		troughEquity = peakEquity
		troughTime = peakTime
		events = append(events, event)
	}

	addContrib := func(t SummaryTradeRow) {
		name := strings.TrimSpace(t.Strategy)
		if name == "" {
			name = "Unlabeled"
		}
		contrib[name] += t.PnLAbs
	}

	for _, t := range rows {
		equity += t.PnLAbs
		if !isFinite(equity) {
			continue
		}

		if !inDD {
			if equity > peakEquity {
				peakEquity = equity
				peakTime = t.Dt
				continue
			}
			if equity < peakEquity {
				inDD = true
				startEquity = peakEquity
				startTime = peakTime
				if startTime.IsZero() {
					startTime = t.Dt
				}
				troughEquity = equity
				troughTime = t.Dt
				contrib = map[string]float64{}
				addContrib(t)
			}
			continue
		}

		addContrib(t)

		if equity < troughEquity {
			troughEquity = equity
			troughTime = t.Dt
		}

		if equity >= peakEquity {
			endTime := t.Dt
			appendEvent(&endTime, false)
			peakEquity = equity
			peakTime = t.Dt
		}
	}

	if inDD {
		appendEvent(nil, true)
	}

	if len(events) == 0 {
		return []DrawdownEvent{}
	}

	sort.Slice(events, func(i, j int) bool {
		return math.Abs(events[i].DepthPct) > math.Abs(events[j].DepthPct)
	})

	if len(events) > maxEvents {
		events = events[:maxEvents]
	}

	return events
}

func buildDrawdownContributors(contrib map[string]float64, depthUsd float64, limit int) []DrawdownContributor {
	if len(contrib) == 0 {
		return nil
	}
	depthAbs := math.Abs(depthUsd)
	if depthAbs == 0 {
		depthAbs = 1
	}

	list := make([]DrawdownContributor, 0, len(contrib))
	for name, pnl := range contrib {
		if !isFinite(pnl) {
			continue
		}
		list = append(list, DrawdownContributor{Strategy: name, PnL: pnl})
	}

	neg := make([]DrawdownContributor, 0, len(list))
	for _, c := range list {
		if c.PnL < 0 {
			neg = append(neg, c)
		}
	}
	if len(neg) > 0 {
		list = neg
	}

	sort.Slice(list, func(i, j int) bool { return list[i].PnL < list[j].PnL })
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}

	for i := range list {
		list[i].Share = math.Abs(list[i].PnL) / depthAbs
	}

	return list
}
