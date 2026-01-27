package main

import (
	"time"
)

type DurationBucket struct {
	Label    string  // e.g. "0-15m", "15-30m", "1-2h", etc.
	AvgPnL   float64 // average PnL for this bucket
	Count    int     // number of trades in this bucket
	WinCount int     // number of profitable trades
}

func buildTradeDurationBuckets(trades []AnalysisTradeRow) []DurationBucket {
	// Define duration buckets in minutes and labels
	buckets := []struct {
		min   int
		max   int
		label string
	}{
		{0, 15, "0–15m"},
		{15, 30, "15–30m"},
		{30, 60, "30–60m"},
		{60, 120, "1–2h"},
		{120, 240, "2–4h"},
		{240, 480, "4–8h"},
		{480, 1440, "8–24h"},
		{1440, 100000, "24h+"},
	}

	// Prepare result buckets
	result := make([]DurationBucket, len(buckets))
	for i, b := range buckets {
		result[i].Label = b.label
	}

	loc, _ := time.LoadLocation("America/Chicago")
	for _, t := range trades {
		entry, err1 := time.ParseInLocation("2006-01-02 15:04:05", t.EntryTime, loc)
		if err1 != nil {
			entry, err1 = time.ParseInLocation("2006-01-02 15:04", t.EntryTime, loc)
			if err1 != nil {
				continue
			}
		}
		exit, err2 := time.ParseInLocation("2006-01-02 15:04:05", t.ExitTime, loc)
		if err2 != nil {
			exit, err2 = time.ParseInLocation("2006-01-02 15:04", t.ExitTime, loc)
			if err2 != nil {
				continue
			}
		}
		durationMin := int(exit.Sub(entry).Minutes())
		for i, b := range buckets {
			if durationMin >= b.min && durationMin < b.max {
				result[i].Count++
				result[i].AvgPnL += t.WeightedPnL
				if t.WeightedPnL > 0 {
					result[i].WinCount++
				}
				break
			}
		}
	}
	// Finalize averages
	for i := range result {
		if result[i].Count > 0 {
			result[i].AvgPnL /= float64(result[i].Count)
		}
	}
	return result
}
