package gocoverage

import (
	"math"
	"net/url"
	"testing"

	"github.com/benedictemarty/xarray"
)

func TestDistToPolyline(t *testing.T) {
	line := [][2]float64{{0, 0}, {10, 0}} // segment horizontal
	if d := distToPolyline(5, 3, line); math.Abs(d-3) > 1e-9 {
		t.Errorf("distance = %v, attendu 3", d)
	}
	if d := distToPolyline(-4, 0, line); math.Abs(d-4) > 1e-9 { // au-delà de l'extrémité
		t.Errorf("distance extrémité = %v, attendu 4", d)
	}
	if d := distToPolyline(3, 0, line); d != 0 { // sur le segment
		t.Errorf("distance sur segment = %v, attendu 0", d)
	}
}

func gridColl(t *testing.T, n int) *Collection {
	t.Helper()
	lat := make([]float64, n)
	lon := make([]float64, n)
	for i := 0; i < n; i++ {
		lat[i], lon[i] = float64(n-1-i), float64(i)
	}
	d := make([]float64, n*n)
	for i := range d {
		d[i] = 1
	}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{n, n}, d,
		map[string][]float64{"latitude": lat, "longitude": lon}, "v")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"v": da})
	return &Collection{ID: "c", XDim: "longitude", YDim: "latitude", Data: ds}
}

// TestCorridorDiagonal : une route diagonale + demi-largeur laisse des coins hors
// tube (NaN), preuve du masquage par distance.
func TestCorridorDiagonal(t *testing.T) {
	c := gridColl(t, 7) // lon 0..6, lat 6..0
	line := [][2]float64{{0, 0}, {6, 6}}
	res, err := c.Corridor(line, 1.5, "", EDRParams{})
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
		t.Errorf("tube diagonal devrait laisser des coins hors : %d dedans, %d hors", inside, outside)
	}
	// Erreurs.
	if _, err := c.Corridor([][2]float64{{0, 0}}, 1, "", EDRParams{}); err == nil {
		t.Error("erreur attendue : < 2 points")
	}
	if _, err := c.Corridor(line, 0, "", EDRParams{}); err == nil {
		t.Error("erreur attendue : demi-largeur nulle")
	}
}

// TestCorridorHTTP : endpoint EDR corridor.
func TestCorridorHTTP(t *testing.T) {
	p := NewMemProvider()
	if err := p.Add(gridColl(t, 7)); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)
	u := "/collections/c/corridor?coords=" + url.QueryEscape("LINESTRING(0 0, 6 6)") + "&corridor-width=3"
	if rec := doGET(t, srv, u); rec.Code != 200 {
		t.Fatalf("code %d : %s", rec.Code, rec.Body.String())
	}
	// corridor-width manquant -> 400.
	bad := doGET(t, srv, "/collections/c/corridor?coords="+url.QueryEscape("LINESTRING(0 0, 6 6)"))
	if bad.Code != 400 {
		t.Errorf("sans corridor-width : code %d, attendu 400", bad.Code)
	}
}
