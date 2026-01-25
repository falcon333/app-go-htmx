package trades

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type PortfolioFilters struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Balance   float64 `json:"balance"`
	RangeMode string  `json:"range_mode"`
}

type PortfolioMapping struct {
	Strategy    string  `json:"strategy"`
	Enabled     bool    `json:"enabled"`
	Weight      float64 `json:"weight"`
	RatioMode   bool    `json:"ratio_mode"`
	RatioUnit   float64 `json:"ratio_unit"`
	RatioAmount float64 `json:"ratio_amount"`
	Notes       string  `json:"notes"`
}

type PortfolioMeta struct {
	StrategyCount int `json:"strategy_count"`
}

type Portfolio struct {
	Name      string             `json:"name"`
	BaseName  string             `json:"base_name"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Filters   PortfolioFilters   `json:"filters"`
	Mappings  []PortfolioMapping `json:"mappings"`
	Meta      PortfolioMeta      `json:"meta"`
}

type PortfolioStore struct {
	Path string
}

func DefaultPortfolioStore() PortfolioStore {
	dataDir := os.Getenv("TRADES_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	return PortfolioStore{Path: filepath.Join(dataDir, "portfolios.json")}
}

func (s PortfolioStore) LoadAll() ([]Portfolio, error) {
	if _, err := os.Stat(s.Path); err != nil {
		if os.IsNotExist(err) {
			return []Portfolio{}, nil
		}
		return nil, err
	}

	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return []Portfolio{}, nil
	}

	var portfolios []Portfolio
	if err := json.Unmarshal(data, &portfolios); err != nil {
		return nil, err
	}

	return portfolios, nil
}

func (s PortfolioStore) SaveAll(portfolios []Portfolio) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(portfolios, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.Path, data, 0o644)
}

func ListSavedPortfolios() ([]Portfolio, error) {
	store := DefaultPortfolioStore()
	portfolios, err := store.LoadAll()
	if err != nil {
		return nil, err
	}

	sort.SliceStable(portfolios, func(i, j int) bool {
		return strings.ToLower(portfolios[i].Name) < strings.ToLower(portfolios[j].Name)
	})

	return portfolios, nil
}

func LoadPortfolioByName(name string) (*Portfolio, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil
	}

	store := DefaultPortfolioStore()
	portfolios, err := store.LoadAll()
	if err != nil {
		return nil, err
	}

	for _, p := range portfolios {
		if strings.EqualFold(strings.TrimSpace(p.Name), trimmed) {
			copy := p
			return &copy, nil
		}
	}

	return nil, nil
}

func SavePortfolio(input Portfolio, overwrite bool) (Portfolio, bool, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Portfolio{}, false, errors.New("portfolio name is required")
	}

	store := DefaultPortfolioStore()
	portfolios, err := store.LoadAll()
	if err != nil {
		return Portfolio{}, false, err
	}

	now := time.Now().UTC()
	exists := false
	for i, p := range portfolios {
		if strings.EqualFold(strings.TrimSpace(p.Name), name) {
			exists = true
			if !overwrite {
				return Portfolio{}, true, nil
			}
			input.CreatedAt = p.CreatedAt
			input.UpdatedAt = now
			portfolios[i] = input
			if err := store.SaveAll(portfolios); err != nil {
				return Portfolio{}, true, err
			}
			return input, true, nil
		}
	}

	input.CreatedAt = now
	input.UpdatedAt = now
	portfolios = append(portfolios, input)
	if err := store.SaveAll(portfolios); err != nil {
		return Portfolio{}, exists, err
	}

	return input, false, nil
}

func DeletePortfolioByName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil
	}

	store := DefaultPortfolioStore()
	portfolios, err := store.LoadAll()
	if err != nil {
		return err
	}

	filtered := make([]Portfolio, 0, len(portfolios))
	for _, p := range portfolios {
		if strings.EqualFold(strings.TrimSpace(p.Name), trimmed) {
			continue
		}
		filtered = append(filtered, p)
	}

	return store.SaveAll(filtered)
}
