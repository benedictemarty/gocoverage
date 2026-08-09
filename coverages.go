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
		"http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/html",
		"http://www.opengis.net/spec/ogcapi-common-2/1.0/conf/collections",
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/html",
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
		"http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/oas30",
		"http://www.opengis.net/spec/ogcapi-edr-1/1.1/conf/core",
		// OGC API - Maps (successeur de WMS) : rendu image d'une couverture.
		"http://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/geodata-maps",
		"http://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/spatial-subsetting",
		"http://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/png",
		"http://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/jpeg",
		"http://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/oas30",
		"http://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/tilesets",
		// OGC API - Tiles : tuiles carte matricielles.
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tileset",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tilesets-list",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/png",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/jpeg",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/oas30",
		"http://www.opengis.net/spec/ogcapi-tilematrixset-1/1.0/conf/tilematrixset",
		// OGC API - Features (successeur de WFS) : mailles en Features Point.
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/oas30",
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson",
		// OGC API - Features Part 3 : filtrage CQL2.
		"http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/filter",
		"http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/features-filter",
		"http://www.opengis.net/spec/cql2/1.0/conf/cql2-text",
		"http://www.opengis.net/spec/cql2/1.0/conf/basic-cql2",
		"http://www.opengis.net/spec/cql2/1.0/conf/advanced-comparison-operators",
		"http://www.opengis.net/spec/cql2/1.0/conf/spatial-operators",
		"http://www.opengis.net/spec/cql2/1.0/conf/basic-spatial-operators",
	}
}

// openapi : GET /api — description OpenAPI 3.0 minimale du service (classe
// oas30). Squelette (pas une spec exhaustive) : suffit à annoncer les chemins
// principaux et leur négociation de contenu.
func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	path := func(desc string) map[string]interface{} {
		return map[string]interface{}{"get": map[string]interface{}{
			"description": desc,
			"responses":   map[string]interface{}{"200": map[string]string{"description": "OK"}},
		}}
	}
	doc := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "gocoverage — OGC API Coverages / EDR",
			"version":     "0.20.0",
			"description": "Serveur de couvertures (xarray-go) : OGC API - Coverages + EDR.",
		},
		"paths": map[string]interface{}{
			"/collections":                         path("Liste des collections"),
			"/collections/{collectionId}":          path("Description d'une collection (extent, data_queries)"),
			"/collections/{collectionId}/coverage": path("Récupération de la couverture (bbox, subset, datetime, scale-*, f)"),
			"/collections/{collectionId}/map":      path("Rendu image de la couverture (bbox, width, height, datetime, z, properties, colorscalerange, style, f=png|jpeg)"),
			"/collections/{collectionId}/map/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}": path("Tuile carte (OGC API - Tiles)"),
			"/tileMatrixSets":                               path("TileMatrixSets gérés (WorldCRS84Quad, WebMercatorQuad)"),
			"/collections/{collectionId}/items":             path("Entités (mailles) GeoJSON (OGC API - Features : bbox, limit, offset, datetime, properties)"),
			"/collections/{collectionId}/items/{featureId}": path("Une entité (maille) par identifiant"),
			"/collections/{collectionId}/queryables":        path("Schéma JSON des propriétés filtrables (CQL2)"),
			"/collections/{collectionId}/position":          path("Requête EDR position"),
			"/collections/{collectionId}/area":              path("Requête EDR area (polygone, trous)"),
			"/collections/{collectionId}/cube":              path("Requête EDR cube"),
			"/conformance":                                  path("Classes de conformité"),
		},
	}
	writeJSON(w, 200, doc)
}

// conformance : GET /conformance (OGC API - Common).
func (s *Server) conformance(w http.ResponseWriter, r *http.Request) {
	if wantsHTML(r) {
		conformanceHTML(w, conformanceClasses())
		return
	}
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
	xv, yv := c.coordOf(c.XDim), c.coordOf(c.YDim)
	if len(xv) == 0 || len(yv) == 0 {
		return nil, fmt.Errorf("coordonnées X/Y %q/%q introuvables", c.XDim, c.YDim)
	}

	axisLabels := []string{"x", "y"}
	// Axe régulier si le pas est constant, sinon axe irrégulier par coordonnées
	// (remarque C : ne pas annoncer une résolution constante inexistante).
	axes := []interface{}{
		gridAxisDesc("x", xv),
		gridAxisDesc("y", yv),
	}
	gridAxes := []interface{}{
		covIndexAxis{Type: "IndexAxis", AxisLabel: "i", LowerBound: 0, UpperBound: len(xv) - 1},
		covIndexAxis{Type: "IndexAxis", AxisLabel: "j", LowerBound: 0, UpperBound: len(yv) - 1},
	}
	gridLabels := []string{"i", "j"}

	if c.ZDim != "" {
		if zv := c.coordOf(c.ZDim); len(zv) > 0 {
			axisLabels = append(axisLabels, "z")
			axes = append(axes, covIrregularAxisDesc{Type: "IrregularAxis", AxisLabel: "z", Coordinate: zv})
			gridLabels = append(gridLabels, "k")
			gridAxes = append(gridAxes, covIndexAxis{Type: "IndexAxis", AxisLabel: "k", LowerBound: 0, UpperBound: len(zv) - 1})
		}
	}
	if c.TDim != "" {
		if tv := c.coordOf(c.TDim); len(tv) > 0 {
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

// gridAxisDesc décrit un axe de grille : RegularAxis si le pas est constant,
// sinon IrregularAxis (coordonnées explicites).
func gridAxisDesc(label string, v []float64) interface{} {
	if isRegularAxis(v) {
		return covRegularAxisDesc{Type: "RegularAxis", AxisLabel: label, LowerBound: v[0], UpperBound: v[len(v)-1], Resolution: axisResolution(v)}
	}
	return covIrregularAxisDesc{Type: "IrregularAxis", AxisLabel: label, Coordinate: v}
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

// parseScaling lit scale-factor, scale-axes et scale-size et renvoie un facteur
// entier (≥ 1) par dimension résolue. scale-factor s'applique aux axes x et y ;
// scale-axes(Axe(n),…) fixe un facteur par axe ; scale-size(Axe(n),…) fixe une
// taille cible (converti en facteur ≈ taille_source/cible). Renvoie nil si aucun.
func (c *Collection) parseScaling(scaleFactor, scaleAxes, scaleSize string) (map[string]int, error) {
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
	if err := c.parseAxisFactors(scaleAxes, "scale-axes", factors, false); err != nil {
		return nil, err
	}
	if err := c.parseAxisFactors(scaleSize, "scale-size", factors, true); err != nil {
		return nil, err
	}
	if len(factors) == 0 {
		return nil, nil
	}
	return factors, nil
}

// parseAxisFactors analyse une liste Axe(n),… . Si asSize, n est une taille cible
// convertie en facteur (taille_source ÷ cible) ; sinon n est le facteur direct.
func (c *Collection) parseAxisFactors(spec, label string, factors map[string]int, asSize bool) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	for _, expr := range splitTopLevel(spec, ',') {
		expr = strings.TrimSpace(expr)
		open := strings.IndexByte(expr, '(')
		if open < 0 || !strings.HasSuffix(expr, ")") {
			return fmt.Errorf("%s invalide %q (attendu Axe(n))", label, expr)
		}
		dim, err := c.resolveAxis(strings.TrimSpace(expr[:open]))
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(strings.TrimSpace(expr[open+1 : len(expr)-1]))
		if err != nil || n < 1 {
			return fmt.Errorf("%s %q: entier ≥ 1 attendu", label, expr)
		}
		factor := n
		if asSize {
			factor = 1
			if src := c.grid().Dims()[dim]; src > n {
				factor = src / n // taille cible → facteur d'agrégation
			}
		}
		if factor > 1 {
			factors[dim] = factor
		} else {
			delete(factors, dim)
		}
	}
	return nil
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
		// Pas d'origine, pour recentrer ensuite les coordonnées sur le milieu du
		// bloc (Coarsen étiquette le bloc par sa borne gauche — remarque M).
		orig, _ := ds.Coord(dim)
		step := 0.0
		if len(orig) >= 2 {
			step = orig[1] - orig[0]
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
		if shift := step * float64(factor-1) / 2; shift != 0 {
			if ds, err = shiftCoord(ds, dim, shift); err != nil {
				return nil, fmt.Errorf("scaling %s (recentrage): %w", dim, err)
			}
		}
	}
	return ds, nil
}

// shiftCoord reconstruit le Dataset en ajoutant delta à la coordonnée de la
// dimension dim (recentrage des mailles après agrégation). Les variables sans
// cette dimension sont inchangées.
func shiftCoord(ds *xarray.Dataset[float64], dim string, delta float64) (*xarray.Dataset[float64], error) {
	vars := map[string]*xarray.DataArray[float64]{}
	for _, name := range ds.VarNames() {
		da, err := ds.Get(name)
		if err != nil {
			return nil, err
		}
		if !da.HasDim(dim) {
			vars[name] = da
			continue
		}
		dims := da.Variable().Dims()
		coords := map[string][]float64{}
		for _, d := range dims {
			cv, err := da.Coord(d)
			if err != nil {
				continue
			}
			c2 := append([]float64(nil), cv...)
			if d == dim {
				for i := range c2 {
					c2[i] += delta
				}
			}
			coords[d] = c2
		}
		nda, err := xarray.NewDataArray(dims, da.Shape(), append([]float64(nil), da.Data()...), coords, name)
		if err != nil {
			return nil, err
		}
		vars[name] = nda
	}
	return xarray.NewDataset(vars)
}
