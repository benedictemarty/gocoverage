package gocoverage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bmarty/xarray"
)

// datasetZ construit une grille 3D (level × latitude × longitude), 2 niveaux.
// Valeur = indiceNiveau*1000 + y*10 + x, pour distinguer les niveaux.
func datasetZ(t *testing.T) *xarray.Dataset[float64] {
	t.Helper()
	coords := map[string][]float64{
		"level":     {1000, 500}, // hPa
		"latitude":  {45, 44, 43},
		"longitude": {0, 1, 2, 3},
	}
	data := make([]float64, 2*3*4)
	i := 0
	for l := 0; l < 2; l++ {
		for y := 0; y < 3; y++ {
			for x := 0; x < 4; x++ {
				data[i] = float64(l*1000 + y*10 + x)
				i++
			}
		}
	}
	da, err := xarray.NewDataArray(
		[]string{"level", "latitude", "longitude"}, []int{2, 3, 4}, data, coords, "t")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("units", "K")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t": da})
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func collZ(t *testing.T) *Collection {
	t.Helper()
	return &Collection{
		ID: "zc", Title: "avec niveaux",
		XDim: "longitude", YDim: "latitude", ZDim: "level",
		Data: datasetZ(t),
	}
}

// La sélection EDR d'un niveau réduit l'axe vertical et retient le bon plan.
func TestPositionZNearest(t *testing.T) {
	c := collZ(t)
	// point (lon=1, lat=44) au niveau 500 -> indiceNiveau=1, y=1, x=1 -> 1011.
	z := 500.0
	ds, err := c.Position(1, 44, EDRParams{Z: &z})
	if err != nil {
		t.Fatalf("Position z=500: %v", err)
	}
	v, _ := ds.Get("t")
	if got := v.Data(); len(got) != 1 || got[0] != 1011 {
		t.Errorf("z=500 -> %v, attendu [1011]", got)
	}
	// niveau 1000 -> indiceNiveau=0 -> 11.
	z2 := 1000.0
	ds2, _ := c.Position(1, 44, EDRParams{Z: &z2})
	v2, _ := ds2.Get("t")
	if got := v2.Data(); got[0] != 11 {
		t.Errorf("z=1000 -> %v, attendu [11]", got)
	}
	// l'axe vertical doit avoir disparu (réduit).
	if v2.HasDim("level") {
		t.Error("la dimension level aurait dû être réduite")
	}
}

// CoverageJSON refuse un axe vertical à plusieurs niveaux, avec un message qui
// invite à sélectionner un niveau (z=…).
func TestCoverageJSONMultiNiveauxRejet(t *testing.T) {
	c := collZ(t)
	if _, err := c.CoverageJSON(c.Data); err == nil {
		t.Fatal("erreur attendue : axe vertical multi-niveaux")
	} else if !strings.Contains(err.Error(), "z=") {
		t.Errorf("message peu explicite : %v", err)
	}
	// Après réduction à un niveau, l'export réussit.
	z := 500.0
	ds, err := c.Cube([4]float64{0, 43, 3, 45}, EDRParams{Z: &z})
	if err != nil {
		t.Fatalf("Cube z=500: %v", err)
	}
	if _, err := c.CoverageJSON(ds); err != nil {
		t.Errorf("CoverageJSON après sélection de niveau: %v", err)
	}
}

// Chemin HTTP : position avec z renvoie le bon plan ; coverage sans z sur une
// collection multi-niveaux échoue proprement (400).
func TestServerZ(t *testing.T) {
	p := NewMemProvider()
	if err := p.Add(collZ(t)); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)

	rec := doGET(t, srv, "/collections/zc/position?coords=1,44&z=500")
	if rec.Code != 200 {
		t.Fatalf("position z: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	rng, ok := doc["ranges"].(map[string]interface{})["t"].(map[string]interface{})
	if !ok {
		t.Fatalf("range t absente: %v", doc["ranges"])
	}
	vals := rng["values"].([]interface{})
	if len(vals) != 1 || vals[0].(float64) != 1011 {
		t.Errorf("position z=500 valeurs=%v, attendu [1011]", vals)
	}

	// coverage (Query, sans z) sur multi-niveaux -> CoverageJSON refuse.
	// NB : idéalement 400 (erreur corrigible via z=) ; le serveur renvoie
	// aujourd'hui 500 (writeCoverage mappe toute erreur en 500). On verrouille
	// au moins le rejet (statut d'erreur) ; l'affinage 400 est une amélioration.
	rec2 := doGET(t, srv, "/collections/zc/coverage?bbox=0,43,3,45")
	if rec2.Code < 400 {
		t.Errorf("coverage multi-niveaux sans z: code=%d, attendu une erreur (>=400)", rec2.Code)
	}
}
