package excel

import (
	"at.ourproject/energystore/model"
	"at.ourproject/energystore/store"
	"at.ourproject/energystore/store/ebow"
	"at.ourproject/energystore/utils"
	"errors"
	"fmt"
	"github.com/golang/glog"
	"github.com/xuri/excelize/v2"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var dateLine = regexp.MustCompile(`^[0-9]{2}\.[0-9]{2}\.[0-9]{4}\s[0-9]{2}:[0-9]{2}(?::[0-9]{2})?$`)
var numberPattern = regexp.MustCompile(`^[0-9\\.,]+$`)

func OpenExceFile(path string) (*excelize.File, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		glog.Error(err)
		return nil, err
	}
	return f, nil
}

func OpenReader(r io.Reader, filename string, opt ...excelize.Options) (*excelize.File, error) {
	f, err := excelize.OpenReader(r, opt...)
	if err != nil {
		return nil, err
	}
	f.Path = filename

	glog.V(3).Infof("Read Excel File with Sheets %+v", f.GetSheetList())
	return f, nil
}

type MeterCodeType int

const (
	Total MeterCodeType = iota
	Share
	Coverage
	Profit
	TotalTF
	ShareTF
	CoverageTF
	ProfitTF
	TotalProd
	Bad
)

type excelHeader struct {
	meteringPointId map[int]string
	energyDirection map[int]model.MeterDirection
	periodStart     map[int]string
	periodEnd       map[int]string
	meterCode       map[int]MeterCodeType
}

type excelCounterPointMeta struct {
	*model.CounterPointMeta
	Idx     int
	IdxG2   int
	IdxG3   int
	IdxG1TF int
	IdxG2TF int
	IdxG3TF int
}

func ImportExcelEnergyFile(f *excelize.File, sheet string, db *ebow.BowStorage) ([]int, error) {

	exp := "DD.MM.YYYY HH:MM:SS"
	style, err := f.NewStyle(&excelize.Style{CustomNumFmt: &exp})
	err = f.SetCellStyle("Sheet1", "A12", "A15", style)

	rows, err := f.Rows(sheet)
	if err != nil {
		glog.Error(err)
		return []int{}, err
	}
	glog.Infof("Rows Error: %+v", rows.Error())

	var rIdx int = 1
	var rawDatas []*model.RawSourceLine = []*model.RawSourceLine{}
	var rawDatasG2 []*model.RawSourceLine = []*model.RawSourceLine{}
	var rawDatasG3 []*model.RawSourceLine = []*model.RawSourceLine{}

	var excelHeader excelHeader
	excelHeaderInitialized := false

	var excelCpMeta map[int]*excelCounterPointMeta
	var updatedCpMeta []*model.CounterPointMeta
	var yearSet map[int]bool = make(map[int]bool)

	t := time.Now()
	totalRowCols := 0
	for rows.Next() {
		totalRowCols = totalRowCols + 1
		if cols, err := rows.Columns(excelize.Options{RawCellValue: true}); err == nil && len(cols) > 0 {
			switch cols[0] {
			case "MeteringpointID":
				excelHeader.meteringPointId = make(map[int]string, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.meteringPointId[i] = c
				}
			case "Spaltensumme", "Metering Interval", "Name", "MeteringReason", "Number of Metering Intervals":
				continue
			case "Energy direction":
				excelHeader.energyDirection = make(map[int]model.MeterDirection, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.energyDirection[i] = model.MeterDirection(c)
				}
			case "Period end", "Data Period end":
				excelHeader.periodEnd = make(map[int]string, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.periodEnd[i] = excelDateToString(c)
				}
			case "Period start", "Data Period start":
				excelHeader.periodStart = make(map[int]string, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.periodStart[i] = excelDateToString(c)
				}
			case "Metercode":
				excelHeader.meterCode = make(map[int]MeterCodeType, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.meterCode[i] = returnMeterCode(strings.ToUpper(c))
				}
			default:
				if isDate(cols[0]) {
					d, m, y, hh, mm, ss := getExcelDate(cols[0])
					yearSet[y] = true
					if !excelHeaderInitialized {
						excelCpMeta, updatedCpMeta, err = buildMatrixMetaStruct(db, excelHeader)
						excelHeaderInitialized = true
					}

					//
					// Insert G1 values
					//
					rawData := &model.RawSourceLine{Consumers: []float64{}, Producers: []float64{}, QoVConsumers: []int{}, QoVProducers: []int{}}
					rawData.Id = fmt.Sprintf("CP-G.01/%d/%.2d/%.2d/%.2d/%.2d/%.2d", y, m, d, hh, mm, ss)
					_ = db.GetLine(rawData)
					for i := 0; i < len(excelCpMeta); i++ {
						v := excelCpMeta[i]
						value := returnFloat(cols[v.Idx+1])
						switch v.Dir {
						case "CONSUMPTION":
							rawData.Consumers = utils.Insert(rawData.Consumers, v.SourceIdx, value)
							v.Count += 1
						case "GENERATION":
							rawData.Producers = utils.Insert(rawData.Producers, v.SourceIdx, value)
							v.Count += 1
						}
					}
					rawDatas = append(rawDatas, rawData)

					//
					// Insert G2 values
					//
					rawDataG2 := &model.RawSourceLine{Consumers: []float64{}, Producers: []float64{}}
					rawDataG2.Id = fmt.Sprintf("CP-G.02/%d/%.2d/%.2d/%.2d/%.2d/%.2d", y, m, d, hh, mm, ss)
					_ = db.GetLineG2(rawDataG2)
					for i := 0; i < len(excelCpMeta); i++ {
						v := excelCpMeta[i]
						if v.IdxG2 < 0 {
							continue
						}
						value := returnFloat(cols[v.IdxG2+1])
						switch v.Dir {
						case "CONSUMPTION":
							rawDataG2.Consumers = utils.Insert(rawDataG2.Consumers, v.SourceIdx, value)
							v.Count += 1
						case "GENERATION":
							rawDataG2.Producers = utils.Insert(rawDataG2.Producers, v.SourceIdx, value)
							v.Count += 1
						}
					}
					rawDatasG2 = append(rawDatasG2, rawDataG2)

					//
					// Insert G3 values
					//
					rawDataG3 := &model.RawSourceLine{Consumers: []float64{}, Producers: []float64{}}
					rawDataG3.Id = fmt.Sprintf("CP-G.03/%d/%.2d/%.2d/%.2d/%.2d/%.2d", y, m, d, hh, mm, ss)
					_ = db.GetLineG3(rawData)
					for i := 0; i < len(excelCpMeta); i++ {
						v := excelCpMeta[i]
						if v.IdxG3 < 0 {
							continue
						}
						value := returnFloat(cols[v.IdxG3+1])
						switch v.Dir {
						case "CONSUMPTION":
							rawDataG3.Consumers = utils.Insert(rawDataG3.Consumers, v.SourceIdx, value)
							v.Count += 1
						case "GENERATION":
							rawDataG3.Producers = utils.Insert(rawDataG3.Producers, v.SourceIdx, value)
							v.Count += 1
						}
					}
					rawDatasG3 = append(rawDatasG3, rawDataG3)
					//
					//
					//
					rIdx += 1
				} else {
					s, e := f.GetCellStyle(sheet, cols[0])
					if err != nil {
						glog.Errorf("Error get cell format %+v", e)
					}
					glog.V(3).Infof("Could not handle row format (%d). Cols %+v <%v>", s, cols, cols[0])
				}
			}
		}
	}
	glog.Infof("Time taken via read file: %v (%d Rows)", time.Since(t), totalRowCols)
	if err := db.SetLines(rawDatas); err != nil {
		return []int{}, err
	}
	if err := db.SetLinesG2(rawDatasG2); err != nil {
		return []int{}, err
	}
	if err := db.SetLinesG3(rawDatasG3); err != nil {
		return []int{}, err
	}

	glog.V(3).Infof("Import <%d> G1 lines", len(rawDatas))
	glog.V(3).Infof("Import <%d> G2 lines", len(rawDatasG2))
	glog.V(3).Infof("Import <%d> G3 lines", len(rawDatasG3))

	rawMeta := &model.RawSourceMeta{Id: fmt.Sprintf("cpmeta/%d", 0), CounterPoints: updatedCpMeta, NumberOfMetering: rIdx}
	err = db.SetMeta(rawMeta)

	glog.V(3).Infof("Update Metadata: %+v", rawMeta)

	if err != nil {
		glog.Error(err.Error())
		return []int{}, err
	}
	glog.V(3).Infof("Time taken via write batch: %v", time.Since(t))

	years := []int{}
	for k, _ := range yearSet {
		years = append(years, k)
	}
	return years, nil
}

func buildMatrixMetaStruct(db ebow.IBowStorage, excelHeader excelHeader) (map[int]*excelCounterPointMeta, []*model.CounterPointMeta, error) {
	glog.V(3).Info("Build Meta Matrix")

	ensureMeteringPoint := func(meter string) (string, error) {
		if len(meter) < 33 {
			return "", errors.New("not a metercode")
		}
		return meter, nil
	}

	filterExcelHeaderCol := func(pattern ...string) func(string) bool {
		return func(name string) bool {
			for _, p := range pattern {
				if name == p {
					return false
				}
			}
			return true
		}
	}

	type pair struct {
		key   string
		value int
		vG2   int
		vG3   int
		vG1TF int
		vG2TF int
		vG3TF int
		dir   model.MeterDirection
		end   string
		begin string
	}
	msSet := map[string]pair{}
	meteringIdSet := map[string]int{}
	headerFilter := filterExcelHeaderCol("MM", "TOTAL")

	for i := 0; i < len(excelHeader.meteringPointId); i++ {
		if headerFilter(strings.ToUpper(excelHeader.meteringPointId[i])) {
			if i < len(excelHeader.meterCode) {
				if v, err := ensureMeteringPoint(excelHeader.meteringPointId[i]); err == nil {
					if excelHeader.meterCode[i] == Total {
						if _, ok := meteringIdSet[v]; !ok && strings.ToLower(v) != "total" {
							meteringIdSet[v] = i
							if _ms, ok := msSet[v]; ok {
								_ms.value = i
								msSet[v] = _ms
							} else {
								msSet[v] = pair{v, i, -1, -1, -1, -1, -1, excelHeader.energyDirection[i], excelHeader.periodEnd[i], excelHeader.periodStart[i]}
							}
						}
					} else if strings.ToLower(v) != "total" && (excelHeader.meterCode[i] == Share || excelHeader.meterCode[i] == Profit) {
						if _ms, ok := msSet[v]; ok {
							_ms.vG2 = i
							msSet[v] = _ms
						} else {
							msSet[v] = pair{v, -1, i, -1, -1, -1, -1, excelHeader.energyDirection[i], excelHeader.periodEnd[i], excelHeader.periodStart[i]}
						}
					} else if strings.ToLower(v) != "total" && excelHeader.meterCode[i] == Coverage {
						if _ms, ok := msSet[v]; ok {
							_ms.vG3 = i
							msSet[v] = _ms
						} else {
							msSet[v] = pair{v, -1, -1, i, -1, -1, -1, excelHeader.energyDirection[i], excelHeader.periodEnd[i], excelHeader.periodStart[i]}
						}
					} else if strings.ToLower(v) != "total" && excelHeader.meterCode[i] == TotalTF {
						if _ms, ok := msSet[v]; ok {
							_ms.vG1TF = i
							msSet[v] = _ms
						} else {
							msSet[v] = pair{v, -1, -1, -1, i, -1, -1, excelHeader.energyDirection[i], excelHeader.periodEnd[i], excelHeader.periodStart[i]}
						}
					} else if strings.ToLower(v) != "total" && excelHeader.meterCode[i] == ProfitTF {
						if _ms, ok := msSet[v]; ok {
							_ms.vG2TF = i
							msSet[v] = _ms
						} else {
							msSet[v] = pair{v, -1, -1, -1, -1, i, -1, excelHeader.energyDirection[i], excelHeader.periodEnd[i], excelHeader.periodStart[i]}
						}
					} else if strings.ToLower(v) != "total" && excelHeader.meterCode[i] == CoverageTF {
						if _ms, ok := msSet[v]; ok {
							_ms.vG3TF = i
							msSet[v] = _ms
						} else {
							msSet[v] = pair{v, -1, -1, -1, -1, -1, i, excelHeader.energyDirection[i], excelHeader.periodEnd[i], excelHeader.periodStart[i]}
						}
					}
				}
			}
		}
	}

	// Validate and Transform read metering point data
	// - Transform Meterstructure to a Map, SortedList
	// - Check for valid period date
	ms := []pair{}
	for _, v := range msSet {
		if !(v.value < 0) {
			// check date
			_, err := utils.ParseTime(v.end, 0)
			if err != nil {
				return nil, nil, errors.New("'Period End' date missing or wrong format")
			}
			_, err = utils.ParseTime(v.begin, 0)
			if err != nil {
				return nil, nil, errors.New("'Period Start' date missing or wrong format")
			}

			// Setup Map
			ms = append(ms, v)
		}
	}

	sort.Slice(ms, func(i, j int) bool {
		return ms[i].value < ms[j].value
	})

	excelCpMeta := make(map[int]*excelCounterPointMeta, len(ms))
	storedCpMeta, metaInfo, err := store.GetMetaInfo(db)
	if err != nil {
		return nil, nil, err
	}
	for i, kv := range ms {
		_, ok := storedCpMeta[kv.key]
		if !ok {
			meterpoint := kv.key
			switch kv.dir {
			case model.CONSUMER_DIRECTION:
				metaInfo.ConsumerCount += 1
				metaInfo.MaxConsumerIdx += 1
				storedCpMeta[meterpoint] = &model.CounterPointMeta{
					ID:          fmt.Sprintf("%.3d", len(storedCpMeta)),
					SourceIdx:   metaInfo.MaxConsumerIdx,
					Name:        meterpoint,
					Dir:         model.CONSUMER_DIRECTION,
					PeriodStart: kv.begin,
					PeriodEnd:   kv.end,
				}
			case model.PRODUCER_DIRECTION:
				metaInfo.ProducerCount += 1
				metaInfo.MaxProducerIdx += 1
				storedCpMeta[meterpoint] = &model.CounterPointMeta{
					ID:          fmt.Sprintf("%.3d", len(storedCpMeta)),
					SourceIdx:   metaInfo.MaxProducerIdx,
					Name:        meterpoint,
					Dir:         model.PRODUCER_DIRECTION,
					PeriodStart: kv.begin,
					PeriodEnd:   kv.end,
				}
			}
		}
		storedMeta := storedCpMeta[kv.key]
		nStoredPeriodEnd, _ := utils.ParseTime(storedMeta.PeriodEnd, 0)
		nExcelPeriodEnd, _ := utils.ParseTime(kv.end, time.Now().UnixMilli())
		if nExcelPeriodEnd.Unix() > nStoredPeriodEnd.Unix() {
			storedMeta.PeriodEnd = kv.end
		}

		nStoredPeriodStart, _ := utils.ParseTime(storedMeta.PeriodStart, time.Now().UnixMilli())
		nExcelPeriodStart, _ := utils.ParseTime(kv.begin, time.Now().UnixMilli())
		if nStoredPeriodStart.Unix() > nExcelPeriodStart.Unix() {
			storedMeta.PeriodStart = kv.begin
		}

		excelCpMeta[i] = &excelCounterPointMeta{CounterPointMeta: storedMeta}
		switch kv.dir {
		case model.PRODUCER_DIRECTION:
			excelCpMeta[i].Idx = kv.value
			excelCpMeta[i].IdxG2 = kv.vG2
			excelCpMeta[i].IdxG3 = kv.vG3
			excelCpMeta[i].IdxG1TF = kv.vG1TF
			excelCpMeta[i].IdxG2TF = kv.vG2TF
			excelCpMeta[i].IdxG3TF = kv.vG3TF
		default:
			excelCpMeta[i].Idx = kv.value
			excelCpMeta[i].IdxG2 = kv.vG2
			excelCpMeta[i].IdxG3 = kv.vG3
			excelCpMeta[i].IdxG1TF = kv.vG1TF
			excelCpMeta[i].IdxG2TF = kv.vG2TF
			excelCpMeta[i].IdxG3TF = kv.vG3TF
		}
	}

	updateCpMeta := []*model.CounterPointMeta{}
	for _, v := range storedCpMeta {
		updateCpMeta = append(updateCpMeta, v)
	}

	sort.Slice(updateCpMeta, func(i, j int) bool {
		return updateCpMeta[i].SourceIdx < updateCpMeta[j].SourceIdx
	})
	glog.V(4).Info("ExcelMeta:")
	for k, v := range excelCpMeta {
		glog.V(4).Infof("Key: %+v Value: %+v\n", k, *v.CounterPointMeta)
	}
	glog.V(4).Info("UpdateMeta:")
	for i, v := range updateCpMeta {
		glog.V(4).Infof("Idx: %+v Value: %+v\n", i, v)
	}
	return excelCpMeta, updateCpMeta, nil
}

func returnInt(c string) int {
	if len(c) == 0 {
		return 0
	}
	i, err := strconv.Atoi(c)
	if err != nil {
		return 0
	}
	return i
}

func returnFloat(c string) float64 {
	if len(c) == 0 {
		return 0
	}
	f, err := strconv.ParseFloat(c, 64)
	if err != nil {
		return 0
	}
	return f
}

func returnMeterValue(cols []string, idx int) float64 {
	if idx < 0 || len(cols) <= (idx+1) {
		return 0
	}
	return returnFloat(cols[idx+1])
}

func returnMeterCode(c string) MeterCodeType {
	switch {
	case strings.Contains(c, "GESAMTVERBRAUCH LT. MESSUNG (BEI TEILNAHME GEM. ERZEUGUNG)"):
		return Total // G1 Consumer
	case strings.Contains(c, "VERBRAUCH LT. MESSUNG ENTSPRECHEND DEM TEILNAHMEFAKTOR JE ZP UND EC-ID"):
		return TotalTF
	case strings.Contains(c, "ANTEIL GEMEINSCHAFTLICHE ERZEUGUNG"):
		return Share // G2 Consumer
	case strings.Contains(c, "EIGENDECKUNG GEMEINSCHAFTLICHE ERZEUGUNG"):
		return Coverage // G3 Consumer
	case strings.Contains(c, "EIGENDECKUNG AUS ERNEUERBARER ENERGIE"):
		return CoverageTF // G3 Consumer
	case strings.Contains(c, "GESAMT/ÜBERSCHUSSERZEUGUNG, GEMEINSCHAFTSÜBERSCHUSS"):
		return Profit // G2 Producer
	case strings.Contains(c, "RESTÜBERSCHUSS BEI EG UND JE ZP"):
		return ProfitTF // G2 Producer
	case strings.Contains(c, "GESAMTE GEMEINSCHAFTLICHE ERZEUGUNG [KWH]"):
		return Total // G1 Producer
	case strings.Contains(c, "ERZEUGUNG LT. MESSUNG ENTSPRECHEND DEM TEILNAHMEFAKTOR UND EC-ID"):
		return TotalTF // G1 Producer
	default:
		return Bad
	}
}

func convertExcelMeterCode(code MeterCodeType) string {
	switch code {
	case Total:
		return "G.01"
	case Share:
		return "G.02"
	case Coverage:
		return "G.03"
	case Profit:
		return "G.02"
	}
	return ""
}

func isDate(cell string) bool {
	if len(cell) > 0 {
		if numberPattern.MatchString(cell) {
			return true
		}

		if dateLine.MatchString(cell) {
			return true
		}
	}
	return false
}

func isDateString(cell string) bool {
	if dateLine.MatchString(cell) {
		return true
	}
	return false
}

func getExcelDate(cell string) (int, int, int, int, int, int) {
	excelTime := parseExcelDate(cell).Round(15 * time.Minute)
	return excelTime.Day(), int(excelTime.Month()), excelTime.Year(), excelTime.Hour(), excelTime.Minute(), excelTime.Second()
}

func parseExcelDate(cell string) time.Time {
	if isDateString(cell) {
		return utils.StringToTime(cell, time.Now())
	} else {
		var excelEpoch = time.Date(1899, time.December, 30, 0, 0, 0, 0, time.UTC)
		var days, _ = strconv.ParseFloat(cell, 64)
		return excelEpoch.Add(time.Second * time.Duration(days*86400))
	}
	return time.Now()
}

func excelDateToString(cell string) string {
	d, m, y, hh, mm, ss := getExcelDate(cell)
	return fmt.Sprintf("%.2d.%.2d.%.4d %.2d:%.2d:%.2d", d, m, y, hh, mm, ss)
}
