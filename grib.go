package gocoverage

import (
	"fmt"
	"io"
	"os"

	"github.com/benedictemarty/xarray"
)

// Prise en charge GRIB2 (grille lat/lon régulière) via xarray.ReadGrib :
//   - LoadGrib          : GRIB2 → Collection (comme LoadNetCDF/LoadZarr) ;
//   - ConvertGribToZarr : GRIB2 → store Zarr (chunké/compressé), ensuite servi en
//     lecture élaguée par LoadChunkedZarr.
//
// Limites (héritées du décodeur xarray-go) : GRIB **édition 2**, grille lat/lon
// régulière (`regular_ll`, template 3.0), **simple packing** (5.0) **et complex
// packing** (5.2/5.3, sans bitmap de valeurs manquantes). Non gérés : JPEG2000/PNG
// packing et templates locaux (ex. 50002, requièrent ecCodes), grilles gaussiennes
// réduites, GRIB1. Le décodeur renvoie une erreur explicite pour ces cas. Les
// métadonnées de paramètre/niveau/échéance ne sont pas extraites : chaque message
// devient une variable `grib_N` (grille 2D lat/lon). Pour les formats non gérés,
// convertir en amont (wgrib2/cdo/eccodes).

// gribDataset lit tous les messages GRIB2 d'un flux et les assemble en un Dataset
// (une variable `grib_N` par message ; les messages doivent partager la grille).
func gribDataset(r io.Reader) (*xarray.Dataset[float64], error) {
	msgs, err := xarray.ReadGrib(r)
	if err != nil {
		return nil, fmt.Errorf("lecture GRIB: %w", err)
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("aucun message GRIB2 dans le flux")
	}
	vars := map[string]*xarray.DataArray[float64]{}
	for i, m := range msgs {
		da, err := m.ToDataArray(fmt.Sprintf("grib_%d", i))
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", i, err)
		}
		vars[fmt.Sprintf("grib_%d", i)] = da
	}
	ds, err := xarray.NewDataset(vars)
	if err != nil {
		return nil, fmt.Errorf("assemblage GRIB (messages à grilles hétérogènes ?): %w", err)
	}
	return ds, nil
}

// LoadGrib ouvre un fichier GRIB2 et construit une Collection (détection des axes
// lat/lon comme LoadNetCDF/LoadZarr). tDim reste généralement vide (le décodeur
// n'expose pas d'axe temporel).
func LoadGrib(path, id, title, xDim, yDim, tDim string) (*Collection, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ouverture GRIB %q: %w", path, err)
	}
	defer f.Close()
	ds, err := gribDataset(f)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", path, err)
	}
	return collectionFromDataset(ds, id, title, xDim, yDim, tDim)
}

// ConvertGribToZarr lit un fichier GRIB2 et l'écrit en store Zarr. Si chunks est
// non vide, le store est chunké (recommandé, pour la lecture élaguée ultérieure) ;
// comp choisit la compression (ZarrNone/ZarrZlib/ZarrZstd).
func ConvertGribToZarr(gribPath, zarrDir string, chunks map[string]int, comp xarray.ZarrCompression) error {
	f, err := os.Open(gribPath)
	if err != nil {
		return fmt.Errorf("ouverture GRIB %q: %w", gribPath, err)
	}
	defer f.Close()
	ds, err := gribDataset(f)
	if err != nil {
		return fmt.Errorf("%q: %w", gribPath, err)
	}
	if len(chunks) == 0 {
		return xarray.WriteDatasetZarr(zarrDir, ds, comp)
	}
	return xarray.WriteDatasetZarrChunked(zarrDir, ds, chunks, comp)
}
