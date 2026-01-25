package trades

import "time"

type Trade struct {
	ID              string    `json:"id"`
	Index           int       `json:"index"`
	Portfolio       string    `json:"portfolio"`
	Strategy        string    `json:"strategy"`
	Symbol          string    `json:"symbol"`
	Timeframe       int       `json:"timeframe"`
	Direction       string    `json:"direction"`
	Size            float64   `json:"size"`
	EntryDatetime   time.Time `json:"entry_datetime"`
	ExitDatetime    time.Time `json:"exit_datetime"`
	DurationMinutes int       `json:"duration_minutes"`
	EntryPrice      float64   `json:"entry_price"`
	ExitPrice       float64   `json:"exit_price"`
	NetPnL          float64   `json:"net_pnl"`
	Profit          float64   `json:"profit"`
	FavExcursion    float64   `json:"fav_excursion"`
	AdvExcursion    float64   `json:"adv_excursion"`
}

type RowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

type ImportSummary struct {
	Imported   int        `json:"imported"`
	Duplicates int        `json:"duplicates"`
	Skipped    int        `json:"skipped"`
	Errors     []RowError `json:"errors"`
}

type StrategyMetadata struct {
	Strategy  string
	Symbol    string
	Timeframe int
}
