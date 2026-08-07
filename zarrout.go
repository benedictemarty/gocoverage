package gocoverage

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bmarty/xarray"
)

// zarrZip écrit le Dataset au format Zarr dans un répertoire temporaire, puis le
// compresse en archive ZIP renvoyée sous forme d'octets — pendant Go de
// _get_zarr_data de pygeoapi (sortie native `format_=zarr`).
func zarrZip(ds *xarray.Dataset[float64], name string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "gocoverage-zarr-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	dir := filepath.Join(tmp, name+".zarr")
	if err := xarray.WriteDatasetZarr(dir, ds, xarray.ZarrNone); err != nil {
		return nil, fmt.Errorf("écriture Zarr: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Chemin relatif à tmp → l'archive contient « <name>.zarr/… ».
		rel, err := filepath.Rel(tmp, path)
		if err != nil {
			return err
		}
		f, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(f, src)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
