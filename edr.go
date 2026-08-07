package gocoverage

import (
	"fmt"

	"github.com/bmarty/xarray"
)

// EDRParams rassemble les paramètres communs des requêtes EDR position/cube.
type EDRParams struct {
	SelectProperties []string    // sous-ensemble de paramètres (vide = tous)
	Datetime         *[2]float64 // plage temporelle, nil si absente
}

// Position reproduit XarrayEDRProvider.position : sélection au point (x, y) le
// plus proche, avec éventuellement une plage temporelle et un sous-ensemble de
// paramètres. Renvoie un Dataset réduit (grille 1×1 → PointSeries en CoverageJSON).
func (c *Collection) Position(x, y float64, p EDRParams) (*xarray.Dataset[float64], error) {
	ds := c.Data
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
	if ds, err = dsSelNearest(ds, c.XDim, x); err != nil {
		return nil, fmt.Errorf("position X: %w", err)
	}
	if ds, err = dsSelNearest(ds, c.YDim, y); err != nil {
		return nil, fmt.Errorf("position Y: %w", err)
	}
	return ds, nil
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
	return c.Query(q)
}
