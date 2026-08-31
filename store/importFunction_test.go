package store

import (
	"at.ourproject/energystore/model"
	"at.ourproject/energystore/store/ebow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func Test_updateMetaCP(t *testing.T) {
	type args struct {
		metaCP *model.CounterPointMeta
		begin  time.Time
		end    time.Time
	}
	tests := []struct {
		name string
		args args
		test func(t *testing.T, args args)
	}{
		{
			name: "Adjust meta period end time",
			args: args{
				metaCP: &model.CounterPointMeta{
					ID:          "000",
					Name:        "IV0000999222222222221",
					SourceIdx:   0,
					Dir:         "CONSUMPTION",
					Count:       0,
					PeriodStart: "30.12.2023 15:00:0000",
					PeriodEnd:   "30.12.2023 15:00:0000",
				},
				begin: time.Date(2023, 12, 30, 15, 1, 0, 0, time.Local),
				end:   time.Date(2023, 12, 30, 15, 15, 0, 0, time.Local),
			},
			test: func(t *testing.T, args args) {
				result := updateMetaCP(args.metaCP, args.begin, args.end)
				assert.Equalf(t, true, result, "updateMetaCP(%v, %v, %v)", args.metaCP, args.begin, args.end)
				assert.Equal(t, "30.12.2023 15:00:0000", args.metaCP.PeriodStart)
				assert.Equal(t, "30.12.2023 15:15:00", args.metaCP.PeriodEnd)
			},
		},
		{
			name: "Adjust meta period start time",
			args: args{
				metaCP: &model.CounterPointMeta{
					ID:          "000",
					Name:        "IV0000999222222222221",
					SourceIdx:   0,
					Dir:         "CONSUMPTION",
					Count:       0,
					PeriodStart: "30.12.2023 15:00:0000",
					PeriodEnd:   "30.12.2023 15:15:0000",
				},
				begin: time.Date(2023, 12, 30, 14, 0, 0, 0, time.Local),
				end:   time.Date(2023, 12, 30, 15, 15, 0, 0, time.Local),
			},
			test: func(t *testing.T, args args) {
				result := updateMetaCP(args.metaCP, args.begin, args.end)
				assert.Equalf(t, true, result, "updateMetaCP(%v, %v, %v)", args.metaCP, args.begin, args.end)
				assert.Equal(t, "30.12.2023 14:00:00", args.metaCP.PeriodStart)
				// Not widened, so the stored legacy value stays byte-identical.
				assert.Equal(t, "30.12.2023 15:15:0000", args.metaCP.PeriodEnd)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.test(t, tt.args)
		})
	}
}

// metaOnlyStorage is the slice of ebow.IBowStorage that updateMeta touches;
// the remaining methods are never reached from there.
type metaOnlyStorage struct {
	meta *model.RawSourceMeta
}

func (s *metaOnlyStorage) GetMeta(string) (*model.RawSourceMeta, error) { return s.meta, nil }
func (s *metaOnlyStorage) SetMeta(line *model.RawSourceMeta) error      { s.meta = line; return nil }
func (s *metaOnlyStorage) GetLineRange(_, _, _ string) ebow.IRange      { panic("not used") }
func (s *metaOnlyStorage) SetLines([]*model.RawSourceLine) error        { panic("not used") }
func (s *metaOnlyStorage) GetLine(*model.RawSourceLine) error           { panic("not used") }
func (s *metaOnlyStorage) ListBuckets() ([]string, error)               { panic("not used") }
func (s *metaOnlyStorage) GetTenant() string                            { return "TE000000" }

// A month message is split into day blocks that each run StoreEnergyV2 against
// the same EC-wide cpmeta/0 record. Every block reads the metadata once at the
// start, so by the time it writes, another block may already have widened the
// period. updateMeta used to assign its own — by then stale — values, which let
// the block finishing last win with an older end date: the measured values
// stayed complete while the dashboard cut off early.
func Test_updateMeta_keepsWiderPeriodWrittenConcurrently(t *testing.T) {
	const cp = "IV0000999222222222221"

	// State on disk: a later day block already extended the period to 15:15.
	stored := &metaOnlyStorage{meta: &model.RawSourceMeta{
		Id: "cpmeta/0",
		CounterPoints: []*model.CounterPointMeta{{
			ID:          "000",
			Name:        cp,
			PeriodStart: "30.12.2023 15:00:00",
			PeriodEnd:   "30.12.2023 15:15:00",
		}},
	}}

	// The older block still holds the metadata as it looked before that write.
	stale := &model.CounterPointMeta{
		ID:          "000",
		Name:        cp,
		PeriodStart: "30.12.2023 15:00:00",
		PeriodEnd:   "30.12.2023 15:00:00",
	}
	begin := time.Date(2023, 12, 30, 14, 0, 0, 0, time.Local)
	end := time.Date(2023, 12, 30, 15, 0, 0, 0, time.Local)

	require.NoError(t, updateMeta(stored, stale, cp, begin, end))

	written := stored.meta.CounterPoints[0]
	assert.Equal(t, "30.12.2023 15:15:00", written.PeriodEnd, "the wider end date must survive")
	assert.Equal(t, "30.12.2023 14:00:00", written.PeriodStart, "the older start date must still be applied")
}
