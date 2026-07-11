package store

import (
	"testing"
	"time"

	"at.ourproject/energystore/model"
	"at.ourproject/energystore/utils"
)

// TestDeleteRange_EndOfRangeIncludedInFoldTimezone guards the time-boundary bug:
// row-ids encode wall-clock time in the fold timezone (the image bakes
// TZ=Europe/Berlin, offset-identical to Vienna, so the delete uses time.Local).
// The browser sends [from,to] as absolute instants of the local wall-clock pick.
// Interpreting the row-id in UTC (the old bug) shifted it by the +1h offset, so
// the last quarter-hours before the range end (local 23:15/23:30/23:45) fell past
// `to` and were skipped. Using the fold zone keeps them in range.
func TestDeleteRange_EndOfRangeIncludedInFoldTimezone(t *testing.T) {
	fold := time.FixedZone("CET", 3600) // +1h, Vienna/Berlin winter (== time.Local in the image)
	from := time.Date(2023, 2, 1, 0, 0, 0, 0, fold)
	to := time.Date(2023, 3, 1, 0, 0, 0, 0, fold) // browser pick 01.03 00:00 -> absolute instant

	// Last real datapoint of February at wall-clock 23:45.
	_, ts, err := utils.ConvertRowIdToTimeString("CP", "CP/2023/02/28/23/45/00", fold)
	if err != nil || ts == nil {
		t.Fatalf("convert failed: %v", err)
	}
	if ts.Before(from) || ts.After(to) {
		t.Fatalf("end-of-range timestep %v wrongly excluded from [%v, %v]", ts, from, to)
	}

	// Regression guard: the old time.UTC interpretation reproduces the bug
	// (row-id parsed as 23:45 UTC is after to = 23:00 UTC).
	_, tsUTC, _ := utils.ConvertRowIdToTimeString("CP", "CP/2023/02/28/23/45/00", time.UTC)
	if tsUTC == nil || !tsUTC.After(to) {
		t.Fatalf("sanity: expected UTC-parsed ts (%v) to be after to (%v) — the bug", tsUTC, to)
	}
}

// TestInspectMeteringSlots_ZerosOnlyTargetConsumer is the core-correctness test:
// a RawSourceLine packs multiple metering points into shared arrays. Zeroing one
// consumer (SourceIdx 1) must leave the co-located consumer (SourceIdx 0) and any
// producer slots completely untouched.
func TestInspectMeteringSlots_ZerosOnlyTargetConsumer(t *testing.T) {
	line := &model.RawSourceLine{
		Id:           "CP/2026/01/15/08/30",
		Consumers:    []float64{1.1, 1.2, 1.3, 2.1, 2.2, 2.3}, // idx0: [0..2], idx1: [3..5]
		QoVConsumers: []int{1, 1, 1, 1, 1, 1},
		Producers:    []float64{9.1, 9.2},
		QoVProducers: []int{1, 1},
	}

	measured, hadData := inspectMeteringSlots(line, model.CONSUMER_DIRECTION, 1, true)

	if !hadData {
		t.Fatalf("expected hadData=true for a metering point with values")
	}
	if measured != 2.1 {
		t.Fatalf("expected measured=2.1 (slot 0 of idx1), got %v", measured)
	}
	// Target consumer (idx1) zeroed:
	for i := 3; i <= 5; i++ {
		if line.Consumers[i] != 0 || line.QoVConsumers[i] != 0 {
			t.Fatalf("target slot %d not zeroed: val=%v qov=%v", i, line.Consumers[i], line.QoVConsumers[i])
		}
	}
	// Neighbour consumer (idx0) untouched:
	if line.Consumers[0] != 1.1 || line.Consumers[1] != 1.2 || line.Consumers[2] != 1.3 {
		t.Fatalf("neighbour consumer idx0 was modified: %v", line.Consumers[:3])
	}
	for i := 0; i <= 2; i++ {
		if line.QoVConsumers[i] != 1 {
			t.Fatalf("neighbour consumer qov idx0 was modified at %d: %v", i, line.QoVConsumers[i])
		}
	}
	// Producers untouched (different array):
	if line.Producers[0] != 9.1 || line.Producers[1] != 9.2 {
		t.Fatalf("producers were modified: %v", line.Producers)
	}
}

// TestInspectMeteringSlots_ProducerAndDryRun checks the producer offset (idx*2)
// and that dry-run (zero=false) never mutates the line.
func TestInspectMeteringSlots_ProducerAndDryRun(t *testing.T) {
	line := &model.RawSourceLine{
		Id:           "CP/2026/01/15/08/30",
		Producers:    []float64{5.0, 6.0, 7.0, 8.0}, // idx0: [0..1], idx1: [2..3]
		QoVProducers: []int{1, 1, 1, 1},
	}

	measured, hadData := inspectMeteringSlots(line, model.PRODUCER_DIRECTION, 1, false)
	if !hadData || measured != 7.0 {
		t.Fatalf("expected hadData=true, measured=7.0 (slot 0 of producer idx1), got had=%v measured=%v", hadData, measured)
	}
	// dry-run must not mutate:
	if line.Producers[2] != 7.0 || line.Producers[3] != 8.0 {
		t.Fatalf("dry-run mutated producer slots: %v", line.Producers)
	}
}

// TestInspectMeteringSlots_NoDataOutOfBounds: a line predating the metering point
// (arrays too short for the SourceIdx) reports no data and does not panic.
func TestInspectMeteringSlots_NoDataOutOfBounds(t *testing.T) {
	line := &model.RawSourceLine{
		Id:        "CP/2026/01/15/08/30",
		Consumers: []float64{1.1, 1.2, 1.3}, // only idx0 exists
	}
	measured, hadData := inspectMeteringSlots(line, model.CONSUMER_DIRECTION, 2, true)
	if hadData || measured != 0 {
		t.Fatalf("expected no data for out-of-bounds SourceIdx, got had=%v measured=%v", hadData, measured)
	}
}
