package gocoverage

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bmarty/xarray"
)

// CRS décrit le système de coordonnées de référence d'une collection, à la
// manière de pygeoapi (_get_coverage_properties : bbox_crs + crs_type). Aucune
// reprojection n'est effectuée — comme pygeoapi, le CRS est seulement **décrit**
// (bbox_crs non géré en requête).
type CRS struct {
	ID   string // URI du CRS ; vide → CRS84
	Type string // "GeographicCRS" | "ProjectedCRS" ; vide → GeographicCRS
	EPSG int    // code EPSG ; 0 → 4326 / CRS84
}

// CRS84 renvoie le CRS géographique par défaut (WGS84, lon/lat).
func CRS84() CRS {
	return CRS{ID: crs84, Type: "GeographicCRS", EPSG: 4326}
}

// EPSGCRS construit un CRS à partir d'un code EPSG (projeté ou géographique).
func EPSGCRS(code int, projected bool) CRS {
	t := "GeographicCRS"
	if projected {
		t = "ProjectedCRS"
	}
	return CRS{
		ID:   fmt.Sprintf("http://www.opengis.net/def/crs/EPSG/0/%d", code),
		Type: t,
		EPSG: code,
	}
}

// id renvoie l'URI du CRS (CRS84 par défaut).
func (c CRS) id() string {
	if c.ID == "" {
		return crs84
	}
	return c.ID
}

// typ renvoie le type du CRS ("GeographicCRS" par défaut).
func (c CRS) typ() string {
	if c.Type == "" {
		return "GeographicCRS"
	}
	return c.Type
}

// crsVarNames : noms usuels d'une variable de conteneur CRS (grid_mapping) CF.
var crsVarNames = map[string]bool{
	"crs": true, "spatial_ref": true, "projection": true,
	"grid_mapping": true, "lambert_conformal_conic": true,
	"transverse_mercator": true, "polar_stereographic": true,
	"latitude_longitude": true,
}

// detectCRS recherche une variable de conteneur CRS (attributs CF
// grid_mapping_name / epsg_code, ou nom usuel) et en déduit le CRS. Renvoie le
// CRS trouvé et le nom de la variable à retirer des paramètres (vide si aucune).
// À défaut, CRS84.
func detectCRS(ds *xarray.Dataset[float64]) (CRS, string) {
	for _, name := range ds.VarNames() {
		da, err := ds.Get(name)
		if err != nil {
			continue
		}
		attrs := da.Variable().Attrs()
		gm := attrs["grid_mapping_name"]
		epsgStr := firstNonEmpty(attrs["epsg_code"], attrs["spatial_epsg"], attrs["EPSG"])
		if gm == "" && epsgStr == "" && !crsVarNames[strings.ToLower(name)] {
			continue
		}
		// Variable de conteneur CRS : ne pas l'exposer comme paramètre.
		projected := gm != "" && gm != "latitude_longitude" && gm != "rotated_latitude_longitude"
		if code, err := strconv.Atoi(strings.TrimSpace(epsgStr)); err == nil && code > 0 {
			return EPSGCRS(code, projected && code != 4326), name
		}
		if projected {
			// Projeté mais code EPSG inconnu : type décrit, id omis (honnête).
			return CRS{Type: "ProjectedCRS"}, name
		}
		return CRS84(), name
	}
	return CRS84(), ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
