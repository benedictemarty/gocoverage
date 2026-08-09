package gocoverage

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
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
	Palette  string      // nom de palette (viridis|plasma|magma|inferno|turbo|coolwarm|grayscale)
	Datetime *[2]float64 // sélection temporelle, nil = tous (premier pas rendu)
	Z        *float64    // niveau vertical, nil = premier
	Bilinear bool        // interpolation bilinéaire (défaut : plus proche voisin)
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

	sf, err := c.sampledField(o)
	if err != nil {
		return nil, err
	}

	// Emprise linéaire en lon/lat (CRS84) : position géographique par pixel.
	minX, maxX := o.BBox[0], o.BBox[2]
	minY, maxY := o.BBox[1], o.BBox[3]
	lonAt := func(px int) float64 { return minX + (float64(px)+0.5)/float64(o.Width)*(maxX-minX) }
	// L'axe image descend (py=0 en haut = latitude max).
	latAt := func(py int) float64 { return maxY - (float64(py)+0.5)/float64(o.Height)*(maxY-minY) }

	if o.Bilinear {
		return sf.fillBilinear(o.Width, o.Height, lonAt, latAt), nil
	}
	// Pré-calcul des indices source par colonne/ligne (-1 = hors grille source).
	colIx := make([]int, o.Width)
	for px := 0; px < o.Width; px++ {
		colIx[px] = nearestInRange(sf.xs, lonAt(px))
	}
	rowIy := make([]int, o.Height)
	for py := 0; py < o.Height; py++ {
		rowIy[py] = nearestInRange(sf.ys, latAt(py))
	}
	return sf.fill(o.Width, o.Height, colIx, rowIy), nil
}

// sampledField prépare l'échantillonnage d'une variable : élagage (bbox/datetime/
// z/champ via Query), accès valAt(ix,iy), coordonnées x/y et bornes de rampe +
// palette. Partagé par le rendu carte (/map) et le rendu de tuiles.
type sampledField struct {
	xs, ys     []float64
	valAt      func(ix, iy int) float64
	vmin, vmax float64
	pal        palette
}

func (c *Collection) sampledField(o MapOptions) (*sampledField, error) {
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
	return &sampledField{xs: xs, ys: ys, valAt: valAt, vmin: vmin, vmax: vmax, pal: paletteByName(o.Palette)}, nil
}

// fill colorie une image width×height : colIx[px]/rowIy[py] donnent la maille
// source (-1 = hors grille → transparent), NaN → transparent.
func (sf *sampledField) fill(width, height int, colIx, rowIy []int) *image.NRGBA {
	span := sf.vmax - sf.vmin
	if span <= 0 {
		span = 1 // évite la division par zéro (champ constant)
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for py := 0; py < height; py++ {
		iy := rowIy[py]
		if iy < 0 {
			continue
		}
		for px := 0; px < width; px++ {
			ix := colIx[px]
			if ix < 0 {
				continue // transparent (hors grille)
			}
			v := sf.valAt(ix, iy)
			if math.IsNaN(v) {
				continue // transparent (donnée manquante)
			}
			img.SetNRGBA(px, py, sf.pal((v-sf.vmin)/span))
		}
	}
	return img
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

// paletteStops rassemble les rampes nommées (points de contrôle RGB, valeur basse
// → valeur haute ; matplotlib pour viridis/plasma/magma/inferno/turbo/coolwarm).
var paletteStops = map[string][][3]float64{
	"viridis": {
		{68, 1, 84}, {72, 40, 120}, {62, 74, 137}, {49, 104, 142},
		{38, 130, 142}, {31, 158, 137}, {53, 183, 121}, {109, 205, 89},
		{180, 222, 44}, {253, 231, 37},
	},
	"plasma": {
		{13, 8, 135}, {75, 3, 161}, {125, 3, 168}, {168, 34, 150}, {203, 70, 121},
		{229, 107, 93}, {248, 148, 65}, {253, 195, 40}, {240, 249, 33},
	},
	"magma": {
		{0, 0, 4}, {28, 16, 68}, {79, 18, 123}, {129, 37, 129}, {181, 54, 122},
		{229, 80, 100}, {251, 135, 97}, {254, 194, 135}, {252, 253, 191},
	},
	"inferno": {
		{0, 0, 4}, {31, 12, 72}, {85, 15, 109}, {136, 34, 106}, {186, 54, 85},
		{227, 89, 51}, {249, 140, 10}, {249, 201, 50}, {252, 255, 164},
	},
	"turbo": {
		{48, 18, 59}, {62, 73, 213}, {40, 150, 242}, {22, 209, 174}, {112, 242, 90},
		{212, 225, 44}, {252, 150, 44}, {225, 66, 14}, {122, 4, 3},
	},
	"coolwarm": {
		{59, 76, 192}, {98, 130, 234}, {141, 176, 254}, {184, 208, 249}, {221, 221, 221},
		{245, 196, 173}, {244, 154, 123}, {222, 96, 77}, {180, 4, 38},
	},
}

// paletteByName renvoie la palette nommée (défaut : viridis).
func paletteByName(name string) palette {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "grayscale", "gray", "greyscale", "grey":
		return grayPalette
	default:
		if stops, ok := paletteStops[strings.ToLower(strings.TrimSpace(name))]; ok {
			return stopPalette(stops)
		}
		return stopPalette(paletteStops["viridis"])
	}
}

// grayPalette : rampe noir → blanc.
func grayPalette(t float64) color.NRGBA {
	t = clamp01(t)
	g := uint8(t*255 + 0.5)
	return color.NRGBA{R: g, G: g, B: g, A: 255}
}

// stopPalette construit une palette par interpolation linéaire entre points de
// contrôle RGB.
func stopPalette(stops [][3]float64) palette {
	n := len(stops)
	return func(t float64) color.NRGBA {
		t = clamp01(t)
		x := t * float64(n-1)
		i := int(x)
		if i >= n-1 {
			s := stops[n-1]
			return color.NRGBA{R: uint8(s[0]), G: uint8(s[1]), B: uint8(s[2]), A: 255}
		}
		f := x - float64(i)
		a, b := stops[i], stops[i+1]
		lerp := func(u, v float64) uint8 { return uint8(u + (v-u)*f + 0.5) }
		return color.NRGBA{R: lerp(a[0], b[0]), G: lerp(a[1], b[1]), B: lerp(a[2], b[2]), A: 255}
	}
}

// -----------------------------------------------------------------------------
// Rendu bilinéaire (interpolation=bilinear)
// -----------------------------------------------------------------------------

// axBracket encadre une position cible entre deux indices d'un axe : valeur =
// v[i0]·(1-f) + v[i1]·f. ok=false si la cible sort de l'axe.
type axBracket struct {
	i0, i1 int
	f      float64
	ok     bool
}

// bracketAxis encadre target dans coords (monotone croissant ou décroissant).
func bracketAxis(coords []float64, target float64) axBracket {
	n := len(coords)
	lo, hi := coords[0], coords[n-1]
	mn, mx := lo, hi
	if mn > mx {
		mn, mx = mx, mn
	}
	if target < mn || target > mx {
		return axBracket{ok: false}
	}
	if n == 1 {
		return axBracket{i0: 0, i1: 0, f: 0, ok: true}
	}
	asc := hi >= lo
	for i := 0; i < n-1; i++ {
		a, b := coords[i], coords[i+1]
		if asc && target >= a && target <= b {
			f := 0.0
			if b != a {
				f = (target - a) / (b - a)
			}
			return axBracket{i0: i, i1: i + 1, f: f, ok: true}
		}
		if !asc && target <= a && target >= b {
			f := 0.0
			if a != b {
				f = (a - target) / (a - b)
			}
			return axBracket{i0: i, i1: i + 1, f: f, ok: true}
		}
	}
	return axBracket{i0: n - 1, i1: n - 1, f: 0, ok: true}
}

// bilerp interpole bilinéairement la valeur aux 4 mailles encadrantes, en
// ignorant les NaN (moyenne pondérée des mailles valides). ok=false si les 4
// sont manquantes.
func (sf *sampledField) bilerp(cx, ry axBracket) (float64, bool) {
	fx, fy := cx.f, ry.f
	ws := [4]float64{(1 - fx) * (1 - fy), fx * (1 - fy), (1 - fx) * fy, fx * fy}
	vs := [4]float64{
		sf.valAt(cx.i0, ry.i0), sf.valAt(cx.i1, ry.i0),
		sf.valAt(cx.i0, ry.i1), sf.valAt(cx.i1, ry.i1),
	}
	sw, sv := 0.0, 0.0
	for k := 0; k < 4; k++ {
		if ws[k] == 0 || math.IsNaN(vs[k]) {
			continue
		}
		sw += ws[k]
		sv += ws[k] * vs[k]
	}
	if sw == 0 {
		return 0, false
	}
	return sv / sw, true
}

// fillBilinear colorie une image width×height par interpolation bilinéaire ;
// lonAt/latAt donnent la position géographique de chaque pixel.
func (sf *sampledField) fillBilinear(width, height int, lonAt, latAt func(int) float64) *image.NRGBA {
	span := sf.vmax - sf.vmin
	if span <= 0 {
		span = 1
	}
	cols := make([]axBracket, width)
	for px := 0; px < width; px++ {
		cols[px] = bracketAxis(sf.xs, lonAt(px))
	}
	rows := make([]axBracket, height)
	for py := 0; py < height; py++ {
		rows[py] = bracketAxis(sf.ys, latAt(py))
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for py := 0; py < height; py++ {
		ry := rows[py]
		if !ry.ok {
			continue
		}
		for px := 0; px < width; px++ {
			cx := cols[px]
			if !cx.ok {
				continue
			}
			v, ok := sf.bilerp(cx, ry)
			if !ok {
				continue
			}
			img.SetNRGBA(px, py, sf.pal((v-sf.vmin)/span))
		}
	}
	return img
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
