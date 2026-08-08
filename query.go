package gocoverage

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/benedictemarty/xarray"
)

// isoLayouts : formats de date/heure ISO 8601 acceptés en entrée pour les axes
// temporels (le temps interne est en secondes depuis l'epoch Unix).
var isoLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
	"2006-01-02",
}

// parseTimeOrFloat interprète une borne d'axe : un nombre (epoch/valeur brute)
// ou une date ISO 8601 convertie en secondes depuis l'epoch Unix. Permet
// d'exprimer datetime/subset temporel en ISO 8601 comme pygeoapi.
func parseTimeOrFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v, nil
	}
	for _, l := range isoLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return float64(t.UTC().Unix()), nil
		}
	}
	return 0, fmt.Errorf("valeur temporelle invalide %q (nombre ou date ISO 8601 attendu)", s)
}

// QueryParams rassemble les paramètres d'une requête « query » (à la manière de
// pygeoapi XarrayProvider.query) : sélection de champs, emprise, sous-ensembles
// par axe nommé et plage temporelle.
type QueryParams struct {
	Properties []string    // noms des paramètres à conserver (vide = tous)
	BBox       *[4]float64 // [minX, minY, maxX, maxY], nil si absent
	Subsets    []Subset    // sous-ensembles par axe nommé
	Datetime   *[2]float64 // plage temporelle [lo, hi], nil si absente
}

// Subset est un sous-ensemble sur un axe : Lo..Hi, ou point unique si Lo==Hi
// avec Point=true (sélection au plus proche).
type Subset struct {
	Axis   string
	Lo, Hi float64
	Point  bool
}

// Query applique les paramètres de requête à la collection et renvoie le
// Dataset résultant restreint aux champs demandés.
func (c *Collection) Query(q QueryParams) (*xarray.Dataset[float64], error) {
	// Exclusivités reproduites de pygeoapi (évaluées sur le bbox d'origine).
	subsetDims := map[string]bool{}
	for _, s := range q.Subsets {
		if dim, err := c.resolveAxis(s.Axis); err == nil {
			subsetDims[dim] = true
		}
	}
	if q.BBox != nil && (subsetDims[c.XDim] || subsetDims[c.YDim]) {
		return nil, fmt.Errorf("bbox et subset de coordonnées sont exclusifs")
	}
	if q.Datetime != nil && c.TDim != "" && subsetDims[c.TDim] {
		return nil, fmt.Errorf("datetime et subset temporel sont exclusifs")
	}

	// Base : lecture élaguée par chunks si un Window est disponible et qu'une
	// emprise est demandée (ne lit que les chunks nécessaires, sans matérialiser
	// toute la grille) ; sinon la grille complète. On évite d'appeler grid() dans
	// le cas élagué (sinon la grille complète serait chargée inutilement).
	var ds *xarray.Dataset[float64]
	if c.Window != nil && q.BBox != nil {
		win, err := c.Window(q.BBox)
		if err != nil {
			return nil, fmt.Errorf("lecture élaguée: %w", err)
		}
		ds = win
		q.BBox = nil // emprise déjà appliquée par la lecture élaguée
	} else {
		ds = c.grid()
	}

	// 1. Sélection des champs (properties / range-subset).
	if len(q.Properties) > 0 {
		var err error
		ds, err = selectVars(ds, q.Properties)
		if err != nil {
			return nil, err
		}
	}

	// 2. Emprise spatiale (bbox) sur X et Y. La sélection en X gère le passage
	//    de l'antiméridien (minX > maxX ⇒ union [minX,180] ∪ [-180,maxX]).
	if q.BBox != nil {
		bb := *q.BBox
		var err error
		if ds, err = selBBoxX(ds, c.XDim, bb[0], bb[2]); err != nil {
			return nil, fmt.Errorf("bbox X: %w", err)
		}
		if ds, err = dsSelRange(ds, c.YDim, bb[1], bb[3]); err != nil {
			return nil, fmt.Errorf("bbox Y: %w", err)
		}
	}

	// 3. Sous-ensembles par axe nommé (subset=Lat(43:45),Long(0:2)).
	for _, s := range q.Subsets {
		dim, err := c.resolveAxis(s.Axis)
		if err != nil {
			return nil, err
		}
		if s.Point {
			ds, err = dsSelNearest(ds, dim, s.Lo)
		} else {
			ds, err = dsSelRange(ds, dim, s.Lo, s.Hi)
		}
		if err != nil {
			return nil, fmt.Errorf("subset %s: %w", s.Axis, err)
		}
	}

	// 4. Plage temporelle (datetime).
	if q.Datetime != nil {
		if c.TDim == "" {
			return nil, fmt.Errorf("la collection %q n'a pas d'axe temporel", c.ID)
		}
		dt := *q.Datetime
		var err error
		if ds, err = dsSelRange(ds, c.TDim, dt[0], dt[1]); err != nil {
			return nil, fmt.Errorf("datetime: %w", err)
		}
	}

	return ds, nil
}

// resolveAxis fait correspondre un nom d'axe de requête à une dimension du
// Dataset. Accepte le nom exact de la dimension (insensible à la casse) et les
// alias usuels Lat/Long/Lon/Time.
func (c *Collection) resolveAxis(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "lat", "latitude", "y":
		return c.YDim, nil
	case "long", "lon", "longitude", "x":
		return c.XDim, nil
	case "time", "t", "datetime":
		if c.TDim == "" {
			return "", fmt.Errorf("axe %q: la collection n'a pas d'axe temporel", name)
		}
		return c.TDim, nil
	case "z", "level", "height", "depth", "elevation", "vertical":
		if c.ZDim == "" {
			return "", fmt.Errorf("axe %q: la collection n'a pas d'axe vertical", name)
		}
		return c.ZDim, nil
	}
	for dim := range c.grid().Dims() {
		if strings.ToLower(dim) == n {
			return dim, nil
		}
	}
	return "", fmt.Errorf("axe inconnu: %q", name)
}

// selectVars restreint le Dataset aux variables demandées. Un nom demandé qui
// n'existe pas est une erreur (remarque G : un paramètre inconnu ne doit pas
// être ignoré silencieusement, mais produire une 400).
func selectVars(ds *xarray.Dataset[float64], names []string) (*xarray.Dataset[float64], error) {
	exists := map[string]bool{}
	for _, v := range ds.VarNames() {
		exists[v] = true
	}
	keep := map[string]bool{}
	var unknown []string
	for _, n := range names {
		if !exists[n] {
			unknown = append(unknown, n)
			continue
		}
		keep[n] = true
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("paramètre(s) inconnu(s): %s", strings.Join(unknown, ", "))
	}
	var drop []string
	for _, v := range ds.VarNames() {
		if !keep[v] {
			drop = append(drop, v)
		}
	}
	kept := len(ds.VarNames()) - len(drop)
	if kept == 0 {
		return nil, fmt.Errorf("aucun paramètre valide dans %v", names)
	}
	if len(drop) == 0 {
		return ds, nil
	}
	return ds.DropVars(drop...)
}

// selBBoxX sélectionne l'emprise en longitude en gérant l'antiméridien : si
// minX ≤ maxX, plage simple ; sinon union de [minX, 180] et [-180, maxX]
// concaténée le long de l'axe X (remarque N). Suppose une convention -180..180.
func selBBoxX(ds *xarray.Dataset[float64], xDim string, minX, maxX float64) (*xarray.Dataset[float64], error) {
	if minX <= maxX {
		return dsSelRange(ds, xDim, minX, maxX)
	}
	left, err := dsSelRange(ds, xDim, minX, 180)
	if err != nil {
		return nil, err
	}
	right, err := dsSelRange(ds, xDim, -180, maxX)
	if err != nil {
		return nil, err
	}
	ln, rn := coordLen(left, xDim), coordLen(right, xDim)
	switch {
	case ln == 0:
		return right, nil
	case rn == 0:
		return left, nil
	default:
		return dsConcat(left, right, xDim)
	}
}

// coordLen renvoie la longueur de la coordonnée dim (0 si absente).
func coordLen(ds *xarray.Dataset[float64], dim string) int {
	if cv, err := ds.Coord(dim); err == nil {
		return len(cv)
	}
	return 0
}

// dsConcat concatène deux Datasets le long de dim (variables partagées), les
// autres variables étant reprises de a.
func dsConcat(a, b *xarray.Dataset[float64], dim string) (*xarray.Dataset[float64], error) {
	vars := map[string]*xarray.DataArray[float64]{}
	for _, name := range a.VarNames() {
		da, err := a.Get(name)
		if err != nil {
			return nil, err
		}
		db, err := b.Get(name)
		if err != nil || !da.HasDim(dim) {
			vars[name] = da
			continue
		}
		m, err := xarray.Concat([]*xarray.DataArray[float64]{da, db}, dim)
		if err != nil {
			return nil, err
		}
		vars[name] = m
	}
	return xarray.NewDataset(vars)
}

// dsSelRange applique SelRange à chaque variable possédant la dimension dim et
// reconstruit un Dataset (Dataset n'expose pas SelRange directement).
func dsSelRange(ds *xarray.Dataset[float64], dim string, lo, hi float64) (*xarray.Dataset[float64], error) {
	return dsMap(ds, dim, func(da *xarray.DataArray[float64]) (*xarray.DataArray[float64], error) {
		return da.SelRange(dim, lo, hi)
	})
}

// dsSelNearest sélectionne au plus proche sur la dimension dim, en CONSERVANT
// la dimension (taille 1) et sa coordonnée — indispensable pour construire un
// CoverageJSON PointSeries. S'appuie sur xarray.SelNearestKeep, qui reproduit
// sel(dim=[val], method="nearest") de xarray (SelNearest scalaire, lui,
// supprimerait la dimension comme sel(dim=val)).
func dsSelNearest(ds *xarray.Dataset[float64], dim string, val float64) (*xarray.Dataset[float64], error) {
	return dsMap(ds, dim, func(da *xarray.DataArray[float64]) (*xarray.DataArray[float64], error) {
		return da.SelNearestKeep(dim, val)
	})
}

// dsMap applique fn aux variables qui possèdent la dimension dim, laisse les
// autres intactes, et reconstruit un Dataset.
func dsMap(ds *xarray.Dataset[float64], dim string, fn func(*xarray.DataArray[float64]) (*xarray.DataArray[float64], error)) (*xarray.Dataset[float64], error) {
	vars := map[string]*xarray.DataArray[float64]{}
	for _, name := range ds.VarNames() {
		da, err := ds.Get(name)
		if err != nil {
			return nil, err
		}
		if da.HasDim(dim) {
			da, err = fn(da)
			if err != nil {
				return nil, err
			}
		}
		vars[name] = da
	}
	return xarray.NewDataset(vars)
}

// parseSubsets analyse la chaîne « subset » d'OGC API : une liste séparée par
// des virgules d'expressions Axe(lo:hi) ou Axe(val). Ex. "Lat(43:45),Long(0:2)".
func parseSubsets(s string) ([]Subset, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []Subset
	// Découpe sur les virgules situées hors des parenthèses.
	for _, expr := range splitTopLevel(s, ',') {
		expr = strings.TrimSpace(expr)
		open := strings.IndexByte(expr, '(')
		if open < 0 || !strings.HasSuffix(expr, ")") {
			return nil, fmt.Errorf("subset invalide %q (attendu Axe(lo:hi))", expr)
		}
		axis := strings.TrimSpace(expr[:open])
		inner := expr[open+1 : len(expr)-1]
		sub := Subset{Axis: axis}
		// Séparateur de plage lo:hi. Les dates seules ISO (« 2020-01-01 ») sont
		// acceptées ; pour un datetime complet (avec heure « hh:mm:ss »), utiliser
		// plutôt le paramètre datetime, dont le séparateur « / » n'est pas ambigu.
		if i := strings.IndexByte(inner, ':'); i >= 0 {
			lo, err := parseTimeOrFloat(inner[:i])
			if err != nil {
				return nil, fmt.Errorf("subset %q: borne basse: %w", axis, err)
			}
			hi, err := parseTimeOrFloat(inner[i+1:])
			if err != nil {
				return nil, fmt.Errorf("subset %q: borne haute: %w", axis, err)
			}
			sub.Lo, sub.Hi = lo, hi
		} else {
			v, err := parseTimeOrFloat(inner)
			if err != nil {
				return nil, fmt.Errorf("subset %q: valeur: %w", axis, err)
			}
			sub.Lo, sub.Hi, sub.Point = v, v, true
		}
		out = append(out, sub)
	}
	return out, nil
}

// parseDatetime analyse le paramètre datetime d'OGC API : "lo/hi", ou instant
// unique "v" (interprété comme [v, v]). Les bornes ouvertes ".." sont acceptées.
func parseDatetime(s string, ext [2]float64) (*[2]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	lo, hi := ext[0], ext[1]
	if i := strings.IndexByte(s, '/'); i >= 0 {
		a, b := strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
		if a != "" && a != ".." {
			v, err := parseTimeOrFloat(a)
			if err != nil {
				return nil, fmt.Errorf("datetime borne basse: %w", err)
			}
			lo = v
		}
		if b != "" && b != ".." {
			v, err := parseTimeOrFloat(b)
			if err != nil {
				return nil, fmt.Errorf("datetime borne haute: %w", err)
			}
			hi = v
		}
	} else {
		v, err := parseTimeOrFloat(s)
		if err != nil {
			return nil, fmt.Errorf("datetime: %w", err)
		}
		lo, hi = v, v
	}
	return &[2]float64{lo, hi}, nil
}

// splitTopLevel découpe s sur le séparateur sep en ignorant ceux situés à
// l'intérieur de parenthèses.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
