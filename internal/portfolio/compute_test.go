package portfolio

import (
	"testing"
	"time"
)

func TestCompute_EmptyTrades(t *testing.T) {
	startingCapital := 10_000.0

	result := Compute(startingCapital, nil)

	if result.StartingCapital != startingCapital {
		t.Fatalf("expected starting capital %.2f, got %.2f",
			startingCapital, result.StartingCapital)
	}

	if result.EndingCapital != startingCapital {
		t.Fatalf("expected ending capital %.2f, got %.2f",
			startingCapital, result.EndingCapital)
	}

	if result.TotalNetPnL != 0 {
		t.Fatalf("expected total PnL 0, got %.2f", result.TotalNetPnL)
	}

	if result.MaxDrawdown != 0 {
		t.Fatalf("expected max drawdown 0, got %.2f", result.MaxDrawdown)
	}

	if len(result.EquityCurve) != 0 {
		t.Fatalf("expected empty equity curve")
	}
}

func TestCompute_SingleTradeProfit(t *testing.T) {
	startingCapital := 10_000.0

	trades := []Trade{
		{
			ExitTime: mustTime("2024-01-01"),
			NetPnL:   500,
		},
	}

	result := Compute(startingCapital, trades)

	assertFloat(t, result.EndingCapital, 10_500)
	assertFloat(t, result.TotalNetPnL, 500)
	assertFloat(t, result.MaxDrawdown, 0)

	if len(result.EquityCurve) != 1 {
		t.Fatalf("expected 1 equity point, got %d", len(result.EquityCurve))
	}
}

func TestCompute_Drawdown(t *testing.T) {
	startingCapital := 10_000.0

	trades := []Trade{
		{ExitTime: mustTime("2024-01-01"), NetPnL: 1_000},  // 11,000
		{ExitTime: mustTime("2024-01-02"), NetPnL: -2_000}, // 9,000 (−2,000 DD)
		{ExitTime: mustTime("2024-01-03"), NetPnL: 500},   // 9,500
	}

	result := Compute(startingCapital, trades)

	assertFloat(t, result.EndingCapital, 9_500)
	assertFloat(t, result.TotalNetPnL, -500)
	assertFloat(t, result.MaxDrawdown, -2_000)
}

func TestCompute_UnorderedTradesAreSorted(t *testing.T) {
	startingCapital := 10_000.0

	trades := []Trade{
		{ExitTime: mustTime("2024-01-03"), NetPnL: 500},
		{ExitTime: mustTime("2024-01-01"), NetPnL: 1_000},
		{ExitTime: mustTime("2024-01-02"), NetPnL: -2_000},
	}

	result := Compute(startingCapital, trades)

	if len(result.EquityCurve) != 3 {
		t.Fatalf("expected 3 equity points")
	}

	// Equity after sorted trades:
	// 10,000 + 1,000 = 11,000
	// 11,000 - 2,000 = 9,000
	// 9,000 + 500   = 9,500

	assertFloat(t, result.EquityCurve[0].Equity, 11_000)
	assertFloat(t, result.EquityCurve[1].Equity, 9_000)
	assertFloat(t, result.EquityCurve[2].Equity, 9_500)
}

func mustTime(value string) time.Time {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return t
}

func assertFloat(t *testing.T, got, want float64) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %.2f, got %.2f", want, got)
	}
}