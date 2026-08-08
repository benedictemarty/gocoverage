package gocoverage

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/benedictemarty/xarray"
)

// Sortie GeoJSON d'une couverture (négociation de contenu EDR `f=geojson`).
// Chaque cellule (x, y) de valeur non nulle devient un Feature Point dont les
// propriétés portent la valeur de chaque paramètre (au premier pas des
// dimensions supplémentaires temps/niveau, comme le fait pygeoapi pour GeoJSON).
// Adapté aux requêtes ponctuelles/petites emprises (position, radius, area…).

// GeoJSON sérialise le Dataset en FeatureCollection GeoJSON.
//
// Un Feature Point est produit par cellule (x, y). Le GeoJSON ne modélise pas
// les dimensions supplémentaires : si l'axe temporel ou vertical compte plus
// d'un pas, la sortie serait tronquée au premier pas — c'est refusé (remarque D :
// pas de perte de données silencieuse ; sélectionner un pas via datetime/z ou
// utiliser un format multidimensionnel comme CoverageJSON/netCDF).
func (c *Collection) GeoJSON(ds *xarray.Dataset[float64]) ([]byte, error) {
	dims := ds.Dims()
	if c.TDim != "" && dims[c.TDim] > 1 {
		return nil, fmt.Errorf("geojson: %d pas de temps — sélectionnez un instant (datetime=…) ou utilisez f=covjson/netcdf", dims[c.TDim])
	}
	if c.ZDim != "" && dims[c.ZDim] > 1 {
		return nil, fmt.Errorf("geojson: %d niveaux verticaux — sélectionnez un niveau (z=…) ou utilisez f=covjson/netcdf", dims[c.ZDim])
	}
	xs, err := ds.Coord(c.XDim)
	if err != nil {
		return nil, fmt.Errorf("coordonnée X %q: %w", c.XDim, err)
	}
	ys, err := ds.Coord(c.YDim)
	if err != nil {
		return nil, fmt.Errorf("coordonnée Y %q: %w", c.YDim, err)
	}
	names := ds.VarNames()

	// Pré-calcule, pour chaque variable, ses strides et les indices d'axes x/y.
	type varInfo struct {
		da            *xarray.DataArray[float64]
		strides       []int
		shape         []int
		xi, yi        int
		otherBaseFlat int // contribution des autres dims fixées à l'indice 0
	}
	infos := make(map[string]varInfo, len(names))
	for _, name := range names {
		da, err := ds.Get(name)
		if err != nil {
			return nil, err
		}
		dims := da.Variable().Dims()
		infos[name] = varInfo{
			da: da, strides: cStrides(da.Shape()), shape: da.Shape(),
			xi: indexOf(dims, c.XDim), yi: indexOf(dims, c.YDim),
		}
	}

	features := make([]map[string]interface{}, 0, len(xs)*len(ys))
	for iy := 0; iy < len(ys); iy++ {
		for ix := 0; ix < len(xs); ix++ {
			props := map[string]interface{}{}
			any := false
			for _, name := range names {
				vi := infos[name]
				if vi.xi < 0 || vi.yi < 0 {
					continue
				}
				// Indice plat : x/y positionnés, autres dims à 0.
				flat := ix*vi.strides[vi.xi] + iy*vi.strides[vi.yi]
				v := vi.da.Data()[flat]
				if math.IsNaN(v) {
					props[name] = nil
				} else {
					props[name] = v
					any = true
				}
			}
			if !any {
				continue // cellule entièrement vide -> pas de feature
			}
			features = append(features, map[string]interface{}{
				"type":       "Feature",
				"geometry":   map[string]interface{}{"type": "Point", "coordinates": []float64{xs[ix], ys[iy]}},
				"properties": props,
			})
		}
	}
	return json.MarshalIndent(map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	}, "", "  ")
}
