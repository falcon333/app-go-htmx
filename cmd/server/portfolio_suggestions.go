package main

import (
	"fmt"
	"math"
	"sort"
)

type SuggestedStrategy struct {
	Strategy    string
	Weight      float64
	DDContrib   float64
	Scorecard   float64
	CAGR        float64
	MaxDrawdown float64
	Trades      int
}

type SuggestedPortfolio struct {
	Name           string
	Score          float64
	ExpectedCAGR   float64
	MaxDrawdown    float64
	Strategies     []SuggestedStrategy
	Rationale      string
	CandidateCount int
}

const (
	minSuggestionTrades = 30
	minSuggestionPF     = 1.20
	maxSuggestionDD     = 0.10
	maxSuggestionCount  = 5
)

func buildPortfolioSuggestions(rows []StrategySummaryRow) []SuggestedPortfolio {
	if len(rows) == 0 {
		return nil
	}

	candidates := filterSuggestionCandidates(rows)
	fallback := false
	if len(candidates) == 0 {
		candidates = append([]StrategySummaryRow{}, rows...)
		fallback = true
	}

	pickTop := func(source []StrategySummaryRow, less func(a, b StrategySummaryRow) bool) []StrategySummaryRow {
		copyRows := append([]StrategySummaryRow{}, source...)
		sort.Slice(copyRows, func(i, j int) bool { return less(copyRows[i], copyRows[j]) })
		limit := maxSuggestionCount
		if len(copyRows) < limit {
			limit = len(copyRows)
		}
		return copyRows[:limit]
	}

	byReturn := pickTop(candidates, func(a, b StrategySummaryRow) bool {
		if a.CAGR == b.CAGR {
			return a.Scorecard > b.Scorecard
		}
		return a.CAGR > b.CAGR
	})
	byDrawdown := pickTop(candidates, func(a, b StrategySummaryRow) bool {
		ad := math.Abs(a.MaxDrawdown)
		bd := math.Abs(b.MaxDrawdown)
		if ad == bd {
			return a.Scorecard > b.Scorecard
		}
		return ad < bd
	})
	byScore := pickTop(candidates, func(a, b StrategySummaryRow) bool {
		if a.Scorecard == b.Scorecard {
			return a.CAGR > b.CAGR
		}
		return a.Scorecard > b.Scorecard
	})

	suggestions := []SuggestedPortfolio{
		buildSuggestedPortfolio("Return Max", byReturn, candidates, fallback, "Highest CAGR subject to risk caps."),
		buildSuggestedPortfolio("Low Drawdown", byDrawdown, candidates, fallback, "Lowest drawdown with solid scores."),
		buildSuggestedPortfolio("Balanced", byScore, candidates, fallback, "Best overall scorecard balance."),
	}

	return suggestions
}

func filterSuggestionCandidates(rows []StrategySummaryRow) []StrategySummaryRow {
	out := make([]StrategySummaryRow, 0, len(rows))
	for _, row := range rows {
		if row.Trades < minSuggestionTrades {
			continue
		}
		if row.ProfitFactor < minSuggestionPF {
			continue
		}
		if math.Abs(row.MaxDrawdown) > maxSuggestionDD {
			continue
		}
		out = append(out, row)
	}
	return out
}

func buildSuggestedPortfolio(name string, rows []StrategySummaryRow, candidates []StrategySummaryRow, relaxed bool, baseRationale string) SuggestedPortfolio {
	strategies, score, cagr, maxDD := buildSuggestedStrategies(rows)
	rationale := fmt.Sprintf("%s Constraints: trades≥%d, PF≥%.2f, maxDD≤%.0f%%.", baseRationale, minSuggestionTrades, minSuggestionPF, maxSuggestionDD*100)
	if relaxed {
		rationale = baseRationale + " Not enough strategies met constraints, so filters were relaxed."
	}
	return SuggestedPortfolio{
		Name:           name,
		Score:          score,
		ExpectedCAGR:   cagr,
		MaxDrawdown:    maxDD,
		Strategies:     strategies,
		Rationale:      rationale,
		CandidateCount: len(candidates),
	}
}

func buildSuggestedStrategies(rows []StrategySummaryRow) ([]SuggestedStrategy, float64, float64, float64) {
	if len(rows) == 0 {
		return nil, 0, 0, 0
	}

	minScore := rows[0].Scorecard
	maxScore := rows[0].Scorecard
	for _, row := range rows[1:] {
		if row.Scorecard < minScore {
			minScore = row.Scorecard
		}
		if row.Scorecard > maxScore {
			maxScore = row.Scorecard
		}
	}

	rawWeights := make([]float64, len(rows))
	ddMagnitudes := make([]float64, len(rows))
	weightSum := 0.0
	ddSum := 0.0
	for i, row := range rows {
		w := row.Allocation
		if w < 1 {
			w = 1
		}
		if maxScore > minScore {
			normalizedScore := (row.Scorecard - minScore) / (maxScore - minScore)
			w *= 1 + (normalizedScore * 2.0)
		}
		rawWeights[i] = w
		weightSum += w
		ddMag := math.Abs(row.MaxDrawdown)
		ddMagnitudes[i] = ddMag
		ddSum += ddMag
	}

	if weightSum <= 0 {
		for i := range rawWeights {
			rawWeights[i] = 1.0
		}
		weightSum = float64(len(rawWeights))
	}

	strategies := make([]SuggestedStrategy, 0, len(rows))
	weightedScore := 0.0
	weightedCAGR := 0.0
	worstDD := 0.0
	for i, row := range rows {
		normalizedWeight := rawWeights[i] / weightSum
		ddContrib := 0.0
		if ddSum > 0 {
			ddContrib = ddMagnitudes[i] / ddSum
		}
		strategies = append(strategies, SuggestedStrategy{
			Strategy:    row.Strategy,
			Weight:      rawWeights[i],
			DDContrib:   ddContrib,
			Scorecard:   row.Scorecard,
			CAGR:        row.CAGR,
			MaxDrawdown: row.MaxDrawdown,
			Trades:      row.Trades,
		})
		weightedScore += row.Scorecard * normalizedWeight
		weightedCAGR += row.CAGR * normalizedWeight
		if math.Abs(row.MaxDrawdown) > math.Abs(worstDD) {
			worstDD = row.MaxDrawdown
		}
	}

	return strategies, weightedScore, weightedCAGR, worstDD
}
