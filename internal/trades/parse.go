package trades

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

type parsedRow struct {
	rowIndex int
	date     time.Time
	signal   string
	tp       string
	price    float64
	size     float64
	favExc   float64
	advExc   float64
}

func ParseTVCSV(r io.Reader, loc *time.Location) ([]Trade, []RowError, error) {
	if loc == nil {
		loc = time.UTC
	}

	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("read csv: %w", err)
	}

	if len(records) < 2 {
		return nil, nil, fmt.Errorf("csv has no data rows")
	}

	headerIndex := make(map[string]int, len(records[0]))
	for i, h := range records[0] {
		name := normalizeHeader(h)
		if name != "" {
			headerIndex[name] = i
		}
	}

	dateAliases := []string{"date", "date and time", "datetime", "date time", "date/time", "time"}
	typeAliases := []string{"type"}
	signalAliases := []string{"signal"}
	priceAliases := []string{"price", "price usd", "price (usd)", "price $", "price ($)"}
	sizeAliases := []string{"size", "position size", "qty", "quantity", "contracts"}
	favExcAliases := []string{"fav exc usd", "favorable excursion", "fav excursion", "fav exc", "fav exc $", "fav exc (usd)", "favorable excursion usd"}
	advExcAliases := []string{"adv exc usd", "adverse excursion", "adv excursion", "adv exc", "adv exc $", "adv exc (usd)", "adverse excursion usd"}

	if !headerExists(headerIndex, dateAliases) {
		return nil, nil, fmt.Errorf("missing required header: Date or Date and time")
	}
	if !headerExists(headerIndex, priceAliases) {
		return nil, nil, fmt.Errorf("missing required header: Price or Price USD")
	}
	if !(headerExists(headerIndex, typeAliases) || headerExists(headerIndex, signalAliases)) {
		return nil, nil, fmt.Errorf("missing required header: Type or Signal")
	}

	rows := make([]parsedRow, 0, len(records)-1)
	var rowErrors []RowError

	for i := 1; i < len(records); i++ {
		row := records[i]

		dtRaw := getValue(row, headerIndex, dateAliases)
		tpRaw := getValue(row, headerIndex, typeAliases)
		signalRaw := getValue(row, headerIndex, signalAliases)
		priceRaw := getValue(row, headerIndex, priceAliases)
		sizeRaw := getValue(row, headerIndex, sizeAliases)
		favExcRaw := getValue(row, headerIndex, favExcAliases)
		advExcRaw := getValue(row, headerIndex, advExcAliases)

		dt, ok := parseDateUTC(dtRaw, loc)
		if !ok {
			rowErrors = append(rowErrors, RowError{Row: i + 1, Message: "invalid date"})
			continue
		}

		price, priceOk := parseNumber(priceRaw)
		if !priceOk || !validPrice(price) {
			rowErrors = append(rowErrors, RowError{Row: i + 1, Message: "invalid price"})
			continue
		}

		size, sizeOk := parseNumber(sizeRaw)
		if !sizeOk || size == 0 {
			size = 1
		}

		favExc, _ := parseNumber(favExcRaw)
		advExc, _ := parseNumber(advExcRaw)

		rows = append(rows, parsedRow{
			rowIndex: i + 1,
			date:     dt,
			signal:   strings.ToUpper(strings.TrimSpace(signalRaw)),
			tp:       strings.ToUpper(strings.TrimSpace(tpRaw)),
			price:    price,
			size:     size,
			favExc:   favExc,
			advExc:   advExc,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].date.Before(rows[j].date)
	})

	var trades []Trade
	var open *Trade

	for _, row := range rows {
		datetime := row.date
		price := row.price
		size := row.size
		favExc := row.favExc
		advExc := row.advExc

		signalKind, direction := classifySignal(row.tp, row.signal)
		if signalKind == "" {
			rowErrors = append(rowErrors, RowError{Row: row.rowIndex, Message: "unrecognized signal/type"})
			continue
		}
		if direction == "" {
			rowErrors = append(rowErrors, RowError{Row: row.rowIndex, Message: "missing direction"})
			continue
		}

		if signalKind == "ENTRY" {
			if open != nil {
				rowErrors = append(rowErrors, RowError{Row: row.rowIndex, Message: "entry while position open"})
				continue
			}

			open = &Trade{
				Direction:     direction,
				EntryDatetime: datetime,
				EntryPrice:    price,
				Size:          size,
			}
			continue
		}

		if signalKind == "EXIT" {
			if open == nil {
				rowErrors = append(rowErrors, RowError{Row: row.rowIndex, Message: "exit without open"})
				continue
			}

			exitDatetime := datetime
			exitPrice := price

			duration := int(exitDatetime.Sub(open.EntryDatetime).Minutes())
			if duration < 0 {
				rowErrors = append(rowErrors, RowError{Row: row.rowIndex, Message: "exit before entry"})
				open = nil
				continue
			}

			multiplier := 1.0
			if open.Direction == "SHORT" {
				multiplier = -1.0
			}

			netPnL := (exitPrice - open.EntryPrice) * open.Size * multiplier

			trades = append(trades, Trade{
				Index:           len(trades) + 1,
				Direction:       open.Direction,
				Size:            open.Size,
				EntryDatetime:   open.EntryDatetime,
				ExitDatetime:    exitDatetime,
				DurationMinutes: duration,
				EntryPrice:      open.EntryPrice,
				ExitPrice:       exitPrice,
				NetPnL:          netPnL,
				Profit:          netPnL,
				FavExcursion:    favExc,
				AdvExcursion:    advExc,
			})

			open = nil
		}
	}

	if open != nil {
		rowErrors = append(rowErrors, RowError{Row: 0, Message: "open position without exit"})
	}

	return trades, rowErrors, nil
}

func classifySignal(tpRaw, signalRaw string) (kind string, direction string) {
	src := tpRaw
	if src == "" {
		src = signalRaw
	}

	up := strings.ToUpper(strings.TrimSpace(src))

	if strings.Contains(up, "ENTRY") {
		kind = "ENTRY"
	} else if strings.Contains(up, "EXIT") {
		kind = "EXIT"
	} else {
		return "", ""
	}

	if strings.Contains(up, "LONG") {
		direction = "LONG"
	} else if strings.Contains(up, "SHORT") {
		direction = "SHORT"
	}

	return kind, direction
}

func normalizeHeader(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "\ufeff"))
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)

	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSpace = false
			continue
		}
		if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}

	return strings.TrimSpace(b.String())
}

func headerExists(headerIndex map[string]int, aliases []string) bool {
	for _, a := range aliases {
		if _, ok := headerIndex[normalizeHeader(a)]; ok {
			return true
		}
	}
	return false
}

func getValue(row []string, headerIndex map[string]int, aliases []string) string {
	for _, a := range aliases {
		if idx, ok := headerIndex[normalizeHeader(a)]; ok {
			if idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
		}
	}
	return ""
}

func parseNumber(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	clean := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return -1
	}, value)

	parsed, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func validPrice(price float64) bool {
	return price != 0
}

func parseDateUTC(value string, loc *time.Location) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}

	if loc == nil {
		loc = time.UTC
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"1/2/2006 15:04:05",
		"1/2/2006 15:04",
		"1/2/2006 3:04:05 PM",
		"1/2/2006 3:04 PM",
	}

	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t.UTC(), true
		}
	}

	if t, err := time.Parse(time.RFC1123, value); err == nil {
		return t.UTC(), true
	}

	return time.Time{}, false
}
