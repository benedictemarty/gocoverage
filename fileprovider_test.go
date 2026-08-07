package gocoverage

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/benedictemarty/xarray"
)

// TestLoadNetCDFCF écrit un netCDF avec attributs CF (units + packing), le
// recharge via LoadNetCDF et vérifie le décodage et l'exposition des champs.
func TestLoadNetCDFCF(t *testing.T) {
	coords := map[string][]float64{"latitude": {45, 44}, "longitude": {0, 1}}
	// Données brutes « packées » : valeur réelle = brut*0.1 + 273.15 ; 999 = fill.
	da, err := xarray.NewDataArray(
		[]string{"latitude", "longitude"}, []int{2, 2},
		[]float64{100, 200, 300, 999}, coords, "t2m")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("units", "K")
	da.Variable().SetAttr("long_name", "Température 2 m")
	da.Variable().SetAttr("scale_factor", "0.1")
	da.Variable().SetAttr("add_offset", "273.15")
	da.Variable().SetAttr("_FillValue", "999")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "demo.nc")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.WriteNetCDF(f); err != nil {
		t.Fatal(err)
	}
	f.Close()

	c, err := LoadNetCDF(path, "demo", "Démo netCDF", "", "", "")
	if err != nil {
		t.Fatalf("LoadNetCDF: %v", err)
	}
	if c.XDim != "longitude" || c.YDim != "latitude" {
		t.Errorf("axes détectés = %s/%s", c.XDim, c.YDim)
	}

	// Champs : units doit être exposé (get_fields).
	fields := c.Fields()
	if len(fields) != 1 || fields[0].Unit != "K" || fields[0].Title != "Température 2 m" {
		t.Errorf("champs = %+v", fields)
	}

	// Décodage CF : 100*0.1+273.15 = 283.15 ; 999 -> NaN.
	arr, err := c.Data.Get("t2m")
	if err != nil {
		t.Fatal(err)
	}
	d := arr.Data()
	if math.Abs(d[0]-283.15) > 1e-9 {
		t.Errorf("valeur décodée = %v, attendu 283.15", d[0])
	}
	if !math.IsNaN(d[3]) {
		t.Errorf("_FillValue non converti en NaN: %v", d[3])
	}
}
