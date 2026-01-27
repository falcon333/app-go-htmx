package main

import (
	"time"
)

type HourlyHeatmapCell struct {
	Hour     int     // 0-23
	AvgPnL   float64 // average PnL for this hour
	Count    int     // number of trades in this hour
	WinCount int     // number of profitable trades
}

type HourlyHeatmapRow struct {
	Label string
	Cells [24]HourlyHeatmapCell
}

type HourlyHeatmapTable struct {
	Rows []HourlyHeatmapRow
}

// buildHourlyHeatmap aggregates trades by entry hour (Chicago time)
func buildHourlyHeatmap(trades []AnalysisTradeRow) HourlyHeatmapTable {
	// Map: label (e.g. "All" or strategy name) -> [24]cell
	rows := []HourlyHeatmapRow{}
	// For now, just one row: "All"
	var cells [24]HourlyHeatmapCell
	loc, _ := time.LoadLocation("America/Chicago")
	for i := 0; i < 24; i++ {
		cells[i].Hour = i
	}
	for _, t := range trades {
		entry, err := time.ParseInLocation("2006-01-02 15:04:05", t.EntryTime, loc)
		if err != nil {
			entry, err = time.ParseInLocation("2006-01-02 15:04", t.EntryTime, loc)
			if err != nil {
				continue
			}
		}
		hour := entry.Hour()
		cells[hour].Count++
		cells[hour].AvgPnL += t.WeightedPnL
		if t.WeightedPnL > 0 {
			cells[hour].WinCount++
		}
	}
	for i := 0; i < 24; i++ {
		if cells[i].Count > 0 {
			cells[i].AvgPnL /= float64(cells[i].Count)
		}
	}
	rows = append(rows, HourlyHeatmapRow{
		Label: "All",
		Cells: cells,
	})
	return HourlyHeatmapTable{Rows: rows}
}
