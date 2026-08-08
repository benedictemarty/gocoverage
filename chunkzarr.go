package gocoverage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/benedictemarty/xarray"
	"github.com/klauspost/compress/zstd"
)

// zstdDec : décodeur zstd partagé (DecodeAll est sûr en concurrence).
var zstdDec, _ = zstd.NewReader(nil)

// Lecture Zarr v2 « élaguée par chunks » : pour une requête à emprise (bbox), on
// ne lit que les fichiers-chunks qui recouvrent la fenêtre demandée — vraie
// entrée/sortie partielle, sans jamais matérialiser toute la grille.
//
// xarray-go réalise cet élagage en interne (zarrRowSource) mais ne l'expose pas
// (types non exportés, ChunkZarr ne rend qu'un LazyArray sans accès aux blocs).
// D'où ce mini-lecteur, volontairement borné : Zarr v2, dtype <f8 (float64
// little-endian), ordre C, compresseur none/zlib/zstd, variables de données 2D [y, x]. Tout
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
	dir              string
	xDim, yDim, tDim string // tDim = "" si pas d'axe temporel (variables 2D)
	coords           map[string][]float64
	dataVars         []zvarMin
	chunksRead       int // nombre de fichiers-chunks lus au dernier ReadWindow (observabilité/tests)
}

// TDim renvoie le nom de l'axe temporel détecté ("" si variables 2D).
func (r *ZarrWindowReader) TDim() string { return r.tDim }

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
	// Variables de données : 2D [yDim, xDim] ou 3D [tDim, yDim, xDim].
	for _, v := range vars {
		switch {
		case len(v.dims) == 2 && v.dims[0] == yDim && v.dims[1] == xDim:
			r.dataVars = append(r.dataVars, v)
		case len(v.dims) == 3 && v.dims[1] == yDim && v.dims[2] == xDim:
			if r.tDim == "" {
				r.tDim = v.dims[0]
			} else if r.tDim != v.dims[0] {
				return nil, fmt.Errorf("axes temporels incohérents (%s vs %s)", r.tDim, v.dims[0])
			}
			r.dataVars = append(r.dataVars, v)
		}
	}
	if len(r.dataVars) == 0 {
		return nil, fmt.Errorf("aucune variable de données 2D/3D [%s, %s] dans le store", yDim, xDim)
	}
	// Axe temporel : décodage CF (« <unité> since <date> ») réutilisé de xarray-go,
	// pour que les valeurs soient comparables à un datetime (secondes epoch).
	if r.tDim != "" {
		if raw, ok := r.coords[r.tDim]; ok {
			r.coords[r.tDim] = decodeTimeCoord(r.tDim, raw, vars[r.tDim].attrs["units"])
		} else {
			return nil, fmt.Errorf("coordonnée temporelle %q absente du store", r.tDim)
		}
	}
	sort.Slice(r.dataVars, func(i, j int) bool { return r.dataVars[i].name < r.dataVars[j].name })
	return r, nil
}

// decodeTimeCoord applique le décodage CF du temps (unités « <u> since <date> »)
// en réutilisant xarray.DecodeTime. Sans unités CF, renvoie les valeurs brutes.
func decodeTimeCoord(dim string, raw []float64, units string) []float64 {
	if !strings.Contains(strings.ToLower(units), " since ") {
		return raw
	}
	da, err := xarray.NewDataArray([]string{dim}, []int{len(raw)}, append([]float64(nil), raw...), map[string][]float64{dim: raw}, dim)
	if err != nil {
		return raw
	}
	da.Variable().SetAttr("units", units)
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{dim: da})
	if err != nil {
		return raw
	}
	dec, err := xarray.DecodeTime(ds, dim)
	if err != nil {
		return raw
	}
	if cv, err := dec.Coord(dim); err == nil && len(cv) == len(raw) {
		return cv
	}
	return raw
}

// Coords renvoie les coordonnées lues (x, y).
func (r *ZarrWindowReader) Coords() (x, y []float64) {
	return r.coords[r.xDim], r.coords[r.yDim]
}

// LoadChunkedZarr construit une Collection à lecture élaguée par chunks depuis un
// store Zarr v2 (2D lon/lat, <f8, none/zlib/zstd). Les requêtes à emprise ne
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
	c := &Collection{ID: id, Title: title, XDim: xDim, YDim: yDim, TDim: r.TDim(), Window: r.ReadWindow}
	// Indices de métadonnées : description/domainset/rangetype servis sans jamais
	// matérialiser les données (coords + schéma légers déjà lus à l'ouverture).
	c.coordHint = r.coords
	c.fieldHint = r.schemaFields()
	c.dimsHint = r.dimSizes()
	if tv := r.coords[r.tDim]; r.tDim != "" && len(tv) > 0 {
		c.TExtent = &[2]float64{minOf(tv), maxOf(tv)}
	}
	return c, nil
}

// schemaFields renvoie le schéma léger des variables de données (nom, type, unité,
// libellé) sans lire les données.
func (r *ZarrWindowReader) schemaFields() []Field {
	out := make([]Field, 0, len(r.dataVars))
	for _, v := range r.dataVars {
		out = append(out, Field{
			Name:  v.name,
			Type:  fieldTypeFromAttrs(v.attrs),
			Title: v.attrs["long_name"],
			Unit:  v.attrs["units"],
		})
	}
	return out
}

// dimSizes renvoie les tailles des dimensions x/y/(t) depuis les coordonnées.
func (r *ZarrWindowReader) dimSizes() map[string]int {
	d := map[string]int{r.xDim: len(r.coords[r.xDim]), r.yDim: len(r.coords[r.yDim])}
	if r.tDim != "" {
		d[r.tDim] = len(r.coords[r.tDim])
	}
	return d
}

// ChunksRead renvoie le nombre de fichiers-chunks ouverts au dernier ReadWindow.
func (r *ZarrWindowReader) ChunksRead() int { return r.chunksRead }

// ReadWindow lit la fenêtre décrite par sel (emprise et/ou plage temporelle ;
// nil = axe entier), en n'ouvrant que les chunks recouvrant les indices retenus.
func (r *ZarrWindowReader) ReadWindow(sel WindowSel) (*xarray.Dataset[float64], error) {
	xv, yv := r.coords[r.xDim], r.coords[r.yDim]
	c0, c1 := 0, len(xv)
	r0, r1 := 0, len(yv)
	if sel.BBox != nil {
		c0, c1 = indexRange(xv, sel.BBox[0], sel.BBox[2])
		r0, r1 = indexRange(yv, sel.BBox[1], sel.BBox[3])
	}
	if c1 <= c0 || r1 <= r0 {
		return nil, fmt.Errorf("fenêtre vide (bbox hors emprise)")
	}
	// Plage temporelle (variables 3D).
	var tv []float64
	t0, t1 := 0, 0
	if r.tDim != "" {
		tv = r.coords[r.tDim]
		t0, t1 = 0, len(tv)
		if sel.TRange != nil {
			t0, t1 = indexRange(tv, sel.TRange[0], sel.TRange[1])
		}
		if t1 <= t0 {
			return nil, fmt.Errorf("fenêtre temporelle vide")
		}
	}
	r.chunksRead = 0
	out := map[string]*xarray.DataArray[float64]{}
	for _, v := range r.dataVars {
		var da *xarray.DataArray[float64]
		var err error
		if len(v.dims) == 3 {
			da, err = r.read3D(v, t0, t1, r0, r1, c0, c1, tv, yv, xv)
		} else {
			da, err = r.read2D(v, r0, r1, c0, c1, yv, xv)
		}
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

// read2D lit la fenêtre [r0,r1)×[c0,c1) d'une variable 2D [y, x].
func (r *ZarrWindowReader) read2D(v zvarMin, r0, r1, c0, c1 int, yv, xv []float64) (*xarray.DataArray[float64], error) {
	C := v.meta.Shape[1]
	cr, cc := v.meta.Chunks[0], v.meta.Chunks[1]
	comp, err := compressorID(v.meta.Compressor)
	if err != nil {
		return nil, err
	}
	nr, nc := r1-r0, c1-c0
	out := nanSlice(nr * nc)
	for rc := r0 / cr; rc <= (r1-1)/cr; rc++ {
		for cci := c0 / cc; cci <= (c1-1)/cc; cci++ {
			block, present, err := r.readChunk(v.name, comp, []int{rc, cci}, cr*cc)
			if err != nil {
				return nil, err
			}
			if !present {
				continue
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
	return xarray.NewDataArray([]string{r.yDim, r.xDim}, []int{nr, nc}, out,
		map[string][]float64{r.yDim: clone(yv[r0:r1]), r.xDim: clone(xv[c0:c1])}, v.name)
}

// read3D lit la fenêtre [t0,t1)×[r0,r1)×[c0,c1) d'une variable 3D [t, y, x].
func (r *ZarrWindowReader) read3D(v zvarMin, t0, t1, r0, r1, c0, c1 int, tv, yv, xv []float64) (*xarray.DataArray[float64], error) {
	Y, C := v.meta.Shape[1], v.meta.Shape[2]
	ct, cr, cc := v.meta.Chunks[0], v.meta.Chunks[1], v.meta.Chunks[2]
	comp, err := compressorID(v.meta.Compressor)
	if err != nil {
		return nil, err
	}
	nt, nr, nc := t1-t0, r1-r0, c1-c0
	out := nanSlice(nt * nr * nc)
	for tc := t0 / ct; tc <= (t1-1)/ct; tc++ {
		for rc := r0 / cr; rc <= (r1-1)/cr; rc++ {
			for cci := c0 / cc; cci <= (c1-1)/cc; cci++ {
				block, present, err := r.readChunk(v.name, comp, []int{tc, rc, cci}, ct*cr*cc)
				if err != nil {
					return nil, err
				}
				if !present {
					continue
				}
				for lt := 0; lt < ct; lt++ {
					gt := tc*ct + lt
					if gt < t0 || gt >= t1 {
						continue
					}
					for li := 0; li < cr; li++ {
						gr := rc*cr + li
						if gr < r0 || gr >= r1 || gr >= Y {
							continue
						}
						for lj := 0; lj < cc; lj++ {
							gc := cci*cc + lj
							if gc < c0 || gc >= c1 || gc >= C {
								continue
							}
							out[((gt-t0)*nr+(gr-r0))*nc+(gc-c0)] = block[(lt*cr+li)*cc+lj]
						}
					}
				}
			}
		}
	}
	return xarray.NewDataArray([]string{r.tDim, r.yDim, r.xDim}, []int{nt, nr, nc}, out,
		map[string][]float64{r.tDim: clone(tv[t0:t1]), r.yDim: clone(yv[r0:r1]), r.xDim: clone(xv[c0:c1])}, v.name)
}

// readChunk lit le fichier-chunk aux indices coord (clé « i.j[.k] »), le
// décompresse et le décode en n float64 (<f8 LE). Chunk absent → fill_value.
func (r *ZarrWindowReader) readChunk(varName, comp string, coord []int, n int) ([]float64, bool, error) {
	parts := make([]string, len(coord))
	for i, c := range coord {
		parts[i] = strconv.Itoa(c)
	}
	raw, err := os.ReadFile(filepath.Join(r.dir, varName, strings.Join(parts, ".")))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	r.chunksRead++
	dec, err := decompress(comp, raw)
	if err != nil {
		return nil, false, err
	}
	return decodeF8LE(dec, n)
}

func nanSlice(n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = math.NaN()
	}
	return s
}

func clone(s []float64) []float64 { return append([]float64(nil), s...) }

// readArray1D lit intégralement un tableau 1D (coordonnée).
func (r *ZarrWindowReader) readArray1D(name string, meta zarrayMetaMin) ([]float64, error) {
	n := meta.Shape[0]
	cr := meta.Chunks[0]
	comp, err := compressorID(meta.Compressor)
	if err != nil {
		return nil, err
	}
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
		dec, err := decompress(comp, raw)
		if err != nil {
			return nil, err
		}
		block, _, err := decodeF8LE(dec, cr)
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
	id, err := compressorID(m.Compressor)
	if err != nil {
		return err
	}
	if id != "" && id != "zlib" && id != "zstd" {
		return fmt.Errorf("compresseur %q non supporté par le lecteur élagué (none/zlib/zstd)", id)
	}
	return nil
}

// compressorID extrait l'identifiant du compresseur d'un .zarray (""=aucun).
func compressorID(raw json.RawMessage) (string, error) {
	s := strings.TrimSpace(string(raw))
	if len(raw) == 0 || s == "null" {
		return "", nil
	}
	var c struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", fmt.Errorf("compresseur illisible: %w", err)
	}
	return c.ID, nil
}

// decompress applique le décodeur du compresseur (none/zlib/zstd) à un chunk brut.
func decompress(id string, raw []byte) ([]byte, error) {
	switch id {
	case "":
		return raw, nil
	case "zlib":
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("zlib: %w", err)
		}
		defer zr.Close()
		return io.ReadAll(zr)
	case "zstd":
		return zstdDec.DecodeAll(raw, nil)
	default:
		return nil, fmt.Errorf("compresseur %q non supporté", id)
	}
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
