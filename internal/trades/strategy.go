package trades

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseStrategyMetadata(strategyID string) (StrategyMetadata, error) {
	parts := strings.FieldsFunc(strategyID, func(r rune) bool { return r == '-' })
	if len(parts) < 3 {
		return StrategyMetadata{}, fmt.Errorf("invalid strategy_id format: %s", strategyID)
	}

	tf, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return StrategyMetadata{}, fmt.Errorf("invalid strategy timeframe: %s", parts[len(parts)-1])
	}

	return StrategyMetadata{
		Strategy:  strategyID,
		Symbol:    parts[len(parts)-2],
		Timeframe: tf,
	}, nil
}
