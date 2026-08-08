package gocoverage

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/benedictemarty/xarray"
)

// buildMinimalGrib2 fabrique un message GRIB2 minimal (grille 2×2, simple packing) :
// latitude {45,43}, longitude {0,1}, valeurs {10,20,30,40}. Répliqué du test
// interne de xarray-go (non importable).
func buildMinimalGrib2() []byte {
	be := binary.BigEndian
	sec1 := make([]byte, 21)
	be.PutUint32(sec1[0:4], 21)
	sec1[4] = 1

	sec3 := make([]byte, 72)
	be.PutUint32(sec3[0:4], 72)
	sec3[4] = 3
	be.PutUint32(sec3[6:10], 4)
	be.PutUint16(sec3[12:14], 0)
	sec3[14] = 6
	be.PutUint32(sec3[30:34], 2)          // Ni
	be.PutUint32(sec3[34:38], 2)          // Nj
	be.PutUint32(sec3[46:50], 45_000_000) // La1
	be.PutUint32(sec3[50:54], 0)          // Lo1
	be.PutUint32(sec3[54:58], 44_000_000)
	be.PutUint32(sec3[58:62], 1_000_000)
	be.PutUint32(sec3[63:67], 1_000_000) // Di
	be.PutUint32(sec3[67:71], 2_000_000) // Dj
	sec3[71] = 0

	sec5 := make([]byte, 21)
	be.PutUint32(sec5[0:4], 21)
	sec5[4] = 5
	be.PutUint32(sec5[5:9], 4)
	be.PutUint16(sec5[9:11], 0)
	be.PutUint32(sec5[11:15], math.Float32bits(0))
	be.PutUint16(sec5[15:17], 0)
	be.PutUint16(sec5[17:19], 0)
	sec5[19] = 8

	sec6 := make([]byte, 6)
	be.PutUint32(sec6[0:4], 6)
	sec6[4] = 6
	sec6[5] = 255

	data := []byte{10, 20, 30, 40}
	sec7 := make([]byte, 5+len(data))
	be.PutUint32(sec7[0:4], uint32(5+len(data)))
	sec7[4] = 7
	copy(sec7[5:], data)

	sec8 := []byte{'7', '7', '7', '7'}
	body := bytes.Join([][]byte{sec1, sec3, sec5, sec6, sec7, sec8}, nil)
	total := 16 + len(body)
	sec0 := make([]byte, 16)
	copy(sec0[0:4], "GRIB")
	sec0[6] = 0
	sec0[7] = 2
	be.PutUint64(sec0[8:16], uint64(total))
	return append(sec0, body...)
}

func writeGribFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "in.grib")
	if err := os.WriteFile(path, buildMinimalGrib2(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadGrib(t *testing.T) {
	dir := t.TempDir()
	path := writeGribFile(t, dir)
	c, err := LoadGrib(path, "g", "GRIB", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.XDim != "longitude" || c.YDim != "latitude" {
		t.Errorf("axes détectés: X=%q Y=%q", c.XDim, c.YDim)
	}
	// La variable grib_0 doit être présente et servie.
	srv := NewServer(func() *MemProvider { p := NewMemProvider(); p.Add(c); return p }())
	rec := doGET(t, srv, "/collections/g/coverage")
	if rec.Code != 200 {
		t.Fatalf("coverage code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("grib_0")) {
		t.Errorf("variable grib_0 absente de la réponse")
	}
}

func TestConvertGribToZarrRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writeGribFile(t, dir)
	zdir := filepath.Join(dir, "out.zarr")
	if err := ConvertGribToZarr(path, zdir, map[string]int{"latitude": 2, "longitude": 2}, xarray.ZarrZstd); err != nil {
		t.Fatal(err)
	}
	// Relire le Zarr produit via la lecture élaguée.
	c, err := LoadChunkedZarr(zdir, "z", "Zarr", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := c.Query(QueryParams{BBox: &[4]float64{0, 43, 1, 45}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := ds.Get("grib_0")
	if err != nil {
		t.Fatal(err)
	}
	// Valeurs GRIB {10,20,30,40} conservées après conversion.
	want := map[float64]bool{10: true, 20: true, 30: true, 40: true}
	for _, x := range v.Data() {
		if !want[x] {
			t.Errorf("valeur inattendue %v après conversion GRIB→Zarr", x)
		}
	}
}

func TestLoadGribInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.grib")
	os.WriteFile(path, []byte("pas du grib"), 0o644)
	if _, err := LoadGrib(path, "x", "X", "", "", ""); err == nil {
		t.Error("un fichier non-GRIB devrait échouer")
	}
}
