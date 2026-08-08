package gocoverage

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/benedictemarty/xarray"
)

// Requête EDR « radius » : sous-ensemble dans un disque de rayon donné autour
// d'un point. On restreint à l'emprise carrée du disque puis on masque (→ null)
// les cellules dont le centre est à plus de `radius` du point. Le rayon est
// exprimé en degrés (défaut) ou converti depuis km / m (approximation sphérique
// simple : 1° ≈ 111,32 km — sans correction de latitude).

// radiusInDegrees convertit une valeur + unité en degrés (approximatif pour km/m).
func radiusInDegrees(v float64, units string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(units)) {
	case "", "deg", "degree", "degrees":
		return v, nil
	case "km", "kilometre", "kilometres", "kilometer", "kilometers":
		return v / 111.32, nil
	case "m", "metre", "metres", "meter", "meters":
		return v / 111320.0, nil
	default:
		return 0, fmt.Errorf("within-units %q non pris en charge (deg/km/m)", units)
	}
}

// Radius restreint la collection au disque de rayon radiusDeg (degrés) centré sur
// (cx, cy), puis masque (NaN) les cellules hors du disque.
func (c *Collection) Radius(cx, cy, radiusDeg float64, p EDRParams) (*xarray.Dataset[float64], error) {
	if radiusDeg <= 0 {
		return nil, fmt.Errorf("radius: rayon (within) > 0 requis")
	}
	bbox := [4]float64{cx - radiusDeg, cy - radiusDeg, cx + radiusDeg, cy + radiusDeg}
	ds, err := c.Query(QueryParams{Properties: p.SelectProperties, BBox: &bbox, Datetime: p.Datetime})
	if err != nil {
		return nil, err
	}
	r2 := radiusDeg * radiusDeg
	return maskDataset(ds, c.XDim, c.YDim, func(x, y float64) bool {
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= r2
	})
}

// parsePoint analyse un WKT POINT(lon lat) ou « lon,lat » en (lon, lat).
func parsePoint(s string) (float64, float64, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToUpper(s), "POINT") {
		open := strings.IndexByte(s, '(')
		if open < 0 || !strings.HasSuffix(s, ")") {
			return 0, 0, fmt.Errorf("WKT POINT mal formé")
		}
		s = s[open+1 : len(s)-1]
	}
	f := strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool { return r == ' ' || r == ',' })
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("point invalide (attendu « lon lat »)")
	}
	lon, err1 := strconv.ParseFloat(f[0], 64)
	lat, err2 := strconv.ParseFloat(f[1], 64)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("coordonnées non numériques")
	}
	return lon, lat, nil
}
