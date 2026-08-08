package gocoverage

import (
	"path/filepath"
	"testing"

	"github.com/benedictemarty/xarray"
)

// writePyramid écrit une pyramide 16×16 (3 niveaux, facteur 2 → 16, 8, 4).
func writePyramid(t *testing.T, dir string) {
	t.Helper()
	n := 16
	lat := make([]float64, n)
	lon := make([]float64, n)
	for i := 0; i < n; i++ {
		lat[i] = float64(n - i) // descendant 16..1
		lon[i] = float64(i)     // 0..15
	}
	d := make([]float64, n*n)
	for i := range d {
		d[i] = float64(i)
	}
	da, err := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{n, n}, d,
		map[string][]float64{"latitude": lat, "longitude": lon}, "t2m")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("units", "K")
	if err := xarray.WritePyramidZarr(dir, da, "latitude", "longitude", 3, 2, xarray.ZarrNone); err != nil {
		t.Fatal(err)
	}
}

func openPyramid(t *testing.T, dir string, target int) *pyramidReader {
	t.Helper()
	levels, err := xarray.PyramidLevels(dir)
	if err != nil {
		t.Fatal(err)
	}
	pr := &pyramidReader{target: target}
	for _, l := range levels {
		r, err := OpenZarrWindow(filepath.Join(dir, l.Path), "longitude", "latitude")
		if err != nil {
			t.Fatalf("niveau %s: %v", l.Path, err)
		}
		pr.levels = append(pr.levels, r)
	}
	return pr
}

func TestPyramidLevelSelection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pyr")
	writePyramid(t, dir)
	pr := openPyramid(t, dir, 100) // budget 100 cellules
	if len(pr.levels) != 3 {
		t.Fatalf("niveaux=%d, attendu 3", len(pr.levels))
	}
	// Emprise complète : niveau 0 = 256 > 100, niveau 1 = 64 ≤ 100 → niveau 1.
	if L := pr.chooseLevel(nil); L != 1 {
		t.Errorf("emprise complète → niveau %d, attendu 1 (grossier)", L)
	}
	ds, err := pr.ReadWindow(WindowSel{})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := ds.Get("t2m"); len(v.Data()) != 64 { // 8×8
		t.Errorf("emprise complète → %d cellules, attendu 64 (niveau 1)", len(v.Data()))
	}
}

func TestPyramidEndToEnd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pyr")
	writePyramid(t, dir)
	c, err := LoadPyramidZarr(dir, "p", "Pyramide", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p := NewMemProvider()
	if err := p.Add(c); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)
	if rec := doGET(t, srv, "/collections/p/coverage?bbox=0,1,15,16"); rec.Code != 200 {
		t.Fatalf("coverage code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doGET(t, srv, "/collections/p"); rec.Code != 200 {
		t.Fatalf("describe code=%d", rec.Code)
	}
	if c.Data != nil {
		t.Error("pyramide : les métadonnées/données ne doivent pas matérialiser la grille")
	}
}
