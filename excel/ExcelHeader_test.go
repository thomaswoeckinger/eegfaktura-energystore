package excel

import "testing"

func TestNormalizeHeaderAcceptsEdaVariants(t *testing.T) {
	tests := map[string]string{
		"MeteringPointId":              "meteringpointid",
		"MeteringpointID":              "meteringpointid",
		"Number of Metering Intervals": "numberofmeteringintervals",
		"Number of MeteringIntervals":  "numberofmeteringintervals",
		"Report Filter End":            "reportfilterend",
		"Report Filter end":            "reportfilterend",
	}
	for input, expected := range tests {
		if got := normalizeHeader(input); got != expected {
			t.Errorf("normalizeHeader(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestExcelDateAcceptsMissingSeconds(t *testing.T) {
	if !isDate("31.07.2026 23:45") {
		t.Fatal("EDA timestamp without seconds was rejected")
	}
	if got := excelDateToString("31.07.2026 23:45"); got != "31.07.2026 23:45:00" {
		t.Fatalf("excelDateToString() = %q", got)
	}
}
