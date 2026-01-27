package main

import (
	"fmt"
	"strings"
	"time"
)

type TimeHeatmapCell struct {
	AvgPnL   float64 // average PnL for this bucket
	Count    int     // number of trades in this bucket
	WinCount int     // number of profitable trades
}

type TimeHeatmapRow struct {
	Label string
	Cells []TimeHeatmapCell
}

type TimeHeatmapTable struct {
	Interval     string
	ColumnLabels []string
	Rows         []TimeHeatmapRow
}

func normalizeHeatmapInterval(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "daily":
		return "daily"
	case "weekly":
		return "weekly"
	case "monthly":
		return "monthly"
	default:
		return "hourly"
	}
}

// buildTimeHeatmap aggregates trades by entry time bucket (Chicago time)
func buildTimeHeatmap(trades []AnalysisTradeRow, interval string) TimeHeatmapTable {
	interval = normalizeHeatmapInterval(interval)
	labels := heatmapColumnLabels(interval)
	cells := make([]TimeHeatmapCell, len(labels))
	loc, _ := time.LoadLocation("America/Chicago")

	for _, t := range trades {
		entry, ok := parseHeatmapEntryTime(t, loc)
		if !ok {
			continue
		}
		idx := heatmapBucketIndex(interval, entry)
		if idx < 0 || idx >= len(cells) {
			continue
		}
		cells[idx].Count++
		cells[idx].AvgPnL += t.WeightedPnL
		if t.WeightedPnL > 0 {
			cells[idx].WinCount++
		}
	}

	for i := range cells {
		if cells[i].Count > 0 {
			cells[i].AvgPnL /= float64(cells[i].Count)
		}
	}

	rows := []TimeHeatmapRow{}
	if len(trades) > 0 {
		rows = append(rows, TimeHeatmapRow{
			Label: "All",
			Cells: cells,
		})
	}

	return TimeHeatmapTable{
		Interval:     interval,
		ColumnLabels: labels,
		Rows:         rows,
	}
}

func heatmapColumnLabels(interval string) []string {
	switch interval {
	case "daily":
		return []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	case "weekly":
		labels := make([]string, 53)
		for i := range labels {
			labels[i] = fmt.Sprintf("W%02d", i+1)
		}
		return labels
	case "monthly":
		return []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	default:
		labels := make([]string, 24)
		for i := range labels {
			labels[i] = fmt.Sprintf("%02d:00", i)
		}
		return labels
	}
}

func heatmapBucketIndex(interval string, t time.Time) int {
	switch interval {
	case "daily":
		weekday := int(t.Weekday())
		return (weekday + 6) % 7 // Monday=0
	case "weekly":
		_, week := t.ISOWeek()
		if week < 1 {
			week = 1
		}
		if week > 53 {
			week = 53
		}
		return week - 1
	case "monthly":
		return int(t.Month()) - 1
	default:
		return t.Hour()
	}
}

func parseHeatmapEntryTime(t AnalysisTradeRow, loc *time.Location) (time.Time, bool) {
	if !t.EntryTimeSort.IsZero() {
		return t.EntryTimeSort.In(loc), true
	}

	raw := strings.TrimSpace(t.EntryTime)
	if raw == "" {
		return time.Time{}, false
	}

	layouts := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
