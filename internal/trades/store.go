package trades

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Store struct {
	Path string
}

func DefaultStore() Store {
	dataDir := os.Getenv("TRADES_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	return Store{Path: filepath.Join(dataDir, "trades.json")}
}

func (s Store) Load() ([]Trade, error) {
	if _, err := os.Stat(s.Path); err != nil {
		if os.IsNotExist(err) {
			return []Trade{}, nil
		}
		return nil, err
	}

	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return []Trade{}, nil
	}

	var trades []Trade
	if err := json.Unmarshal(data, &trades); err != nil {
		return nil, err
	}

	return trades, nil
}

func (s Store) Save(trades []Trade) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(trades, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.Path, data, 0o644)
}

func InsertTradesDedup(ctx context.Context, store Store, trades []Trade) (ImportSummary, error) {
	existing, err := store.Load()
	if err != nil {
		return ImportSummary{}, err
	}

	seen := make(map[string]struct{}, len(existing))
	for _, t := range existing {
		if t.ID != "" {
			seen[t.ID] = struct{}{}
		}
	}

	summary := ImportSummary{}
	for _, trade := range trades {
		if err := ctx.Err(); err != nil {
			return summary, err
		}

		trade.ID = ComputeTradeID(trade)
		if _, ok := seen[trade.ID]; ok {
			summary.Duplicates++
			continue
		}

		existing = append(existing, trade)
		seen[trade.ID] = struct{}{}
		summary.Imported++
	}

	if err := store.Save(existing); err != nil {
		return summary, err
	}

	return summary, nil
}

func ComputeTradeID(trade Trade) string {
	input := trade.Strategy + "|" + trade.Portfolio + "|" + trade.Symbol + "|" +
		trade.Direction + "|" + trade.EntryDatetime.UTC().Format(time.RFC3339Nano) + "|" +
		trade.ExitDatetime.UTC().Format(time.RFC3339Nano) + "|" +
		formatFloat(trade.EntryPrice) + "|" + formatFloat(trade.ExitPrice) + "|" +
		formatFloat(trade.Size)

	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}