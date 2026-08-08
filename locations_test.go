package gocoverage

import (
	"encoding/json"
	"testing"

	"github.com/benedictemarty/xarray"
)

func locColl(t *testing.T) *Collection {
	t.Helper()
	lat := []float64{50, 49, 48, 47}
	lon := []float64{0, 1, 2, 3}
	d := make([]float64, 16)
	for i := range d {
		d[i] = float64(i)
	}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{4, 4}, d,
		map[string][]float64{"latitude": lat, "longitude": lon}, "t2m")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	return &Collection{
		ID: "c", XDim: "longitude", YDim: "latitude", Data: ds,
		Locations: []NamedLocation{
			{ID: "LFPG", Name: "Paris CDG", Lon: 2, Lat: 49},
			{ID: "LFPO", Name: "Paris Orly", Lon: 2, Lat: 48},
		},
	}
}

func TestLocationsList(t *testing.T) {
	c := locColl(t)
	b, err := c.LocationsGeoJSON()
	if err != nil {
		t.Fatal(err)
	}
	var fc map[string]interface{}
	if err := json.Unmarshal(b, &fc); err != nil {
		t.Fatal(err)
	}
	if fc["type"] != "FeatureCollection" {
		t.Errorf("type = %v", fc["type"])
	}
	feats := fc["features"].([]interface{})
	if len(feats) != 2 {
		t.Fatalf("features = %d, attendu 2", len(feats))
	}
	f0 := feats[0].(map[string]interface{})
	if f0["id"] != "LFPG" {
		t.Errorf("id = %v", f0["id"])
	}
	geom := f0["geometry"].(map[string]interface{})
	coords := geom["coordinates"].([]interface{})
	if coords[0].(float64) != 2 || coords[1].(float64) != 49 {
		t.Errorf("coordinates = %v", coords)
	}
}

func TestLocationByIDAndSample(t *testing.T) {
	c := locColl(t)
	if _, ok := c.LocationByID("LFPO"); !ok {
		t.Error("LFPO devrait exister")
	}
	if _, ok := c.LocationByID("XXXX"); ok {
		t.Error("XXXX ne devrait pas exister")
	}
	// LFPG (2,49) -> data[lat=49 (idx1)][lon=2 (idx2)] = 1*4+2 = 6.
	b, err := c.LocationCoverageJSON("LFPG", EDRParams{})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	json.Unmarshal(b, &doc)
	vals := doc["ranges"].(map[string]interface{})["t2m"].(map[string]interface{})["values"].([]interface{})
	if vals[0].(float64) != 6 {
		t.Errorf("LFPG t2m = %v, attendu 6", vals[0])
	}
	// Location inconnue -> erreur.
	if _, err := c.LocationCoverageJSON("XXXX", EDRParams{}); err == nil {
		t.Error("erreur attendue : location inconnue")
	}
}

func TestLocationsHTTP(t *testing.T) {
	p := NewMemProvider()
	if err := p.Add(locColl(t)); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)
	if rec := doGET(t, srv, "/collections/c/locations"); rec.Code != 200 {
		t.Errorf("liste : code %d", rec.Code)
	}
	if rec := doGET(t, srv, "/collections/c/locations/LFPG"); rec.Code != 200 {
		t.Errorf("LFPG : code %d : %s", rec.Code, rec.Body.String())
	}
	if rec := doGET(t, srv, "/collections/c/locations/XXXX"); rec.Code != 404 {
		t.Errorf("inconnu : code %d, attendu 404", rec.Code)
	}
}
