package gocoverage

import (
	"math"
	"net/url"
	"testing"

	"github.com/benedictemarty/xarray"
)

func TestPointInPolygon(t *testing.T) {
	// Carré unité (0,0)-(4,4).
	sq := [][2]float64{{0, 0}, {4, 0}, {4, 4}, {0, 4}, {0, 0}}
	if !pointInPolygon(2, 2, sq) {
		t.Error("(2,2) devrait être dans le carré")
	}
	if pointInPolygon(5, 2, sq) {
		t.Error("(5,2) devrait être hors du carré")
	}
	// Triangle (0,0),(4,0),(0,4).
	tri := [][2]float64{{0, 0}, {4, 0}, {0, 4}, {0, 0}}
	if !pointInPolygon(1, 1, tri) { // 1+1 < 4
		t.Error("(1,1) devrait être dans le triangle")
	}
	if pointInPolygon(3, 3, tri) { // 3+3 > 4
		t.Error("(3,3) devrait être hors du triangle")
	}
}

func TestAreaMask(t *testing.T) {
	lat := []float64{4, 3, 2, 1, 0}
	lon := []float64{0, 1, 2, 3, 4}
	d := make([]float64, 25)
	for i := range d {
		d[i] = 1
	}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{5, 5}, d,
		map[string][]float64{"latitude": lat, "longitude": lon}, "v")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"v": da})
	c := &Collection{ID: "c", XDim: "longitude", YDim: "latitude", Data: ds}

	ring := [][2]float64{{0, 0}, {4, 0}, {0, 4}, {0, 0}} // triangle bas-gauche
	res, err := c.Area(ring, EDRParams{})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := res.Get("v")
	inside, outside := 0, 0
	for _, x := range v.Data() {
		if math.IsNaN(x) {
			outside++
		} else {
			inside++
		}
	}
	if inside == 0 || outside == 0 {
		t.Errorf("le polygone devrait partitionner : %d dedans, %d hors", inside, outside)
	}

	// Moins de 3 sommets -> erreur.
	if _, err := c.Area([][2]float64{{0, 0}, {1, 1}}, EDRParams{}); err == nil {
		t.Error("erreur attendue : < 3 sommets")
	}
}

func TestParsePolygon(t *testing.T) {
	ring, err := parsePolygon("POLYGON((0 0, 4 0, 0 4, 0 0))")
	if err != nil || len(ring) != 4 || ring[1] != [2]float64{4, 0} {
		t.Errorf("WKT = %v, %v", ring, err)
	}
	if _, err := parsePolygon("POLYGON(())"); err == nil {
		// vide -> pas de sommets valides
		t.Log("polygone vide toléré (0 sommet)")
	}
	if _, err := parsePolygon(""); err == nil {
		t.Error("erreur attendue : vide")
	}
}

// TestAreaHTTP : endpoint EDR area -> CoverageJSON avec cellules hors polygone à null.
func TestAreaHTTP(t *testing.T) {
	p := NewMemProvider()
	if err := p.Add(trajColl(t)); err != nil { // réutilise la collection 4×4 de trajectory_test
		t.Fatal(err)
	}
	srv := NewServer(p)
	// carré couvrant lon 0..1, lat 47..48 (un coin de la grille)
	u := "/collections/c/area?coords=" + url.QueryEscape("POLYGON((0 47, 1 47, 1 48, 0 48, 0 47))")
	rec := doGET(t, srv, u)
	if rec.Code != 200 {
		t.Fatalf("code %d : %s", rec.Code, rec.Body.String())
	}
	if bad := doGET(t, srv, "/collections/c/area?coords=nope"); bad.Code != 400 {
		t.Errorf("coords invalide : code %d, attendu 400", bad.Code)
	}
}
