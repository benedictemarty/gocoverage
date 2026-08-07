package gocoverage

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/bmarty/xarray"
)

// timeDataset construit une grille (time × latitude × longitude) 2×2×2.
func timeDataset(t *testing.T) *xarray.Dataset[float64] {
	t.Helper()
	coords := map[string][]float64{
		"time":      {0, 1},
		"latitude":  {45, 44},
		"longitude": {0, 1},
	}
	// valeurs = t*100 + lat_idx*10 + lon_idx
	data := []float64{
		0, 1, 10, 11, // t=0
		100, 101, 110, 111, // t=1
	}
	da, err := xarray.NewDataArray(
		[]string{"time", "latitude", "longitude"}, []int{2, 2, 2}, data, coords, "t2m")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("units", "K")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func timeProvider(t *testing.T) *MemProvider {
	t.Helper()
	p := NewMemProvider()
	if err := p.Add(&Collection{
		ID: "meteo", Title: "Séries temporelles",
		XDim: "longitude", YDim: "latitude", TDim: "time", Data: timeDataset(t),
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDatetimeSubset(t *testing.T) {
	srv := NewServer(timeProvider(t))
	// datetime=1 -> une seule tranche temporelle
	req := httptest.NewRequest("GET", "/collections/meteo/coverage?datetime=1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	shape := doc["ranges"].(map[string]interface{})["t2m"].(map[string]interface{})["shape"].([]interface{})
	if shape[0].(float64) != 1 {
		t.Errorf("time steps = %v, attendu 1", shape[0])
	}
	tAxis := doc["domain"].(map[string]interface{})["axes"].(map[string]interface{})["t"]
	if tAxis == nil {
		t.Error("axe temporel absent du domaine")
	}
}

func TestDatetimeTimeSubsetExclusive(t *testing.T) {
	srv := NewServer(timeProvider(t))
	req := httptest.NewRequest("GET", "/collections/meteo/coverage?datetime=0/1&subset=time(0:1)", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("code = %d, attendu 400 (datetime/subset temporel exclusifs)", rec.Code)
	}
}

func TestParseSubsets(t *testing.T) {
	subs, err := parseSubsets("Lat(43:45),Long(0:2),time(5)")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 3 {
		t.Fatalf("subs = %v", subs)
	}
	if subs[0].Axis != "Lat" || subs[0].Lo != 43 || subs[0].Hi != 45 || subs[0].Point {
		t.Errorf("subs[0] = %+v", subs[0])
	}
	if !subs[2].Point || subs[2].Lo != 5 {
		t.Errorf("subs[2] = %+v (attendu point 5)", subs[2])
	}
	if _, err := parseSubsets("Lat43-45"); err == nil {
		t.Error("attendu une erreur sur syntaxe invalide")
	}
}

func TestParseDatetime(t *testing.T) {
	ext := [2]float64{0, 10}
	got, err := parseDatetime("2/5", ext)
	if err != nil || got == nil || *got != [2]float64{2, 5} {
		t.Errorf("got=%v err=%v", got, err)
	}
	// borne ouverte
	got, _ = parseDatetime("../5", ext)
	if *got != [2]float64{0, 5} {
		t.Errorf("borne ouverte = %v", *got)
	}
	// instant unique
	got, _ = parseDatetime("7", ext)
	if *got != [2]float64{7, 7} {
		t.Errorf("instant = %v", *got)
	}
}
