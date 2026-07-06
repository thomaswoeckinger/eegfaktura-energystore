package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"at.ourproject/energystore/model"
	"at.ourproject/energystore/store/ebow"
	"at.ourproject/energystore/utils"
	"github.com/golang/glog"
)

var re = regexp.MustCompile(`^(\w*)[^(]*\(([^)]*)\)$`)

type ReportData struct {
	Consumed    float64 `json:"consumed"`
	Allocated   float64 `json:"allocated"`
	Distributed float64 `json:"distributed"`
	Produced    float64 `json:"produced"`
	Unused      float64 `json:"unused"` // energy which fed back to the supplier
	QoVConsumer int     `json:"qoVConsumer"`
	QoVProducer int     `json:"qoVProducer"`
	CntProducer int     `json:"cntProducer"`
	CntConsumer int     `json:"cntConsumer"`
}

type ReportNamedData struct {
	*ReportData
	Name string `json:"name"`
}

type RawData struct {
	Ts    int64     `json:"ts"`
	Value []float64 `json:"value"`
	Qov   []int     `json:"qov"`
}

type RawDataResult struct {
	Data      []RawData            `json:"data"`
	Direction model.MeterDirection `json:"direction"`
}

type RawDataEngine struct {
	cps      []TargetMP
	params   map[string][]string
	function IQueryFunction
}

type MetaData struct {
	PeriodBegin int64 `json:"periodBegin"`
	PeriodEnd   int64 `json:"periodEnd"`
}

func (rde *RawDataEngine) HandleStart(ctx *EngineContext) error {

	rde.function = &DefaultFunction{ParentFunction{cps: rde.cps}}

	if len(rde.params) > 0 {
		if v, ok := rde.params["f"]; ok {
			fn, pa, err := parseFunction(v)
			if err != nil {
				return err
			}
			qfn, ok := Functions[strings.ToUpper(fn)]
			if !ok {
				return errors.New(fmt.Sprintf("Unknown function found %s", fn))
			}
			rde.function, err = qfn(strings.Split(pa, ","), rde.cps)
			if err != nil {
				return err
			}
		}
	}

	return rde.function.HandleInit(ctx)
}

func (rde *RawDataEngine) HandleLine(ctx *EngineContext, line *model.RawSourceLine) error {
	return rde.function.HandleLine(ctx, line)
}

func (rde *RawDataEngine) HandleEnd(ctx *EngineContext) error {
	return rde.function.HandleFinish(ctx)
}

func QueryIntraDayReport(tenant, ecid string, start, end time.Time) ([]interface{}, error) {
	c, _ := NewIntraDayFunction()
	e := &Engine{c}

	sm := time.Now()
	if err := e.Query(tenant, ecid, start, end); err != nil && !errors.Is(err, ebow.ErrNoRows) {
		return nil, err
	}
	glog.V(5).Infof("Query Intra Day Report took %v (%s)", time.Since(sm).Seconds(), tenant)
	return (c.(EnergyReportConsumer)).GetResult(), nil
}

func QueryLoadCurveReport(tenant, ecid string, start, end time.Time, nameFunc *string) ([]interface{}, error) {
	tsFn, initFn, nameFn := determineTimeShiftFunction(start, end)
	c, _ := NewLoadCurveFunction(tsFn, nameFn, initFn)
	e := &Engine{c}

	sm := time.Now()
	if err := e.Query(tenant, ecid, start, end); err != nil && !errors.Is(err, ebow.ErrNoRows) {
		return nil, err
	}
	glog.V(5).Infof("Query Load Curve Report took %v (%s)", time.Since(sm).Seconds(), tenant)
	return (c.(EnergyReportConsumer)).GetResult(), nil
}

func QueryCombinedReports(tenant, ecid string, reports []string, start, end time.Time) ([]interface{}, error) {
	c, _ := NewCombinedConsumers(reports, start, end)
	e := &Engine{c}

	sm := time.Now()
	if err := e.Query(tenant, ecid, start, end); err != nil && !errors.Is(err, ebow.ErrNoRows) {
		return nil, err
	}
	glog.V(5).Infof("Query Combined Report took %v [%v] (%s)", time.Since(sm).Seconds(), reports, tenant)
	return (c.(EnergyReportConsumer)).GetResult(), nil
}

func QueryRawData(tenant, ecid string, start, end time.Time, cps []TargetMP, params map[string][]string) (map[string]*RawDataResult, error) {
	c := &RawDataEngine{cps: dedupTargets(cps), params: params}
	e := &Engine{c}

	sm := time.Now()
	if err := e.Query(tenant, ecid, start, end); err != nil {
		if errors.Is(err, ebow.ErrNoRows) {
			return make(map[string]*RawDataResult), nil
		}
		return nil, err
	}
	glog.V(5).Infof("Query Row Data API took %v (%s)", time.Since(sm).Seconds(), tenant)
	return c.function.GetResult(), nil
}

// dedupTargets removes duplicate metering points from the target list,
// keeping the first occurrence and preserving order. The store holds exactly
// one data series per metering-point name (one SourceIdx), so a caller that
// lists the same ZP more than once — e.g. a metering point re-registered under
// a new member while the old, overlapping participant row is still listed —
// would otherwise get that ZP's identical series appended once per duplicate
// entry (DefaultFunction) or double-counted (Aggregate). Deduplicating here
// covers both raw endpoints (queryRawData and fetchRawV2 both call QueryRawData).
func dedupTargets(cps []TargetMP) []TargetMP {
	seen := make(map[string]struct{}, len(cps))
	out := make([]TargetMP, 0, len(cps))
	for _, cp := range cps {
		if _, ok := seen[cp.MeteringPoint]; ok {
			continue
		}
		seen[cp.MeteringPoint] = struct{}{}
		out = append(out, cp)
	}
	return out
}

func QueryMetaData(tenant, ecid string) (map[string]*MetaData, error) {
	db, err := ebow.OpenStorage(tenant, ecid)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	sm := time.Now()
	result := map[string]*MetaData{}
	metaMap, _, err := GetMetaInfo(db)
	for k, v := range metaMap {
		begin, _ := utils.ParseTime(v.PeriodStart, 0)
		end, _ := utils.ParseTime(v.PeriodEnd, 0)
		result[k] = &MetaData{
			PeriodBegin: begin.UnixMilli(),
			PeriodEnd:   end.UnixMilli(),
		}
	}

	glog.V(5).Infof("Query Meta Data API took %v (%s)", time.Since(sm).Seconds(), tenant)
	return result, err
}

func determineTimeShiftFunction(start, end time.Time) (AddCacheTimeFunc, InitCacheTimeFunc, SeriesNameFunc) {
	if start.AddDate(0, 0, 1).Add(time.Minute).After(end) {
		return nil, InitDefault(), dayRawNameFunc()
	} else if start.AddDate(0, 1, 0).Add(time.Minute).After(end) {
		return AddDate(0, 0, 1), InitDefault(), monthDayNameFunc()
	} else if start.AddDate(0, 6, 0).Add(time.Minute).After(end) {
		return AddDate(0, 0, 7), InitWeek(), weekYearNameFunc()
	} else if start.AddDate(0, 12, 0).Add(time.Minute).After(end) {
		return AddDate(0, 1, 0), InitMonth(), monthYearNameFunc()
	} else {
		return nil, InitDefault(), dayRawNameFunc()
	}
}

func determineSeriesNameFunction(start, end time.Time, nameFunc *string) SeriesNameFunc {
	//if nameFunc == nil {
	if start.AddDate(0, 0, 1).Add(time.Minute).After(end) {
		return dayRawNameFunc()
	} else if start.AddDate(0, 1, 0).Add(time.Minute).After(end) {
		return monthDayNameFunc()
	} else if start.AddDate(0, 6, 0).Add(time.Minute).After(end) {
		return weekYearNameFunc()
	} else if start.AddDate(0, 12, 0).Add(time.Minute).After(end) {
		return monthYearNameFunc()
	} else {
		return dayRawNameFunc()
	}
	//}
	//return nameFunctionRepo[strings.ToUpper(*nameFunc)]
}

func parseFunction(f []string) (fn string, pa string, err error) {

	if len(f) > 1 {
		err = errors.New(fmt.Sprintf("Unknown function declared %+v", f))
		return
	}
	m := re.FindStringSubmatch(f[0])
	if len(m) < 3 {
		err = errors.New("parser error while interpret function name")
		return
	}

	fn = m[1]
	pa = m[2]
	return
}
