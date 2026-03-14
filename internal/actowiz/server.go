package actowiz

import (
	"encoding/json"
	"net/http"
)

const scrapeIntervalDays = 7

var defaultStoreIDs = []string{
	"safeway_490",      // 1645 140th ave
	"safeway_1600",     // 300 bellevue way
	"safeway_496",      // 15000 ne 24th st
	"safeway_1444",     // se 38th
	"safeway_1472",     // 35 e college way mt vernon
	"haggen_3450",      // 2601 e division st mt vernon
	"safeway_423",      // 35th ave ne, u district
	"safeway_1550",     // 7300 Roosevelt way ne, u district
	"starmarket_366",   // 177 Beacon St, jamaica plains
	"starmarket_2576",  // 33 kilmarnock st, jamaica plains
	"starmarket_2573",  // 130 granite st, boston
	"albertsons_453",   // 462 ne sunset blvd, renton
	"safeway_3319",     // 4300 ne 4th st
	"safeway_336",      // 2725 ne sunset bl"
	"safeway_1502",     // 1701 sant rita rd, pleasanton
	"safeway_1880",     // 8858 waltham woods rd baltimore.
	"jewelosco_3170",   // S pulaski rd, chicago
	"albertsons_4260",  // 2164 e buckingham dr dallas
	"safeway_2917",     // 1605 bridge st denver
	"albertsons_4131",  // 3910 Crenshaw blvd, los angeles
	"vons_2261",        // 31 w 3rd los angeles
	"acmemarkets_1777", // 481 river rd ny
	"acmemarkets_1856", // 19-21 ave at port imperial
	"acmemarkets_2704", // a6601 roosevelt bld philly
	"acmemarkets_2680", // 100 Grove lane deleware
	"acmemarkets_806",  // 100 suburban dr
	"acmemarkets_882",  // 907 paoli pike west chester
	"safeway_2799",     // 28455 n vistancia blvd phoenix
	"safeway_72",       // 910 w happy val rid phoenix
	"safeway_1231",     // 12032 sunnyside rd portland
	"safeway_382",      // 3527 se 1222nd ave portland
	"albertsons_1641",  // 7070 arcibald ave san bernardino
	"safeway_1215",     // 660 bailey rd san fran
	"safeway_1294",     // 210 washington ave s seattle
	"safeway_1668",     // 5510 norbeck rd DC
	"safeway_2781",     // 11201 georgia ave DC
}

type storesResponse struct {
	StoreIDs           []string `json:"store_ids"`
	ScrapeIntervalDays int      `json:"scrape_interval_days"`
}

type server struct {
	storeIDs []string
}

func NewServer() *server {
	return &server{
		storeIDs: append([]string(nil), defaultStoreIDs...),
	}
}

func (s *server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /actowiz/stores.json", s.handleStores)
}

func (s *server) handleStores(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(storesResponse{
		StoreIDs:           s.storeIDs,
		ScrapeIntervalDays: scrapeIntervalDays,
	}); err != nil {
		http.Error(w, "failed to encode stores", http.StatusInternalServerError)
	}
}
