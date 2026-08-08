package gocoverage

import (
	"fmt"
	"math"

	"github.com/benedictemarty/xarray"
)

// Requête EDR « corridor » : sous-ensemble dans un tube (buffer) autour d'une
// polyligne (route), de demi-largeur donnée. On restreint à l'emprise élargie
// puis on masque (→ null) les cellules dont le centre est à plus de halfWidth de
// la polyligne. La largeur est exprimée dans l'unité des coordonnées (degrés en
// CRS84). Utile en aviation (couloir autour d'une route de vol).

// distPointSegment renvoie la distance euclidienne du point (px, py) au segment
// [a, b].
func distPointSegment(px, py float64, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	if dx == 0 && dy == 0 {
		return math.Hypot(px-a[0], py-a[1])
	}
	t := ((px-a[0])*dx + (py-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(a[0]+t*dx), py-(a[1]+t*dy))
}

// distToPolyline renvoie la distance minimale du point (x, y) à la polyligne.
func distToPolyline(x, y float64, line [][2]float64) float64 {
	best := math.Inf(1)
	for i := 0; i+1 < len(line); i++ {
		if d := distPointSegment(x, y, line[i], line[i+1]); d < best {
			best = d
		}
	}
	return best
}

// Corridor restreint la collection au tube de demi-largeur halfWidth (dans units :
// deg/km/m) autour de la polyligne line, puis masque (NaN) les cellules hors du
// tube. En unité métrique, la distance à la polyligne est calculée en mètres.
func (c *Collection) Corridor(line [][2]float64, halfWidth float64, units string, p EDRParams) (*xarray.Dataset[float64], error) {
	if len(line) < 2 {
		return nil, fmt.Errorf("corridor: au moins 2 points requis")
	}
	if halfWidth <= 0 {
		return nil, fmt.Errorf("corridor: demi-largeur (corridor-width) > 0 requise")
	}
	meters, metric, err := lengthMeters(halfWidth, units)
	if err != nil {
		return nil, err
	}
	// Marge d'emprise en degrés (borne supérieure en métrique).
	marginDeg := halfWidth
	if metric {
		marginDeg = meters / metersPerDegLat
	}
	minx, miny := math.Inf(1), math.Inf(1)
	maxx, maxy := math.Inf(-1), math.Inf(-1)
	for _, pt := range line {
		minx, maxx = math.Min(minx, pt[0]), math.Max(maxx, pt[0])
		miny, maxy = math.Min(miny, pt[1]), math.Max(maxy, pt[1])
	}
	bbox := [4]float64{minx - marginDeg, miny - marginDeg, maxx + marginDeg, maxy + marginDeg}
	ds, err := c.Query(QueryParams{Properties: p.SelectProperties, BBox: &bbox, Datetime: p.Datetime})
	if err != nil {
		return nil, err
	}
	keep := func(x, y float64) bool { return distToPolyline(x, y, line) <= halfWidth }
	if metric {
		keep = func(x, y float64) bool { return distMetersToPolyline(x, y, line) <= meters }
	}
	return maskDataset(ds, c.XDim, c.YDim, keep)
}
