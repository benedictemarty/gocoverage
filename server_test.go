package gocoverage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bmarty/xarray"
)

// demoDataset construit une grille 3×4 (latitude × longitude) à deux paramètres.
func demoDataset(t *testing.T) *xarray.Dataset[float64] {
	t.Helper()
	coords := map[string][]float64{"latitude": {45, 44, 43}, "longitude": {0, 1, 2, 3}}
	daT, err := xarray.NewDataArray(
		[]string{"latitude", "longitude"}, []int{3, 4},
		[]float64{0, 1, 2, 3, 10, 11, 12, 13, 20, 21, 22, 23}, coords, "t2m")
	if err != nil {
		t.Fatal(err)
	}
	daT.Variable().SetAttr("units", "K")
	daT.Variable().SetAttr("long_name", "Température")
	daU, err := xarray.NewDataArray(
		[]string{"latitude", "longitude"}, []int{3, 4},
		[]float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, coords, "uwind")
	if err != nil {
		t.Fatal(err)
	}
	daU.Variable().SetAttr("units", "m/s")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": daT, "uwind": daU})
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func demoProvider(t *testing.T) *MemProvider {
	t.Helper()
	p := NewMemProvider()
	if err := p.Add(&Collection{
		ID: "demo", Title: "Démo",
		XDim: "longitude", YDim: "latitude", Data: demoDataset(t),
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func doGET(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestServerCollections(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections")
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var body map[string][]CollectionInfo
	json.Unmarshal(rec.Body.Bytes(), &body)
	cols := body["collections"]
	if len(cols) != 1 || cols[0].ID != "demo" {
		t.Fatalf("collections = %v", cols)
	}
	if cols[0].BBox != [4]float64{0, 43, 3, 45} {
		t.Errorf("bbox = %v", cols[0].BBox)
	}
	if len(cols[0].Parameters) != 2 {
		t.Errorf("parameters = %v", cols[0].Parameters)
	}
}

func TestServerDescribe(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo")
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var body map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &body)
	var fields []Field
	json.Unmarshal(body["parameters"], &fields)
	if len(fields) != 2 {
		t.Fatalf("champs = %v", fields)
	}
	var props CoverageProperties
	json.Unmarshal(body["properties"], &props)
	if props.Width != 4 || props.Height != 3 {
		t.Errorf("width/height = %d/%d", props.Width, props.Height)
	}
	if props.ResX != 1 || props.ResY != 1 {
		t.Errorf("res = %v/%v", props.ResX, props.ResY)
	}
}

func TestServerPosition(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// point (lon=1, lat=44) -> t2m[lat=44][lon=1] = 11
	rec := doGET(t, srv, "/collections/demo/position?coords=1,44&parameter-name=t2m")
	if rec.Code != 200 {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc["domain"].(map[string]interface{})["domainType"] != "PointSeries" {
		t.Errorf("domainType = %v", doc["domain"])
	}
	rng := doc["ranges"].(map[string]interface{})
	if _, ok := rng["uwind"]; ok {
		t.Errorf("uwind ne devrait pas être présent (parameter-name=t2m)")
	}
	vals := rng["t2m"].(map[string]interface{})["values"].([]interface{})
	if len(vals) != 1 || vals[0].(float64) != 11 {
		t.Errorf("values = %v, attendu [11]", vals)
	}
}

func TestServerCoverageBBox(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// bbox lon [1,2], lat [44,45] -> sous-cube 2x2
	rec := doGET(t, srv, "/collections/demo/coverage?bbox=1,44,2,45&properties=t2m")
	if rec.Code != 200 {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc["type"] != "Coverage" {
		t.Errorf("type = %v", doc["type"])
	}
	shape := doc["ranges"].(map[string]interface{})["t2m"].(map[string]interface{})["shape"].([]interface{})
	if shape[0].(float64) != 2 || shape[1].(float64) != 2 {
		t.Errorf("shape sous-cube = %v, attendu [2 2]", shape)
	}
}

func TestServerCoverageSubset(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// subset Lat(44:45),Long(0:1) -> 2x2
	rec := doGET(t, srv, "/collections/demo/coverage?subset=Lat(44:45),Long(0:1)")
	if rec.Code != 200 {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	xaxis := doc["domain"].(map[string]interface{})["axes"].(map[string]interface{})["x"].(map[string]interface{})
	if xaxis["num"].(float64) != 2 {
		t.Errorf("axe x num = %v, attendu 2", xaxis["num"])
	}
}

func TestServerBBoxSubsetExclusive(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?bbox=0,43,3,45&subset=Lat(44:45)")
	if rec.Code != 400 {
		t.Errorf("code = %d, attendu 400 (exclusivité bbox/subset)", rec.Code)
	}
}

func TestServerCube(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/cube?bbox=0,43,1,45&parameter-name=uwind")
	if rec.Code != 200 {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	rng := doc["ranges"].(map[string]interface{})
	if _, ok := rng["t2m"]; ok {
		t.Errorf("t2m ne devrait pas être présent (parameter-name=uwind)")
	}
}

func TestServerNotFound(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/inconnu/coverage")
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, attendu 404", rec.Code)
	}
}

func TestServerDatetimeSansAxe(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?datetime=1/2")
	if rec.Code != 400 {
		t.Errorf("code = %d, attendu 400 (pas d'axe temporel)", rec.Code)
	}
}
