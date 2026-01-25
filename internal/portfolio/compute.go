package portfolio

import "time"

type Trade struct {
	ExitTime time.Time
	NetPnL   float64
}

type Result struct {
	StartingCapital float64
	EndingCapital   float64

	TotalNetPnL float64
	MaxDrawdown float64

	EquityCurve []EquityPoint
}

type EquityPoint struct {
	Time   time.Time
	Equity float64
}

func Compute(startingCapital float64, trades []Trade) Result {
	result := Result{
		StartingCapital: startingCapital,
		EndingCapital:   startingCapital,
	}

	if len(trades) == 0 {
		return result
	}

	sorted := make([]Trade, len(trades))
	copy(sorted, trades)

	sortTradesByExitTime(sorted)

	equity := startingCapital
	peak := startingCapital
	maxDrawdown := 0.0

	for _, trade := range sorted {
		equity += trade.NetPnL

		if equity > peak {
			peak = equity
		}

		drawdown := equity - peak
		if drawdown < maxDrawdown {
			maxDrawdown = drawdown
		}

		result.EquityCurve = append(result.EquityCurve, EquityPoint{
			Time:   trade.ExitTime,
			Equity: equity,
		})
	}

	result.EndingCapital = equity
	result.TotalNetPnL = equity - startingCapital
	result.MaxDrawdown = maxDrawdown

	return result
}