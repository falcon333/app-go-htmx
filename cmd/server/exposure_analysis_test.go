package main

import (
	"math"
	"testing"
	"time"
)

func TestMergeIntervals(t *testing.T) {
	intervals := []exposureInterval{
		{Start: 1, End: 3},
		{Start: 2, End: 4},
		{Start: 5, End: 6},
	}
	merged := mergeIntervals(intervals)
	if len(merged) != 2 {
		t.Fatalf("expected 2 intervals, got %d", len(merged))
	}
	if merged[0].Start != 1 || merged[0].End != 4 {
		t.Fatalf("expected [1,4], got [%d,%d]", merged[0].Start, merged[0].End)
	}
	if merged[1].Start != 5 || merged[1].End != 6 {
		t.Fatalf("expected [5,6], got [%d,%d]", merged[1].Start, merged[1].End)
	}
}

func TestIntersectIntervals(t *testing.T) {
	a := []exposureInterval{{Start: 1, End: 5}, {Start: 7, End: 9}}
	b := []exposureInterval{{Start: 2, End: 6}, {Start: 8, End: 10}}
	out := intersectIntervals(a, b)
	if len(out) != 2 {
		t.Fatalf("expected 2 intersections, got %d", len(out))
	}
	if out[0].Start != 2 || out[0].End != 5 {
		t.Fatalf("expected [2,5], got [%d,%d]", out[0].Start, out[0].End)
	}
	if out[1].Start != 8 || out[1].End != 9 {
		t.Fatalf("expected [8,9], got [%d,%d]", out[1].Start, out[1].End)
	}
}

func TestUnderwaterIntervals(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := []ExposureTradeRow{
		{Strategy: "A", EntryTime: base, ExitTime: base.Add(1 * time.Hour), PnLAbs: 10},
		{Strategy: "A", EntryTime: base.Add(1 * time.Hour), ExitTime: base.Add(2 * time.Hour), PnLAbs: -5},
		{Strategy: "A", EntryTime: base.Add(2 * time.Hour), ExitTime: base.Add(3 * time.Hour), PnLAbs: -10},
		{Strategy: "A", EntryTime: base.Add(3 * time.Hour), ExitTime: base.Add(4 * time.Hour), PnLAbs: 20},
	}

	intervalsByStrat := buildUnderwaterIntervalsByStrategy(trades)
	intervals := intervalsByStrat["A"]
	if len(intervals) != 2 {
		t.Fatalf("expected 2 underwater intervals, got %d", len(intervals))
	}
	if intervals[0].Start >= intervals[0].End || intervals[1].Start >= intervals[1].End {
		t.Fatalf("invalid underwater intervals: %#v", intervals)
	}
}

func TestExposureJaccardAndAttribution(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := []ExposureTradeRow{
		{Strategy: "A", EntryTime: base, ExitTime: base.Add(12 * time.Hour), PnLAbs: -10},
		{Strategy: "A", EntryTime: base.Add(12 * time.Hour), ExitTime: base.Add(16 * time.Hour), PnLAbs: 5},
		{Strategy: "B", EntryTime: base.Add(5 * time.Hour), ExitTime: base.Add(14 * time.Hour), PnLAbs: -10},
		{Strategy: "B", EntryTime: base.Add(14 * time.Hour), ExitTime: base.Add(16 * time.Hour), PnLAbs: 5},
	}

	summary, analysis := buildExposureAnalysis(trades)
	if summary == nil || analysis == nil {
		t.Fatal("expected exposure analysis")
	}
	if len(summary.TopByExposureOverlap) != 1 {
		t.Fatalf("expected 1 summary row, got %d", len(summary.TopByExposureOverlap))
	}
	row := summary.TopByExposureOverlap[0]
	if math.Abs(row.ExposureOverlapHours-11) > 0.0001 {
		t.Fatalf("expected overlap hours 11, got %f", row.ExposureOverlapHours)
	}
	if math.Abs(row.ExposureJaccard-0.6875) > 0.0001 {
		t.Fatalf("expected jaccard 0.6875, got %f", row.ExposureJaccard)
	}
	if math.Abs(row.DDCoincidenceAttribution-1.0) > 0.0001 {
		t.Fatalf("expected attribution 1.0, got %f", row.DDCoincidenceAttribution)
	}
}
