package gocoverage

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/benedictemarty/xarray"
)

func refColl(t *testing.T) *Collection {
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
	return &Collection{ID: "c", XDim: "longitude", YDim: "latitude", Data: ds}
}

// TestPositionBilinear : l'interpolation bilinéaire donne la valeur au point
// exact (vs plus proche voisin).
func TestPositionBilinear(t *testing.T) {
	c := refColl(t)
	// Point (0.5, 49.5), entre 4 cellules {0,1,4,5} -> moyenne 2.5.
	ds, err := c.Position(0.5, 49.5, EDRParams{Bilinear: true})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := ds.Get("t2m")
	if math.Abs(v.Data()[0]-2.5) > 1e-9 {
		t.Errorf("bilinéaire = %v, attendu 2.5", v.Data()[0])
	}
	// Le point exact est conservé comme coordonnée x/y (PointSeries).
	if xs, _ := ds.Coord("longitude"); len(xs) != 1 || xs[0] != 0.5 {
		t.Errorf("coord x = %v, attendu [0.5]", xs)
	}
	// Plus proche voisin (défaut) : cellule (50,0) = 0.
	dsn, _ := c.Position(0.5, 49.5, EDRParams{})
	vn, _ := dsn.Get("t2m")
	if vn.Data()[0] != 0 {
		t.Errorf("nearest = %v, attendu 0", vn.Data()[0])
	}
	// Hors grille -> erreur.
	if _, err := c.Position(99, 99, EDRParams{Bilinear: true}); err == nil {
		t.Error("erreur attendue : point hors grille (bilinéaire)")
	}
}

// TestGeoJSONOutput : f=geojson renvoie une FeatureCollection avec la valeur.
func TestGeoJSONOutput(t *testing.T) {
	p := NewMemProvider()
	if err := p.Add(refColl(t)); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)
	rec := doGET(t, srv, "/collections/c/position?coords=1,49&f=geojson")
	if rec.Code != 200 {
		t.Fatalf("code %d : %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/geo+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var fc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc["type"] != "FeatureCollection" {
		t.Errorf("type = %v", fc["type"])
	}
	feats := fc["features"].([]interface{})
	if len(feats) != 1 {
		t.Fatalf("features = %d, attendu 1", len(feats))
	}
	f0 := feats[0].(map[string]interface{})
	props := f0["properties"].(map[string]interface{})
	// (1,49) -> data[lat49 idx1][lon1 idx1] = 5.
	if props["t2m"].(float64) != 5 {
		t.Errorf("t2m = %v, attendu 5", props["t2m"])
	}
}

// TestCorridorMetric : corridor-width en km (distance métrique).
func TestCorridorMetric(t *testing.T) {
	c := gridColl(t, 7) // lon 0..6, lat 6..0
	line := [][2]float64{{0, 3}, {6, 3}}
	// 100 km ≈ 0,9° : demi-largeur 50 km ≈ 0,45° -> seule la ligne lat=3 dans le tube.
	res, err := c.Corridor(line, 50, "km", EDRParams{})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := res.Get("v")
	inside := 0
	for _, x := range v.Data() {
		if !math.IsNaN(x) {
			inside++
		}
	}
	if inside != 7 { // uniquement la rangée lat=3 (7 colonnes)
		t.Errorf("corridor 50 km : %d cellules, attendu 7", inside)
	}
	// Unité inconnue -> erreur.
	if _, err := c.Corridor(line, 50, "furlongs", EDRParams{}); err == nil {
		t.Error("erreur attendue : unité inconnue")
	}
}

// TestBadFormat : un format inconnu -> 400.
func TestBadFormat(t *testing.T) {
	p := NewMemProvider()
	if err := p.Add(refColl(t)); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)
	if rec := doGET(t, srv, "/collections/c/position?coords=1,49&f=xml"); rec.Code != 400 {
		t.Errorf("format xml : code %d, attendu 400", rec.Code)
	}
}
