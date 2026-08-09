package gocoverage

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// OGC API - Maps (successeur moderne de WMS, style OpenAPI). Rend une variable de
// couverture (grille lat/lon) en image matricielle pour une emprise (bbox) et une
// taille de sortie (width × height) données.
//
// Le rendu échantillonne la grille source au plus proche voisin (pas de
// reprojection : cohérent avec le reste de gocoverage, cf. Collection.CRS) et
// applique une palette de couleurs entre deux bornes (auto min/max, ou fixées via
// colorscalerange). Les valeurs manquantes (NaN) et les pixels hors emprise
// source sont transparents.

// maxMapPixels borne le nombre de pixels d'une image rendue (garde-fou mémoire,
// pendant de maxCoverageCells pour la sortie image).
const maxMapPixels = 8_000_000

// defaultMapWidth est la largeur de sortie par défaut quand width/height sont
// absents. La hauteur en découle par conservation du rapport d'emprise.
const defaultMapWidth = 512

// MapOptions rassemble les paramètres d'un rendu carte.
type MapOptions struct {
	Field    string      // variable à rendre ("" = première variable)
	BBox     [4]float64  // emprise de sortie [minX, minY, maxX, maxY]
	Width    int         // largeur en pixels
	Height   int         // hauteur en pixels
	VMin     *float64    // borne basse de la rampe (nil = min des données)
	VMax     *float64    // borne haute de la rampe (nil = max des données)
	Palette  string      // nom de palette (viridis|grayscale)
	Datetime *[2]float64 // sélection temporelle, nil = tous (premier pas rendu)
	Z        *float64    // niveau vertical, nil = premier
}

// RenderMap rend une variable de la collection en image NRGBA de Width × Height
// couvrant BBox. Réutilise Query pour l'élagage (bbox/datetime/champ), puis
// échantillonne au plus proche voisin sur la grille de sortie.
func (c *Collection) RenderMap(o MapOptions) (*image.NRGBA, error) {
	if o.Width < 1 || o.Height < 1 {
		return nil, fmt.Errorf("dimensions image invalides (%d×%d)", o.Width, o.Height)
	}
	if o.Width*o.Height > maxMapPixels {
		return nil, fmt.Errorf("image trop volumineuse (%d pixels > %d) : réduisez width/height", o.Width*o.Height, maxMapPixels)
	}

	// Emprise demandée hors de la couverture : image entièrement transparente
	// (comportement WMS pour une zone sans donnée), plutôt qu'une erreur.
	cb := c.BBox()
	if o.BBox[0] >= cb[2] || o.BBox[2] <= cb[0] || o.BBox[1] >= cb[3] || o.BBox[3] <= cb[1] {
		return image.NewNRGBA(image.Rect(0, 0, o.Width, o.Height)), nil
	}

	// Élagage : sélection du champ, de l'emprise et de la plage temporelle.
	qp := QueryParams{BBox: &o.BBox, Datetime: o.Datetime}
	if o.Field != "" {
		qp.Properties = []string{o.Field}
	}
	if c.ZDim != "" && o.Z != nil {
		qp.Subsets = []Subset{{Axis: c.ZDim, Lo: *o.Z, Hi: *o.Z, Point: true}}
	}
	ds, err := c.Query(qp)
	if err != nil {
		return nil, err
	}

	name := o.Field
	if name == "" {
		names := ds.VarNames()
		if len(names) == 0 {
			return nil, fmt.Errorf("aucune variable à rendre")
		}
		name = names[0]
	}
	da, err := ds.Get(name)
	if err != nil {
		return nil, fmt.Errorf("variable %q: %w", name, err)
	}
	dims := da.Variable().Dims()
	xi, yi := indexOf(dims, c.XDim), indexOf(dims, c.YDim)
	if xi < 0 || yi < 0 {
		return nil, fmt.Errorf("variable %q sans axe x/y", name)
	}
	xs, err := ds.Coord(c.XDim)
	if err != nil || len(xs) == 0 {
		return nil, fmt.Errorf("coordonnée X %q: %w", c.XDim, err)
	}
	ys, err := ds.Coord(c.YDim)
	if err != nil || len(ys) == 0 {
		return nil, fmt.Errorf("coordonnée Y %q: %w", c.YDim, err)
	}

	strides := cStrides(da.Shape())
	data := da.Data()
	// Valeur à la maille (ix, iy), autres dimensions (temps/niveau) fixées à 0
	// — premier pas, comme la sortie GeoJSON.
	valAt := func(ix, iy int) float64 {
		return data[ix*strides[xi]+iy*strides[yi]]
	}

	// Bornes de la rampe : fournies, ou min/max des données (NaN ignorés).
	vmin, vmax := math.Inf(1), math.Inf(-1)
	if o.VMin != nil && o.VMax != nil {
		vmin, vmax = *o.VMin, *o.VMax
	} else {
		for _, v := range data {
			if math.IsNaN(v) {
				continue
			}
			if v < vmin {
				vmin = v
			}
			if v > vmax {
				vmax = v
			}
		}
		if o.VMin != nil {
			vmin = *o.VMin
		}
		if o.VMax != nil {
			vmax = *o.VMax
		}
	}
	if math.IsInf(vmin, 0) || math.IsInf(vmax, 0) { // toutes valeurs manquantes
		vmin, vmax = 0, 1
	}
	span := vmax - vmin
	if span <= 0 {
		span = 1 // évite la division par zéro (champ constant)
	}
	pal := paletteByName(o.Palette)

	// Pré-calcul des indices source par colonne (lon) et par ligne (lat) : évite
	// une recherche par pixel. -1 = hors emprise de la grille source.
	minX, maxX := o.BBox[0], o.BBox[2]
	minY, maxY := o.BBox[1], o.BBox[3]
	colIx := make([]int, o.Width)
	for px := 0; px < o.Width; px++ {
		lon := minX + (float64(px)+0.5)/float64(o.Width)*(maxX-minX)
		colIx[px] = nearestInRange(xs, lon)
	}
	rowIy := make([]int, o.Height)
	for py := 0; py < o.Height; py++ {
		// L'axe image descend (py=0 en haut = latitude max).
		lat := maxY - (float64(py)+0.5)/float64(o.Height)*(maxY-minY)
		rowIy[py] = nearestInRange(ys, lat)
	}

	img := image.NewNRGBA(image.Rect(0, 0, o.Width, o.Height))
	for py := 0; py < o.Height; py++ {
		iy := rowIy[py]
		for px := 0; px < o.Width; px++ {
			ix := colIx[px]
			if ix < 0 || iy < 0 {
				continue // transparent (hors grille)
			}
			v := valAt(ix, iy)
			if math.IsNaN(v) {
				continue // transparent (donnée manquante)
			}
			t := (v - vmin) / span
			img.SetNRGBA(px, py, pal(t))
		}
	}
	return img, nil
}

// nearestInRange renvoie l'indice de la coordonnée la plus proche de target dans
// coords (trié croissant ou décroissant), ou -1 si target sort de l'intervalle
// [min, max] des coordonnées. Recherche linéaire (les axes de sortie sont
// pré-calculés une seule fois par colonne/ligne).
func nearestInRange(coords []float64, target float64) int {
	lo, hi := coords[0], coords[len(coords)-1]
	if lo > hi {
		lo, hi = hi, lo
	}
	if target < lo || target > hi {
		return -1
	}
	best, bestD := 0, math.Abs(coords[0]-target)
	for i := 1; i < len(coords); i++ {
		if d := math.Abs(coords[i] - target); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// palette associe une position normalisée t∈[0,1] (bornée) à une couleur opaque.
type palette func(t float64) color.NRGBA

// paletteByName renvoie la palette nommée (défaut : viridis).
func paletteByName(name string) palette {
	switch name {
	case "grayscale", "gray", "greyscale", "grey":
		return grayPalette
	default:
		return viridisPalette
	}
}

// grayPalette : rampe noir → blanc.
func grayPalette(t float64) color.NRGBA {
	t = clamp01(t)
	g := uint8(t*255 + 0.5)
	return color.NRGBA{R: g, G: g, B: g, A: 255}
}

// viridisStops : points de contrôle de la palette viridis (matplotlib), de la
// valeur basse (violet foncé) à la valeur haute (jaune). Interpolation linéaire.
var viridisStops = [][3]float64{
	{68, 1, 84}, {72, 40, 120}, {62, 74, 137}, {49, 104, 142},
	{38, 130, 142}, {31, 158, 137}, {53, 183, 121}, {109, 205, 89},
	{180, 222, 44}, {253, 231, 37},
}

// viridisPalette interpole la rampe viridis pour t∈[0,1].
func viridisPalette(t float64) color.NRGBA {
	t = clamp01(t)
	n := len(viridisStops)
	x := t * float64(n-1)
	i := int(x)
	if i >= n-1 {
		s := viridisStops[n-1]
		return color.NRGBA{R: uint8(s[0]), G: uint8(s[1]), B: uint8(s[2]), A: 255}
	}
	f := x - float64(i)
	a, b := viridisStops[i], viridisStops[i+1]
	lerp := func(u, v float64) uint8 { return uint8(u+(v-u)*f + 0.5) }
	return color.NRGBA{R: lerp(a[0], b[0]), G: lerp(a[1], b[1]), B: lerp(a[2], b[2]), A: 255}
}

func clamp01(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}
