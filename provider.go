// Package gocoverage est un serveur OGC API - Coverages / EDR en Go, adossé à
// xarray-go comme couche de données (« provider »).
//
// Il reproduit les fonctions du provider xarray de pygeoapi (XarrayProvider) :
// description du domaine (domainset), du type de portée (rangetype) et requête
// (query) avec sélection de paramètres, sous-échantillonnage par axe nommé,
// emprise (bbox) et plage temporelle (datetime) — export en CoverageJSON.
package gocoverage

import (
	"fmt"

	"github.com/bmarty/xarray"
)

// Collection décrit une couverture servie : un Dataset[float64] (une ou
// plusieurs variables partageant une grille latitude × longitude, et
// éventuellement un axe temporel) et ses métadonnées.
type Collection struct {
	ID    string
	Title string
	XDim  string // dimension des longitudes (ex. "longitude")
	YDim  string // dimension des latitudes  (ex. "latitude")
	TDim  string // dimension temporelle (ex. "time"), "" si absente
	ZDim  string // dimension verticale (ex. "z"/"level"/"height"), "" si absente
	CRS   CRS    // système de coordonnées ; zéro-valeur = CRS84
	Data  *xarray.Dataset[float64]
}

// Params renvoie les noms des paramètres (variables) exposés par la collection.
func (c *Collection) Params() []string { return c.Data.VarNames() }

// BBox renvoie l'emprise [minX, minY, maxX, maxY] à partir des coordonnées.
func (c *Collection) BBox() [4]float64 {
	xs, _ := c.Data.Coord(c.XDim)
	ys, _ := c.Data.Coord(c.YDim)
	return [4]float64{minOf(xs), minOf(ys), maxOf(xs), maxOf(ys)}
}

// TimeExtent renvoie l'étendue temporelle [min, max] et true si la collection
// possède un axe temporel exploitable.
func (c *Collection) TimeExtent() ([2]float64, bool) {
	if c.TDim == "" {
		return [2]float64{}, false
	}
	ts, err := c.Data.Coord(c.TDim)
	if err != nil || len(ts) == 0 {
		return [2]float64{}, false
	}
	return [2]float64{minOf(ts), maxOf(ts)}, true
}

// CollectionInfo est la vue « métadonnées » d'une collection.
type CollectionInfo struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	BBox       [4]float64 `json:"bbox"`
	Parameters []string   `json:"parameters"`
}

// info construit la vue métadonnées d'une collection.
func (c *Collection) info() CollectionInfo {
	return CollectionInfo{ID: c.ID, Title: c.Title, BBox: c.BBox(), Parameters: c.Params()}
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

// Add enregistre une collection. Renvoie une erreur si la collection est
// invalide (dimensions X/Y/T absentes du Dataset).
func (p *MemProvider) Add(c *Collection) error {
	dims := c.Data.Dims()
	if _, ok := dims[c.XDim]; !ok {
		return fmt.Errorf("gocoverage: dimension X %q absente du Dataset", c.XDim)
	}
	if _, ok := dims[c.YDim]; !ok {
		return fmt.Errorf("gocoverage: dimension Y %q absente du Dataset", c.YDim)
	}
	if c.TDim != "" {
		if _, ok := dims[c.TDim]; !ok {
			return fmt.Errorf("gocoverage: dimension T %q absente du Dataset", c.TDim)
		}
	}
	if c.ZDim != "" {
		if _, ok := dims[c.ZDim]; !ok {
			return fmt.Errorf("gocoverage: dimension Z %q absente du Dataset", c.ZDim)
		}
	}
	if _, ok := p.colls[c.ID]; !ok {
		p.order = append(p.order, c.ID)
	}
	p.colls[c.ID] = c
	return nil
}

// Collections liste les métadonnées des collections.
func (p *MemProvider) Collections() []CollectionInfo {
	out := make([]CollectionInfo, 0, len(p.order))
	for _, id := range p.order {
		out = append(out, p.colls[id].info())
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
