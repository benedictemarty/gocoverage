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

// Radius restreint la collection au disque de rayon within (dans units :
// deg/km/m) centré sur (cx, cy), puis masque (NaN) les cellules hors du disque.
// En unité métrique (km/m), la distance est calculée en mètres (projection
// équirectangulaire locale, correcte à haute latitude) ; en degrés, en distance
// euclidienne sur les coordonnées.
func (c *Collection) Radius(cx, cy, within float64, units string, p EDRParams) (*xarray.Dataset[float64], error) {
	if within <= 0 {
		return nil, fmt.Errorf("radius: rayon (within) > 0 requis")
	}
	meters, metric, err := lengthMeters(within, units)
	if err != nil {
		return nil, err
	}
	// Emprise (bbox) de pré-filtrage : marges séparées en longitude/latitude.
	// En métrique, la marge de longitude est corrigée par cos(lat) (remarque K).
	marginLon, marginLat := within, within
	if metric {
		marginLon, marginLat = degMargins(meters, cy)
	}
	bbox := [4]float64{cx - marginLon, cy - marginLat, cx + marginLon, cy + marginLat}
	ds, err := c.Query(QueryParams{Properties: p.SelectProperties, BBox: &bbox, Datetime: p.Datetime})
	if err != nil {
		return nil, err
	}
	keep := func(x, y float64) bool {
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= within*within
	}
	if metric {
		keep = func(x, y float64) bool { return distMeters(cx, cy, x, y) <= meters }
	}
	return maskDataset(ds, c.XDim, c.YDim, keep)
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
