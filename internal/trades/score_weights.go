package trades

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type ScoreWeightsSettings struct {
	R2         float64 `json:"r2"`
	Net        float64 `json:"net"`
	DD         float64 `json:"dd"`
	PF         float64 `json:"pf"`
	Expectancy float64 `json:"expectancy"`
}

type ScoreWeightsStore struct {
	Path string
}

func DefaultScoreWeightsStore() ScoreWeightsStore {
	return ScoreWeightsStore{Path: filepath.Join("internal", "trades", "score_weights.json")}
}

func LoadScoreWeights() (*ScoreWeightsSettings, error) {
	store := DefaultScoreWeightsStore()
	data, err := os.ReadFile(store.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var settings ScoreWeightsSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

func SaveScoreWeights(settings ScoreWeightsSettings) error {
	store := DefaultScoreWeightsStore()
	if err := os.MkdirAll(filepath.Dir(store.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(store.Path, data, 0o644)
}
