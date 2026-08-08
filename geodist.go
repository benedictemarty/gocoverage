package gocoverage

import (
	"fmt"
	"math"
	"strings"
)

// Distances géodésiques approchées pour les requêtes à rayon/largeur métriques
// (radius, corridor). On utilise une projection équirectangulaire locale : les
// écarts de coordonnées sont convertis en mètres via le nombre de mètres par
// degré à la latitude considérée (longitude corrigée par cos(lat)). Précis à
// l'échelle régionale (dizaines à centaines de km) ; pour de très grandes
// distances, préférer une vraie formule géodésique.

const (
	metersPerDegLat = 111132.0 // ~ constant
	metersPerDegLon = 111320.0 // à l'équateur ; × cos(lat) ailleurs
)

// metersPerDegree renvoie les mètres par degré de longitude et de latitude à la
// latitude lat (degrés).
func metersPerDegree(lat float64) (mLon, mLat float64) {
	return metersPerDegLon * math.Cos(lat*math.Pi/180), metersPerDegLat
}

// distMeters renvoie la distance (mètres) entre deux points proches (lon/lat, °).
func distMeters(lon1, lat1, lon2, lat2 float64) float64 {
	mLon, mLat := metersPerDegree((lat1 + lat2) / 2)
	dx := (lon2 - lon1) * mLon
	dy := (lat2 - lat1) * mLat
	return math.Hypot(dx, dy)
}

// distMetersPointSeg renvoie la distance (mètres) du point (px, py) au segment
// [a, b], en projetant localement autour de la latitude du point.
func distMetersPointSeg(px, py float64, a, b [2]float64) float64 {
	mLon, mLat := metersPerDegree(py)
	// Coordonnées en mètres relatives au point (origine).
	ax, ay := (a[0]-px)*mLon, (a[1]-py)*mLat
	bx, by := (b[0]-px)*mLon, (b[1]-py)*mLat
	return distPointSegment(0, 0, [2]float64{ax, ay}, [2]float64{bx, by})
}

// distMetersToPolyline renvoie la distance minimale (mètres) de (x, y) à la
// polyligne line.
func distMetersToPolyline(x, y float64, line [][2]float64) float64 {
	best := math.Inf(1)
	for i := 0; i+1 < len(line); i++ {
		if d := distMetersPointSeg(x, y, line[i], line[i+1]); d < best {
			best = d
		}
	}
	return best
}

// lengthMeters convertit une longueur v exprimée dans units (deg/km/m) en mètres,
// et indique si l'unité est métrique (auquel cas les distances doivent être
// calculées en mètres plutôt qu'en degrés).
func lengthMeters(v float64, units string) (meters float64, metric bool, err error) {
	switch strings.ToLower(strings.TrimSpace(units)) {
	case "", "deg", "degree", "degrees":
		return 0, false, nil // non métrique : la valeur reste en degrés
	case "km", "kilometre", "kilometres", "kilometer", "kilometers":
		return v * 1000, true, nil
	case "m", "metre", "metres", "meter", "meters":
		return v, true, nil
	default:
		return 0, false, fmt.Errorf("unité %q non prise en charge (deg/km/m)", units)
	}
}
