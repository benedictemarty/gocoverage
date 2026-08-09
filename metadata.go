package gocoverage

import (
	"net/http"
	"strings"
	"time"

	"github.com/benedictemarty/xarray"
)

// Métadonnées de collection conformes OGC API - Common / EDR : `extent`
// (spatial + temporel), `parameter_names` et `data_queries` (découverte des
// requêtes EDR), plus la négociation de contenu (`Accept`) et le contrôle du
// CRS de requête. Complète la simple liste de liens historique.

// outputFormat associe un identifiant `f` à son étiquette et son type MIME.
type outputFormat struct {
	F     string
	Label string
	MIME  string
}

// supportedFormats : formats de sortie négociables sur les endpoints de données.
var supportedFormats = []outputFormat{
	{"json", "CoverageJSON", "application/prs.coverage+json"},
	{"geojson", "GeoJSON", "application/geo+json"},
	{"netcdf", "netCDF", "application/x-netcdf"},
	{"zarr", "Zarr", "application/zip"},
}

func outputFormatLabels() []string {
	out := make([]string, len(supportedFormats))
	for i, f := range supportedFormats {
		out[i] = f.Label
	}
	return out
}

// formatFromAccept déduit un identifiant `f` depuis l'en-tête Accept (remarque H
// : la négociation de contenu ne reposait que sur `?f=`). Renvoie "" si aucun
// type connu n'est demandé (→ défaut CoverageJSON).
func formatFromAccept(accept string) string {
	accept = strings.ToLower(accept)
	switch {
	case strings.Contains(accept, "coverage+json"):
		return "json"
	case strings.Contains(accept, "geo+json"):
		return "geojson"
	case strings.Contains(accept, "x-netcdf"), strings.Contains(accept, "netcdf"):
		return "netcdf"
	case strings.Contains(accept, "application/zip"):
		return "zarr"
	case strings.Contains(accept, "application/json"):
		return "json"
	}
	return ""
}

// negotiateFormat choisit le format : `?f=` prioritaire, sinon en-tête `Accept`,
// sinon "" (défaut CoverageJSON).
func negotiateFormat(r *http.Request) string {
	if f := strings.TrimSpace(r.URL.Query().Get("f")); f != "" {
		return strings.ToLower(f)
	}
	return formatFromAccept(r.Header.Get("Accept"))
}

// acceptExplicitUnsupported indique qu'un en-tête Accept demande explicitement un
// type qu'on ne sait pas produire (ni joker `*/*`, ni type connu) → 406.
func acceptExplicitUnsupported(accept string) bool {
	accept = strings.ToLower(strings.TrimSpace(accept))
	if accept == "" || strings.Contains(accept, "*/*") {
		return false
	}
	return formatFromAccept(accept) == ""
}

// datasetCells renvoie le nombre maximal d'éléments parmi les variables du
// Dataset (borne de la taille sérialisée).
func datasetCells(ds *xarray.Dataset[float64]) int {
	max := 0
	for _, name := range ds.VarNames() {
		if da, err := ds.Get(name); err == nil {
			if n := len(da.Data()); n > max {
				max = n
			}
		}
	}
	return max
}

// crsParamSupported indique si une valeur de paramètre CRS de requête
// (crs/bbox-crs/subset-crs) est acceptable. gocoverage ne reprojette pas
// (remarque B) : seul le CRS de stockage de la collection — ou un synonyme de
// CRS84 quand la collection est en CRS84 — est accepté. Toute autre valeur est
// rejetée (400) plutôt qu'ignorée silencieusement.
func crsParamSupported(c *Collection, val string) bool {
	v := strings.TrimSpace(val)
	if v == "" {
		return true
	}
	lv := strings.ToLower(strings.Trim(v, "<>"))
	if lv == strings.ToLower(c.CRS.id()) {
		return true
	}
	// Synonymes de CRS84 uniquement (lon/lat). EPSG:4326 est volontairement exclu :
	// son ordre d'axes est lat/lon et, sans reprojection, l'accepter inverserait
	// silencieusement les coordonnées d'un bbox (remarque L). → rejeté (400).
	if c.CRS.id() == crs84 {
		switch lv {
		case strings.ToLower(crs84),
			"crs84", "ogc:crs84", "urn:ogc:def:crs:ogc:1.3:crs84",
			"http://www.opengis.net/def/crs/ogc/1.3/crs84":
			return true
		}
	}
	return false
}

// checkRequestCRS vérifie les paramètres CRS d'une requête. Renvoie un message
// d'erreur non vide si l'un d'eux désigne un CRS non supporté.
func checkRequestCRS(c *Collection, q map[string][]string) string {
	for _, key := range []string{"crs", "bbox-crs", "subset-crs", "coords-crs"} {
		if vals, ok := q[key]; ok {
			for _, v := range vals {
				if !crsParamSupported(c, v) {
					return "CRS non supporté pour " + key + ": " + v +
						" (pas de reprojection ; seul " + c.CRS.id() + " est accepté)"
				}
			}
		}
	}
	return ""
}

// -----------------------------------------------------------------------------
// Description de collection conforme (extent + parameter_names + data_queries).
// -----------------------------------------------------------------------------

// extentDoc construit l'objet `extent` OGC Common : emprise spatiale et,
// si présent, intervalle temporel (ISO 8601 quand le temps est en secondes epoch).
func (c *Collection) extentDoc() map[string]interface{} {
	bb := c.BBox()
	spatial := map[string]interface{}{
		"bbox": [][]float64{{bb[0], bb[1], bb[2], bb[3]}},
		"crs":  c.CRS.id(),
	}
	ext := map[string]interface{}{"spatial": spatial}
	if tr, ok := c.TimeExtent(); ok {
		ts := c.coordOf(c.TDim)
		var lo, hi interface{} = tr[0], tr[1]
		if c.timeIsEpoch(ts) {
			lo = time.Unix(int64(tr[0]), 0).UTC().Format(time.RFC3339)
			hi = time.Unix(int64(tr[1]), 0).UTC().Format(time.RFC3339)
		}
		ext["temporal"] = map[string]interface{}{
			"interval": [][]interface{}{{lo, hi}},
			"trs":      "http://www.opengis.net/def/uom/ISO-8601/0/Gregorian",
		}
	}
	return ext
}

// parameterNames construit l'objet EDR `parameter_names` (un descripteur par
// champ : type, unité, propriété observée).
func (c *Collection) parameterNames() map[string]interface{} {
	out := map[string]interface{}{}
	for _, f := range c.Fields() {
		desc := map[string]interface{}{
			"type":             "Parameter",
			"description":      labelOr(f.Title, f.Name),
			"observedProperty": map[string]interface{}{"id": f.Name, "label": labelOr(f.Title, f.Name)},
		}
		if f.Unit != "" {
			desc["unit"] = map[string]interface{}{"symbol": f.Unit}
		}
		out[f.Name] = desc
	}
	return out
}

// dataQueries construit l'objet EDR `data_queries` décrivant chaque type de
// requête disponible sur la collection (remarque A : découverte conforme).
func (c *Collection) dataQueries() map[string]interface{} {
	base := "/collections/" + c.ID + "/"
	types := []string{"position", "cube", "area", "corridor", "radius", "trajectory"}
	if len(c.Locations) > 0 {
		types = append(types, "locations")
	}
	if len(c.Instances) > 0 {
		types = append(types, "instances")
	}
	out := map[string]interface{}{}
	for _, t := range types {
		vars := map[string]interface{}{
			"title":                 t + " query",
			"query_type":            t,
			"output_formats":        outputFormatLabels(),
			"default_output_format": "CoverageJSON",
			"crs_details":           []map[string]string{{"crs": c.CRS.id()}},
		}
		// Unités de distance déclarées pour les requêtes à rayon/largeur.
		if t == "radius" || t == "corridor" {
			vars["within_units"] = []string{"deg", "km", "m"}
		}
		out[t] = map[string]interface{}{
			"link": map[string]interface{}{
				"href":      base + t,
				"rel":       "data",
				"templated": true,
				"variables": vars,
			},
		}
	}
	return out
}

// collectionDoc assemble la description conforme d'une collection (OGC Common +
// EDR + Coverages). Réutilisé par describe().
func (c *Collection) collectionDoc() map[string]interface{} {
	base := "/collections/" + c.ID + "/"
	links := []map[string]string{
		{"rel": "self", "href": "/collections/" + c.ID},
		{"rel": "coverage", "href": base + "coverage"},
		{"rel": "http://www.opengis.net/def/rel/ogc/1.0/coverage-domainset", "href": base + "coverage/domainset"},
		{"rel": "http://www.opengis.net/def/rel/ogc/1.0/coverage-rangetype", "href": base + "coverage/rangetype"},
		// OGC API - Maps : rendu image par défaut de la collection.
		{"rel": "http://www.opengis.net/def/rel/ogc/1.0/map", "href": base + "map", "type": "image/png"},
		// OGC API - Tiles : tuiles carte matricielles.
		{"rel": "http://www.opengis.net/def/rel/ogc/1.0/tilesets-map", "href": base + "map/tiles", "type": "application/json"},
		// OGC API - Features : mailles de la grille en Features Point.
		{"rel": "items", "href": base + "items", "type": "application/geo+json"},
	}
	for _, t := range []string{"position", "cube", "trajectory", "area", "corridor", "radius", "locations", "instances"} {
		links = append(links, map[string]string{"rel": "data", "href": base + t})
	}
	return map[string]interface{}{
		"id":              c.ID,
		"title":           c.Title,
		"itemType":        "coverage",
		"crs":             []string{c.CRS.id()},
		"extent":          c.extentDoc(),
		"parameter_names": c.parameterNames(),
		"data_queries":    c.dataQueries(),
		"parameters":      c.Fields(),     // conservé (compat)
		"properties":      c.Properties(), // conservé (coverage_properties pygeoapi)
		"links":           links,
	}
}
