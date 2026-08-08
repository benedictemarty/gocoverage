package gocoverage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/benedictemarty/xarray"
)

// Lecture Zarr v2 « élaguée par chunks » : pour une requête à emprise (bbox), on
// ne lit que les fichiers-chunks qui recouvrent la fenêtre demandée — vraie
// entrée/sortie partielle, sans jamais matérialiser toute la grille.
//
// xarray-go réalise cet élagage en interne (zarrRowSource) mais ne l'expose pas
// (types non exportés, ChunkZarr ne rend qu'un LazyArray sans accès aux blocs).
// D'où ce mini-lecteur, volontairement borné : Zarr v2, dtype <f8 (float64
// little-endian), ordre C, compressor null, variables de données 2D [y, x]. Tout
// écart (v3, compressé, >2D, autre dtype) renvoie une erreur — l'appelant peut
// alors retomber sur LoadZarr (lecture complète).

type zarrayMetaMin struct {
	ZarrFormat int             `json:"zarr_format"`
	Shape      []int           `json:"shape"`
	Chunks     []int           `json:"chunks"`
	Dtype      string          `json:"dtype"`
	Compressor json.RawMessage `json:"compressor"`
	Order      string          `json:"order"`
}

type zvarMin struct {
	name  string
	dims  []string
	meta  zarrayMetaMin
	attrs map[string]string
}

// ZarrWindowReader lit un groupe Zarr v2 en n'ouvrant que les chunks nécessaires.
type ZarrWindowReader struct {
	dir        string
	xDim, yDim string
	coords     map[string][]float64
	dataVars   []zvarMin
	chunksRead int // nombre de fichiers-chunks lus au dernier ReadWindow (observabilité/tests)
}

// OpenZarrWindow ouvre un groupe Zarr v2 pour lecture élaguée. Lit uniquement les
// métadonnées et les coordonnées (1D, légères) — pas les données. Renvoie une
// erreur si le store n'est pas dans le sous-ensemble supporté.
func OpenZarrWindow(dir, xDim, yDim string) (*ZarrWindowReader, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	r := &ZarrWindowReader{dir: dir, xDim: xDim, yDim: yDim, coords: map[string][]float64{}}
	vars := map[string]zvarMin{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		var meta zarrayMetaMin
		if err := readJSON(filepath.Join(sub, ".zarray"), &meta); err != nil {
			continue // pas un tableau Zarr
		}
		if err := supported(meta); err != nil {
			return nil, fmt.Errorf("variable %q non supportée: %w", e.Name(), err)
		}
		dims, attrs := readZAttrs(filepath.Join(sub, ".zattrs"))
		vars[e.Name()] = zvarMin{name: e.Name(), dims: dims, meta: meta, attrs: attrs}
	}
	// Coordonnées = variables 1D dont le nom est une dimension.
	for name, v := range vars {
		if len(v.meta.Shape) == 1 && (len(v.dims) == 0 || v.dims[0] == name) {
			cv, err := r.readArray1D(name, v.meta)
			if err != nil {
				return nil, err
			}
			r.coords[name] = cv
		}
	}
	if _, ok := r.coords[xDim]; !ok {
		return nil, fmt.Errorf("coordonnée X %q absente du store Zarr", xDim)
	}
	if _, ok := r.coords[yDim]; !ok {
		return nil, fmt.Errorf("coordonnée Y %q absente du store Zarr", yDim)
	}
	// Variables de données = 2D portant [yDim, xDim].
	for _, v := range vars {
		if len(v.meta.Shape) == 2 && len(v.dims) == 2 && v.dims[0] == yDim && v.dims[1] == xDim {
			r.dataVars = append(r.dataVars, v)
		}
	}
	if len(r.dataVars) == 0 {
		return nil, fmt.Errorf("aucune variable de données 2D [%s, %s] dans le store", yDim, xDim)
	}
	sort.Slice(r.dataVars, func(i, j int) bool { return r.dataVars[i].name < r.dataVars[j].name })
	return r, nil
}

// Coords renvoie les coordonnées lues (x, y).
func (r *ZarrWindowReader) Coords() (x, y []float64) {
	return r.coords[r.xDim], r.coords[r.yDim]
}

// LoadChunkedZarr construit une Collection à lecture élaguée par chunks depuis un
// store Zarr v2 (2D lon/lat, <f8, non compressé). Les requêtes à emprise ne
// lisent que les chunks nécessaires (via Collection.Window) ; les accès sans
// emprise (métadonnées) matérialisent la grille complète à la demande.
//
// Si le store n'est pas dans le sous-ensemble supporté, renvoie une erreur —
// l'appelant peut alors retomber sur LoadZarr (lecture complète). Les axes sont
// détectés par nom (longitude/lon/x, latitude/lat/y) si xDim/yDim sont vides.
func LoadChunkedZarr(dir, id, title, xDim, yDim string) (*Collection, error) {
	xCands := []string{xDim}
	yCands := []string{yDim}
	if xDim == "" {
		xCands = []string{"longitude", "lon", "x"}
	}
	if yDim == "" {
		yCands = []string{"latitude", "lat", "y"}
	}
	var r *ZarrWindowReader
	var lastErr error
	for _, x := range xCands {
		for _, y := range yCands {
			if x == "" || y == "" {
				continue
			}
			if rr, err := OpenZarrWindow(dir, x, y); err == nil {
				r, xDim, yDim = rr, x, y
			} else {
				lastErr = err
			}
			if r != nil {
				break
			}
		}
		if r != nil {
			break
		}
	}
	if r == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("axes lon/lat introuvables dans %q", dir)
		}
		return nil, lastErr
	}
	return &Collection{ID: id, Title: title, XDim: xDim, YDim: yDim, Window: r.ReadWindow}, nil
}

// ChunksRead renvoie le nombre de fichiers-chunks ouverts au dernier ReadWindow.
func (r *ZarrWindowReader) ChunksRead() int { return r.chunksRead }

// ReadWindow lit la fenêtre correspondant à bbox (nil = tout), en n'ouvrant que
// les chunks recouvrant les indices retenus, et renvoie un Dataset[float64].
func (r *ZarrWindowReader) ReadWindow(bbox *[4]float64) (*xarray.Dataset[float64], error) {
	xv, yv := r.coords[r.xDim], r.coords[r.yDim]
	c0, c1 := 0, len(xv)
	r0, r1 := 0, len(yv)
	if bbox != nil {
		c0, c1 = indexRange(xv, bbox[0], bbox[2])
		r0, r1 = indexRange(yv, bbox[1], bbox[3])
	}
	if c1 <= c0 || r1 <= r0 {
		return nil, fmt.Errorf("fenêtre vide (bbox hors emprise)")
	}
	r.chunksRead = 0
	out := map[string]*xarray.DataArray[float64]{}
	for _, v := range r.dataVars {
		data, err := r.readVarWindow(v, r0, r1, c0, c1)
		if err != nil {
			return nil, err
		}
		da, err := xarray.NewDataArray(
			[]string{r.yDim, r.xDim}, []int{r1 - r0, c1 - c0}, data,
			map[string][]float64{r.yDim: append([]float64(nil), yv[r0:r1]...), r.xDim: append([]float64(nil), xv[c0:c1]...)},
			v.name)
		if err != nil {
			return nil, err
		}
		for k, val := range v.attrs {
			da.Variable().SetAttr(k, val)
		}
		out[v.name] = da
	}
	return xarray.NewDataset(out)
}

// readVarWindow lit les lignes [r0,r1) × colonnes [c0,c1) d'une variable 2D en
// n'ouvrant que les chunks nécessaires.
func (r *ZarrWindowReader) readVarWindow(v zvarMin, r0, r1, c0, c1 int) ([]float64, error) {
	C := v.meta.Shape[1]
	cr, cc := v.meta.Chunks[0], v.meta.Chunks[1]
	nr, nc := r1-r0, c1-c0
	out := make([]float64, nr*nc)
	for i := range out {
		out[i] = math.NaN()
	}
	rcStart, rcEnd := r0/cr, (r1-1)/cr
	ccStart, ccEnd := c0/cc, (c1-1)/cc
	for rc := rcStart; rc <= rcEnd; rc++ {
		for cci := ccStart; cci <= ccEnd; cci++ {
			block, present, err := r.readChunk2D(v.name, rc, cci, cr*cc)
			if err != nil {
				return nil, err
			}
			if !present {
				continue // chunk absent → fill_value (NaN)
			}
			for li := 0; li < cr; li++ {
				gr := rc*cr + li
				if gr < r0 || gr >= r1 {
					continue
				}
				for lj := 0; lj < cc; lj++ {
					gc := cci*cc + lj
					if gc < c0 || gc >= c1 || gc >= C {
						continue
					}
					out[(gr-r0)*nc+(gc-c0)] = block[li*cc+lj]
				}
			}
		}
	}
	return out, nil
}

// readChunk2D lit le fichier-chunk "rc.cc" et le décode en n float64 (<f8 LE).
func (r *ZarrWindowReader) readChunk2D(varName string, rc, cc, n int) ([]float64, bool, error) {
	key := strconv.Itoa(rc) + "." + strconv.Itoa(cc)
	raw, err := os.ReadFile(filepath.Join(r.dir, varName, key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	r.chunksRead++
	return decodeF8LE(raw, n)
}

// readArray1D lit intégralement un tableau 1D (coordonnée).
func (r *ZarrWindowReader) readArray1D(name string, meta zarrayMetaMin) ([]float64, error) {
	n := meta.Shape[0]
	cr := meta.Chunks[0]
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	nchunks := (n + cr - 1) / cr
	for k := 0; k < nchunks; k++ {
		raw, err := os.ReadFile(filepath.Join(r.dir, name, strconv.Itoa(k)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		block, _, err := decodeF8LE(raw, cr)
		if err != nil {
			return nil, err
		}
		for li := 0; li < cr; li++ {
			gi := k*cr + li
			if gi < n {
				out[gi] = block[li]
			}
		}
	}
	return out, nil
}

// --- helpers ---

func supported(m zarrayMetaMin) error {
	if m.ZarrFormat != 2 {
		return fmt.Errorf("seul Zarr v2 est supporté (format %d)", m.ZarrFormat)
	}
	if dt := strings.TrimPrefix(m.Dtype, "|"); dt != "<f8" && dt != "=f8" {
		return fmt.Errorf("seul le dtype <f8 est supporté (%q)", m.Dtype)
	}
	if m.Order != "" && m.Order != "C" {
		return fmt.Errorf("seul l'ordre C est supporté (%q)", m.Order)
	}
	if len(m.Compressor) > 0 && string(m.Compressor) != "null" {
		return fmt.Errorf("chunks compressés non supportés par le lecteur élagué")
	}
	return nil
}

func decodeF8LE(raw []byte, n int) ([]float64, bool, error) {
	if len(raw) < n*8 {
		return nil, false, fmt.Errorf("chunk tronqué: %d o pour %d float64", len(raw), n)
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(raw[i*8:]))
	}
	return out, true, nil
}

func readJSON(path string, v interface{}) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// readZAttrs lit _ARRAY_DIMENSIONS et les attributs libres (chaînes) d'un .zattrs.
func readZAttrs(path string) (dims []string, attrs map[string]string) {
	attrs = map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, attrs
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return nil, attrs
	}
	for k, raw := range obj {
		if k == "_ARRAY_DIMENSIONS" {
			_ = json.Unmarshal(raw, &dims)
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			attrs[k] = s
		}
	}
	return dims, attrs
}

// indexRange renvoie [lo, hi) des indices de v (trié asc ou desc) dont la valeur
// tombe dans [a, b]. Les indices retenus sont contigus.
func indexRange(v []float64, a, b float64) (int, int) {
	if a > b {
		a, b = b, a
	}
	lo, hi := len(v), 0
	for i, x := range v {
		if x >= a && x <= b {
			if i < lo {
				lo = i
			}
			if i+1 > hi {
				hi = i + 1
			}
		}
	}
	if lo > hi {
		return 0, 0
	}
	return lo, hi
}
