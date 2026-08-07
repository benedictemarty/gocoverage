package gocoverage

import (
	"bytes"
	"testing"

	"github.com/bmarty/xarray"
)

func TestCoverageFormatNetCDF(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// f=netcdf : sous-cube 2x2 exporté en netCDF natif.
	rec := doGET(t, srv, "/collections/demo/coverage?bbox=1,44,2,45&properties=t2m&f=netcdf")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-netcdf" {
		t.Errorf("Content-Type = %q", ct)
	}
	// Relecture du netCDF renvoyé → doit redonner la variable t2m 2x2.
	ds, err := xarray.ReadDatasetNetCDF[float64](bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("relecture netCDF: %v", err)
	}
	da, err := ds.Get("t2m")
	if err != nil {
		t.Fatal(err)
	}
	if s := da.Shape(); len(s) != 2 || s[0] != 2 || s[1] != 2 {
		t.Errorf("shape = %v, attendu [2 2]", da.Shape())
	}
}

func TestCoverageFormatInconnu(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?f=geotiff")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 (format inconnu)", rec.Code)
	}
}

func TestCoverageFormatZarrIndisponible(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?f=zarr")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 (zarr en sortie indisponible)", rec.Code)
	}
}
