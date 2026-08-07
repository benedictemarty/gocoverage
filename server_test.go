package gocoverage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bmarty/xarray"
)

func demoProvider(t *testing.T) *MemProvider {
	t.Helper()
	da, _ := xarray.NewDataArray(
		[]string{"latitude", "longitude"}, []int{3, 4},
		[]float64{0, 1, 2, 3, 10, 11, 12, 13, 20, 21, 22, 23},
		map[string][]float64{"latitude": {45, 44, 43}, "longitude": {0, 1, 2, 3}}, "t2m")
	p := NewMemProvider()
	p.Add(&Collection{ID: "demo", Title: "Démo", Param: "t2m", XDim: "longitude", YDim: "latitude", Data: da})
	return p
}

func TestServerCollections(t *testing.T) {
	srv := NewServer(demoProvider(t))
	req := httptest.NewRequest("GET", "/collections", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var body map[string][]CollectionInfo
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body["collections"]) != 1 || body["collections"][0].ID != "demo" {
		t.Errorf("collections = %v", body["collections"])
	}
	// bbox : lon [0,3], lat [43,45]
	bb := body["collections"][0].BBox
	if bb != [4]float64{0, 43, 3, 45} {
		t.Errorf("bbox = %v", bb)
	}
}

func TestServerPosition(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// point (lon=1, lat=44) -> data[lat=44][lon=1] = 11
	req := httptest.NewRequest("GET", "/collections/demo/position?coords=1,44", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["value"].(float64) != 11 {
		t.Errorf("value = %v, attendu 11", body["value"])
	}
}

func TestServerCoverageBBox(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// bbox lon [1,2], lat [44,45] -> sous-cube 2x2
	req := httptest.NewRequest("GET", "/collections/demo/coverage?bbox=1,44,2,45", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc["type"] != "Coverage" {
		t.Errorf("type = %v", doc["type"])
	}
	rng := doc["ranges"].(map[string]interface{})["t2m"].(map[string]interface{})
	shape := rng["shape"].([]interface{})
	if shape[0].(float64) != 2 || shape[1].(float64) != 2 {
		t.Errorf("shape sous-cube = %v, attendu [2 2]", shape)
	}
}

func TestServerNotFound(t *testing.T) {
	srv := NewServer(demoProvider(t))
	req := httptest.NewRequest("GET", "/collections/inconnu/coverage", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, attendu 404", rec.Code)
	}
}
