package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

type MonthlyTable struct {
	MonthLabels []string
	Rows        []MonthlyRow
	Totals      MonthlyRow
}

type MonthlyRow struct {
	Year      string
	Cells     []MonthlyCell
	YearTotal MonthlyCell
}

type MonthlyCell struct {
	Display  string
	Class    string
	HasValue bool
}

type monthlyBucket struct {
	startEq *float64
	endEq   *float64
	usd     float64
}

const (
	defaultMonthlyPctMax = 0.10
	defaultStartBalance  = 10000.0
)

func buildMonthlyReturnTables(trades []SummaryTradeRow, startingCapital float64) (MonthlyTable, MonthlyTable) {
	monthLabels := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	pctTable := MonthlyTable{MonthLabels: monthLabels}
	usdTable := MonthlyTable{MonthLabels: monthLabels}
	if len(trades) == 0 {
		return pctTable, usdTable
	}

	sorted := make([]SummaryTradeRow, len(trades))
	copy(sorted, trades)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Dt.Before(sorted[j].Dt)
	})

	if !isFinite(startingCapital) || startingCapital <= 0 {
		startingCapital = defaultStartBalance
	}
	runningEq := startingCapital

	ym := make(map[string]*monthlyBucket)
	for _, t := range sorted {
		if t.Dt.IsZero() {
			continue
		}
		pnl := t.PnLAbs
		if !isFinite(pnl) {
			continue
		}

		y, m := t.Dt.Year(), int(t.Dt.Month())
		key := fmt.Sprintf("%04d-%02d", y, m)
		bucket := ym[key]
		if bucket == nil {
			bucket = &monthlyBucket{}
			ym[key] = bucket
		}
		if bucket.startEq == nil {
			start := runningEq
			bucket.startEq = &start
		}

		runningEq += pnl
		end := runningEq
		bucket.endEq = &end
		bucket.usd += pnl
	}

	if len(ym) == 0 {
		return pctTable, usdTable
	}

	years := make(map[int]map[int]*monthlyBucket)
	for key, bucket := range ym {
		if len(key) < 7 {
			continue
		}
		y, errY := strconv.Atoi(key[:4])
		m, errM := strconv.Atoi(key[5:7])
		if errY != nil || errM != nil {
			continue
		}
		if years[y] == nil {
			years[y] = make(map[int]*monthlyBucket)
		}
		years[y][m] = bucket
	}

	yearList := make([]int, 0, len(years))
	for y := range years {
		yearList = append(yearList, y)
	}
	sort.Ints(yearList)

	pctMax := defaultMonthlyPctMax
	usdMax := math.Abs(startingCapital * pctMax)
	if usdMax <= 0 {
		usdMax = 1000.0
	}

	monthPctSum := make([]float64, 12)
	monthPctCount := make([]int, 12)
	monthPctWins := make([]int, 12)
	monthUsdSum := make([]float64, 12)
	yearPctSum := 0.0
	yearPctCount := 0
	yearPctWins := 0
	yearUsdSum := 0.0

	for _, y := range yearList {
		pctRow := MonthlyRow{Year: strconv.Itoa(y), Cells: make([]MonthlyCell, 0, 12)}
		usdRow := MonthlyRow{Year: strconv.Itoa(y), Cells: make([]MonthlyCell, 0, 12)}

		yearStart := 0.0
		yearEnd := 0.0
		yearStartSet := false
		yearUsd := 0.0

		for mi := 1; mi <= 12; mi++ {
			bucket := years[y][mi]
			if bucket != nil && bucket.startEq != nil && bucket.endEq != nil && *bucket.startEq != 0 {
				pct := (*bucket.endEq / *bucket.startEq) - 1
				pctRow.Cells = append(pctRow.Cells, makePctCell(pct, pctMax))
				monthPctSum[mi-1] += pct
				monthPctCount[mi-1]++
				if pct > 0 {
					monthPctWins[mi-1]++
				}
				if !yearStartSet {
					yearStart = *bucket.startEq
					yearStartSet = true
				}
				yearEnd = *bucket.endEq
			} else {
				pctRow.Cells = append(pctRow.Cells, MonthlyCell{})
			}

			if bucket != nil && isFinite(bucket.usd) {
				usdRow.Cells = append(usdRow.Cells, makeUsdCell(bucket.usd, usdMax))
				yearUsd += bucket.usd
				monthUsdSum[mi-1] += bucket.usd
			} else {
				usdRow.Cells = append(usdRow.Cells, MonthlyCell{})
			}
		}

		if yearStartSet && yearEnd != 0 && yearStart != 0 {
			yPct := (yearEnd / yearStart) - 1
			pctRow.YearTotal = makePctCell(yPct, pctMax)
			yearPctSum += yPct
			yearPctCount++
			if yPct > 0 {
				yearPctWins++
			}
		}
		if yearStartSet {
			usdRow.YearTotal = makeUsdCell(yearUsd, usdMax)
			yearUsdSum += yearUsd
		}

		pctTable.Rows = append(pctTable.Rows, pctRow)
		usdTable.Rows = append(usdTable.Rows, usdRow)
	}

	if len(pctTable.Rows) > 0 {
		totalPct := MonthlyRow{Year: "Totals", Cells: make([]MonthlyCell, 0, 12)}
		for i := 0; i < 12; i++ {
			if monthPctCount[i] == 0 {
				totalPct.Cells = append(totalPct.Cells, MonthlyCell{})
				continue
			}
			avg := monthPctSum[i] / float64(monthPctCount[i])
			cell := makePctCell(avg, pctMax)
			cell.Display = fmt.Sprintf("%.2f%% (%d/%d)", avg*100, monthPctWins[i], monthPctCount[i])
			totalPct.Cells = append(totalPct.Cells, cell)
		}
		if yearPctCount > 0 {
			avgYear := yearPctSum / float64(yearPctCount)
			cell := makePctCell(avgYear, pctMax)
			cell.Display = fmt.Sprintf("%.2f%% (%d/%d)", avgYear*100, yearPctWins, yearPctCount)
			totalPct.YearTotal = cell
		}
		pctTable.Totals = totalPct
	}

	if len(usdTable.Rows) > 0 {
		totalUsd := MonthlyRow{Year: "Totals", Cells: make([]MonthlyCell, 0, 12)}
		for i := 0; i < 12; i++ {
			cell := makeUsdCell(monthUsdSum[i], usdMax)
			if monthUsdSum[i] == 0 {
				cell = MonthlyCell{}
			}
			totalUsd.Cells = append(totalUsd.Cells, cell)
		}
		totalUsd.YearTotal = makeUsdCell(yearUsdSum, usdMax)
		usdTable.Totals = totalUsd
	}

	return pctTable, usdTable
}

func makePctCell(value float64, maxAbs float64) MonthlyCell {
	if !isFinite(value) {
		return MonthlyCell{}
	}
	return MonthlyCell{
		Display:  fmt.Sprintf("%.2f%%", value*100),
		Class:    heatmapClass(value, maxAbs),
		HasValue: true,
	}
}

func makeUsdCell(value float64, maxAbs float64) MonthlyCell {
	if !isFinite(value) {
		return MonthlyCell{}
	}
	display := fmt.Sprintf("$%.2f", value)
	if value < 0 {
		display = fmt.Sprintf("-$%.2f", math.Abs(value))
	}
	return MonthlyCell{
		Display:  display,
		Class:    heatmapClass(value, maxAbs),
		HasValue: true,
	}
}

func heatmapClass(value float64, maxAbs float64) string {
	if !isFinite(value) || maxAbs <= 0 {
		return ""
	}
	if value == 0 {
		return "heatmap-zero"
	}

	ratio := math.Abs(value) / maxAbs
	level := 1
	if ratio >= 0.66 {
		level = 3
	} else if ratio >= 0.33 {
		level = 2
	}

	if value > 0 {
		return fmt.Sprintf("heatmap-pos-%d", level)
	}
	return fmt.Sprintf("heatmap-neg-%d", level)
}

func monthKey(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
}
