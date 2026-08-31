package store

import (
	"fmt"
	"sort"
	"time"

	"at.ourproject/energystore/model"
	"at.ourproject/energystore/store/ebow"
	"at.ourproject/energystore/utils"
	"github.com/golang/glog"
)

func StoreEnergyV2(db *ebow.BowStorage, meteringPoint string, data *model.MqttEnergy) error {

	defaultDirection := utils.ExamineDirection(data.Data)

	var consumerCount int
	var producerCount int
	var metaCP *model.CounterPointMeta

	determineMeta := func() error {
		meta, info, err := PrepareMetaInfoMap(db, meteringPoint, defaultDirection)
		if err != nil {
			return err
		}

		consumerCount = info.ConsumerCount
		producerCount = info.ProducerCount

		metaCP = meta[meteringPoint]
		return nil
	}

	//// GetRawDataStructur from Period xxxx -> yyyy
	if err := determineMeta(); err != nil {
		return err
	}

	var resources *map[string]*model.RawSourceLine = &map[string]*model.RawSourceLine{}
	begin := time.UnixMilli(data.Start)
	end := time.UnixMilli(data.End)

	monitorStart := time.Now()
	fetchSourceRange(db, "CP", begin.Local(), end.Local(), resources)
	glog.V(4).Infof("Fetching source takes %v", time.Since(monitorStart).Milliseconds())

	var err error
	metaMeter := organizeMetaCodeImport(data.Data)
	if len(metaMeter) > 0 {
		monitorStart = time.Now()
		resources, err = importEnergyValuesV2(db.GetTenant(), metaMeter, data, metaCP, consumerCount, producerCount, resources, false)
		glog.V(4).Infof("Updateing source takes %v", time.Since(monitorStart).Milliseconds())
	}
	if err != nil {
		return err
	}

	glog.V(5).Infof("Update CP %s energy values (%d) from %s to %s",
		meteringPoint,
		len(*resources),
		time.UnixMilli(data.Start).Format(time.RFC822),
		time.UnixMilli(data.End).Format(time.RFC822))

	// Store updated RawDataStructure
	glog.V(5).Infof("Update/Override CP %s (%+v) energy values (%d) from %s to %s",
		meteringPoint,
		metaMeter,
		len(*resources),
		time.UnixMilli(data.Start).Format(time.RFC822),
		time.UnixMilli(data.End).Format(time.RFC822))

	updated := make([]*model.RawSourceLine, len(*resources))
	i := 0
	for _, v := range *resources {
		updated[i] = v
		i += 1

		glog.V(4).Infof("Update Source Line %+v", v)
	}

	sort.Slice(updated, func(i, j int) bool {
		return updated[i].Id < updated[j].Id
	})

	monitorStart = time.Now()
	err = db.SetLines(updated)
	glog.V(4).Infof("Writing source takes %v", time.Since(monitorStart).Milliseconds())

	if err != nil {
		glog.Error(err)
		return err
	}

	if c := updateMetaCP(metaCP, begin, end); c {
		err = updateMeta(db, metaCP, meteringPoint, begin, end)
	}
	return err
}

func organizeMetaCodeImport(data []model.MqttEnergyData) []*model.MeterCodeMeta {
	meterCodeMeta := []*model.MeterCodeMeta{}
	meterCodeMetaExt := []*model.MeterCodeMeta{}
	for i, d := range data {
		if meterMeta := utils.DecodeMeterCode(d.MeterCode, i); meterMeta != nil {
			if d.MeterCode == model.CODE_CON_TF || d.MeterCode == model.CODE_GEN_TF || d.MeterCode == model.CODE_COVER_TF || d.MeterCode == model.CODE_PLUS_TF {
				if d.MeterCode != model.CODE_COVER_TF {
					meterCodeMetaExt = append(meterCodeMetaExt, meterMeta)
				}
				continue
			}
			meterCodeMeta = append(meterCodeMeta, meterMeta)
		}
	}
	return append(meterCodeMeta, meterCodeMetaExt...)
}

func importEnergyValuesV2(
	tenant string,
	meterCode []*model.MeterCodeMeta,
	data *model.MqttEnergy,
	metaCP *model.CounterPointMeta,
	consumerCount, producerCount int,
	resources *map[string]*model.RawSourceLine,
	isExt bool) (*map[string]*model.RawSourceLine, error) {

	for _, mc := range meterCode {
		sort.Slice(data.Data[mc.SourceInData].Value, func(i, j int) bool {
			a := time.UnixMilli(data.Data[mc.SourceInData].Value[i].From)
			b := time.UnixMilli(data.Data[mc.SourceInData].Value[j].From)
			return a.Unix() < b.Unix()
		})
	}

	var tablePrefix = "CP/"
	for _, mc := range meterCode {
		if mc.SourceInData < len(data.Data) {
			//_wg.Add(1)
			rowIdVisited := map[string]bool{}
			for i := 0; i < len(data.Data[mc.SourceInData].Value); i++ {
				v := data.Data[mc.SourceInData].Value[i]

				id, err := utils.ConvertUnixTimeToRowId(tablePrefix, time.UnixMilli(v.From))
				if err != nil {
					return resources, err
				}
				_, ok := (*resources)[id]
				if !ok {
					(*resources)[id] = model.MakeRawSourceLine(id, consumerCount, producerCount) //&model.RawSourceLine{Id: id, Consumers: make([]float64, consumerCount), Producers: make([]float64, producerCount)}
				}
				_, visited := rowIdVisited[id]
				if visited {
					// Just a specific function for winter-time-switch. If in an energy day file timestamps occur twice add those values.
					sumEnergyValueToResource((*resources)[id], metaCP, mc, v, isExt)
				} else {
					addEnergyValueToResource((*resources)[id], metaCP, mc, v, isExt)
				}
				rowIdVisited[id] = true
			}
		} else {
			glog.Errorf("Energie Values %+v different %+v", mc, metaCP)
		}
	}
	return resources, nil
}

func sumEnergyValueToResource(resource *model.RawSourceLine, metaCP *model.CounterPointMeta, meterCode *model.MeterCodeMeta, v model.MqttEnergyValue, isExt bool) {
	// Exit the function if the extended MeterCode is zero
	if isExt && v.Value == 0 {
		return
	}

	switch metaCP.Dir {
	case model.CONSUMER_DIRECTION:
		resource.Consumers[(metaCP.SourceIdx*3)+meterCode.SourceDelta] += v.Value
	case model.PRODUCER_DIRECTION:
		resource.Producers[(metaCP.SourceIdx*2)+meterCode.SourceDelta] += v.Value
	}
}

func addEnergyValueToResource(resource *model.RawSourceLine, metaCP *model.CounterPointMeta, meterCode *model.MeterCodeMeta, v model.MqttEnergyValue, isExt bool) {
	// Exit the function if the extended MeterCode is zero
	if isExt && v.Value == 0 {
		qov := 0
		switch metaCP.Dir {
		case model.CONSUMER_DIRECTION:
			qov = utils.GetInt(resource.QoVConsumers, (metaCP.SourceIdx*3)+meterCode.SourceDelta)
		case model.PRODUCER_DIRECTION:
			qov = utils.GetInt(resource.QoVProducers, (metaCP.SourceIdx*2)+meterCode.SourceDelta)
		}
		if qov > 0 {
			return
		}
	}

	switch metaCP.Dir {
	case model.CONSUMER_DIRECTION:
		resource.Consumers = utils.Insert(resource.Consumers, (metaCP.SourceIdx*3)+meterCode.SourceDelta, v.Value)
		resource.QoVConsumers = utils.InsertInt(resource.QoVConsumers, (metaCP.SourceIdx*3)+meterCode.SourceDelta, utils.CastQoVStringToInt(v.Method))
	case model.PRODUCER_DIRECTION:
		resource.Producers = utils.Insert(resource.Producers, (metaCP.SourceIdx*2)+meterCode.SourceDelta, v.Value)
		resource.QoVProducers = utils.InsertInt(resource.QoVProducers, (metaCP.SourceIdx*2)+meterCode.SourceDelta, utils.CastQoVStringToInt(v.Method))
	}
}

func fetchSourceRange(db *ebow.BowStorage, key string, start, end time.Time, resources *map[string]*model.RawSourceLine) {
	sYear, sMonth, sDay := start.Year(), int(start.Month()), start.Day()
	eYear, eMonth, eDay := end.Year(), int(end.Month()), end.Day()

	iter := db.GetLineRange(key, fmt.Sprintf("%.4d/%.2d/%.2d/", sYear, sMonth, sDay), fmt.Sprintf("%.4d/%.2d/%.2d/", eYear, eMonth, eDay))
	defer iter.Close()

	var _line model.RawSourceLine
	for iter.Next(&_line) {
		l := _line.Copy(len(_line.Consumers))
		(*resources)[_line.Id] = &l
	}
}

func updateMetaCP(metaCP *model.CounterPointMeta, begin, end time.Time) bool {

	changed := false
	metaBegin := utils.StringToTime(metaCP.PeriodStart, time.Now())
	metaEnd := utils.StringToTime(metaCP.PeriodEnd, time.Unix(1, 0))

	if begin.Before(metaBegin) {
		metaCP.PeriodStart = utils.DateToString(begin)
		changed = true
	}
	if end.After(metaEnd) {
		metaCP.PeriodEnd = utils.DateToString(end)
		changed = true
	}

	return changed
}

// updateMeta persists the period of one counter point into the EC-wide cpmeta/0
// record.
//
// The widening is re-applied against the record as it currently is on disk
// instead of assigning metaCP's fields: metaCP was read at the start of
// StoreEnergyV2, and another day block of the same message may have extended the
// period in the meantime. Assigning blindly dropped that extension again — the
// block finishing last won even with an older end date, which surfaced as a
// period truncated in the dashboard while the measured values were complete.
func updateMeta(db ebow.IBowStorage, metaCP *model.CounterPointMeta, cp string, begin, end time.Time) error {
	var err error
	var meta *model.RawSourceMeta
	if meta, err = db.GetMeta(fmt.Sprintf("cpmeta/%s", "0")); err == nil {
		for _, m := range meta.CounterPoints {
			if m.Name == cp {
				updateMetaCP(m, begin, end)
				m.Count = metaCP.Count

				return db.SetMeta(meta)
			}
		}
	}
	return err
}
