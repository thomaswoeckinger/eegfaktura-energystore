package excel

import (
	"at.ourproject/energystore/model"
	"at.ourproject/energystore/store/ebow"
	"at.ourproject/energystore/utils"
	"fmt"
	"github.com/golang/glog"
	"github.com/xuri/excelize/v2"
	"math"
	"strings"
	"time"
	"unicode"
)

func normalizeHeader(header string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, header)
}

func calcRawDataMatrixLen(a []float64, step int) int {
	l := len(a) - 1
	if l < 1 {
		return 1
	}
	return int(math.Ceil(float64(l) / float64(step)))
}

func ImportExcelEnergyFileNew(f *excelize.File, sheet string, db ebow.IBowStorage) error {

	exp := "DD.MM.YYYY HH:MM:SS"
	style, err := f.NewStyle(&excelize.Style{CustomNumFmt: &exp})
	err = f.SetCellStyle(sheet, "A12", "A15", style)

	rows, err := f.Rows(sheet)
	if err != nil {
		glog.Error(err)
		return err
	}
	defer rows.Close()

	var rIdx int = 1
	var rawDatas []*model.RawSourceLine = []*model.RawSourceLine{}
	rowIdVisited := map[string]*model.RawSourceLine{}

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
			switch normalizeHeader(cols[0]) {
			case "meteringpointid":
				excelHeader.meteringPointId = make(map[int]string, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.meteringPointId[i] = c
				}
			case "spaltensumme", "meteringinterval",
				"name", "meteringreason", "numberofmeteringintervals",
				"spaltensummeminimalequalität", "datacompleteness",
				"meteringpointactiveend", "meteringpointactivestart",
				"dataperiodend", "dataperiodstart":
				continue
			case "energydirection":
				excelHeader.energyDirection = make(map[int]model.MeterDirection, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.energyDirection[i] = model.MeterDirection(c)
				}
			case "periodend", "reportfilterend":
				excelHeader.periodEnd = make(map[int]string, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.periodEnd[i] = excelDateToString(c)
				}
			case "periodstart", "reportfilterstart":
				excelHeader.periodStart = make(map[int]string, len(cols)-1)
				for i, c := range cols[1:] {
					excelHeader.periodStart[i] = excelDateToString(c)
				}
			case "metercode":
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
						if err != nil {
							return err
						}
						excelHeaderInitialized = true
					}

					//
					// Insert G1 values
					//
					rawDataId := fmt.Sprintf("CP/%d/%.2d/%.2d/%.2d/%.2d/%.2d", y, m, d, hh, mm, ss)
					rawData, visited := rowIdVisited[rawDataId]
					if !visited {
						rawData = &model.RawSourceLine{Consumers: []float64{}, Producers: []float64{}, QoVConsumers: []int{}, QoVProducers: []int{}}
						//rawData.Id = fmt.Sprintf("CP/%d/%.2d/%.2d/%.2d/%.2d/%.2d", y, m, d, hh, mm, ss)
						rawData.Id = rawDataId
						_ = db.GetLine(rawData)
					}

					consumerMatrix := model.MakeMatrix(rawData.Consumers, calcRawDataMatrixLen(rawData.Consumers, 3), 3)
					producerMatrix := model.MakeMatrix(rawData.Producers, calcRawDataMatrixLen(rawData.Producers, 2), 2)

					for i := 0; i < len(excelCpMeta); i++ {
						v := excelCpMeta[i]
						switch v.Dir {
						case "CONSUMPTION":
							if visited {
								consumerMatrix.SumElm(v.SourceIdx, 0, returnMeterValue(cols, v.Idx))
								consumerMatrix.SumElm(v.SourceIdx, 1, returnMeterValue(cols, v.IdxG2))
								consumerMatrix.SumElm(v.SourceIdx, 2, returnMeterValue(cols, v.IdxG3))

								overrideWithExtendedValues(consumerMatrix, cols, 0, v.SourceIdx, v.IdxG1TF, visited)
								overrideWithExtendedValues(consumerMatrix, cols, 1, v.SourceIdx, v.IdxG2TF, visited)
								overrideWithExtendedValues(consumerMatrix, cols, 2, v.SourceIdx, v.IdxG3TF, visited)
							} else {
								consumerMatrix.SetElm(v.SourceIdx, 0, returnMeterValue(cols, v.Idx))
								consumerMatrix.SetElm(v.SourceIdx, 1, returnMeterValue(cols, v.IdxG2))
								consumerMatrix.SetElm(v.SourceIdx, 2, returnMeterValue(cols, v.IdxG3))

								overrideWithExtendedValues(consumerMatrix, cols, 0, v.SourceIdx, v.IdxG1TF, visited)
								overrideWithExtendedValues(consumerMatrix, cols, 1, v.SourceIdx, v.IdxG2TF, visited)
								overrideWithExtendedValues(consumerMatrix, cols, 2, v.SourceIdx, v.IdxG3TF, visited)
							}
							rawData.QoVConsumers = utils.InsertInt(rawData.QoVConsumers, v.SourceIdx*3, 1)
							rawData.QoVConsumers = utils.InsertInt(rawData.QoVConsumers, (v.SourceIdx*3)+1, 1)
							rawData.QoVConsumers = utils.InsertInt(rawData.QoVConsumers, (v.SourceIdx*3)+2, 1)
							v.Count += 1
						case "GENERATION":
							if visited {
								producerMatrix.SumElm(v.SourceIdx, 0, returnMeterValue(cols, v.Idx))
								producerMatrix.SumElm(v.SourceIdx, 1, returnMeterValue(cols, v.IdxG2))

								overrideWithExtendedValues(producerMatrix, cols, 0, v.SourceIdx, v.IdxG1TF, visited)
								overrideWithExtendedValues(producerMatrix, cols, 1, v.SourceIdx, v.IdxG2TF, visited)
							} else {
								producerMatrix.SetElm(v.SourceIdx, 0, returnMeterValue(cols, v.Idx))
								producerMatrix.SetElm(v.SourceIdx, 1, returnMeterValue(cols, v.IdxG2))

								overrideWithExtendedValues(producerMatrix, cols, 0, v.SourceIdx, v.IdxG1TF, visited)
								overrideWithExtendedValues(producerMatrix, cols, 1, v.SourceIdx, v.IdxG2TF, visited)
							}
							rawData.QoVProducers = utils.InsertInt(rawData.QoVProducers, v.SourceIdx*2, 1)
							rawData.QoVProducers = utils.InsertInt(rawData.QoVProducers, (v.SourceIdx*2)+1, 1)
							v.Count += 1
						}
					}
					//rawDatas = append(rawDatas,
					//	&model.RawSourceLine{Id: rawData.Id, Consumers: consumerMatrix.Elements, Producers: producerMatrix.Elements,
					//		QoVConsumers: rawData.QoVConsumers, QoVProducers: rawData.QoVProducers})
					rIdx += 1
					rowIdVisited[rawDataId] = &model.RawSourceLine{Id: rawData.Id, Consumers: consumerMatrix.Elements, Producers: producerMatrix.Elements,
						QoVConsumers: rawData.QoVConsumers, QoVProducers: rawData.QoVProducers}
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

	for _, v := range rowIdVisited {
		rawDatas = append(rawDatas, v)
	}

	if err := db.SetLines(rawDatas); err != nil {
		return err
	}

	rawMeta := &model.RawSourceMeta{Id: fmt.Sprintf("cpmeta/%d", 0), CounterPoints: updatedCpMeta, NumberOfMetering: rIdx}
	err = db.SetMeta(rawMeta)
	if err != nil {
		glog.Error(err.Error())
		return err
	}
	glog.V(4).Infof("Import %d lines", len(rawDatas))
	glog.Infof("Time taken for import energy via excel: %v (%d Rows) tenant=%s", time.Since(t), totalRowCols, db.GetTenant())

	years := []int{}
	for k, _ := range yearSet {
		years = append(years, k)
	}
	return nil
}

func overrideWithExtendedValues(matrix *model.Matrix, cols []string, matrixIdx, sourceIdx, idx int, visited bool) {
	extVal := returnMeterValue(cols, idx)
	if extVal > 0 {
		if visited {
			matrix.SumElm(sourceIdx, matrixIdx, extVal)
		} else {
			matrix.SetElm(sourceIdx, matrixIdx, extVal)
		}
	}
}
