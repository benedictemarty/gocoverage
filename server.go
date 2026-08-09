package gocoverage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/benedictemarty/xarray"
)

// Server expose un provider via un sous-ensemble d'OGC API - Coverages / EDR,
// reproduisant les fonctions du provider xarray de pygeoapi.
type Server struct {
	prov Provider
}

// NewServer crée un serveur à partir d'un provider.
func NewServer(p Provider) *Server { return &Server{prov: p} }

// Handler renvoie le routeur HTTP.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.landing)
	mux.HandleFunc("/conformance", s.conformance)
	mux.HandleFunc("/api", s.openapi)
	mux.HandleFunc("/collections", s.collections)
	mux.HandleFunc("/collections/", s.collectionRoutes)
	mux.HandleFunc("/tileMatrixSets", s.tileMatrixSets)
	mux.HandleFunc("/tileMatrixSets/", s.tileMatrixSetByID)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr émet une réponse d'erreur au format d'exception OGC API
// (`{code, description}`), tout en conservant `error` pour rétro-compatibilité.
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{
		"code":        http.StatusText(code),
		"description": msg,
		"error":       msg,
	})
}

func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"title":       "gocoverage — OGC API Coverages / EDR (xarray-go)",
		"description": "Serveur de couvertures reproduisant le provider xarray de pygeoapi",
		"links": []map[string]string{
			{"rel": "data", "href": "/collections"},
			{"rel": "conformance", "href": "/conformance"},
			{"rel": "service-desc", "href": "/api"},
		},
	})
}

func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]interface{}{"collections": s.prov.Collections()})
}

// collectionRoutes gère /collections/{id}, {id}/coverage, {id}/position, {id}/cube.
func (s *Server) collectionRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/collections/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	c, ok := s.prov.Get(id)
	if !ok {
		writeErr(w, 404, "collection inconnue: "+id)
		return
	}
	action := ""
	if len(parts) == 2 {
		action = strings.Trim(parts[1], "/")
	}
	s.dispatchAction(w, r, c, action)
}

// dispatchAction route une action (chemin après /collections/{id}/) vers le bon
// handler. Factorisé pour être réutilisé par les instances (mêmes actions sur une
// sous-collection).
func (s *Server) dispatchAction(w http.ResponseWriter, r *http.Request, c *Collection, action string) {
	// Rejet d'un CRS de requête non supporté (remarque B : pas de reprojection ;
	// un crs/bbox-crs inconnu doit produire une erreur, non être ignoré).
	if msg := checkRequestCRS(c, r.URL.Query()); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	switch action {
	case "":
		s.describe(w, r, c)
	case "coverage":
		s.coverage(w, r, c)
	case "coverage/domainset":
		s.domainset(w, r, c)
	case "coverage/rangetype":
		s.rangetype(w, r, c)
	case "position":
		s.position(w, r, c)
	case "cube":
		s.cube(w, r, c)
	case "trajectory":
		s.trajectory(w, r, c)
	case "area":
		s.area(w, r, c)
	case "corridor":
		s.corridor(w, r, c)
	case "radius":
		s.radius(w, r, c)
	case "locations":
		s.locations(w, r, c)
	case "items":
		s.items(w, r, c)
	case "queryables":
		s.queryables(w, r, c)
	case "map":
		s.mapRender(w, r, c)
	case "instances":
		s.instances(w, r, c)
	default:
		// OGC API - Tiles : /map/tiles[/{tms}/{z}/{y}/{x}].
		if action == "map/tiles" {
			s.mapTiles(w, r, c, "")
			return
		}
		if rest, ok := strings.CutPrefix(action, "map/tiles/"); ok {
			s.mapTiles(w, r, c, rest)
			return
		}
		if rest, ok := strings.CutPrefix(action, "items/"); ok {
			s.itemByID(w, r, c, rest)
			return
		}
		if rest, ok := strings.CutPrefix(action, "locations/"); ok {
			s.locationByID(w, r, c, rest)
			return
		}
		if rest, ok := strings.CutPrefix(action, "instances/"); ok {
			s.instanceRoute(w, r, c, rest)
			return
		}
		writeErr(w, 404, "ressource inconnue: "+action)
	}
}

// instances : liste des instances (versions temporelles) d'une collection.
func (s *Server) instances(w http.ResponseWriter, r *http.Request, c *Collection) {
	writeJSON(w, 200, map[string]interface{}{"instances": c.InstancesInfo()})
}

// instanceRoute route /instances/{instId}[/action] vers la sous-collection.
func (s *Server) instanceRoute(w http.ResponseWriter, r *http.Request, c *Collection, rest string) {
	parts := strings.SplitN(rest, "/", 2)
	inst, ok := c.InstanceByID(parts[0])
	if !ok {
		writeErr(w, 404, "instance inconnue: "+parts[0])
		return
	}
	sub := ""
	if len(parts) == 2 {
		sub = strings.Trim(parts[1], "/")
	}
	// Une instance n'a pas d'instances imbriquées.
	if sub == "instances" || strings.HasPrefix(sub, "instances/") {
		writeErr(w, 404, "instances imbriquées non prises en charge")
		return
	}
	s.dispatchAction(w, r, inst, sub)
}

// locations : liste des points nommés de la collection (FeatureCollection GeoJSON).
func (s *Server) locations(w http.ResponseWriter, r *http.Request, c *Collection) {
	b, err := c.LocationsGeoJSON()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/geo+json")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// locationByID : données au point nommé locID (EDR locations/{locationId}).
func (s *Server) locationByID(w http.ResponseWriter, r *http.Request, c *Collection, locID string) {
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
	b, err := c.LocationCoverageJSON(locID, EDRParams{
		SelectProperties: parseList(q.Get("parameter-name")),
		Datetime:         dt,
		Z:                z,
	})
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/prs.coverage+json")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// radius : requête EDR radius → CoverageJSON (grille masquée par le disque).
// Paramètres : coords=POINT(lon lat), within=<rayon>, within-units=deg|km|m
// (métrique : distance réelle en mètres). f=covjson (défaut) | geojson | netcdf | zarr.
func (s *Server) radius(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()
	lon, lat, err := parsePoint(q.Get("coords"))
	if err != nil {
		writeErr(w, 400, "coords invalide: "+err.Error())
		return
	}
	within, err := strconv.ParseFloat(strings.TrimSpace(q.Get("within")), 64)
	if err != nil || within <= 0 {
		writeErr(w, 400, "within invalide (rayon > 0 attendu)")
		return
	}
	dt, err := s.parseDatetimeParam(q.Get("datetime"), c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ds, err := c.Radius(lon, lat, within, q.Get("within-units"), EDRParams{SelectProperties: parseList(q.Get("parameter-name")), Datetime: dt})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.writeCoverage(w, r, c, ds)
}

// corridor : requête EDR corridor → CoverageJSON (grille masquée par le tube).
// Paramètres : coords=LINESTRING(…), corridor-width=<largeur totale, degrés>.
func (s *Server) corridor(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()
	line, err := parseLineString(q.Get("coords"))
	if err != nil {
		writeErr(w, 400, "coords invalide: "+err.Error())
		return
	}
	width, err := strconv.ParseFloat(strings.TrimSpace(q.Get("corridor-width")), 64)
	if err != nil || width <= 0 {
		writeErr(w, 400, "corridor-width invalide (largeur > 0 en degrés attendue)")
		return
	}
	dt, err := s.parseDatetimeParam(q.Get("datetime"), c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ds, err := c.Corridor(line, width/2, q.Get("corridor-width-units"), EDRParams{SelectProperties: parseList(q.Get("parameter-name")), Datetime: dt})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.writeCoverage(w, r, c, ds)
}

// area : requête EDR area → CoverageJSON (grille masquée par le polygone).
// Paramètre coords : WKT POLYGON((lon lat, …)). Options : datetime, parameter-name.
func (s *Server) area(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()
	rings, err := parsePolygonRings(q.Get("coords"))
	if err != nil {
		writeErr(w, 400, "coords invalide: "+err.Error())
		return
	}
	dt, err := s.parseDatetimeParam(q.Get("datetime"), c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ds, err := c.AreaRings(rings, EDRParams{SelectProperties: parseList(q.Get("parameter-name")), Datetime: dt})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.writeCoverage(w, r, c, ds)
}

// trajectory : requête EDR trajectory → CoverageJSON (domaine Trajectory).
// Paramètre coords : WKT LINESTRING(lon lat, lon lat, …). Options : datetime, z,
// parameter-name.
func (s *Server) trajectory(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()
	pts, err := parseLineString(q.Get("coords"))
	if err != nil {
		writeErr(w, 400, "coords invalide: "+err.Error())
		return
	}
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
	b, err := c.TrajectoryCoverageJSON(pts, EDRParams{
		SelectProperties: parseList(q.Get("parameter-name")),
		Datetime:         dt,
		Z:                z,
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/prs.coverage+json")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// parseLineString analyse une géométrie WKT LINESTRING(lon lat, lon lat, …) en
// une liste de points {lon, lat}. Accepte aussi une simple liste
// « lon,lat;lon,lat;… » comme repli.
func parseLineString(s string) ([][2]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("coords manquant")
	}
	sep := byte(',') // WKT : points séparés par des virgules (lon lat, lon lat)
	if strings.HasPrefix(strings.ToUpper(s), "LINESTRING") {
		open := strings.IndexByte(s, '(')
		if open < 0 || !strings.HasSuffix(s, ")") {
			return nil, fmt.Errorf("WKT LINESTRING mal formé")
		}
		s = s[open+1 : len(s)-1]
	} else {
		sep = ';' // repli : points séparés par des « ; » (lon,lat;lon,lat)
	}
	var pts [][2]float64
	for _, tok := range strings.Split(s, string(sep)) {
		f := strings.FieldsFunc(strings.TrimSpace(tok), func(r rune) bool { return r == ' ' || r == ',' })
		if len(f) != 2 {
			return nil, fmt.Errorf("point invalide %q (attendu « lon lat »)", tok)
		}
		lon, err1 := strconv.ParseFloat(f[0], 64)
		lat, err2 := strconv.ParseFloat(f[1], 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("coordonnées non numériques dans %q", tok)
		}
		pts = append(pts, [2]float64{lon, lat})
	}
	return pts, nil
}

// describe renvoie la description conforme d'une collection : métadonnées OGC
// Common (extent, crs), découverte EDR (parameter_names, data_queries), champs
// (get_fields) et propriétés de couverture (coverage_properties).
func (s *Server) describe(w http.ResponseWriter, r *http.Request, c *Collection) {
	writeJSON(w, 200, c.collectionDoc())
}

// coverage : requête OGC Coverages (query) → CoverageJSON.
// Paramètres : properties, subset, bbox, datetime.
func (s *Server) coverage(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()
	params := QueryParams{Properties: parseList(q.Get("properties"))}

	if bboxStr := q.Get("bbox"); bboxStr != "" {
		bb, err := parseFloats(bboxStr, 4)
		if err != nil {
			writeErr(w, 400, "bbox invalide: "+err.Error())
			return
		}
		params.BBox = &[4]float64{bb[0], bb[1], bb[2], bb[3]}
	}
	if subStr := q.Get("subset"); subStr != "" {
		subs, err := parseSubsets(subStr)
		if err != nil {
			writeErr(w, 400, "subset invalide: "+err.Error())
			return
		}
		params.Subsets = subs
	}
	dt, err := s.parseDatetimeParam(q.Get("datetime"), c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	params.Datetime = dt

	ds, err := c.Query(params)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// Scaling (classe « scaling » de Coverages) : sous-échantillonnage par
	// moyennage de blocs (scale-factor global, scale-axes par axe).
	factors, err := c.parseScaling(q.Get("scale-factor"), q.Get("scale-axes"), q.Get("scale-size"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if factors != nil {
		if ds, err = applyScaling(ds, factors); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	}
	s.writeCoverage(w, r, c, ds)
}

// position : requête EDR position → CoverageJSON (PointSeries).
// Paramètres : coords=x,y, parameter-name, datetime.
func (s *Server) position(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()
	xy, err := parseFloats(q.Get("coords"), 2)
	if err != nil {
		writeErr(w, 400, "coords invalide (attendu x,y): "+err.Error())
		return
	}
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
	ds, err := c.Position(xy[0], xy[1], EDRParams{
		SelectProperties: parseList(q.Get("parameter-name")),
		Datetime:         dt,
		Z:                z,
		Bilinear:         isBilinear(q.Get("interpolation")),
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.writeCoverage(w, r, c, ds)
}

// isBilinear reconnaît les valeurs du paramètre EDR interpolation demandant une
// interpolation bilinéaire (défaut : plus proche voisin).
func isBilinear(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "bilinear", "linear":
		return true
	default:
		return false
	}
}

// cube : requête EDR cube → CoverageJSON.
// Paramètres : bbox=minx,miny,maxx,maxy, parameter-name, datetime.
func (s *Server) cube(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()
	bb, err := parseFloats(q.Get("bbox"), 4)
	if err != nil {
		writeErr(w, 400, "bbox invalide (attendu minx,miny,maxx,maxy): "+err.Error())
		return
	}
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
	ds, err := c.Cube([4]float64{bb[0], bb[1], bb[2], bb[3]}, EDRParams{
		SelectProperties: parseList(q.Get("parameter-name")),
		Datetime:         dt,
		Z:                z,
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.writeCoverage(w, r, c, ds)
}

// parseZ analyse le paramètre EDR z (un niveau vertical unique). Vide → nil.
func parseZ(s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("z invalide (niveau unique attendu): %w", err)
	}
	return &v, nil
}

// writeCoverage sérialise un Dataset dans le format demandé par le paramètre
// `f` — pendant de query(format_=…) de pygeoapi. Défaut : CoverageJSON.
// Formats : json/covjson (CoverageJSON), netcdf/nc (natif netCDF).
// maxCoverageCells borne la taille d'une réponse sérialisée en mémoire (remarque
// Q : sans garde-fou, un GET sur une large emprise charge/duplique tout et peut
// provoquer un OOM). Au-delà, le client doit restreindre bbox/subset/datetime.
const maxCoverageCells = 8_000_000

func (s *Server) writeCoverage(w http.ResponseWriter, r *http.Request, c *Collection, ds *xarray.Dataset[float64]) {
	// Négociation : `?f=` explicite non reconnu, ou `Accept` explicite non
	// satisfiable → 406/400 plutôt qu'un défaut surprenant (remarque H).
	if r.URL.Query().Get("f") == "" && acceptExplicitUnsupported(r.Header.Get("Accept")) {
		writeErr(w, 406, "aucun format supporté dans Accept (json|geojson|netcdf|zarr)")
		return
	}
	// Garde-fou de taille (remarque Q).
	if n := datasetCells(ds); n > maxCoverageCells {
		writeErr(w, 400, fmt.Sprintf("réponse trop volumineuse (%d cellules > %d) : restreignez bbox/subset/datetime ou utilisez scale-factor", n, maxCoverageCells))
		return
	}
	switch negotiateFormat(r) {
	case "", "json", "covjson", "coveragejson":
		b, err := c.CoverageJSON(ds)
		if err != nil {
			// Erreur corrigible par le client (niveau vertical à sélectionner) → 400.
			code := 500
			if errors.Is(err, ErrSelectLevel) {
				code = 400
			}
			writeErr(w, code, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/prs.coverage+json")
		w.WriteHeader(200)
		_, _ = w.Write(b)
	case "netcdf", "nc":
		var buf bytes.Buffer
		if err := ds.WriteNetCDF(&buf); err != nil {
			writeErr(w, 500, "export netCDF: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/x-netcdf")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+c.ID+".nc\"")
		w.WriteHeader(200)
		_, _ = w.Write(buf.Bytes())
	case "zarr":
		b, err := zarrZip(ds, c.ID)
		if err != nil {
			writeErr(w, 500, "export zarr: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+c.ID+".zarr.zip\"")
		w.WriteHeader(200)
		_, _ = w.Write(b)
	case "geojson", "geoj", "geo+json":
		b, err := c.GeoJSON(ds)
		if err != nil {
			writeErr(w, 400, "export geojson: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/geo+json")
		w.WriteHeader(200)
		_, _ = w.Write(b)
	default:
		writeErr(w, 400, "format inconnu: "+r.URL.Query().Get("f")+" (json|geojson|netcdf|zarr)")
	}
}

// parseDatetimeParam analyse le paramètre datetime en s'appuyant sur l'étendue
// temporelle de la collection pour les bornes ouvertes.
func (s *Server) parseDatetimeParam(v string, c *Collection) (*[2]float64, error) {
	if v == "" {
		return nil, nil
	}
	ext, ok := c.TimeExtent()
	if !ok {
		return nil, fmt.Errorf("la collection %q n'a pas d'axe temporel", c.ID)
	}
	return parseDatetime(v, ext)
}

// parseList découpe une liste de valeurs séparées par des virgules.
func parseList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseFloats(s string, n int) ([]float64, error) {
	parts := strings.Split(s, ",")
	if len(parts) != n {
		return nil, fmt.Errorf("%d valeurs attendues, %d fournies", n, len(parts))
	}
	out := make([]float64, n)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}
