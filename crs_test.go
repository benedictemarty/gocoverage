package gocoverage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bmarty/xarray"
)

func TestCoverageJSONProjectedCRS(t *testing.T) {
	c := &Collection{
		ID: "p", XDim: "longitude", YDim: "latitude",
		CRS:  EPSGCRS(3857, true),
		Data: demoDataset(t),
	}
	b, err := c.CoverageJSON(c.Data)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	json.Unmarshal(b, &doc)
	ref := doc["domain"].(map[string]interface{})["referencing"].([]interface{})[0].(map[string]interface{})
	sys := ref["system"].(map[string]interface{})
	if sys["type"] != "ProjectedCRS" {
		t.Errorf("type CRS = %v, attendu ProjectedCRS", sys["type"])
	}
	if id, _ := sys["id"].(string); !strings.Contains(id, "EPSG/0/3857") {
		t.Errorf("id CRS = %v", sys["id"])
	}

	props := c.Properties()
	if props.CRSType != "ProjectedCRS" || !strings.Contains(props.BBoxCRS, "3857") {
		t.Errorf("properties CRS = %+v", props)
	}
}

func TestDefaultCRS84(t *testing.T) {
	// Zéro-valeur CRS → CRS84 géographique.
	c := &Collection{ID: "d", XDim: "longitude", YDim: "latitude", Data: demoDataset(t)}
	if c.CRS.typ() != "GeographicCRS" || c.CRS.id() != crs84 {
		t.Errorf("CRS par défaut = %+v (id=%s)", c.CRS, c.CRS.id())
	}
	props := c.Properties()
	if props.BBoxCRS != crs84 {
		t.Errorf("BBoxCRS par défaut = %s", props.BBoxCRS)
	}
}

func TestDetectCRSFromVar(t *testing.T) {
	coords := map[string][]float64{"latitude": {45, 44}, "longitude": {0, 1}}
	t2m, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{2, 2},
		[]float64{0, 1, 2, 3}, coords, "t2m")
	t2m.Variable().SetAttr("units", "K")
	// Variable de conteneur CRS (grid_mapping CF), portant l'EPSG.
	crsVar, _ := xarray.NewDataArray([]string{"crs"}, []int{1}, []float64{0},
		map[string][]float64{"crs": {0}}, "crs")
	crsVar.Variable().SetAttr("grid_mapping_name", "transverse_mercator")
	crsVar.Variable().SetAttr("epsg_code", "32631")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": t2m, "crs": crsVar})
	if err != nil {
		t.Fatal(err)
	}

	c, err := collectionFromDataset(ds, "utm", "UTM 31N", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.CRS.EPSG != 32631 || c.CRS.Type != "ProjectedCRS" {
		t.Errorf("CRS détecté = %+v, attendu EPSG 32631 ProjectedCRS", c.CRS)
	}
	// La variable de conteneur CRS ne doit pas apparaître comme paramètre.
	for _, p := range c.Params() {
		if p == "crs" {
			t.Error("la variable crs ne devrait pas être exposée comme paramètre")
		}
	}
}
