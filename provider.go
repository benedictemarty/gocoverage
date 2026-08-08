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
	"strconv"
	"time"

	"github.com/benedictemarty/xarray"
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

	// Locations : points nommés prédéfinis (ex. aéroports, stations) exposés par
	// la requête EDR « locations ». Clé = identifiant (ex. code OACI), valeur =
	// {lon, lat}. Optionnel.
	Locations []NamedLocation

	// Instances : versions temporelles de la collection (ex. runs de modèle
	// successifs 00Z/06Z/12Z), chacune une (sous-)collection à part entière.
	// Exposées par la requête EDR « instances ». Optionnel.
	Instances []*Collection

	// TimeEpoch force l'interprétation de l'axe temporel : true = secondes depuis
	// l'epoch Unix (sortie ISO 8601), false = valeurs numériques brutes. nil =
	// détection automatique par heuristique (évite les seuils magiques quand le
	// producteur connaît la nature du temps). Optionnel.
	TimeEpoch *bool

	// Window, si défini, fournit une lecture élaguée (ne lit que les chunks
	// recouvrant l'emprise et/ou la plage temporelle demandées) — cf.
	// LoadChunkedZarr. Quand il est présent, une requête à emprise/datetime n'a
	// pas besoin de charger toute la grille (Data peut être nil et n'est
	// matérialisé que pour les accès globaux / métadonnées). Optionnel.
	Window func(sel WindowSel) (*xarray.Dataset[float64], error)

	// TExtent, si défini, fournit l'étendue temporelle [min, max] sans charger la
	// grille (renseigné par LoadChunkedZarr depuis l'axe temporel décodé) — pour
	// que les requêtes datetime restent paresseuses. Optionnel.
	TExtent *[2]float64
}

// WindowSel décrit la fenêtre d'une lecture élaguée : emprise spatiale et/ou
// plage temporelle (nil = axe entier). Extensible sans casser la signature.
type WindowSel struct {
	BBox   *[4]float64 // [minX, minY, maxX, maxY]
	TRange *[2]float64 // [t0, t1] (valeurs de l'axe temporel décodé)
}

// grid renvoie la grille complète : Data si présent, sinon la matérialise via
// Window(nil) (lecture complète, mise en cache). Utilisé par les accès sans
// emprise (métadonnées) ; les requêtes à emprise passent par la lecture élaguée.
func (c *Collection) grid() *xarray.Dataset[float64] {
	if c.Data == nil && c.Window != nil {
		if ds, err := c.Window(WindowSel{}); err == nil { // fenêtre vide = grille entière
			c.Data = ds
		}
	}
	return c.Data
}

// timeIsEpoch décide si les valeurs temporelles ts sont des secondes epoch :
// override explicite (TimeEpoch) sinon heuristique allEpochSeconds.
func (c *Collection) timeIsEpoch(ts []float64) bool {
	if c.TimeEpoch != nil {
		return *c.TimeEpoch
	}
	return allEpochSeconds(ts)
}

// InstanceInfo est la vue « métadonnées » d'une instance.
type InstanceInfo struct {
	ID    string     `json:"id"`
	Title string     `json:"title,omitempty"`
	BBox  [4]float64 `json:"bbox"`
}

// InstancesInfo liste les métadonnées des instances.
func (c *Collection) InstancesInfo() []InstanceInfo {
	out := make([]InstanceInfo, 0, len(c.Instances))
	for _, in := range c.Instances {
		out = append(out, InstanceInfo{ID: in.ID, Title: in.Title, BBox: in.BBox()})
	}
	return out
}

// InstanceByID retrouve une instance par identifiant.
func (c *Collection) InstanceByID(id string) (*Collection, bool) {
	for _, in := range c.Instances {
		if in.ID == id {
			return in, true
		}
	}
	return nil, false
}

// InstancesFromTime dérive les instances depuis l'axe temporel : une
// (sous-)collection par pas de temps (chacune un instantané sans axe temps),
// identifiée par sa date ISO 8601 quand le temps est en secondes epoch, sinon
// par sa valeur numérique (remarque I : ne plus imposer un découpage manuel).
// Le résultat peut être affecté à c.Instances.
func (c *Collection) InstancesFromTime() ([]*Collection, error) {
	if c.TDim == "" {
		return nil, fmt.Errorf("gocoverage: la collection %q n'a pas d'axe temporel", c.ID)
	}
	ts, err := c.grid().Coord(c.TDim)
	if err != nil || len(ts) == 0 {
		return nil, fmt.Errorf("gocoverage: axe temporel %q illisible", c.TDim)
	}
	iso := c.timeIsEpoch(ts)
	out := make([]*Collection, 0, len(ts))
	for i, t := range ts {
		sub, err := dsMap(c.grid(), c.TDim, func(da *xarray.DataArray[float64]) (*xarray.DataArray[float64], error) {
			return da.Isel(c.TDim, i)
		})
		if err != nil {
			return nil, fmt.Errorf("instance %d: %w", i, err)
		}
		id := strconv.FormatFloat(t, 'g', -1, 64)
		title := id
		if iso {
			id = time.Unix(int64(t), 0).UTC().Format(time.RFC3339)
			title = id
		}
		out = append(out, &Collection{
			ID: id, Title: title,
			XDim: c.XDim, YDim: c.YDim, ZDim: c.ZDim, CRS: c.CRS,
			Data: sub,
		})
	}
	return out, nil
}

// NamedLocation est un point nommé prédéfini d'une collection (EDR locations).
type NamedLocation struct {
	ID       string
	Name     string
	Lon, Lat float64
}

// Params renvoie les noms des paramètres (variables) exposés par la collection.
func (c *Collection) Params() []string { return c.grid().VarNames() }

// BBox renvoie l'emprise [minX, minY, maxX, maxY] à partir des coordonnées.
func (c *Collection) BBox() [4]float64 {
	xs, _ := c.grid().Coord(c.XDim)
	ys, _ := c.grid().Coord(c.YDim)
	return [4]float64{minOf(xs), minOf(ys), maxOf(xs), maxOf(ys)}
}

// TimeExtent renvoie l'étendue temporelle [min, max] et true si la collection
// possède un axe temporel exploitable.
func (c *Collection) TimeExtent() ([2]float64, bool) {
	if c.TDim == "" {
		return [2]float64{}, false
	}
	if c.TExtent != nil { // indice sans chargement (collections élaguées)
		return *c.TExtent, true
	}
	ts, err := c.grid().Coord(c.TDim)
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
	// Collection à lecture élaguée (Data nil + Window) : ses axes ont été validés
	// à l'ouverture du store ; on ne force pas le chargement complet ici pour
	// préserver la laziness (élagage par chunks).
	if c.Data == nil && c.Window != nil {
		if _, ok := p.colls[c.ID]; !ok {
			p.order = append(p.order, c.ID)
		}
		p.colls[c.ID] = c
		return nil
	}
	dims := c.grid().Dims()
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
