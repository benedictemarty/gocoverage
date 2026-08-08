package gocoverage

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/benedictemarty/xarray"
)

// Requête EDR « area » : sous-ensemble d'une couverture par un polygone
// arbitraire (WKT POLYGON). On restreint à l'emprise (bbox) du polygone puis on
// met à NaN les cellules dont le centre est hors du polygone (point-in-polygon).
// Le résultat reste une grille (CoverageJSON), les cellules hors polygone valant
// null.

// pointInPolygon teste l'appartenance d'un point (x, y) à un anneau polygonal
// (algorithme du lancer de rayon).
func pointInPolygon(x, y float64, ring [][2]float64) bool {
	in := false
	n := len(ring)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := ring[i][0], ring[i][1]
		xj, yj := ring[j][0], ring[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

// Area restreint la collection à l'emprise du polygone ring puis masque (NaN) les
// cellules hors du polygone. p permet de sélectionner champs/temps.
func (c *Collection) Area(ring [][2]float64, p EDRParams) (*xarray.Dataset[float64], error) {
	if len(ring) < 3 {
		return nil, fmt.Errorf("area: polygone d'au moins 3 sommets requis")
	}
	// Emprise du polygone.
	minx, miny := math.Inf(1), math.Inf(1)
	maxx, maxy := math.Inf(-1), math.Inf(-1)
	for _, pt := range ring {
		minx, maxx = math.Min(minx, pt[0]), math.Max(maxx, pt[0])
		miny, maxy = math.Min(miny, pt[1]), math.Max(maxy, pt[1])
	}
	bbox := [4]float64{minx, miny, maxx, maxy}
	ds, err := c.Query(QueryParams{Properties: p.SelectProperties, BBox: &bbox, Datetime: p.Datetime})
	if err != nil {
		return nil, err
	}
	return maskOutsidePolygon(ds, c.XDim, c.YDim, ring)
}

// maskOutsidePolygon met à NaN les cellules (x, y) hors du polygone, pour chaque
// variable, en tenant compte de dimensions supplémentaires (temps/niveau).
func maskOutsidePolygon(ds *xarray.Dataset[float64], xDim, yDim string, ring [][2]float64) (*xarray.Dataset[float64], error) {
	xs, err := ds.Coord(xDim)
	if err != nil {
		return nil, err
	}
	ys, err := ds.Coord(yDim)
	if err != nil {
		return nil, err
	}
	out := map[string]*xarray.DataArray[float64]{}
	for _, name := range ds.VarNames() {
		da, err := ds.Get(name)
		if err != nil {
			return nil, err
		}
		dims := da.Variable().Dims()
		shape := da.Shape()
		xi, yi := indexOf(dims, xDim), indexOf(dims, yDim)
		if xi < 0 || yi < 0 { // variable sans axes x/y : conservée telle quelle
			out[name] = da
			continue
		}
		strides := cStrides(shape)
		data := append([]float64(nil), da.Data()...)
		for flat := range data {
			ix := (flat / strides[xi]) % shape[xi]
			iy := (flat / strides[yi]) % shape[yi]
			if !pointInPolygon(xs[ix], ys[iy], ring) {
				data[flat] = math.NaN()
			}
		}
		coords := map[string][]float64{}
		for _, d := range dims {
			if cv, err := da.Coord(d); err == nil {
				coords[d] = cv
			}
		}
		nda, err := xarray.NewDataArray(dims, shape, data, coords, name)
		if err != nil {
			return nil, err
		}
		out[name] = nda
	}
	return xarray.NewDataset(out)
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func cStrides(shape []int) []int {
	st := make([]int, len(shape))
	acc := 1
	for i := len(shape) - 1; i >= 0; i-- {
		st[i] = acc
		acc *= shape[i]
	}
	return st
}

// parsePolygon analyse un WKT POLYGON((lon lat, lon lat, …)) et renvoie l'anneau
// extérieur. Accepte aussi un repli « lon,lat;lon,lat;… ».
func parsePolygon(s string) ([][2]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("coords manquant")
	}
	sep := byte(',')
	if strings.HasPrefix(strings.ToUpper(s), "POLYGON") {
		// POLYGON((...)) : on prend le premier anneau.
		i1 := strings.Index(s, "((")
		i2 := strings.Index(s, "))")
		if i1 < 0 || i2 < 0 || i2 < i1 {
			return nil, fmt.Errorf("WKT POLYGON mal formé")
		}
		s = s[i1+2 : i2]
	} else {
		sep = ';'
	}
	var ring [][2]float64
	for _, tok := range strings.Split(s, string(sep)) {
		f := strings.FieldsFunc(strings.TrimSpace(tok), func(r rune) bool { return r == ' ' || r == ',' })
		if len(f) != 2 {
			return nil, fmt.Errorf("sommet invalide %q", tok)
		}
		lon, e1 := strconv.ParseFloat(f[0], 64)
		lat, e2 := strconv.ParseFloat(f[1], 64)
		if e1 != nil || e2 != nil {
			return nil, fmt.Errorf("coordonnées non numériques dans %q", tok)
		}
		ring = append(ring, [2]float64{lon, lat})
	}
	return ring, nil
}
