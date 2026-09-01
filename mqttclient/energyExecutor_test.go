package mqttclient

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"at.ourproject/energystore/calculation"
	"at.ourproject/energystore/model"
	"at.ourproject/energystore/store"
	"at.ourproject/energystore/store/ebow"
	"at.ourproject/energystore/utils"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encryptMessage(msg []byte) ([]byte, error) {
	compressed, err := gzipData(msg)
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(compressed)
	return []byte(encoded), nil
}

func gzipData(data []byte) ([]byte, error) {
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	defer gz.Close()
	if _, err := gz.Write(data); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func TestDecodeMessageRejectsIncompatibleTransportPayload(t *testing.T) {
	require.Nil(t, decodeMessage([]byte("not-a-base64-cr-msg")))

	notGzip := base64.StdEncoding.EncodeToString([]byte("not-gzip"))
	require.Nil(t, decodeMessage([]byte(notGzip)))
}

func TestNewMqttEnergyImporter(t *testing.T) {
	timeV1, err := utils.ParseTime("24.10.2022 00:00:00", time.Now().UnixMilli())
	timeV2, err := utils.ParseTime("24.10.2022 00:15:00", time.Now().UnixMilli())
	require.NoError(t, err)
	tests := []struct {
		name     string
		energy   *model.MqttEnergyMessage
		expected func(t *testing.T, l *model.RawSourceLine)
	}{
		{
			name: "Insert New Energy Allocated",
			energy: &model.MqttEnergyMessage{
				EcId: "ecIdTest1",
				Meter: model.EnergyMeter{
					MeteringPoint: "AT0030000000000000000000000000001",
					Direction:     string(model.CONSUMER_DIRECTION),
				},
				Energy: []model.MqttEnergy{{
					Start: timeV1.UnixMilli(),
					End:   timeV2.UnixMilli(),
					Data: []model.MqttEnergyData{
						model.MqttEnergyData{
							MeterCode: "1-1:1.9.0 G.01",
							Value: []model.MqttEnergyValue{
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "L1",
									Value:  1.11,
								},
							},
						},
					},
				}},
			},
			expected: func(t *testing.T, l *model.RawSourceLine) {
				require.Equal(t, 1, len(l.Consumers))
				assert.Equal(t, 1.11, l.Consumers[0])
			},
		},
		{
			name: "Second Energy Consumer",
			energy: &model.MqttEnergyMessage{
				EcId: "ecIdTest1",
				Meter: model.EnergyMeter{
					MeteringPoint: "AT0030000000000000000000000000002",
					Direction:     "",
				},
				Energy: []model.MqttEnergy{{
					Start: timeV1.UnixMilli(),
					End:   timeV2.UnixMilli(),
					Data: []model.MqttEnergyData{
						model.MqttEnergyData{
							MeterCode: "1-1:1.9.0 G.01",
							Value: []model.MqttEnergyValue{
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "",
									Value:  0.11,
								},
							},
						},
					},
				}},
			},
			expected: func(t *testing.T, l *model.RawSourceLine) {
				require.Equal(t, 4, len(l.Consumers))
				assert.Equal(t, 1.11, l.Consumers[0])
				assert.Equal(t, 0.11, l.Consumers[3])
			},
		},
		{
			name: "Insert Generator energy values",
			energy: &model.MqttEnergyMessage{
				EcId: "ecIdTest1",
				Meter: model.EnergyMeter{
					MeteringPoint: "AT0030000000000000000000030000011",
					Direction:     "",
				},
				Energy: []model.MqttEnergy{{
					Start: timeV1.UnixMilli(),
					End:   timeV2.UnixMilli(),
					Data: []model.MqttEnergyData{
						model.MqttEnergyData{
							MeterCode: "1-1:2.9.0 P.01",
							Value: []model.MqttEnergyValue{
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "",
									Value:  0.11,
								},
							},
						},
						model.MqttEnergyData{
							MeterCode: "1-1:1.9.0 G.01",
							Value: []model.MqttEnergyValue{
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "",
									Value:  10.1,
								},
							},
						},
					},
				}},
			},
			expected: func(t *testing.T, l *model.RawSourceLine) {
				require.Equal(t, 2, len(l.Producers))
				assert.Equal(t, 10.1, l.Producers[0])
			},
		},
		{
			name: "Insert second Generator Allocated",
			energy: &model.MqttEnergyMessage{
				EcId: "ecIdTest1",
				Meter: model.EnergyMeter{
					MeteringPoint: "AT0030000000000000000000030000010",
					Direction:     "",
				},
				Energy: []model.MqttEnergy{{
					Start: timeV1.UnixMilli(),
					End:   timeV2.UnixMilli(),
					Data: []model.MqttEnergyData{
						model.MqttEnergyData{
							MeterCode: model.CODE_PLUS,
							Value: []model.MqttEnergyValue{
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "",
									Value:  20.1,
								},
							},
						},
						model.MqttEnergyData{
							MeterCode: model.CODE_GEN,
							Value: []model.MqttEnergyValue{
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "",
									Value:  21.1,
								},
							},
						},
					},
				}},
			},
			expected: func(t *testing.T, l *model.RawSourceLine) {
				require.Equal(t, 4, len(l.Producers))
				assert.Equal(t, 10.1, l.Producers[0])
				assert.Equal(t, 21.1, l.Producers[2])
				assert.Equal(t, 20.1, l.Producers[3])
			},
		},
		{
			name: "Insert Generator - summarize energy values",
			energy: &model.MqttEnergyMessage{
				EcId: "ecIdTest2",
				Meter: model.EnergyMeter{
					MeteringPoint: "AT0030000000000000000000030000010",
					Direction:     "",
				},
				Energy: []model.MqttEnergy{{
					Start: timeV1.UnixMilli(),
					End:   timeV2.UnixMilli(),
					Data: []model.MqttEnergyData{
						model.MqttEnergyData{
							MeterCode: model.CODE_PLUS,
							Value: []model.MqttEnergyValue{
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "L1",
									Value:  20.1,
								},
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "L1",
									Value:  10.1,
								},
							},
						},
						model.MqttEnergyData{
							MeterCode: model.CODE_GEN,
							Value: []model.MqttEnergyValue{
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "L1",
									Value:  5.1,
								},
								model.MqttEnergyValue{
									From:   timeV1.UnixMilli(),
									To:     timeV2.UnixMilli(),
									Method: "L1",
									Value:  2.2,
								},
							},
						},
					},
				}},
			},
			expected: func(t *testing.T, l *model.RawSourceLine) {
				fmt.Printf("Producer Line: %+v\n", l)
				require.Equal(t, 2, len(l.Producers))
				assert.Equal(t, 7.3, utils.RoundToFixed(l.Producers[0], 1))
				assert.Equal(t, 1, l.QoVProducers[0])
				assert.Equal(t, 30.2, utils.RoundToFixed(l.Producers[1], 1))
				assert.Equal(t, 1, l.QoVProducers[1])
			},
		},
	}

	viper.Set("persistence.path", "../test/rawdata")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			importer := NewTenantEnergyImporter("importer", &MQTTStreamer{})
			err = importer.Import(tt.energy)
			require.NoError(t, err)

			db, err := ebow.OpenStorage("importer", tt.energy.EcId)
			require.NoError(t, err)
			defer db.Close()

			it := db.GetLinePrefix(fmt.Sprintf("CP/%s", "2022/10/24"))
			defer it.Close()

			var _line model.RawSourceLine

			r := it.Next(&_line)
			assert.Equal(t, true, r)
			assert.Equal(t, "CP/2022/10/24/00/00/00", _line.Id)
			tt.expected(t, &_line)
		})
	}

	assert.NoError(t, os.RemoveAll("../test/rawdata/importer"))
}

func TestImportRawdataStore(t *testing.T) {

	viper.Set("persistence.path", "../test/rawdata")

	jsonRaw, err := os.ReadFile("../test/energy-response-new-text.json")
	require.NoError(t, err)

	compressed, err := encryptMessage(jsonRaw)
	require.NoError(t, err)

	rawData := decodeMessage(compressed)
	require.NotNil(t, rawData)

	importer := NewTenantEnergyImporter("te100190", &MQTTStreamer{})

	err = importer.Import(rawData)
	require.NoError(t, err)

	rawData.Meter.MeteringPoint = "AT0030000000000000000000000381702"
	err = importer.Import(rawData)
	require.NoError(t, err)

	importer.Close()

	db, err := ebow.OpenStorageTest("te100190", "AT00400000000RC101590000000400111", "../test/rawdata")
	require.NoError(t, err)

	meta, err := db.GetMeta("cpmeta/0")
	for i, v := range meta.CounterPoints {
		fmt.Printf("[%d]: %+v\n", i, v)
	}

	it := db.GetLinePrefix("CP/")

	line := model.RawSourceLine{}
	lines := []*model.RawSourceLine{}
	for it.Next(&line) {
		_line := line.Copy(len(line.Consumers))
		lines = append(lines, &_line)
	}
	it.Close()
	db.CloseTestDriver()

	require.Equal(t, 24*4, len(lines)) // one hour is missing from the test source file

	participantReports := []model.ParticipantReport{model.ParticipantReport{
		ParticipantId: "Participant01",
		Meters: []*model.MeterReport{
			&model.MeterReport{
				MeterId: "AT0030000000000000000000000381702",
				From:    time.Date(2023, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli(),
				Until:   time.Date(2023, 12, 31, 0, 0, 0, 0, time.Local).UnixMilli(),
				Report:  nil,
			},
		},
	}}

	energy, err := calculation.EnergyReportV2("te100190", "AT00400000000RC101590000000400111", participantReports, 2023, 3, "YM")
	require.NoError(t, err)

	response, err := json.Marshal(energy)
	require.NoError(t, err)

	require.Equal(t, 1, len(energy.ParticipantReports[0].Meters[0].Report.Intermediate.Allocation))
	//require.Equal(t, 1.088021, energy.Report.Allocated[0])
	//require.Equal(t, 3, len(energy.Report.Consumed))
	//require.Equal(t, 5.388, energy.Report.Consumed[0])

	fmt.Printf("META_DATA: %+v\n", string(response))

	os.RemoveAll("../test/rawdata/rc100190")
}

func loadTestData() (*model.MqttEnergyMessage, error) {
	content, err := os.ReadFile("../energy-mass-test-data.json")
	if err != nil {
		return nil, err
	}
	var obj model.MqttEnergyMessage
	err = json.Unmarshal(content, &obj)
	return &obj, err
}

func TestMassImport(t *testing.T) {

	viper.Set("persistence.path", "../test/rawdata")

	testData, err := loadTestData()
	require.NoError(t, err)

	tenant := "TE100888"
	ecId := "AT00300000000RC100181000000956509"

	startTime := int64(1759269600000)
	endTime := int64(1761951600000)

	importer := NewTenantEnergyImporter("TE100888", &MQTTStreamer{})
	err = importer.Import(testData)
	require.NoError(t, err)

	resp, err := store.QueryRawData(tenant, ecId, time.UnixMilli(startTime), time.UnixMilli(endTime), []store.TargetMP{{MeteringPoint: "AT0030000000000000000000000383545"}}, map[string][]string{})
	require.NoError(t, err)

	resultEntry := resp["AT0030000000000000000000000383545"]
	//fmt.Printf("Length: %d\n", len(resultEntry.Data))
	//fmt.Printf("Data on postition[0000]: %f\n", resultEntry.Data[0].Value)
	//fmt.Printf("Data on postition[1000]: %d\n", resultEntry.Data[1000].Ts)
	//fmt.Printf("Data on postition[2975]: %d\n", resultEntry.Data[2975].Ts)

	assert.Equal(t, 2976, len(resultEntry.Data))

	assert.Equal(t, 0.0, resultEntry.Data[0].Value[0])

	assert.Equal(t, int64(1760169600000), resultEntry.Data[1000].Ts)
	assert.Equal(t, 0.001, resultEntry.Data[1000].Value[0])
	assert.Equal(t, 0.000077, resultEntry.Data[1000].Value[1])
	assert.Equal(t, 0.000077, resultEntry.Data[1000].Value[2])

	assert.Equal(t, int64(1761950700000), resultEntry.Data[2975].Ts)
	assert.Equal(t, 0.001, resultEntry.Data[2975].Value[0])
	assert.Equal(t, 0.000028, resultEntry.Data[2975].Value[1])
	assert.Equal(t, 0.000028, resultEntry.Data[2975].Value[2])

	//fmt.Printf("Response: %+v\n", resp["AT0030000000000000000000000383545"])

}
