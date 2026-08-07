// Package gocoverage est un serveur OGC API - Coverages minimal en Go, adossé à
// xarray-go comme couche de données (« provider »).
//
// Il illustre le rôle de xarray-go dans un service géospatial : lecture,
// sous-échantillonnage (bbox/point), export CoverageJSON — le reste (HTTP,
// endpoints OGC) étant assuré ici, ou par un serveur dédié comme gogeoapi.
package gocoverage

import (
	"github.com/bmarty/xarray"
)

// Collection décrit une couverture servie : un DataArray[float64] 2D
// (latitude × longitude) et ses métadonnées.
type Collection struct {
	ID    string
	Title string
	Param string // nom du paramètre exposé
	XDim  string // dimension des longitudes (ex. "longitude")
	YDim  string // dimension des latitudes (ex. "latitude")
	Data  *xarray.DataArray[float64]
}

// BBox renvoie l'emprise [minX, minY, maxX, maxY] à partir des coordonnées.
func (c *Collection) BBox() [4]float64 {
	xs, _ := c.Data.Coord(c.XDim)
	ys, _ := c.Data.Coord(c.YDim)
	return [4]float64{minOf(xs), minOf(ys), maxOf(xs), maxOf(ys)}
}

// CollectionInfo est la vue « métadonnées » d'une collection.
type CollectionInfo struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	BBox  [4]float64 `json:"bbox"`
}

// Provider fournit l'accès aux collections de couvertures.
type Provider interface {
	Collections() []CollectionInfo
	Get(id string) (*Collection, bool)
}

// MemProvider est un Provider en mémoire (collections chargées via xarray-go).
type MemProvider struct {
	colls map[string]*Collection
	order []string
}

// NewMemProvider crée un provider mémoire vide.
func NewMemProvider() *MemProvider {
	return &MemProvider{colls: map[string]*Collection{}}
}

// Add enregistre une collection.
func (p *MemProvider) Add(c *Collection) {
	if _, ok := p.colls[c.ID]; !ok {
		p.order = append(p.order, c.ID)
	}
	p.colls[c.ID] = c
}

// Collections liste les métadonnées des collections.
func (p *MemProvider) Collections() []CollectionInfo {
	out := make([]CollectionInfo, 0, len(p.order))
	for _, id := range p.order {
		c := p.colls[id]
		out = append(out, CollectionInfo{ID: c.ID, Title: c.Title, BBox: c.BBox()})
	}
	return out
}

// Get renvoie une collection par identifiant.
func (p *MemProvider) Get(id string) (*Collection, bool) {
	c, ok := p.colls[id]
	return c, ok
}

func minOf(s []float64) float64 {
	m := s[0]
	for _, x := range s[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

func maxOf(s []float64) float64 {
	m := s[0]
	for _, x := range s[1:] {
		if x > m {
			m = x
		}
	}
	return m
}
