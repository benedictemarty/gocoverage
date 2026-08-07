package gocoverage

import (
	"fmt"
	"strings"

	"github.com/bmarty/xarray"
)

// LoadNetCDF ouvre un fichier netCDF et construit une Collection — pendant Go de
// l'ouverture xarray.open_dataset dans pygeoapi (avec décodage CF par défaut,
// comme decode_cf=True). Les dimensions X/Y/T sont détectées par nom si
// xDim/yDim/tDim sont laissés vides.
//
// Décodage CF appliqué : packing (scale_factor/add_offset), valeurs manquantes
// (_FillValue/missing_value → NaN), et axe temporel (« <unité> since <date> »
// → secondes depuis l'epoch).
func LoadNetCDF(path, id, title, xDim, yDim, tDim string) (*Collection, error) {
	// OpenNetCDFFile lit directement le CDF-1 ; pour NetCDF-4/HDF5 ou CDF-2/5, il
	// délègue à un convertisseur externe détecté dans le PATH (nccopy/cdo).
	ds, err := xarray.OpenNetCDFFile(path, nil)
	if err != nil {
		return nil, fmt.Errorf("lecture netCDF %q: %w", path, err)
	}
	// Décodage CF du packing (no-op si aucun attribut de packing).
	if ds, err = xarray.DecodeCF(ds); err != nil {
		return nil, fmt.Errorf("décodage CF %q: %w", path, err)
	}
	return collectionFromDataset(ds, id, title, xDim, yDim, tDim)
}

// LoadZarr ouvre un répertoire Zarr et construit une Collection — pendant Go de
// xarray.open_zarr dans pygeoapi.
func LoadZarr(dir, id, title, xDim, yDim, tDim string) (*Collection, error) {
	ds, err := xarray.ReadDatasetZarr(dir)
	if err != nil {
		return nil, fmt.Errorf("lecture Zarr %q: %w", dir, err)
	}
	return collectionFromDataset(ds, id, title, xDim, yDim, tDim)
}

// collectionFromDataset assemble une Collection, en détectant au besoin les axes.
func collectionFromDataset(ds *xarray.Dataset[float64], id, title, xDim, yDim, tDim string) (*Collection, error) {
	dims := ds.Dims()
	if xDim == "" {
		xDim = detectAxis(dims, "longitude", "lon", "x")
	}
	if yDim == "" {
		yDim = detectAxis(dims, "latitude", "lat", "y")
	}
	if tDim == "" {
		tDim = detectAxis(dims, "time", "t")
	}
	zDim := detectAxis(dims, "z", "level", "height", "depth", "elevation", "pressure", "plev", "lev")
	if xDim == "" || yDim == "" {
		return nil, fmt.Errorf("axes X/Y introuvables dans les dimensions %v", keysOf(dims))
	}
	// Décodage de l'axe temporel CF (no-op si la coordonnée n'a pas d'attribut
	// units « <unité> since <date> »).
	if tDim != "" {
		decoded, err := xarray.DecodeTime(ds, tDim)
		if err != nil {
			return nil, fmt.Errorf("décodage temps %q: %w", tDim, err)
		}
		ds = decoded
	}
	return &Collection{ID: id, Title: title, XDim: xDim, YDim: yDim, TDim: tDim, ZDim: zDim, Data: ds}, nil
}

// detectAxis renvoie la première dimension dont le nom (insensible à la casse)
// correspond à l'un des candidats, ou "" si aucune.
func detectAxis(dims map[string]int, candidates ...string) string {
	for name := range dims {
		low := strings.ToLower(name)
		for _, cand := range candidates {
			if low == cand {
				return name
			}
		}
	}
	return ""
}

func keysOf(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
