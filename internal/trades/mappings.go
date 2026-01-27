package trades

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StrategyPortfolioMapping struct {
	ID            string    `json:"id"`
	StrategyKey   string    `json:"strategy_key"`
	PortfolioName string    `json:"portfolio_name"`
	Enabled       bool      `json:"enabled"`
	Weight        float64   `json:"weight"`
	RatioMode     bool      `json:"ratio_mode"`
	RatioUnit     float64   `json:"ratio_unit"`
	RatioAmount   float64   `json:"ratio_amount"`
	Notes         string    `json:"notes"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type MappingInput struct {
	StrategyKey string
	Enabled     bool
	Weight      float64
	RatioMode   bool
	RatioUnit   float64
	RatioAmount float64
	Notes       string
}

type MappingStore struct {
	Path string
}

func DefaultMappingStore() MappingStore {
	return MappingStore{Path: filepath.Join("internal", "trades", "mappings.json")}
}

func (s MappingStore) Load() ([]StrategyPortfolioMapping, error) {
	if _, err := os.Stat(s.Path); err != nil {
		if os.IsNotExist(err) {
			return []StrategyPortfolioMapping{}, nil
		}
		return nil, err
	}

	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return []StrategyPortfolioMapping{}, nil
	}

	var mappings []StrategyPortfolioMapping
	if err := json.Unmarshal(data, &mappings); err != nil {
		return nil, err
	}

	return mappings, nil
}

func (s MappingStore) Save(mappings []StrategyPortfolioMapping) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.Path, data, 0o644)
}

func ListStrategies(store Store) ([]string, error) {
	tradesList, err := store.Load()
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{})
	for _, t := range tradesList {
		key := strings.TrimSpace(t.Strategy)
		if key == "" {
			continue
		}
		set[key] = struct{}{}
	}

	strategies := make([]string, 0, len(set))
	for k := range set {
		strategies = append(strategies, k)
	}
	sort.Strings(strategies)

	return strategies, nil
}

func ListMappings(portfolioName string) ([]StrategyPortfolioMapping, error) {
	store := DefaultMappingStore()
	mappings, err := store.Load()
	if err != nil {
		return nil, err
	}

	portfolio := strings.TrimSpace(portfolioName)
	if portfolio == "" {
		return []StrategyPortfolioMapping{}, nil
	}

	out := make([]StrategyPortfolioMapping, 0)
	for _, m := range mappings {
		if strings.EqualFold(m.PortfolioName, portfolio) {
			out = append(out, m)
		}
	}

	return out, nil
}

func ListPortfolios() ([]string, error) {
	store := DefaultMappingStore()
	mappings, err := store.Load()
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{})
	for _, m := range mappings {
		name := strings.TrimSpace(m.PortfolioName)
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}

	portfolios := make([]string, 0, len(set))
	for k := range set {
		portfolios = append(portfolios, k)
	}
	sort.Strings(portfolios)

	return portfolios, nil
}

func ListPortfoliosFromTrades(store Store) ([]string, error) {
	tradesList, err := store.Load()
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{})
	for _, t := range tradesList {
		name := strings.TrimSpace(t.Portfolio)
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}

	portfolios := make([]string, 0, len(set))
	for k := range set {
		portfolios = append(portfolios, k)
	}
	sort.Strings(portfolios)

	return portfolios, nil
}

func UpsertMappings(portfolioName string, inputs []MappingInput) ([]StrategyPortfolioMapping, error) {
	portfolio := strings.TrimSpace(portfolioName)
	store := DefaultMappingStore()
	mappings, err := store.Load()
	if err != nil {
		return nil, err
	}

	index := make(map[string]int)
	for i, m := range mappings {
		key := strings.ToLower(strings.TrimSpace(m.PortfolioName + "|" + m.StrategyKey))
		if key == "|" {
			continue
		}
		index[key] = i
	}

	now := time.Now().UTC()
	for _, input := range inputs {
		strategyKey := strings.TrimSpace(input.StrategyKey)
		if strategyKey == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(portfolio + "|" + strategyKey))
		mapping := StrategyPortfolioMapping{
			ID:            key,
			StrategyKey:   strategyKey,
			PortfolioName: portfolio,
			Enabled:       input.Enabled,
			Weight:        input.Weight,
			RatioMode:     input.RatioMode,
			RatioUnit:     input.RatioUnit,
			RatioAmount:   input.RatioAmount,
			Notes:         input.Notes,
			UpdatedAt:     now,
		}

		if idx, ok := index[key]; ok {
			mappings[idx] = mapping
		} else {
			mappings = append(mappings, mapping)
			index[key] = len(mappings) - 1
		}
	}

	if err := store.Save(mappings); err != nil {
		return nil, err
	}

	return mappings, nil
}

func EnsureMappingForStrategy(portfolioName, strategyName string) error {
	portfolio := strings.TrimSpace(portfolioName)
	strategy := strings.TrimSpace(strategyName)
	if portfolio == "" || strategy == "" {
		return nil
	}

	store := DefaultMappingStore()
	mappings, err := store.Load()
	if err != nil {
		return err
	}

	key := strings.ToLower(strings.TrimSpace(portfolio + "|" + strategy))
	for _, m := range mappings {
		if strings.ToLower(strings.TrimSpace(m.PortfolioName+"|"+m.StrategyKey)) == key {
			return nil
		}
	}

	mappings = append(mappings, StrategyPortfolioMapping{
		ID:            key,
		StrategyKey:   strategy,
		PortfolioName: portfolio,
		Enabled:       false,
		Weight:        1.0,
		RatioMode:     false,
		RatioUnit:     1.0,
		RatioAmount:   10000,
		Notes:         "Auto-added on import",
		UpdatedAt:     time.Now().UTC(),
	})

	return store.Save(mappings)
}
