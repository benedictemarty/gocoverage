package gocoverage

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/benedictemarty/xarray"
)

// writeChunkedZarr écrit une grille 4×4 en chunks 2×2 (→ 4 chunks pour t2m).
func writeChunkedZarr(t *testing.T, dir string) {
	t.Helper()
	coords := map[string][]float64{"latitude": {4, 3, 2, 1}, "longitude": {0, 1, 2, 3}}
	d := make([]float64, 16)
	for i := range d {
		d[i] = float64(i) // valeur = latidx*4 + lonidx
	}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{4, 4}, d, coords, "t2m")
	da.Variable().SetAttr("units", "K")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err := xarray.WriteDatasetZarrChunked(dir, ds, map[string]int{"latitude": 2, "longitude": 2}, xarray.ZarrNone); err != nil {
		t.Fatal(err)
	}
}

func TestZarrWindowPrunesChunks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "g")
	writeChunkedZarr(t, dir)
	r, err := OpenZarrWindow(dir, "longitude", "latitude")
	if err != nil {
		t.Fatal(err)
	}

	// bbox = coin haut-gauche : lon∈[0,1], lat∈[3,4] → exactement le chunk 0.0.
	ds, err := r.ReadWindow(&[4]float64{0, 3, 1, 4})
	if err != nil {
		t.Fatal(err)
	}
	if r.ChunksRead() != 1 {
		t.Errorf("ChunksRead=%d, attendu 1 (un seul chunk recouvre la fenêtre)", r.ChunksRead())
	}
	v, _ := ds.Get("t2m")
	if got := v.Shape(); got[0] != 2 || got[1] != 2 {
		t.Fatalf("shape=%v, attendu [2 2]", got)
	}
	// Valeurs attendues : latidx {0,1} × lonidx {0,1} → {0,1,4,5}.
	want := map[float64]bool{0: true, 1: true, 4: true, 5: true}
	for _, x := range v.Data() {
		if !want[x] {
			t.Errorf("valeur inattendue %v dans la fenêtre", x)
		}
	}
}

func TestZarrWindowFullReadsAll(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "g")
	writeChunkedZarr(t, dir)
	r, _ := OpenZarrWindow(dir, "longitude", "latitude")
	ds, err := r.ReadWindow(nil) // toute la grille
	if err != nil {
		t.Fatal(err)
	}
	if r.ChunksRead() != 4 {
		t.Errorf("ChunksRead=%d, attendu 4 (tous les chunks)", r.ChunksRead())
	}
	v, _ := ds.Get("t2m")
	if len(v.Data()) != 16 {
		t.Errorf("taille=%d, attendu 16", len(v.Data()))
	}
}

func TestChunkedQueryPrunesWithoutFullLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "g")
	writeChunkedZarr(t, dir)
	r, _ := OpenZarrWindow(dir, "longitude", "latitude")
	c := &Collection{ID: "c", XDim: "longitude", YDim: "latitude", Window: r.ReadWindow}

	// Requête à emprise (coin) : doit passer par la lecture élaguée.
	ds, err := c.Query(QueryParams{BBox: &[4]float64{0, 3, 1, 4}})
	if err != nil {
		t.Fatal(err)
	}
	if r.ChunksRead() != 1 {
		t.Errorf("ChunksRead=%d, attendu 1 (élagage)", r.ChunksRead())
	}
	if c.Data != nil {
		t.Error("la grille complète ne doit PAS être matérialisée pour une requête à emprise")
	}
	if v, _ := ds.Get("t2m"); len(v.Data()) != 4 {
		t.Errorf("fenêtre=%d cellules, attendu 4 (2×2)", len(v.Data()))
	}
}

func TestChunkedCollectionEndToEnd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "g")
	writeChunkedZarr(t, dir)
	c, err := LoadChunkedZarr(dir, "c", "Chunké", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p := NewMemProvider()
	if err := p.Add(c); err != nil {
		t.Fatal(err)
	}
	// L'ajout ne doit pas avoir chargé la grille (laziness préservée).
	if c.Data != nil {
		t.Error("Add ne doit pas matérialiser une collection élaguée")
	}
	srv := NewServer(p)
	rec := doGET(t, srv, "/collections/c/coverage?bbox=0,3,1,4")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Coverage") {
		t.Errorf("réponse CoverageJSON attendue")
	}
	// describe matérialise la grille complète une fois (métadonnées).
	if rec := doGET(t, srv, "/collections/c"); rec.Code != 200 {
		t.Errorf("describe code=%d", rec.Code)
	}
}

func TestZarrWindowUnsupportedFallback(t *testing.T) {
	// Un store compressé n'est pas supporté par le lecteur élagué → erreur
	// (l'appelant retombe sur LoadZarr).
	dir := filepath.Join(t.TempDir(), "z")
	coords := map[string][]float64{"latitude": {2, 1}, "longitude": {0, 1}}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{2, 2}, []float64{0, 1, 2, 3}, coords, "t2m")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err := xarray.WriteDatasetZarrChunked(dir, ds, map[string]int{"latitude": 2, "longitude": 2}, xarray.ZarrZlib); err != nil {
		t.Skip("compression zlib indisponible: " + err.Error())
	}
	if _, err := OpenZarrWindow(dir, "longitude", "latitude"); err == nil {
		t.Error("un store compressé devrait être rejeté par le lecteur élagué")
	}
}
