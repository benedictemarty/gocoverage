package gocoverage

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/benedictemarty/xarray"
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

func TestCoverageFormatZarr(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?bbox=1,44,2,45&properties=t2m&f=zarr")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	// Dézip vers un répertoire puis relecture Zarr.
	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	dir := t.TempDir()
	for _, f := range zr.File {
		dst := filepath.Join(dir, f.Name)
		if f.FileInfo().IsDir() {
			continue
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		rc, _ := f.Open()
		out, _ := os.Create(dst)
		io.Copy(out, rc)
		out.Close()
		rc.Close()
	}
	ds, err := xarray.ReadDatasetZarr(filepath.Join(dir, "demo.zarr"))
	if err != nil {
		t.Fatalf("relecture Zarr: %v", err)
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
