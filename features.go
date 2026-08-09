package gocoverage

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/benedictemarty/xarray"
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

// gridSource fournit l'accès aux valeurs d'une région lue (élaguée par chunks ou
// grille complète) et le lien vers les indices absolus de la grille pleine
// résolution (pour un identifiant d'entité stable).
type gridSource struct {
	xs, ys       []float64 // coordonnées de la région réellement lue
	baseX, baseY int       // offset absolu de xs[0]/ys[0] dans les axes complets
	nxFull       int       // largeur de la grille pleine résolution (pour l'id)
	names        []string
	valAt        map[string]func(lx, ly int) float64 // indices locaux à la région
}

// readRegion lit la région couvrant bbox — lecture élaguée par chunks si un
// Window est disponible (ne matérialise pas toute la grille), sinon grille
// complète — avec les dimensions temps/niveau fixées au pas le plus proche de
// datetime/z. props restreint les variables (vide = toutes).
func (c *Collection) readRegion(bbox *[4]float64, datetime *[2]float64, z *float64, props []string) (*gridSource, error) {
	fullXs, fullYs := c.coordOf(c.XDim), c.coordOf(c.YDim)
	if len(fullXs) == 0 || len(fullYs) == 0 {
		return nil, fmt.Errorf("coordonnées X/Y illisibles")
	}

	// Source : lecture élaguée si Window disponible, sinon grille complète.
	var ds *xarray.Dataset[float64]
	if c.Data == nil && c.Window != nil {
		win, err := c.Window(WindowSel{BBox: bbox, TRange: datetime})
		if err != nil {
			return nil, fmt.Errorf("lecture élaguée: %w", err)
		}
		ds = win
	} else {
		ds = c.grid()
	}
	if ds == nil {
		return nil, fmt.Errorf("données indisponibles")
	}

	xs, err := ds.Coord(c.XDim)
	if err != nil || len(xs) == 0 {
		return nil, fmt.Errorf("coordonnée X %q illisible", c.XDim)
	}
	ys, err := ds.Coord(c.YDim)
	if err != nil || len(ys) == 0 {
		return nil, fmt.Errorf("coordonnée Y %q illisible", c.YDim)
	}

	// Indices des pas temps/niveau, calculés sur les axes réellement lus.
	ti := nearestStep(ds, c.TDim, datetimeToPoint(datetime))
	zi := nearestStep(ds, c.ZDim, z)

	want := map[string]bool{}
	for _, p := range props {
		want[p] = true
	}
	src := &gridSource{
		xs: xs, ys: ys,
		baseX: baseIndex(fullXs, xs[0]), baseY: baseIndex(fullYs, ys[0]),
		nxFull: len(fullXs),
		valAt:  map[string]func(lx, ly int) float64{},
	}
	for _, name := range ds.VarNames() {
		if len(want) > 0 && !want[name] {
			continue
		}
		da, err := ds.Get(name)
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
		src.names = append(src.names, name)
		src.valAt[name] = func(lx, ly int) float64 { return data[base+lx*sx+ly*sy] }
	}
	if len(src.names) == 0 {
		return nil, fmt.Errorf("aucune variable à exposer")
	}
	return src, nil
}

// nearestStep renvoie l'indice du pas le plus proche de target sur la dimension
// dim de ds ; dim vide, target nil ou hors axe → 0 (premier pas).
func nearestStep(ds *xarray.Dataset[float64], dim string, target *float64) int {
	if dim == "" || target == nil {
		return 0
	}
	coord, err := ds.Coord(dim)
	if err != nil || len(coord) == 0 {
		return 0
	}
	if i := nearestInRange(coord, *target); i >= 0 {
		return i
	}
	return 0
}

// baseIndex renvoie l'indice absolu de la valeur v dans l'axe complet (début de
// la région lue). v provient d'un sous-ensemble contigu de full ; 0 par défaut.
func baseIndex(full []float64, v float64) int {
	if i := nearestInRange(full, v); i >= 0 {
		return i
	}
	return 0
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

// feature construit le Feature Point de la maille locale (lx, ly) ; renvoie false
// si la maille est entièrement vide (toutes valeurs NaN). L'identifiant est
// l'indice plat absolu (grille pleine résolution).
func (src *gridSource) feature(lx, ly int) (map[string]interface{}, bool) {
	props := map[string]interface{}{}
	any := false
	for _, name := range src.names {
		v := src.valAt[name](lx, ly)
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
	ix, iy := src.baseX+lx, src.baseY+ly
	return map[string]interface{}{
		"type":       "Feature",
		"id":         iy*src.nxFull + ix,
		"geometry":   map[string]interface{}{"type": "Point", "coordinates": []float64{src.xs[lx], src.ys[ly]}},
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
// bbox et la pagination offset/limit. Renvoie les features de la page et le
// nombre total d'entités filtrées (numberMatched). Lecture élaguée par chunks
// quand un Window est disponible : la région lue peut dépasser bbox (chunks
// entiers), d'où le filtre inBBox conservé.
func (c *Collection) Items(p ItemsParams) (features []map[string]interface{}, numberMatched int, err error) {
	src, err := c.readRegion(p.BBox, p.Datetime, p.Z, p.Properties)
	if err != nil {
		return nil, 0, err
	}
	features = []map[string]interface{}{}
	seen := 0 // entités non vides rencontrées (pour offset/limit)
	for ly := 0; ly < len(src.ys); ly++ {
		for lx := 0; lx < len(src.xs); lx++ {
			if !inBBox(p.BBox, src.xs[lx], src.ys[ly]) {
				continue
			}
			f, ok := src.feature(lx, ly)
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
// si l'identifiant est hors grille ou la maille vide. La maille visée est lue de
// façon élaguée (bbox ponctuelle → seul le chunk concerné est chargé).
func (c *Collection) Item(fid int, p ItemsParams) (map[string]interface{}, error) {
	fullXs, fullYs := c.coordOf(c.XDim), c.coordOf(c.YDim)
	nx, ny := len(fullXs), len(fullYs)
	if nx == 0 || ny == 0 {
		return nil, fmt.Errorf("coordonnées indisponibles")
	}
	if fid < 0 || fid >= nx*ny {
		return nil, fmt.Errorf("identifiant hors grille: %d", fid)
	}
	ix, iy := fid%nx, fid/nx
	tiny := [4]float64{fullXs[ix], fullYs[iy], fullXs[ix], fullYs[iy]}
	src, err := c.readRegion(&tiny, p.Datetime, p.Z, p.Properties)
	if err != nil {
		return nil, err
	}
	lx := nearestInRange(src.xs, fullXs[ix])
	ly := nearestInRange(src.ys, fullYs[iy])
	if lx < 0 || ly < 0 {
		return nil, fmt.Errorf("maille introuvable: %d", fid)
	}
	f, ok := src.feature(lx, ly)
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
