package trades

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AnalysisSettings struct {
	Portfolio        string    `json:"portfolio"`
	RangeQuick       string    `json:"range_quick"`
	AutoRefresh      bool      `json:"auto_refresh"`
	N                string    `json:"n"`
	Unit             string    `json:"unit"`
	StartDate        string    `json:"start_date"`
	EndDate          string    `json:"end_date"`
	Balance          string    `json:"balance"`
	ChartsEnabled    bool      `json:"charts_enabled"`
	ChartsThreshold  int       `json:"charts_threshold"`
	ChartsEnabledSet bool      `json:"charts_enabled_set"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AnalysisSettingsStore struct {
	Path string
}

func DefaultAnalysisSettingsStore() AnalysisSettingsStore {
	return AnalysisSettingsStore{Path: filepath.Join("internal", "trades", "analysis_settings.json")}
}

func (s AnalysisSettingsStore) LoadAll() ([]AnalysisSettings, error) {
	if _, err := os.Stat(s.Path); err != nil {
		if os.IsNotExist(err) {
			return []AnalysisSettings{}, nil
		}
		return nil, err
	}

	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return []AnalysisSettings{}, nil
	}

	var settings []AnalysisSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return settings, nil
}

func (s AnalysisSettingsStore) SaveAll(settings []AnalysisSettings) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.Path, data, 0o644)
}

func LoadAnalysisSettings(portfolioName string) (*AnalysisSettings, error) {
	portfolio := strings.TrimSpace(portfolioName)
	if portfolio == "" {
		return nil, nil
	}

	store := DefaultAnalysisSettingsStore()
	settings, err := store.LoadAll()
	if err != nil {
		return nil, err
	}

	for _, s := range settings {
		if strings.EqualFold(s.Portfolio, portfolio) {
			copy := s
			return &copy, nil
		}
	}

	return nil, nil
}

func SaveAnalysisSettings(input AnalysisSettings) error {
	portfolio := strings.TrimSpace(input.Portfolio)
	if portfolio == "" {
		return nil
	}

	store := DefaultAnalysisSettingsStore()
	settings, err := store.LoadAll()
	if err != nil {
		return err
	}

	input.Portfolio = portfolio
	input.UpdatedAt = time.Now().UTC()

	updated := false
	for i, s := range settings {
		if strings.EqualFold(s.Portfolio, portfolio) {
			settings[i] = input
			updated = true
			break
		}
	}

	if !updated {
		settings = append(settings, input)
	}

	return store.SaveAll(settings)
}
