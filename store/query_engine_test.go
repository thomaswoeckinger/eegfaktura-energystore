package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Regression for the "/query/rawdata returns each timestamp N times" bug
// (support cases RC101586 / RC105720). A metering point that was re-registered
// under a new member while the old, overlapping participant row was still
// listed ends up in the request `cps` N times. Because the store holds exactly
// one series per metering-point name, every duplicate cps entry produced one
// identical extra data series (DefaultFunction) — hence "each timestamp 4x".
// QueryRawData now de-duplicates the target list before querying.
func TestDedupTargets(t *testing.T) {
	zp := "AT0030000000000000000000000363736"
	other := "AT0031000000000000000000099022000"

	t.Run("collapses the re-registered ZP listed multiple times to one", func(t *testing.T) {
		// The web builds cps by flattening active participant rows; with two
		// overlapping rows (old INACTIVE + new ACTIVE) the same ZP appears twice.
		in := []TargetMP{{MeteringPoint: zp}, {MeteringPoint: other}, {MeteringPoint: zp}}
		out := dedupTargets(in)
		assert.Equal(t, []TargetMP{{MeteringPoint: zp}, {MeteringPoint: other}}, out,
			"duplicate metering point must appear once; order preserved (first occurrence)")
	})

	t.Run("four duplicates (original report) collapse to one", func(t *testing.T) {
		in := []TargetMP{{MeteringPoint: zp}, {MeteringPoint: zp}, {MeteringPoint: zp}, {MeteringPoint: zp}}
		out := dedupTargets(in)
		assert.Equal(t, []TargetMP{{MeteringPoint: zp}}, out)
	})

	t.Run("list without duplicates is unchanged", func(t *testing.T) {
		in := []TargetMP{{MeteringPoint: zp}, {MeteringPoint: other}}
		out := dedupTargets(in)
		assert.Equal(t, in, out)
	})

	t.Run("empty stays empty", func(t *testing.T) {
		assert.Equal(t, []TargetMP{}, dedupTargets([]TargetMP{}))
	})
}
