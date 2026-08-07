package gocoverage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bmarty/xarray/geoapi"
)

// Server expose un provider via un sous-ensemble d'OGC API - Coverages / EDR.
type Server struct {
	prov Provider
}

// NewServer crée un serveur à partir d'un provider.
func NewServer(p Provider) *Server { return &Server{prov: p} }

// Handler renvoie le routeur HTTP.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.landing)
	mux.HandleFunc("/collections", s.collections)
	mux.HandleFunc("/collections/", s.collectionRoutes)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"title":       "gocoverage — OGC API Coverages (xarray-go)",
		"description": "Serveur de couvertures minimal adossé à xarray-go",
		"links": []map[string]string{
			{"rel": "data", "href": "/collections"},
		},
	})
}

func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]interface{}{"collections": s.prov.Collections()})
}

// collectionRoutes gère /collections/{id}, {id}/coverage, {id}/position.
func (s *Server) collectionRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/collections/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	c, ok := s.prov.Get(id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "collection inconnue: " + id})
		return
	}
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	switch action {
	case "", "/":
		writeJSON(w, 200, CollectionInfo{ID: c.ID, Title: c.Title, BBox: c.BBox()})
	case "coverage":
		s.coverage(w, r, c)
	case "position":
		s.position(w, r, c)
	default:
		writeJSON(w, 404, map[string]string{"error": "ressource inconnue: " + action})
	}
}

// coverage : GET .../coverage?bbox=minx,miny,maxx,maxy -> CoverageJSON.
func (s *Server) coverage(w http.ResponseWriter, r *http.Request, c *Collection) {
	da := c.Data
	if bboxStr := r.URL.Query().Get("bbox"); bboxStr != "" {
		bb, err := parseFloats(bboxStr, 4)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "bbox invalide: " + err.Error()})
			return
		}
		sub, err := geoapi.SubsetBBox(da, c.XDim, c.YDim, geoapi.BBox{MinX: bb[0], MinY: bb[1], MaxX: bb[2], MaxY: bb[3]})
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		da = sub
	}
	b, err := geoapi.ToCoverageJSON(da, c.Param, c.XDim, c.YDim)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/prs.coverage+json")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// position : GET .../position?coords=x,y -> valeur au point le plus proche (EDR).
func (s *Server) position(w http.ResponseWriter, r *http.Request, c *Collection) {
	coords := r.URL.Query().Get("coords")
	xy, err := parseFloats(coords, 2)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "coords invalide (attendu x,y): " + err.Error()})
		return
	}
	v, err := geoapi.Position(c.Data, c.XDim, c.YDim, xy[0], xy[1])
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"parameter": c.Param,
		"x":         xy[0],
		"y":         xy[1],
		"value":     v,
	})
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
