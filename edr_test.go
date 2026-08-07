package gocoverage

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/bmarty/xarray"
)

// zProvider construit une collection (z × latitude × longitude) 2×2×2.
func zProvider(t *testing.T) *MemProvider {
	t.Helper()
	coords := map[string][]float64{
		"z":         {1000, 500}, // deux niveaux (ex. hPa)
		"latitude":  {45, 44},
		"longitude": {0, 1},
	}
	// valeurs = z_idx*100 + lat_idx*10 + lon_idx
	data := []float64{
		0, 1, 10, 11, // z=1000
		100, 101, 110, 111, // z=500
	}
	da, err := xarray.NewDataArray([]string{"z", "latitude", "longitude"}, []int{2, 2, 2}, data, coords, "t")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("units", "K")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t": da})
	if err != nil {
		t.Fatal(err)
	}
	p := NewMemProvider()
	if err := p.Add(&Collection{ID: "z", Title: "Niveaux", XDim: "longitude", YDim: "latitude", ZDim: "z", Data: ds}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPositionWithZ(t *testing.T) {
	srv := NewServer(zProvider(t))
	// z=500 -> niveau du haut ; point (lon=1,lat=44) -> data[z=500][lat=44][lon=1]=111
	rec := doGET(t, srv, "/collections/z/position?coords=1,44&z=500")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	vals := doc["ranges"].(map[string]interface{})["t"].(map[string]interface{})["values"].([]interface{})
	if len(vals) != 1 || vals[0].(float64) != 111 {
		t.Errorf("values = %v, attendu [111]", vals)
	}
}

func TestCoverageZMultiLevelRejected(t *testing.T) {
	srv := NewServer(zProvider(t))
	// coverage sans sélection de z -> deux niveaux -> non représentable -> 500
	rec := doGET(t, srv, "/collections/z/coverage")
	if rec.Code != 500 {
		t.Errorf("code=%d, attendu 500 (axe vertical multi-niveaux)", rec.Code)
	}
}

func TestCubeWithZ(t *testing.T) {
	srv := NewServer(zProvider(t))
	// cube sur toute l'emprise + z=1000 -> un seul niveau, grille 2x2
	rec := doGET(t, srv, "/collections/z/cube?bbox=0,44,1,45&z=1000")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	shape := doc["ranges"].(map[string]interface{})["t"].(map[string]interface{})["shape"].([]interface{})
	if len(shape) != 2 {
		t.Errorf("shape = %v, attendu 2 axes (y,x) après sélection z", shape)
	}
}

func TestZOnCollectionWithoutZ(t *testing.T) {
	// Fournir z à une collection sans axe vertical : ignoré (comme pygeoapi).
	srv := NewServer(demoProvider(t))
	req := httptest.NewRequest("GET", "/collections/demo/position?coords=1,44&z=500", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("code=%d, z devrait être ignoré", rec.Code)
	}
}
