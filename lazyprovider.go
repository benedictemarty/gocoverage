package gocoverage

import (
	"fmt"
	"sync"
)

// LazyFileProvider est un Provider à résidence mémoire bornée. Les collections
// sont déclarées par leur source (fichier Zarr/netCDF) sans être chargées ; le
// Dataset n'est lu qu'à la demande (Get), et un cache LRU limite le nombre de
// collections résidentes simultanément. Le nombre de collections servies est
// ainsi découplé de la mémoire consommée.
//
// Portée & limite honnêtes (remarque P) : le gain porte sur la RÉSIDENCE — on ne
// garde plus tous les Datasets en RAM en permanence (contrairement à MemProvider)
// —, PAS sur le coût unitaire d'une requête : chaque chargement lit le fichier
// entier (xarray-go n'expose pas de sélection paresseuse par label ; seules la
// lecture par blocs et les réductions hors-mémoire le sont). Une collection
// évincée reste valide pour une requête en cours (le handler en garde une
// référence) ; l'éviction ne fait que relâcher la référence du cache.
type LazyFileProvider struct {
	mu          sync.Mutex
	sources     map[string]func() (*Collection, error)
	order       []string
	meta        map[string]CollectionInfo // métadonnées mémorisées (léger, permanent)
	cache       map[string]*Collection    // Datasets résidents (lourd, évinçable)
	lru         []string                  // ordre d'utilisation (plus ancien en tête)
	maxResident int
}

// NewLazyFileProvider crée un provider chargeant à la demande, en gardant au plus
// maxResident collections résidentes (≥ 1).
func NewLazyFileProvider(maxResident int) *LazyFileProvider {
	if maxResident < 1 {
		maxResident = 1
	}
	return &LazyFileProvider{
		sources:     map[string]func() (*Collection, error){},
		meta:        map[string]CollectionInfo{},
		cache:       map[string]*Collection{},
		maxResident: maxResident,
	}
}

// AddSource enregistre une collection chargée paresseusement via loader.
func (p *LazyFileProvider) AddSource(id string, loader func() (*Collection, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.sources[id]; !ok {
		p.order = append(p.order, id)
	}
	p.sources[id] = loader
}

// AddZarr déclare une collection Zarr (chargée à la demande).
func (p *LazyFileProvider) AddZarr(dir, id, title, xDim, yDim, tDim string) {
	p.AddSource(id, func() (*Collection, error) { return LoadZarr(dir, id, title, xDim, yDim, tDim) })
}

// AddNetCDF déclare une collection netCDF (chargée à la demande).
func (p *LazyFileProvider) AddNetCDF(path, id, title, xDim, yDim, tDim string) {
	p.AddSource(id, func() (*Collection, error) { return LoadNetCDF(path, id, title, xDim, yDim, tDim) })
}

// load renvoie la collection (cache ou chargement) et met à jour la LRU. Doit
// être appelé sous verrou.
func (p *LazyFileProvider) load(id string) (*Collection, error) {
	if c, ok := p.cache[id]; ok {
		p.touch(id)
		return c, nil
	}
	loader, ok := p.sources[id]
	if !ok {
		return nil, fmt.Errorf("collection inconnue: %s", id)
	}
	c, err := loader()
	if err != nil {
		return nil, err
	}
	p.cache[id] = c
	p.lru = append(p.lru, id)
	p.meta[id] = c.info()
	p.evict()
	return c, nil
}

// touch remonte id en fin de LRU (le plus récemment utilisé).
func (p *LazyFileProvider) touch(id string) {
	for i, x := range p.lru {
		if x == id {
			p.lru = append(p.lru[:i], p.lru[i+1:]...)
			break
		}
	}
	p.lru = append(p.lru, id)
}

// evict libère les collections les moins récemment utilisées au-delà de la borne
// (les métadonnées sont conservées).
func (p *LazyFileProvider) evict() {
	for len(p.lru) > p.maxResident {
		old := p.lru[0]
		p.lru = p.lru[1:]
		delete(p.cache, old)
	}
}

// Get charge la collection à la demande (interface Provider).
func (p *LazyFileProvider) Get(id string) (*Collection, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, err := p.load(id)
	if err != nil {
		return nil, false
	}
	return c, true
}

// Collections liste les métadonnées. La première fois, chaque source est chargée
// une fois pour capturer bbox/paramètres (puis mémorisés) ; ensuite, la liste ne
// recharge rien.
func (p *LazyFileProvider) Collections() []CollectionInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]CollectionInfo, 0, len(p.order))
	for _, id := range p.order {
		if _, ok := p.meta[id]; !ok {
			if _, err := p.load(id); err != nil {
				continue
			}
		}
		out = append(out, p.meta[id])
	}
	return out
}

// Resident renvoie le nombre de collections actuellement résidentes (Datasets en
// mémoire) — utile pour l'observabilité et les tests.
func (p *LazyFileProvider) Resident() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.cache)
}
