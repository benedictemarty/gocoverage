package gocoverage

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// OGC API - Features (successeur moderne de WFS). Expose chaque maille de la
// grille (au premier pas des dimensions temps/niveau, comme la sortie GeoJSON)
// en **Feature Point** GeoJSON :
//   - /collections/{id}/items         : FeatureCollection paginée (bbox, limit,
//     offset, datetime, properties) ;
//   - /collections/{id}/items/{fid}   : une entité par identifiant.
//
// L'identifiant d'entité est l'indice plat de la maille dans la grille pleine
// résolution : fid = iy·nx + ix (stable, indépendant de bbox/pagination).

// defaultItemsLimit est le nombre d'entités par page par défaut.
const defaultItemsLimit = 10

// maxItemsLimit borne le nombre d'entités par page.
const maxItemsLimit = 10000

// ItemsParams rassemble les paramètres d'une requête /items.
type ItemsParams struct {
	BBox       *[4]float64
	Datetime   *[2]float64
	Properties []string
	Z          *float64
	Limit      int
	Offset     int
}

// cellReader prépare l'accès aux mailles de la grille pleine résolution : axes
// x/y, variables retenues et lecture d'une valeur (ix, iy) avec les dimensions
// temps/niveau fixées à l'indice choisi.
type cellReader struct {
	xs, ys []float64
	names  []string
	valAt  map[string]func(ix, iy int) float64
}

// newCellReader construit le lecteur de mailles pour les variables demandées
// (toutes si props est vide), en fixant le pas temporel (datetime) et le niveau
// vertical (z).
func (c *Collection) newCellReader(props []string, datetime *[2]float64, z *float64) (*cellReader, error) {
	g := c.grid()
	if g == nil {
		return nil, fmt.Errorf("données indisponibles")
	}
	xs, err := g.Coord(c.XDim)
	if err != nil || len(xs) == 0 {
		return nil, fmt.Errorf("coordonnée X %q illisible", c.XDim)
	}
	ys, err := g.Coord(c.YDim)
	if err != nil || len(ys) == 0 {
		return nil, fmt.Errorf("coordonnée Y %q illisible", c.YDim)
	}

	// Indices des pas temps/niveau (défaut : premier pas).
	ti, err := c.stepIndex(c.TDim, datetimeToPoint(datetime))
	if err != nil {
		return nil, err
	}
	zi, err := c.stepIndex(c.ZDim, z)
	if err != nil {
		return nil, err
	}

	want := map[string]bool{}
	for _, p := range props {
		want[p] = true
	}
	r := &cellReader{xs: xs, ys: ys, valAt: map[string]func(ix, iy int) float64{}}
	for _, name := range g.VarNames() {
		if len(want) > 0 && !want[name] {
			continue
		}
		da, err := g.Get(name)
		if err != nil {
			return nil, err
		}
		dims := da.Variable().Dims()
		xi, yi := indexOf(dims, c.XDim), indexOf(dims, c.YDim)
		if xi < 0 || yi < 0 {
			continue // variable sans grille x/y
		}
		strides := cStrides(da.Shape())
		data := da.Data()
		base := 0 // contribution des dimensions temps/niveau fixées
		for k, d := range dims {
			switch d {
			case c.TDim:
				base += ti * strides[k]
			case c.ZDim:
				base += zi * strides[k]
			}
		}
		sx, sy := strides[xi], strides[yi]
		r.names = append(r.names, name)
		r.valAt[name] = func(ix, iy int) float64 { return data[base+ix*sx+iy*sy] }
	}
	if len(r.names) == 0 {
		return nil, fmt.Errorf("aucune variable à exposer")
	}
	return r, nil
}

// stepIndex renvoie l'indice du pas le plus proche de la valeur demandée sur la
// dimension dim (temps/niveau). dim vide ou valeur nil → 0.
func (c *Collection) stepIndex(dim string, target *float64) (int, error) {
	if dim == "" || target == nil {
		return 0, nil
	}
	coord := c.coordOf(dim)
	if len(coord) == 0 {
		return 0, nil
	}
	i := nearestInRange(coord, *target)
	if i < 0 {
		return 0, fmt.Errorf("valeur %g hors de l'axe %q", *target, dim)
	}
	return i, nil
}

// datetimeToPoint réduit une plage temporelle à un point (borne basse) pour la
// sélection du pas ; nil → nil.
func datetimeToPoint(dt *[2]float64) *float64 {
	if dt == nil {
		return nil
	}
	v := dt[0]
	return &v
}

// cellFeature construit le Feature Point de la maille (ix, iy) ; renvoie false si
// la maille est entièrement vide (toutes valeurs NaN).
func (r *cellReader) cellFeature(ix, iy, nx int) (map[string]interface{}, bool) {
	props := map[string]interface{}{}
	any := false
	for _, name := range r.names {
		v := r.valAt[name](ix, iy)
		if math.IsNaN(v) {
			props[name] = nil
			continue
		}
		props[name] = v
		any = true
	}
	if !any {
		return nil, false
	}
	return map[string]interface{}{
		"type":       "Feature",
		"id":         iy*nx + ix,
		"geometry":   map[string]interface{}{"type": "Point", "coordinates": []float64{r.xs[ix], r.ys[iy]}},
		"properties": props,
	}, true
}

// inBBox indique si (x, y) est dans l'emprise (bornes incluses). nil = pas de filtre.
func inBBox(bb *[4]float64, x, y float64) bool {
	if bb == nil {
		return true
	}
	return x >= bb[0] && x <= bb[2] && y >= bb[1] && y <= bb[3]
}

// Items énumère les mailles en Features Point (ordre iy, ix), applique le filtre
// bbox et la pagination offset/limit. Renvoie les features de la page, le nombre
// total d'entités filtrées (numberMatched) et l'ordre stable des identifiants.
func (c *Collection) Items(p ItemsParams) (features []map[string]interface{}, numberMatched int, err error) {
	r, err := c.newCellReader(p.Properties, p.Datetime, p.Z)
	if err != nil {
		return nil, 0, err
	}
	nx, ny := len(r.xs), len(r.ys)
	features = []map[string]interface{}{}
	seen := 0 // entités non vides rencontrées (pour offset/limit)
	for iy := 0; iy < ny; iy++ {
		for ix := 0; ix < nx; ix++ {
			if !inBBox(p.BBox, r.xs[ix], r.ys[iy]) {
				continue
			}
			f, ok := r.cellFeature(ix, iy, nx)
			if !ok {
				continue
			}
			numberMatched++
			if seen >= p.Offset && len(features) < p.Limit {
				features = append(features, f)
			}
			seen++
		}
	}
	return features, numberMatched, nil
}

// Item renvoie l'entité (Feature) d'identifiant fid = iy·nx + ix, ou une erreur
// si l'identifiant est hors grille ou la maille vide.
func (c *Collection) Item(fid int, p ItemsParams) (map[string]interface{}, error) {
	r, err := c.newCellReader(p.Properties, p.Datetime, p.Z)
	if err != nil {
		return nil, err
	}
	nx, ny := len(r.xs), len(r.ys)
	if fid < 0 || fid >= nx*ny {
		return nil, fmt.Errorf("identifiant hors grille: %d", fid)
	}
	ix, iy := fid%nx, fid/nx
	f, ok := r.cellFeature(ix, iy, nx)
	if !ok {
		return nil, fmt.Errorf("maille vide: %d", fid)
	}
	return f, nil
}

// -----------------------------------------------------------------------------
// Handlers HTTP
// -----------------------------------------------------------------------------

// items : GET /collections/{id}/items — FeatureCollection paginée.
func (s *Server) items(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()
	p := ItemsParams{Properties: parseList(q.Get("properties")), Limit: defaultItemsLimit}

	if v := strings.TrimSpace(q.Get("bbox")); v != "" {
		bb, err := parseFloats(v, 4)
		if err != nil {
			writeErr(w, 400, "bbox invalide: "+err.Error())
			return
		}
		p.BBox = &[4]float64{bb[0], bb[1], bb[2], bb[3]}
	}
	dt, err := s.parseDatetimeParam(q.Get("datetime"), c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	p.Datetime = dt
	if p.Z, err = parseZ(q.Get("z")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if p.Limit, err = parseLimit(q.Get("limit")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if p.Offset, err = parseOffset(q.Get("offset")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	features, matched, err := c.Items(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	fc := map[string]interface{}{
		"type":           "FeatureCollection",
		"features":       features,
		"numberMatched":  matched,
		"numberReturned": len(features),
		"links":          itemsLinks(c.ID, q, p, matched),
	}
	b, _ := json.MarshalIndent(fc, "", "  ")
	w.Header().Set("Content-Type", "application/geo+json")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// itemByID : GET /collections/{id}/items/{featureId}.
func (s *Server) itemByID(w http.ResponseWriter, r *http.Request, c *Collection, fidStr string) {
	fid, err := strconv.Atoi(fidStr)
	if err != nil {
		writeErr(w, 400, "identifiant d'entité invalide: "+fidStr)
		return
	}
	q := r.URL.Query()
	dt, err := s.parseDatetimeParam(q.Get("datetime"), c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	z, err := parseZ(q.Get("z"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	f, err := c.Item(fid, ItemsParams{Properties: parseList(q.Get("properties")), Datetime: dt, Z: z})
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	f["links"] = []map[string]string{
		{"rel": "self", "href": "/collections/" + c.ID + "/items/" + fidStr, "type": "application/geo+json"},
		{"rel": "collection", "href": "/collections/" + c.ID, "type": "application/json"},
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	w.Header().Set("Content-Type", "application/geo+json")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// itemsLinks construit les liens self/next/prev de la page courante en
// conservant les filtres (bbox, datetime, properties, limit).
func itemsLinks(id string, q url.Values, p ItemsParams, matched int) []map[string]string {
	base := "/collections/" + id + "/items"
	keep := url.Values{}
	for _, k := range []string{"bbox", "datetime", "properties", "z", "limit"} {
		if v := q.Get(k); v != "" {
			keep.Set(k, v)
		}
	}
	href := func(offset int) string {
		v := url.Values{}
		for k, vals := range keep {
			v[k] = vals
		}
		if offset > 0 {
			v.Set("offset", strconv.Itoa(offset))
		}
		if enc := v.Encode(); enc != "" {
			return base + "?" + enc
		}
		return base
	}
	links := []map[string]string{
		{"rel": "self", "href": href(p.Offset), "type": "application/geo+json"},
		{"rel": "collection", "href": "/collections/" + id, "type": "application/json"},
	}
	if p.Offset+p.Limit < matched {
		links = append(links, map[string]string{"rel": "next", "href": href(p.Offset + p.Limit), "type": "application/geo+json"})
	}
	if p.Offset > 0 {
		prev := p.Offset - p.Limit
		if prev < 0 {
			prev = 0
		}
		links = append(links, map[string]string{"rel": "prev", "href": href(prev), "type": "application/geo+json"})
	}
	return links
}

// parseLimit lit le paramètre limit (défaut defaultItemsLimit, borné à maxItemsLimit).
func parseLimit(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultItemsLimit, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("limit invalide %q (entier ≥ 1 attendu)", s)
	}
	if n > maxItemsLimit {
		n = maxItemsLimit
	}
	return n, nil
}

// parseOffset lit le paramètre offset (défaut 0).
func parseOffset(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("offset invalide %q (entier ≥ 0 attendu)", s)
	}
	return n, nil
}
