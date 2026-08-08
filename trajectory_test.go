package gocoverage

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/benedictemarty/xarray"
)

func trajColl(t *testing.T) *Collection {
	t.Helper()
	// grille 4×4 : t2m[latIdx][lonIdx] = 10*latIdx + lonIdx (via valeurs 0..15).
	d := make([]float64, 16)
	for i := range d {
		d[i] = float64(i)
	}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{4, 4}, d,
		map[string][]float64{"latitude": {50, 49, 48, 47}, "longitude": {0, 1, 2, 3}}, "t2m")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	return &Collection{ID: "c", XDim: "longitude", YDim: "latitude", Data: ds}
}

// TestTrajectorySampling : échantillonnage le long d'une polyligne (diagonale).
func TestTrajectorySampling(t *testing.T) {
	c := trajColl(t)
	pts := [][2]float64{{0, 50}, {1, 49}, {2, 48}, {3, 47}}
	vals, names, err := c.Trajectory(pts, EDRParams{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"t2m"}) {
		t.Errorf("names = %v", names)
	}
	if !reflect.DeepEqual(vals["t2m"], []float64{0, 5, 10, 15}) {
		t.Errorf("valeurs = %v, attendu [0 5 10 15]", vals["t2m"])
	}
	// Moins de 2 points -> erreur.
	if _, _, err := c.Trajectory([][2]float64{{0, 50}}, EDRParams{}); err == nil {
		t.Error("erreur attendue : < 2 points")
	}
}

// TestTrajectoryHTTP : endpoint EDR trajectory -> CoverageJSON domaine Trajectory.
func TestTrajectoryHTTP(t *testing.T) {
	p := NewMemProvider()
	if err := p.Add(trajColl(t)); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)
	u := "/collections/c/trajectory?coords=" + url.QueryEscape("LINESTRING(0 50, 3 47)")
	rec := doGET(t, srv, u)
	if rec.Code != 200 {
		t.Fatalf("code %d : %s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	dom := doc["domain"].(map[string]interface{})
	if dom["domainType"] != "Trajectory" {
		t.Errorf("domainType = %v", dom["domainType"])
	}
	comp := dom["axes"].(map[string]interface{})["composite"].(map[string]interface{})
	if comp["dataType"] != "tuple" {
		t.Errorf("axe composite dataType = %v", comp["dataType"])
	}
	// coords invalides -> 400.
	if bad := doGET(t, srv, "/collections/c/trajectory?coords=oops"); bad.Code != 400 {
		t.Errorf("coords invalide : code %d, attendu 400", bad.Code)
	}
}

func TestParseLineString(t *testing.T) {
	// WKT.
	pts, err := parseLineString("LINESTRING(2 48, 3 49)")
	if err != nil || !reflect.DeepEqual(pts, [][2]float64{{2, 48}, {3, 49}}) {
		t.Errorf("WKT = %v, %v", pts, err)
	}
	// Repli lon,lat;lon,lat.
	pts2, err := parseLineString("2,48;3,49")
	if err != nil || !reflect.DeepEqual(pts2, [][2]float64{{2, 48}, {3, 49}}) {
		t.Errorf("repli = %v, %v", pts2, err)
	}
	if _, err := parseLineString(""); err == nil {
		t.Error("erreur attendue : vide")
	}
}
