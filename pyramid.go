package gocoverage

import (
	"fmt"
	"path/filepath"

	"github.com/benedictemarty/xarray"
)

// Aperçus multi-résolution (pyramide Zarr). Une pyramide écrite par
// xarray.WritePyramidZarr contient des niveaux « 0 » (pleine résolution) …
// « n-1 » (le plus grossier), chacun un groupe Zarr. Pour une requête à grande
// emprise, on sert un niveau grossier (moins de cellules) plutôt que la pleine
// résolution — combiné à l'élagage par chunks au sein du niveau choisi.
//
// Chaque niveau est lu par un ZarrWindowReader ; le choix du niveau vise un
// budget de cellules pour l'emprise demandée. Les métadonnées (coords/schéma)
// proviennent du niveau 0, sans matérialiser les données.

// pyramidTargetCells : budget de cellules visé pour la fenêtre servie ; au-delà,
// on descend d'un niveau (plus grossier). Bien en-deçà du garde-fou de taille.
const pyramidTargetCells = 500_000

// pyramidReader sert une pyramide : choisit le niveau selon l'emprise puis
// délègue la lecture élaguée au ZarrWindowReader de ce niveau.
type pyramidReader struct {
	levels []*ZarrWindowReader // du plus fin (0) au plus grossier
	target int
}

// chooseLevel renvoie le niveau le plus fin dont la fenêtre (emprise) tient dans
// le budget de cellules ; à défaut, le plus grossier.
func (p *pyramidReader) chooseLevel(bbox *[4]float64) int {
	for L, r := range p.levels {
		xv, yv := r.Coords()
		c0, c1 := 0, len(xv)
		r0, r1 := 0, len(yv)
		if bbox != nil {
			c0, c1 = indexRange(xv, bbox[0], bbox[2])
			r0, r1 = indexRange(yv, bbox[1], bbox[3])
		}
		if (c1-c0)*(r1-r0) <= p.target {
			return L
		}
	}
	return len(p.levels) - 1
}

// ReadWindow choisit le niveau adapté à l'emprise puis lit la fenêtre élaguée.
func (p *pyramidReader) ReadWindow(sel WindowSel) (*xarray.Dataset[float64], error) {
	return p.levels[p.chooseLevel(sel.BBox)].ReadWindow(sel)
}

// LoadPyramidZarr construit une Collection servie par une pyramide multi-échelles
// (xarray.WritePyramidZarr). Grande emprise → niveau grossier ; petite emprise →
// niveau fin, avec élagage par chunks dans chaque niveau. Métadonnées (bbox,
// champs) exposées à la résolution native (niveau 0), sans charger les données.
func LoadPyramidZarr(dir, id, title, xDim, yDim string) (*Collection, error) {
	levels, err := xarray.PyramidLevels(dir)
	if err != nil {
		return nil, fmt.Errorf("pyramide %q: %w", dir, err)
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("pyramide %q sans niveau", dir)
	}
	pr := &pyramidReader{target: pyramidTargetCells}
	for _, lvl := range levels {
		var rr *ZarrWindowReader
		if xDim != "" && yDim != "" {
			rr, err = OpenZarrWindow(filepath.Join(dir, lvl.Path), xDim, yDim)
		} else {
			rr, xDim, yDim, err = openZarrWindowAuto(filepath.Join(dir, lvl.Path))
		}
		if err != nil {
			return nil, fmt.Errorf("niveau %q: %w", lvl.Path, err)
		}
		pr.levels = append(pr.levels, rr)
	}
	base := pr.levels[0]
	c := &Collection{ID: id, Title: title, XDim: xDim, YDim: yDim, Window: pr.ReadWindow}
	c.coordHint = base.coords
	c.fieldHint = base.schemaFields()
	c.dimsHint = base.dimSizes()
	return c, nil
}

// openZarrWindowAuto ouvre un groupe Zarr en détectant les axes lon/lat.
func openZarrWindowAuto(dir string) (*ZarrWindowReader, string, string, error) {
	for _, x := range []string{"longitude", "lon", "x"} {
		for _, y := range []string{"latitude", "lat", "y"} {
			if r, err := OpenZarrWindow(dir, x, y); err == nil {
				return r, x, y, nil
			}
		}
	}
	return nil, "", "", fmt.Errorf("axes lon/lat introuvables dans %q", dir)
}
