package utils

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGetMonthDuration(t *testing.T) {
	startTime := time.Date(2022, time.April, 18, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2022, time.May, 30, 23, 45, 0, 0, time.UTC)

	y, d := GetMonthDuration(startTime, endTime)

	assert.Equal(t, y, 2022)
	assert.Equal(t, d, 1)

}

func TestGetMonthDurationDec(t *testing.T) {
	startTime := time.Date(2022, time.December, 31, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2022, time.December, 31, 23, 45, 0, 0, time.UTC)

	y, d := GetMonthDuration(startTime, endTime)

	assert.Equal(t, y, 2022)
	assert.Equal(t, d, 0)
}

func TestParseTime(t *testing.T) {
	expectedTime := time.Date(2022, time.April, 18, 0, 0, 0, 0, time.Local)
	strTime := "18.04.2022 00:00:00"
	d, err := ParseTime(strTime, time.Now().UnixMilli())
	assert.NoError(t, err)

	fmt.Printf("Actual-Time: %v\n", d)
	fmt.Printf("Expected-Time: %v\n", expectedTime)

	assert.Equal(t, d, expectedTime)
}

func TestConvertUnixTimeToRowId(t *testing.T) {
	rowId, err := ConvertUnixTimeToRowId("CP/", time.UnixMilli(1688680800000).UTC())
	require.NoError(t, err)

	fmt.Printf("RowID: %v\n", rowId)
}

// The store writes its period metadata with DateToString and reads it back with
// StringToTime. That round trip was never asserted, which is how the %.4d
// seconds verb survived: the reader happened to be lenient enough to absorb it.
func TestDateToStringStringToTimeRoundTrip(t *testing.T) {
	for _, d := range []time.Time{
		time.Date(2023, time.December, 30, 15, 0, 0, 0, time.Local),
		time.Date(2023, time.December, 30, 15, 15, 30, 0, time.Local),
		time.Date(2026, time.July, 31, 23, 45, 5, 0, time.Local),
	} {
		rendered := DateToString(d)
		require.Equal(t, d, StringToTime(rendered, time.Unix(1, 0)), "round trip of %q", rendered)
	}
}

func TestDateToStringUsesTwoDigitSeconds(t *testing.T) {
	assert.Equal(t, "30.12.2023 15:00:00", DateToString(time.Date(2023, time.December, 30, 15, 0, 0, 0, time.Local)))
	assert.Equal(t, "30.12.2023 15:00:07", DateToString(time.Date(2023, time.December, 30, 15, 0, 7, 0, time.Local)))
}

// Records written before the DateToString fix carry a four-digit seconds field.
// They are what is on disk in production, so they must stay readable.
func TestStringToTimeAcceptsLegacyFourDigitSeconds(t *testing.T) {
	expected := time.Date(2023, time.December, 30, 15, 0, 0, 0, time.Local)
	assert.Equal(t, expected, StringToTime("30.12.2023 15:00:0000", time.Unix(1, 0)))

	expected = time.Date(2023, time.December, 30, 15, 0, 30, 0, time.Local)
	assert.Equal(t, expected, StringToTime("30.12.2023 15:00:0030", time.Unix(1, 0)))
}

// EDA's offline exports omit the seconds.
func TestParseTimeAcceptsMissingSeconds(t *testing.T) {
	parsed, err := ParseTime("31.07.2026 23:45", 0)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.July, 31, 23, 45, 0, 0, time.Local), parsed)
}

func TestParseTimeRejectsGarbage(t *testing.T) {
	parsed, err := ParseTime("not a date", 1234)
	require.Error(t, err)
	assert.Equal(t, time.UnixMilli(1234), parsed)
}

func TestStringToTimeFallsBackOnGarbage(t *testing.T) {
	fallback := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.Local)
	assert.Equal(t, fallback, StringToTime("", fallback))
	assert.Equal(t, fallback, StringToTime("30.12.2023", fallback))
}
