package portfolio

import "sort"

func sortTradesByExitTime(trades []Trade) {
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].ExitTime.Before(trades[j].ExitTime)
	})
}