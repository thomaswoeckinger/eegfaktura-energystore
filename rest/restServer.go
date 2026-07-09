package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"at.ourproject/energystore/calculation"
	"at.ourproject/energystore/excel"
	"at.ourproject/energystore/middleware"
	"at.ourproject/energystore/model"
	"at.ourproject/energystore/services"
	"at.ourproject/energystore/store"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
)

func NewRestServer() *mux.Router {
	//jwtWrapper := middleware.JWTMiddleware(viper.GetString("jwt.pubKeyFile"))
	r := mux.NewRouter()
	//s := r.PathPrefix("/rest").Subrouter()
	//r.HandleFunc("/eeg/{year}/{month}", jwtWrapper(fetchEnergy())).Methods("GET")
	//r.HandleFunc("/eeg/report", middleware.ProtectApp(fetchEnergyReport())).Methods("POST")
	r.HandleFunc("/eeg/v2/{ecid}/report", middleware.ProtectApp(fetchEnergyReportV2())).Methods("POST")
	r.HandleFunc("/eeg/v2/{ecid}/meta", middleware.ProtectApp(queryMetaData())).Methods("GET")
	r.HandleFunc("/eeg/v2/{ecid}/raw", middleware.ProtectApp(fetchRawEnergyV2())).Methods("POST")
	r.HandleFunc("/eeg/v2/{ecid}/rawdata/delete", middleware.ProtectApp(deleteRawData())).Methods("POST")
	r.HandleFunc("/eeg/v2/{ecid}/intra-day-report", middleware.ProtectApp(fetchIntraDayReportV2())).Methods("POST")
	r.HandleFunc("/eeg/v2/{ecid}/load-curve-report", middleware.ProtectApp(fetchLoadCurveReportV2())).Methods("POST")
	r.HandleFunc("/eeg/v2/{ecid}/load-curve-report", middleware.ProtectApp(getLoadCurveReportV2())).Methods("GET")
	r.HandleFunc("/eeg/v2/{ecid}/combined-report", middleware.ProtectApp(fetchCombinedReportV2())).Methods("POST")
	r.HandleFunc("/eeg/v2/{ecid}/combined-report", middleware.ProtectApp(getCombinedReportV2())).Methods("GET")
	r.HandleFunc("/eeg/v2/{ecid}/summary", middleware.ProtectApp(fetchSummaryReportV2())).Methods("POST")
	r.HandleFunc("/eeg/{ecid}/lastRecordDate", middleware.ProtectApp(lastRecordDate())).Methods("GET")
	r.HandleFunc("/eeg/{ecid}/excel/export/{year}/{month}", middleware.ProtectApp(exportMeteringData())).Methods("POST")
	r.HandleFunc("/eeg/{ecid}/excel/report/download", middleware.ProtectApp(exportReport())).Methods("POST")

	r = InitQueryApiRouter(r)
	return r
}

// fetchEnergyReport Rest endpoint retrieve energy values of requested participant and period pattern.
//func fetchEnergyReport() middleware.JWTHandlerFunc {
//	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
//		energy := &model.EegEnergy{}
//
//		var request model.EnergyReportRequest
//		err := json.NewDecoder(r.Body).Decode(&request)
//		if err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//
//		if energy, err = calculation.EnergyReport(tenant, request.Year, request.Segment, request.Period); err != nil {
//			respondWithError(w, http.StatusInternalServerError, err.Error())
//			return
//		}
//
//		resp := struct {
//			Eeg *model.EegEnergy `json:"eeg"`
//		}{Eeg: energy}
//
//		respondWithJSON(w, http.StatusOK, &resp)
//	}
//}

// fetchEnergyReportV2 Rest endpoint retrieve energy values of requested participant and period pattern.
func fetchEnergyReportV2() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecid := vars["ecid"]
		startMonitor := time.Now()
		glog.V(4).Infof("Start Time Monitor fetchEnergyReport. %s\n", tenant)
		energy := &model.ReportResponse{}

		var request model.ReportRequest
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if energy, err = calculation.EnergyReportV2(tenant, ecid, request.Participants, request.ReportInterval.Year, request.ReportInterval.Segment, request.ReportInterval.Period); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		glog.V(4).Infof("Time Monitor fetchEnergyReport. %v\n", time.Now().Sub(startMonitor))
		respondWithJSON(w, http.StatusOK, &energy)
	}
}

// fetchRawEnergyV2 Rest endpoint retrieve energy values of requested participant and period pattern.
func fetchRawEnergyV2() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecId := vars["ecid"]
		startMonitor := time.Now()
		glog.V(4).Infof("Start Time Monitor fetchEnergyReport. %s\n", tenant)

		var request model.RawDataRequest
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cps := make([]store.TargetMP, len(request.Meters))
		for i, meter := range request.Meters {
			cps[i] = store.TargetMP{MeteringPoint: meter}
		}

		resp, err := store.QueryRawData(tenant, ecId, time.UnixMilli(request.Start), time.UnixMilli(request.End), cps, r.URL.Query())
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		glog.V(4).Infof("Time Monitor fetchRawData. %v\n", time.Now().Sub(startMonitor))
		respondWithJSON(w, http.StatusOK, &resp)
	}
}

// deleteRawData zeros the raw energy data of a single metering point within a
// time range (Ops maintenance). dryRun=true previews the affected amount without
// writing. Behind ProtectApp: cross-tenant deletion requires the superuser realm
// role; otherwise the caller's tenant claim must contain the target tenant.
func deleteRawData() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		ecid := mux.Vars(r)["ecid"]

		var request struct {
			MeteringPoint string `json:"meteringPoint"`
			Start         int64  `json:"start"`
			End           int64  `json:"end"`
			DryRun        bool   `json:"dryRun"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		if ecid == "" || request.MeteringPoint == "" || request.Start == 0 || request.End == 0 || request.Start >= request.End {
			respondWithError(w, http.StatusBadRequest, "ecid, meteringPoint and a valid start<end range are required")
			return
		}

		from := time.UnixMilli(request.Start)
		to := time.UnixMilli(request.End)

		affected, sumKwh, err := store.DeleteRawDataForMeteringPoint(tenant, ecid, request.MeteringPoint, from, to, request.DryRun)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		if !request.DryRun {
			glog.Warningf("RAWDATA-DELETE operator=%q tenant=%s ec=%s zp=%s from=%s to=%s timesteps=%d",
				claims.Username, tenant, ecid, request.MeteringPoint,
				from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), affected)
		}

		respondWithJSON(w, http.StatusOK, map[string]interface{}{
			"meteringPoint":     request.MeteringPoint,
			"affectedTimesteps": affected,
			"sumKwh":            sumKwh,
			"deleted":           !request.DryRun,
		})
	}
}

func lastRecordDate() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecid := vars["ecid"]

		lastRecord, err := services.GetLastEnergyEntry(tenant, ecid)
		if err != nil {
			respondWithError(w, http.StatusNotFound, "No entry found")
		}
		respondWithJSON(w, http.StatusOK, map[string]interface{}{"periodEnd": lastRecord})
	}
}

func exportMeteringData() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		email := claims.Email
		vars := mux.Vars(r)
		var year, month int
		var err error
		year, err = strconv.Atoi(vars["year"])
		ecid := vars["ecid"]
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Year not defined")
			return
		}
		month, err = strconv.Atoi(vars["month"])
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Month not defined")
			return
		}

		glog.V(3).Infof("Send Mail to %s", email)

		err = excel.ExportEnergyDataToMail(tenant, ecid, email, year, month, nil)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func exportReport() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {

		start := time.Now()
		vars := mux.Vars(r)
		ecid := vars["ecid"]

		glog.Infof("tenant=%s Start Energy Export", tenant)
		var cps excel.ExportParticipantEnergy
		err := json.NewDecoder(r.Body).Decode(&cps)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		//b, err := excel.CreateExcelFile(tenant, time.UnixMilli(cps.Start), time.UnixMilli(cps.End), &cps)
		b, err := excel.ExportEnergyToExcel(tenant, ecid, time.UnixMilli(cps.Start), time.UnixMilli(cps.End), &cps)
		if err != nil {
			respondWith(w, http.StatusInternalServerError, tenant, err)
			return
		}
		w.Header().Set("Content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="myfile.xlsx"`)
		w.Header().Set("filename", fmt.Sprintf("%s-Energy-Report-%s_%s",
			tenant,
			time.UnixMilli(cps.Start).Format("20060102"),
			time.UnixMilli(cps.End).Format("20060102")))

		if _, err := b.WriteTo(w); err != nil {
			glog.Errorf("tenant=%s error: %v", tenant, err)
			_, err = fmt.Fprintf(w, "%s", err)
			if err != nil {
				glog.Errorf("tenant=%s error: %v", tenant, err)
			}
		}
		glog.Infof("tenant=%s Energy Export finish (%v Sec.)", tenant, time.Now().Sub(start).Seconds())
	}
}

func fetchIntraDayReportV2() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecid := vars["ecid"]

		startMonitor := time.Now()
		glog.V(4).Infof("tenant=%s Start Time Monitor fetchIntraDayReport.", tenant)
		var request struct {
			Start int64 `json:"start"`
			End   int64 `json:"end"`
		}

		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp, err := store.QueryIntraDayReport(tenant, ecid, time.UnixMilli(request.Start), time.UnixMilli(request.End))
		glog.V(4).Infof("tenant=%s Time Monitor fetchIntraDayReport. %v Sec.", tenant, time.Now().Sub(startMonitor).Seconds())
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondWithJSON(w, http.StatusOK, &resp)
	}
}

func fetchLoadCurveReportV2() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecid := vars["ecid"]

		startMonitor := time.Now()
		glog.V(4).Infof("Start Time Monitor fetchLoadCurveReport. %s\n", tenant)
		var request struct {
			Start int64   `json:"start"`
			End   int64   `json:"end"`
			Func  *string `json:"func"`
		}

		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp, err := store.QueryLoadCurveReport(tenant, ecid, time.UnixMilli(request.Start), time.UnixMilli(request.End), request.Func)
		glog.V(4).Infof("Time Monitor fetchLoadCurveReport. %v\n", time.Now().Sub(startMonitor))
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondWithJSON(w, http.StatusOK, &resp)
	}
}

func getLoadCurveReportV2() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecid := vars["ecid"]

		startMonitor := time.Now()
		glog.V(4).Infof("Start Time Monitor fetchLoadCurveReport. %s\n", tenant)
		var request struct {
			Start int64   `json:"start"`
			End   int64   `json:"end"`
			Func  *string `json:"func"`
		}

		startQuery := r.URL.Query().Get("start")
		endQuery := r.URL.Query().Get("end")
		funcQuery := r.URL.Query().Get("func")

		var err error
		if len(startQuery) > 0 {
			request.Start, err = strconv.ParseInt(startQuery, 10, 64)
		}
		if len(endQuery) > 0 {
			request.End, err = strconv.ParseInt(endQuery, 10, 64)
		}

		if len(funcQuery) > 0 {
			request.Func = &funcQuery
		}

		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp, err := store.QueryLoadCurveReport(tenant, ecid, time.UnixMilli(request.Start), time.UnixMilli(request.End), request.Func)
		glog.V(4).Infof("Time Monitor fetchLoadCurveReport. %v\n", time.Now().Sub(startMonitor))
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondWithJSON(w, http.StatusOK, &resp)
	}
}

func fetchCombinedReportV2() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecid := vars["ecid"]

		startMonitor := time.Now()
		glog.V(4).Infof("Start Time Monitor fetchCombinedReport. %s\n", tenant)
		var request struct {
			Reports []string `json:"reports"`
			Start   int64    `json:"start"`
			End     int64    `json:"end"`
		}

		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp, err := store.QueryCombinedReports(tenant, ecid, request.Reports, time.UnixMilli(request.Start), time.UnixMilli(request.End))
		glog.V(4).Infof("Time Monitor fetchCombinedReport. %v\n", time.Now().Sub(startMonitor))
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondWithJSON(w, http.StatusOK, &resp)
	}
}

func getCombinedReportV2() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecid := vars["ecid"]

		startMonitor := time.Now()
		glog.V(4).Infof("Start Time Monitor fetchCombinedReport. %s\n", tenant)
		var request struct {
			Reports []string `json:"reports"`
			Start   int64    `json:"start"`
			End     int64    `json:"end"`
		}

		startQuery := r.URL.Query().Get("start")
		endQuery := r.URL.Query().Get("end")
		reportsQuery := r.URL.Query().Get("reports")

		var err error
		if len(startQuery) > 0 {
			request.Start, err = strconv.ParseInt(startQuery, 10, 64)
		}
		if len(endQuery) > 0 {
			request.End, err = strconv.ParseInt(endQuery, 10, 64)
		}

		if len(reportsQuery) > 0 {
			request.Reports = strings.Split(reportsQuery, ",")
		}

		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp, err := store.QueryCombinedReports(tenant, ecid, request.Reports, time.UnixMilli(request.Start), time.UnixMilli(request.End))
		glog.V(4).Infof("Time Monitor fetchCombinedReport. %v\n", time.Now().Sub(startMonitor))
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondWithJSON(w, http.StatusOK, &resp)
	}
}

func fetchSummaryReportV2() middleware.JWTHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, claims *middleware.PlatformClaims, tenant string) {
		vars := mux.Vars(r)
		ecid := vars["ecid"]

		startMonitor := time.Now()
		glog.V(4).Infof("Start Time Monitor fetchSummaryReport. %s\n", tenant)

		var request model.EnergyReportRequest

		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp, err := calculation.EnergySummary(tenant, ecid, request.Year, request.Segment, request.Period)
		glog.V(4).Infof("Time Monitor fetchSummaryReport. %v\n", time.Now().Sub(startMonitor))
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		respondWithJSON(w, http.StatusOK, &resp)
	}
}
