package gocoverage

import (
	"fmt"

	"github.com/benedictemarty/xarray"
)

// EDRParams rassemble les paramètres communs des requêtes EDR position/cube.
type EDRParams struct {
	SelectProperties []string    // sous-ensemble de paramètres (vide = tous)
	Datetime         *[2]float64 // plage temporelle, nil si absente
	Z                *float64    // niveau vertical (sélection au plus proche), nil si absent
	Bilinear         bool        // échantillonnage x/y bilinéaire (défaut : plus proche voisin)
}

// applyZ sélectionne le niveau vertical le plus proche si p.Z est fourni et que
// la collection possède un axe vertical (comme XarrayEDRProvider : le niveau est
// réduit à un point). Si p.Z est fourni sans axe vertical, il est ignoré.
func (c *Collection) applyZ(ds *xarray.Dataset[float64], p EDRParams) (*xarray.Dataset[float64], error) {
	if p.Z == nil || c.ZDim == "" {
		return ds, nil
	}
	out, err := dsMap(ds, c.ZDim, func(da *xarray.DataArray[float64]) (*xarray.DataArray[float64], error) {
		return da.SelNearest(c.ZDim, *p.Z) // réduit la dimension verticale
	})
	if err != nil {
		return nil, fmt.Errorf("niveau z: %w", err)
	}
	return out, nil
}

// Position reproduit XarrayEDRProvider.position : sélection au point (x, y) le
// plus proche, avec éventuellement une plage temporelle et un sous-ensemble de
// paramètres. Renvoie un Dataset réduit (grille 1×1 → PointSeries en CoverageJSON).
func (c *Collection) Position(x, y float64, p EDRParams) (*xarray.Dataset[float64], error) {
	ds := c.grid()
	var err error
	if len(p.SelectProperties) > 0 {
		if ds, err = selectVars(ds, p.SelectProperties); err != nil {
			return nil, err
		}
	}
	if p.Datetime != nil {
		if c.TDim == "" {
			return nil, fmt.Errorf("la collection %q n'a pas d'axe temporel", c.ID)
		}
		dt := *p.Datetime
		if ds, err = dsSelRange(ds, c.TDim, dt[0], dt[1]); err != nil {
			return nil, fmt.Errorf("datetime: %w", err)
		}
	}
	if ds, err = c.applyZ(ds, p); err != nil {
		return nil, err
	}
	if p.Bilinear {
		// Interpolation bilinéaire au point exact (conserve la dim taille 1 en
		// x/y via SelNearestKeep autour du point interpolé).
		return dsInterpBilinear(ds, c.XDim, c.YDim, x, y)
	}
	if ds, err = dsSelNearest(ds, c.XDim, x); err != nil {
		return nil, fmt.Errorf("position X: %w", err)
	}
	if ds, err = dsSelNearest(ds, c.YDim, y); err != nil {
		return nil, fmt.Errorf("position Y: %w", err)
	}
	return ds, nil
}

// dsInterpBilinear applique l'interpolation bilinéaire (xarray.InterpBilinear) à
// chaque variable portant les axes x/y, puis ré-attache le point (x, y) comme
// axes x/y de taille 1 (pour que CoverageJSON produise un domaine PointSeries
// centré sur le point exact). Le point doit être dans la grille.
func dsInterpBilinear(ds *xarray.Dataset[float64], xDim, yDim string, x, y float64) (*xarray.Dataset[float64], error) {
	vars := map[string]*xarray.DataArray[float64]{}
	for _, name := range ds.VarNames() {
		da, err := ds.Get(name)
		if err != nil {
			return nil, err
		}
		if !da.HasDim(xDim) || !da.HasDim(yDim) {
			vars[name] = da
			continue
		}
		r, err := xarray.InterpBilinear(da, xDim, yDim, x, y) // dims x/y retirées
		if err != nil {
			return nil, err
		}
		// Ré-attache yDim, xDim (taille 1) en tête, avec la valeur du point.
		newDims := append([]string{yDim, xDim}, r.Dims()...)
		newShape := append([]int{1, 1}, r.Shape()...)
		coords := map[string][]float64{xDim: {x}, yDim: {y}}
		for _, d := range r.Dims() {
			if cv, e := r.Coord(d); e == nil {
				coords[d] = cv
			}
		}
		nda, err := xarray.NewDataArray(newDims, newShape, r.Data(), coords, name)
		if err != nil {
			return nil, err
		}
		vars[name] = nda
	}
	return xarray.NewDataset(vars)
}

// Cube reproduit XarrayEDRProvider.cube : sous-cube défini par une emprise
// [minX, minY, maxX, maxY], avec éventuellement une plage temporelle et un
// sous-ensemble de paramètres.
func (c *Collection) Cube(bbox [4]float64, p EDRParams) (*xarray.Dataset[float64], error) {
	q := QueryParams{
		Properties: p.SelectProperties,
		BBox:       &bbox,
		Datetime:   p.Datetime,
	}
	ds, err := c.Query(q)
	if err != nil {
		return nil, err
	}
	return c.applyZ(ds, p)
}
