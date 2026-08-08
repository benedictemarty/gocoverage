package gocoverage

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/benedictemarty/xarray"
)

// OGC API - Coverages (successeur moderne de WCS, style OpenAPI). Complète le
// point d'accès `/coverage` déjà présent avec :
//   - /conformance : classes de conformité déclarées ;
//   - /collections/{id}/coverage/domainset : description du domaine (grille CIS) ;
//   - /collections/{id}/coverage/rangetype : description des champs (SWE DataRecord) ;
//   - scaling (scale-factor / scale-axes) : sous-échantillonnage par moyennage,
//     pendant de la classe « scaling » de WCS.
//
// Le CRS est décrit, jamais reprojeté (cf. Collection.CRS) — cohérent avec le
// reste de gocoverage.

// conformanceClasses liste les classes de conformité annoncées par le serveur.
func conformanceClasses() []string {
	return []string{
		"http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-common-2/1.0/conf/collections",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/coverage",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/coverage-subset",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/coverage-bbox",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/coverage-datetime",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/coverage-rangesubset",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/coverage-scaling",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/coverage-domainset",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/coverage-rangetype",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/covjson",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/netcdf",
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/zarr",
	}
}

// conformance : GET /conformance (OGC API - Common).
func (s *Server) conformance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]interface{}{"conformsTo": conformanceClasses()})
}

// -----------------------------------------------------------------------------
// DomainSet — description du domaine de la couverture (CIS 1.1 GeneralGrid).
// -----------------------------------------------------------------------------

// covRegularAxisDesc : axe régulier du domainset (pas constant).
type covRegularAxisDesc struct {
	Type       string  `json:"type"` // "RegularAxis"
	AxisLabel  string  `json:"axisLabel"`
	LowerBound float64 `json:"lowerBound"`
	UpperBound float64 `json:"upperBound"`
	Resolution float64 `json:"resolution"`
	UomLabel   string  `json:"uomLabel,omitempty"`
}

// covIrregularAxisDesc : axe irrégulier (coordonnées explicites, ex. temps).
type covIrregularAxisDesc struct {
	Type       string    `json:"type"` // "IrregularAxis"
	AxisLabel  string    `json:"axisLabel"`
	Coordinate []float64 `json:"coordinate"`
	UomLabel   string    `json:"uomLabel,omitempty"`
}

type covIndexAxis struct {
	Type       string `json:"type"` // "IndexAxis"
	AxisLabel  string `json:"axisLabel"`
	LowerBound int    `json:"lowerBound"`
	UpperBound int    `json:"upperBound"`
}

// DomainSet décrit le domaine de la couverture : axes x/y (réguliers) et, le cas
// échéant, l'axe temporel (irrégulier) et vertical, plus les limites d'index de
// la grille. Pendant du DomainSet de WCS DescribeCoverage.
func (c *Collection) DomainSet() (map[string]interface{}, error) {
	xv, err := c.Data.Coord(c.XDim)
	if err != nil {
		return nil, fmt.Errorf("coordonnée X %q: %w", c.XDim, err)
	}
	yv, err := c.Data.Coord(c.YDim)
	if err != nil {
		return nil, fmt.Errorf("coordonnée Y %q: %w", c.YDim, err)
	}

	axisLabels := []string{"x", "y"}
	axes := []interface{}{
		covRegularAxisDesc{Type: "RegularAxis", AxisLabel: "x", LowerBound: xv[0], UpperBound: xv[len(xv)-1], Resolution: axisResolution(xv)},
		covRegularAxisDesc{Type: "RegularAxis", AxisLabel: "y", LowerBound: yv[0], UpperBound: yv[len(yv)-1], Resolution: axisResolution(yv)},
	}
	gridAxes := []interface{}{
		covIndexAxis{Type: "IndexAxis", AxisLabel: "i", LowerBound: 0, UpperBound: len(xv) - 1},
		covIndexAxis{Type: "IndexAxis", AxisLabel: "j", LowerBound: 0, UpperBound: len(yv) - 1},
	}
	gridLabels := []string{"i", "j"}

	if c.ZDim != "" {
		if zv, err := c.Data.Coord(c.ZDim); err == nil && len(zv) > 0 {
			axisLabels = append(axisLabels, "z")
			axes = append(axes, covIrregularAxisDesc{Type: "IrregularAxis", AxisLabel: "z", Coordinate: zv})
			gridLabels = append(gridLabels, "k")
			gridAxes = append(gridAxes, covIndexAxis{Type: "IndexAxis", AxisLabel: "k", LowerBound: 0, UpperBound: len(zv) - 1})
		}
	}
	if c.TDim != "" {
		if tv, err := c.Data.Coord(c.TDim); err == nil && len(tv) > 0 {
			axisLabels = append(axisLabels, "t")
			axes = append(axes, covIrregularAxisDesc{Type: "IrregularAxis", AxisLabel: "t", Coordinate: tv, UomLabel: "s"})
			gridLabels = append(gridLabels, "l")
			gridAxes = append(gridAxes, covIndexAxis{Type: "IndexAxis", AxisLabel: "l", LowerBound: 0, UpperBound: len(tv) - 1})
		}
	}

	return map[string]interface{}{
		"type": "DomainSet",
		"generalGrid": map[string]interface{}{
			"type":       "GeneralGridCoverage",
			"srsName":    c.CRS.id(),
			"axisLabels": axisLabels,
			"axis":       axes,
			"gridLimits": map[string]interface{}{
				"type":       "GridLimits",
				"srsName":    "http://www.opengis.net/def/crs/OGC/0/Index2D",
				"axisLabels": gridLabels,
				"axis":       gridAxes,
			},
		},
	}, nil
}

// axisResolution renvoie le pas (constant supposé) d'un axe régulier, ou 0 si un
// seul point.
func axisResolution(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	return (v[len(v)-1] - v[0]) / float64(len(v)-1)
}

// domainset : GET /collections/{id}/coverage/domainset.
func (s *Server) domainset(w http.ResponseWriter, r *http.Request, c *Collection) {
	ds, err := c.DomainSet()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ds)
}

// -----------------------------------------------------------------------------
// RangeType — description des champs de la couverture (SWE Common DataRecord).
// -----------------------------------------------------------------------------

// RangeType décrit les champs (paramètres) de la couverture : un enregistrement
// SWE Common (DataRecord) avec le nom, la description et l'unité de chaque champ.
// Pendant du RangeType de WCS DescribeCoverage.
func (c *Collection) RangeType() map[string]interface{} {
	var fields []interface{}
	for _, f := range c.Fields() {
		q := map[string]interface{}{
			"type":        "Quantity",
			"description": labelOr(f.Title, f.Name),
			"encodingInfo": map[string]interface{}{
				"dataType": "http://www.opengis.net/def/dataType/OGC/0/" + fieldType(f),
			},
		}
		if f.Unit != "" {
			q["uom"] = map[string]interface{}{"type": "UnitReference", "code": f.Unit}
		}
		fields = append(fields, map[string]interface{}{
			"type":     "Field",
			"name":     f.Name,
			"quantity": q,
		})
	}
	return map[string]interface{}{"type": "DataRecord", "field": fields}
}

// rangetype : GET /collections/{id}/coverage/rangetype.
func (s *Server) rangetype(w http.ResponseWriter, r *http.Request, c *Collection) {
	writeJSON(w, 200, c.RangeType())
}

// -----------------------------------------------------------------------------
// Scaling — sous-échantillonnage (classe « scaling » de WCS/Coverages).
// -----------------------------------------------------------------------------

// parseScaling lit les paramètres scale-factor et scale-axes et renvoie un
// facteur entier (≥ 1) par dimension résolue. scale-factor s'applique aux axes
// x et y ; scale-axes(Axis(n),…) le raffine par axe. Renvoie nil si aucun.
func (c *Collection) parseScaling(scaleFactor, scaleAxes string) (map[string]int, error) {
	factors := map[string]int{}
	if s := strings.TrimSpace(scaleFactor); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("scale-factor invalide %q (entier ≥ 1 attendu)", scaleFactor)
		}
		if n > 1 {
			factors[c.XDim] = n
			factors[c.YDim] = n
		}
	}
	if s := strings.TrimSpace(scaleAxes); s != "" {
		for _, expr := range splitTopLevel(s, ',') {
			expr = strings.TrimSpace(expr)
			open := strings.IndexByte(expr, '(')
			if open < 0 || !strings.HasSuffix(expr, ")") {
				return nil, fmt.Errorf("scale-axes invalide %q (attendu Axe(n))", expr)
			}
			dim, err := c.resolveAxis(strings.TrimSpace(expr[:open]))
			if err != nil {
				return nil, err
			}
			n, err := strconv.Atoi(strings.TrimSpace(expr[open+1 : len(expr)-1]))
			if err != nil || n < 1 {
				return nil, fmt.Errorf("scale-axes %q: facteur entier ≥ 1 attendu", expr)
			}
			if n > 1 {
				factors[dim] = n
			} else {
				delete(factors, dim)
			}
		}
	}
	if len(factors) == 0 {
		return nil, nil
	}
	return factors, nil
}

// applyScaling sous-échantillonne le Dataset en moyennant des blocs de `factor`
// éléments le long de chaque dimension indiquée (agrégation, comme le
// rééchantillonnage « average » de WCS).
func applyScaling(ds *xarray.Dataset[float64], factors map[string]int) (*xarray.Dataset[float64], error) {
	var err error
	for dim, factor := range factors {
		if factor <= 1 {
			continue
		}
		ds, err = dsMap(ds, dim, func(da *xarray.DataArray[float64]) (*xarray.DataArray[float64], error) {
			r, err := da.Coarsen(dim, factor)
			if err != nil {
				return nil, err
			}
			return r.Mean()
		})
		if err != nil {
			return nil, fmt.Errorf("scaling %s: %w", dim, err)
		}
	}
	return ds, nil
}
