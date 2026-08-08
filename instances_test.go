package gocoverage

import (
	"encoding/json"
	"testing"

	"github.com/benedictemarty/xarray"
)

func instColl(t *testing.T, id string, base float64) *Collection {
	t.Helper()
	lat := []float64{50, 49, 48, 47}
	lon := []float64{0, 1, 2, 3}
	d := make([]float64, 16)
	for i := range d {
		d[i] = base + float64(i)
	}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{4, 4}, d,
		map[string][]float64{"latitude": lat, "longitude": lon}, "t2m")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	return &Collection{ID: id, XDim: "longitude", YDim: "latitude", Data: ds}
}

func TestInstancesList(t *testing.T) {
	c := instColl(t, "model", 0)
	c.Instances = []*Collection{instColl(t, "00Z", 0), instColl(t, "06Z", 100)}
	if _, ok := c.InstanceByID("06Z"); !ok {
		t.Error("06Z devrait exister")
	}
	if _, ok := c.InstanceByID("12Z"); ok {
		t.Error("12Z ne devrait pas exister")
	}
	info := c.InstancesInfo()
	if len(info) != 2 || info[0].ID != "00Z" {
		t.Errorf("instances = %+v", info)
	}
}

// TestInstancesRouting : une requête EDR sur /instances/{id}/... s'exécute sur la
// bonne sous-collection.
func TestInstancesRouting(t *testing.T) {
	c := instColl(t, "model", 0)
	c.Instances = []*Collection{instColl(t, "00Z", 0), instColl(t, "06Z", 100)}
	p := NewMemProvider()
	if err := p.Add(c); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)

	// Liste des instances.
	rec := doGET(t, srv, "/collections/model/instances")
	if rec.Code != 200 {
		t.Fatalf("liste : code %d", rec.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body["instances"].([]interface{})) != 2 {
		t.Errorf("nombre d'instances = %v", body["instances"])
	}

	// Position sur l'instance 06Z (base 100) : (1,49) -> 100 + (1*4+1) = 105.
	rec2 := doGET(t, srv, "/collections/model/instances/06Z/position?coords=1,49")
	if rec2.Code != 200 {
		t.Fatalf("06Z position : code %d : %s", rec2.Code, rec2.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &doc)
	vals := doc["ranges"].(map[string]interface{})["t2m"].(map[string]interface{})["values"].([]interface{})
	if vals[0].(float64) != 105 {
		t.Errorf("06Z (1,49) t2m = %v, attendu 105", vals[0])
	}

	// Description de l'instance (action vide).
	if rec := doGET(t, srv, "/collections/model/instances/00Z"); rec.Code != 200 {
		t.Errorf("description 00Z : code %d", rec.Code)
	}
	// Instance inconnue -> 404.
	if rec := doGET(t, srv, "/collections/model/instances/99Z/position?coords=1,49"); rec.Code != 404 {
		t.Errorf("instance inconnue : code %d, attendu 404", rec.Code)
	}
	// Instances imbriquées -> 404.
	if rec := doGET(t, srv, "/collections/model/instances/06Z/instances"); rec.Code != 404 {
		t.Errorf("instances imbriquées : code %d, attendu 404", rec.Code)
	}
}
