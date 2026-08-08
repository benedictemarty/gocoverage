package gocoverage

import (
	"encoding/json"
	"os"
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
	ds, err := r.ReadWindow(WindowSel{BBox: &[4]float64{0, 3, 1, 4}})
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
	ds, err := r.ReadWindow(WindowSel{}) // toute la grille
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

// writeChunkedZarrComp écrit la grille 4×4 (chunks 2×2) avec un compresseur donné.
func writeChunkedZarrComp(t *testing.T, dir string, comp xarray.ZarrCompression) {
	t.Helper()
	coords := map[string][]float64{"latitude": {4, 3, 2, 1}, "longitude": {0, 1, 2, 3}}
	d := make([]float64, 16)
	for i := range d {
		d[i] = float64(i)
	}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{4, 4}, d, coords, "t2m")
	da.Variable().SetAttr("units", "K")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err := xarray.WriteDatasetZarrChunked(dir, ds, map[string]int{"latitude": 2, "longitude": 2}, comp); err != nil {
		t.Fatal(err)
	}
}

// TestZarrWindowCompressed vérifie la lecture élaguée de stores compressés
// (zlib, zstd) : bbox coin → 1 chunk lu, valeurs correctes.
func TestZarrWindowCompressed(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp xarray.ZarrCompression
	}{
		{"zlib", xarray.ZarrZlib},
		{"zstd", xarray.ZarrZstd},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "g")
			writeChunkedZarrComp(t, dir, tc.comp)
			r, err := OpenZarrWindow(dir, "longitude", "latitude")
			if err != nil {
				t.Fatalf("ouverture %s: %v", tc.name, err)
			}
			ds, err := r.ReadWindow(WindowSel{BBox: &[4]float64{0, 3, 1, 4}})
			if err != nil {
				t.Fatal(err)
			}
			if r.ChunksRead() != 1 {
				t.Errorf("ChunksRead=%d, attendu 1 (élagage %s)", r.ChunksRead(), tc.name)
			}
			v, _ := ds.Get("t2m")
			want := map[float64]bool{0: true, 1: true, 4: true, 5: true}
			for _, x := range v.Data() {
				if !want[x] {
					t.Errorf("%s: valeur inattendue %v", tc.name, x)
				}
			}
		})
	}
}

// TestChunkedMetadataStaysLazy : description/domainset/rangetype d'une collection
// élaguée ne matérialisent jamais les données (servis depuis les indices).
func TestChunkedMetadataStaysLazy(t *testing.T) {
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
	srv := NewServer(p)
	for _, path := range []string{
		"/collections/c",
		"/collections/c/coverage/domainset",
		"/collections/c/coverage/rangetype",
	} {
		if rec := doGET(t, srv, path); rec.Code != 200 {
			t.Fatalf("%s: code=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	if c.Data != nil {
		t.Error("les métadonnées ne doivent PAS matérialiser les données (indices coords/schéma)")
	}
	// La bbox exposée doit être correcte (depuis les coords lues).
	if bb := c.BBox(); bb[0] != 0 || bb[2] != 3 {
		t.Errorf("bbox=%v, attendu X∈[0,3]", bb)
	}
}

// TestZarrWindowTransposedAxes : store stocké en [longitude, latitude] (x-major).
// La sortie doit être canonique [lat, lon] avec les valeurs correctement placées.
func TestZarrWindowTransposedAxes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "g")
	coords := map[string][]float64{"longitude": {0, 1, 2, 3}, "latitude": {4, 3, 2, 1}}
	d := make([]float64, 16)
	for lonI := 0; lonI < 4; lonI++ {
		for latI := 0; latI < 4; latI++ {
			d[lonI*4+latI] = float64(lonI*4 + latI) // x-major
		}
	}
	da, _ := xarray.NewDataArray([]string{"longitude", "latitude"}, []int{4, 4}, d, coords, "t2m")
	da.Variable().SetAttr("units", "K")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err := xarray.WriteDatasetZarrChunked(dir, ds, map[string]int{"longitude": 2, "latitude": 2}, xarray.ZarrNone); err != nil {
		t.Fatal(err)
	}
	r, err := OpenZarrWindow(dir, "longitude", "latitude")
	if err != nil {
		t.Fatal(err)
	}
	// Coin lon∈[0,1], lat∈[3,4] : lonI {0,1}, latI {0,1}.
	out, err := r.ReadWindow(WindowSel{BBox: &[4]float64{0, 3, 1, 4}})
	if err != nil {
		t.Fatal(err)
	}
	if r.ChunksRead() != 1 {
		t.Errorf("ChunksRead=%d, attendu 1", r.ChunksRead())
	}
	v, _ := out.Get("t2m")
	if got := v.Shape(); got[0] != 2 || got[1] != 2 {
		t.Fatalf("shape=%v, attendu [2 2] canonique [lat,lon]", got)
	}
	want := map[float64]bool{0: true, 4: true, 1: true, 5: true}
	for _, x := range v.Data() {
		if !want[x] {
			t.Errorf("valeur inattendue %v (placement transposé incorrect)", x)
		}
	}
}

// writeCube3D écrit un cube [time, lat, lon] 4×4×4 en chunks 1×2×2.
func writeCube3D(t *testing.T, dir string) {
	t.Helper()
	coords := map[string][]float64{"time": {0, 1, 2, 3}, "latitude": {4, 3, 2, 1}, "longitude": {0, 1, 2, 3}}
	d := make([]float64, 64)
	for i := range d {
		d[i] = float64(i) // = t*16 + lat*4 + lon
	}
	da, _ := xarray.NewDataArray([]string{"time", "latitude", "longitude"}, []int{4, 4, 4}, d, coords, "t2m")
	da.Variable().SetAttr("units", "K")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err := xarray.WriteDatasetZarrChunked(dir, ds, map[string]int{"time": 1, "latitude": 2, "longitude": 2}, xarray.ZarrNone); err != nil {
		t.Fatal(err)
	}
}

// TestZarrWindow3DPrunesTimeAndSpace : cube temporel, élagage sur temps ET espace.
func TestZarrWindow3DPrunesTimeAndSpace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cube")
	writeCube3D(t, dir)
	r, err := OpenZarrWindow(dir, "longitude", "latitude")
	if err != nil {
		t.Fatal(err)
	}
	if r.TDim() != "time" {
		t.Fatalf("TDim=%q, attendu time", r.TDim())
	}
	// bbox coin (1 chunk spatial) + temps [1,2] (2 chunks temps de taille 1).
	ds, err := r.ReadWindow(WindowSel{BBox: &[4]float64{0, 3, 1, 4}, TRange: &[2]float64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if r.ChunksRead() != 2 { // 2 (temps) × 1 (spatial)
		t.Errorf("ChunksRead=%d, attendu 2 (2 chunks temps × 1 spatial)", r.ChunksRead())
	}
	v, _ := ds.Get("t2m")
	if got := v.Shape(); len(got) != 3 || got[0] != 2 || got[1] != 2 || got[2] != 2 {
		t.Fatalf("shape=%v, attendu [2 2 2]", got)
	}
	tv, _ := ds.Coord("time")
	if len(tv) != 2 || tv[0] != 1 || tv[1] != 2 {
		t.Errorf("temps=%v, attendu [1 2]", tv)
	}
}

// TestChunked3DQueryDatetime : requête bbox+datetime via Collection → élagage
// temps+espace, sans matérialiser le cube complet.
func TestChunked3DQueryDatetime(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cube")
	writeCube3D(t, dir)
	c, err := LoadChunkedZarr(dir, "c", "Cube", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.TDim != "time" {
		t.Fatalf("TDim=%q, attendu time", c.TDim)
	}
	ds, err := c.Query(QueryParams{BBox: &[4]float64{0, 3, 1, 4}, Datetime: &[2]float64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Data != nil {
		t.Error("le cube complet ne doit PAS être matérialisé")
	}
	v, _ := ds.Get("t2m")
	if len(v.Data()) != 8 { // 2×2×2
		t.Errorf("fenêtre=%d cellules, attendu 8", len(v.Data()))
	}
}

// TestZarrWindowUnsupportedFallback : un compresseur non géré (blosc) est rejeté
// à l'ouverture — l'appelant retombe alors sur LoadZarr (lecture complète).
func TestZarrWindowUnsupportedFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "g")
	writeChunkedZarr(t, dir)
	// Falsifie le compresseur de t2m en « blosc » (non supporté par le lecteur élagué).
	zpath := filepath.Join(dir, "t2m", ".zarray")
	b, _ := os.ReadFile(zpath)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	m["compressor"] = map[string]interface{}{"id": "blosc", "cname": "lz4"}
	nb, _ := json.Marshal(m)
	if err := os.WriteFile(zpath, nb, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenZarrWindow(dir, "longitude", "latitude"); err == nil {
		t.Error("un store blosc devrait être rejeté par le lecteur élagué")
	}
}
