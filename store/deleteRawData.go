package store

import (
	"fmt"
	"time"

	"at.ourproject/energystore/model"
	"at.ourproject/energystore/store/ebow"
	"at.ourproject/energystore/utils"
	"github.com/golang/glog"
)

// deleteWriteBatch bounds how many rewritten RawSourceLines are flushed per PutBatch.
const deleteWriteBatch = 500

// inspectMeteringSlots reports the measured value (slot 0) and whether the given
// metering point has any data in this line, and — when zero is true — sets that
// metering point's slots (value + QoV) to zero.
//
// A RawSourceLine packs *all* metering points of the EC into shared arrays: a
// consumer occupies Consumers[idx*3 .. idx*3+2] (+ matching QoV), a producer
// Producers[idx*2 .. idx*2+1]. Only this one metering point's block is touched —
// every co-located metering point in the same line is left untouched. That is the
// core correctness property of the delete (see deleteRawData_test.go).
func inspectMeteringSlots(line *model.RawSourceLine, dir model.MeterDirection, sourceIdx int, zero bool) (measured float64, hadData bool) {
	var vals []float64
	var qov []int
	var base, width int

	switch dir {
	case model.CONSUMER_DIRECTION:
		vals, qov, base, width = line.Consumers, line.QoVConsumers, sourceIdx*3, 3
	case model.PRODUCER_DIRECTION:
		vals, qov, base, width = line.Producers, line.QoVProducers, sourceIdx*2, 2
	default:
		return 0, false
	}

	if base >= 0 && base < len(vals) {
		measured = vals[base]
	}
	for j := 0; j < width; j++ {
		p := base + j
		if p >= 0 && p < len(vals) {
			if vals[p] != 0 {
				hadData = true
			}
			if zero {
				vals[p] = 0
			}
		}
		if p >= 0 && p < len(qov) {
			if qov[p] != 0 {
				hadData = true
			}
			if zero {
				qov[p] = 0
			}
		}
	}
	return measured, hadData
}

func copyRawSourceLine(l *model.RawSourceLine) *model.RawSourceLine {
	return &model.RawSourceLine{
		Id:           l.Id,
		Consumers:    append([]float64(nil), l.Consumers...),
		Producers:    append([]float64(nil), l.Producers...),
		QoVConsumers: append([]int(nil), l.QoVConsumers...),
		QoVProducers: append([]int(nil), l.QoVProducers...),
	}
}

// DeleteRawDataForMeteringPoint zeros every 15-minute value of a single metering
// point within [from, to] in the (tenant, ecid) BadgerDB store. It is the same
// iteration for dry-run and execute: with dryRun=true nothing is written and only
// the affected timestep count and summed measured kWh are returned, so the preview
// matches exactly what an execute would remove.
//
// The metering point's SourceIdx/direction is resolved via GetMetaInfo — the same
// mapping the read/report path uses — so the zeroed slots are exactly the ones
// reads consult, and neighbouring metering points in the same line stay untouched.
// The operation is idempotent (re-zeroing already-zero slots is a no-op), so an
// aborted run is safe to repeat.
func DeleteRawDataForMeteringPoint(tenant, ecid, meteringPoint string, from, to time.Time, dryRun bool) (affectedTimesteps int, sumKwh float64, err error) {
	if from.After(to) {
		return 0, 0, fmt.Errorf("invalid range: from is after to")
	}

	db, err := ebow.OpenStorage(tenant, ecid)
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()

	meta, _, err := GetMetaInfo(db)
	if err != nil {
		return 0, 0, err
	}
	cp, ok := meta[meteringPoint]
	if !ok {
		// Metering point not known in this EC → nothing to delete (no-op).
		return 0, 0, nil
	}

	// Scan a day window around [from, to] (one day of slack on each side to be
	// robust against timezone/day-boundary key formatting) and filter each row
	// precisely by its timestamp.
	scanStart := from.AddDate(0, 0, -1)
	scanEnd := to.AddDate(0, 0, 1)
	startKey := fmt.Sprintf("%.4d/%.2d/%.2d/", scanStart.Year(), int(scanStart.Month()), scanStart.Day())
	endKey := fmt.Sprintf("%.4d/%.2d/%.2d/", scanEnd.Year(), int(scanEnd.Month()), scanEnd.Day())

	iter := db.GetLineRange("CP", startKey, endKey)
	defer iter.Close()

	batch := make([]*model.RawSourceLine, 0, deleteWriteBatch)
	flush := func() error {
		if dryRun || len(batch) == 0 {
			return nil
		}
		if e := db.SetLines(batch); e != nil {
			return e
		}
		batch = batch[:0]
		return nil
	}

	var line model.RawSourceLine
	for iter.Next(&line) {
		_, ts, e := utils.ConvertRowIdToTimeString("CP", line.Id, time.UTC)
		if e != nil || ts == nil {
			continue
		}
		if ts.Before(from) || ts.After(to) {
			continue
		}

		if dryRun {
			measured, hadData := inspectMeteringSlots(&line, cp.Dir, cp.SourceIdx, false)
			if hadData {
				affectedTimesteps++
				sumKwh += measured
			}
			continue
		}

		nl := copyRawSourceLine(&line)
		measured, hadData := inspectMeteringSlots(nl, cp.Dir, cp.SourceIdx, true)
		if hadData {
			affectedTimesteps++
			sumKwh += measured
			batch = append(batch, nl)
			if len(batch) >= deleteWriteBatch {
				if e := flush(); e != nil {
					return affectedTimesteps, sumKwh, e
				}
			}
		}
	}
	if e := iter.Err(); e != nil {
		return affectedTimesteps, sumKwh, e
	}
	if e := flush(); e != nil {
		return affectedTimesteps, sumKwh, e
	}

	glog.V(4).Infof("DeleteRawDataForMeteringPoint tenant=%s ec=%s zp=%s dryRun=%t timesteps=%d",
		tenant, ecid, meteringPoint, dryRun, affectedTimesteps)
	return affectedTimesteps, sumKwh, nil
}
