package gocoverage

import (
	"fmt"
	"image"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// OGC API - Tiles : tuiles carte matricielles (pyramide de tuiles 256×256),
// réutilisant le moteur de rendu d'OGC API - Maps. Deux TileMatrixSets sont
// gérés :
//   - WorldCRS84Quad  : grille géographique lat/lon (aucune reprojection) ;
//   - WebMercatorQuad : projection Web Mercator (le mapping pixel→latitude est
//     non linéaire ; seule la géométrie d'échantillonnage change — les données
//     ne sont jamais reprojetées, on lit la grille source en lon/lat).
//
// Chemin d'une tuile : /collections/{id}/map/tiles/{tmsId}/{z}/{y}/{x}
// ({z}=tileMatrix, {y}=tileRow, {x}=tileCol).

// tileSize est la taille (en pixels) d'une tuile carrée.
const tileSize = 256

// maxTileZoom borne le niveau de zoom accepté (garde-fou).
const maxTileZoom = 26

// webMercR est le rayon terrestre du Web Mercator (EPSG:3857, sphère WGS84).
const webMercR = 6378137.0

// webMercMax est la demi-étendue du Web Mercator en mètres (± π·R).
var webMercMax = math.Pi * webMercR

// supportedTMS liste les TileMatrixSets gérés (identifiants OGC).
var supportedTMS = []string{"WebMercatorQuad", "WorldCRS84Quad"}

// tmsURI renvoie l'URI de définition officielle d'un TileMatrixSet.
func tmsURI(id string) string {
	return "http://www.opengis.net/def/tilematrixset/OGC/1.0/" + id
}

// tileGeom décrit la géométrie d'une tuile : emprise géographique (pour
// l'élagage Query) et les fonctions pixel→lon / pixel→lat.
type tileGeom struct {
	bbox  [4]float64 // [lonMin, latMin, lonMax, latMax]
	lonAt func(px int) float64
	latAt func(py int) float64
}

// tileMatrixDims renvoie le nombre de tuiles (colonnes, lignes) au niveau z pour
// le TMS donné, ou une erreur si le TMS est inconnu.
func tileMatrixDims(tms string, z int) (cols, rows int, err error) {
	n := 1 << uint(z)
	switch tms {
	case "WebMercatorQuad":
		return n, n, nil
	case "WorldCRS84Quad":
		return 2 * n, n, nil // 2 colonnes × 1 ligne au niveau 0
	default:
		return 0, 0, fmt.Errorf("TileMatrixSet inconnu: %s", tms)
	}
}

// tileGeometry calcule la géométrie de la tuile (z, x=col, y=row) du TMS.
func tileGeometry(tms string, z, x, y int) (tileGeom, error) {
	if z < 0 || z > maxTileZoom {
		return tileGeom{}, fmt.Errorf("niveau de zoom invalide: %d", z)
	}
	cols, rows, err := tileMatrixDims(tms, z)
	if err != nil {
		return tileGeom{}, err
	}
	if x < 0 || x >= cols || y < 0 || y >= rows {
		return tileGeom{}, fmt.Errorf("tuile hors grille (%d/%d/%d), matrice %d×%d", z, y, x, cols, rows)
	}

	switch tms {
	case "WebMercatorQuad":
		size := 2 * webMercMax / float64(cols) // = 2·max / 2^z
		xmin := -webMercMax + float64(x)*size
		ymax := webMercMax - float64(y)*size
		lonFromX := func(mx float64) float64 { return mx / webMercMax * 180.0 }
		latFromY := func(my float64) float64 { return math.Atan(math.Sinh(my/webMercR)) * 180.0 / math.Pi }
		g := tileGeom{
			bbox: [4]float64{
				lonFromX(xmin), latFromY(ymax - size),
				lonFromX(xmin + size), latFromY(ymax),
			},
			lonAt: func(px int) float64 {
				return lonFromX(xmin + (float64(px)+0.5)/float64(tileSize)*size)
			},
			latAt: func(py int) float64 {
				return latFromY(ymax - (float64(py)+0.5)/float64(tileSize)*size)
			},
		}
		return g, nil

	case "WorldCRS84Quad":
		lonSpan := 360.0 / float64(cols)
		latSpan := 180.0 / float64(rows)
		lonMin := -180.0 + float64(x)*lonSpan
		latMax := 90.0 - float64(y)*latSpan
		g := tileGeom{
			bbox: [4]float64{lonMin, latMax - latSpan, lonMin + lonSpan, latMax},
			lonAt: func(px int) float64 {
				return lonMin + (float64(px)+0.5)/float64(tileSize)*lonSpan
			},
			latAt: func(py int) float64 {
				return latMax - (float64(py)+0.5)/float64(tileSize)*latSpan
			},
		}
		return g, nil
	}
	return tileGeom{}, fmt.Errorf("TileMatrixSet inconnu: %s", tms)
}

// RenderTile rend la tuile (tms, z, x=col, y=row) en image 256×256. Les options
// de champ/palette/bornes/datetime/z (vertical) sont reprises de MapOptions ;
// BBox/Width/Height sont dérivées de la tuile.
func (c *Collection) RenderTile(o MapOptions, tms string, z, x, y int) (*image.NRGBA, error) {
	g, err := tileGeometry(tms, z, x, y)
	if err != nil {
		return nil, err
	}
	// Emprise de la tuile hors couverture → tuile entièrement transparente.
	cb := c.BBox()
	if g.bbox[0] >= cb[2] || g.bbox[2] <= cb[0] || g.bbox[1] >= cb[3] || g.bbox[3] <= cb[1] {
		return image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize)), nil
	}

	o.BBox = g.bbox
	o.Width, o.Height = tileSize, tileSize
	sf, err := c.sampledField(o)
	if err != nil {
		return nil, err
	}
	colIx := make([]int, tileSize)
	for px := 0; px < tileSize; px++ {
		colIx[px] = nearestInRange(sf.xs, g.lonAt(px))
	}
	rowIy := make([]int, tileSize)
	for py := 0; py < tileSize; py++ {
		rowIy[py] = nearestInRange(sf.ys, g.latAt(py))
	}
	return sf.fill(tileSize, tileSize, colIx, rowIy), nil
}

// -----------------------------------------------------------------------------
// Handlers HTTP
// -----------------------------------------------------------------------------

// tileMatrixSets : GET /tileMatrixSets — liste des TMS gérés.
func (s *Server) tileMatrixSets(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/tileMatrixSets" {
		http.NotFound(w, r)
		return
	}
	sets := make([]map[string]interface{}, 0, len(supportedTMS))
	for _, id := range supportedTMS {
		sets = append(sets, map[string]interface{}{
			"id":  id,
			"uri": tmsURI(id),
			"links": []map[string]string{
				{"rel": "self", "href": "/tileMatrixSets/" + id, "type": "application/json"},
			},
		})
	}
	writeJSON(w, 200, map[string]interface{}{"tileMatrixSets": sets})
}

// tileMatrixSetByID : GET /tileMatrixSets/{id} — définition (minimale) d'un TMS.
func (s *Server) tileMatrixSetByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/tileMatrixSets/"), "/")
	if !isSupportedTMS(id) {
		writeErr(w, 404, "TileMatrixSet inconnu: "+id)
		return
	}
	crs := "http://www.opengis.net/def/crs/EPSG/0/3857"
	if id == "WorldCRS84Quad" {
		crs = "http://www.opengis.net/def/crs/OGC/1.3/CRS84"
	}
	writeJSON(w, 200, map[string]interface{}{
		"id":    id,
		"uri":   tmsURI(id),
		"crs":   crs,
		"title": id,
	})
}

// isSupportedTMS indique si le TMS est géré.
func isSupportedTMS(id string) bool {
	for _, s := range supportedTMS {
		if s == id {
			return true
		}
	}
	return false
}

// mapTiles route /collections/{id}/map/tiles[/{tms}[/{z}/{y}/{x}]].
func (s *Server) mapTiles(w http.ResponseWriter, r *http.Request, c *Collection, rest string) {
	rest = strings.Trim(rest, "/")
	if rest == "" {
		// Liste des tilesets (un par TMS géré).
		sets := make([]map[string]interface{}, 0, len(supportedTMS))
		for _, id := range supportedTMS {
			sets = append(sets, map[string]interface{}{
				"tileMatrixSetURI": tmsURI(id),
				"dataType":         "map",
				"links": []map[string]string{
					{"rel": "self", "href": "/collections/" + c.ID + "/map/tiles/" + id, "type": "application/json"},
					{"rel": "item", "href": "/collections/" + c.ID + "/map/tiles/" + id + "/{tileMatrix}/{tileRow}/{tileCol}", "type": "image/png"},
				},
			})
		}
		writeJSON(w, 200, map[string]interface{}{"tilesets": sets})
		return
	}
	parts := strings.Split(rest, "/")
	tms := parts[0]
	if !isSupportedTMS(tms) {
		writeErr(w, 404, "TileMatrixSet inconnu: "+tms)
		return
	}
	switch len(parts) {
	case 1:
		// Métadonnées du tileset pour ce TMS.
		writeJSON(w, 200, map[string]interface{}{
			"tileMatrixSetURI": tmsURI(tms),
			"dataType":         "map",
			"links": []map[string]string{
				{"rel": "item", "href": "/collections/" + c.ID + "/map/tiles/" + tms + "/{tileMatrix}/{tileRow}/{tileCol}", "type": "image/png"},
				{"rel": "http://www.opengis.net/def/rel/ogc/1.0/tiling-scheme", "href": "/tileMatrixSets/" + tms},
			},
		})
	case 4:
		s.renderTile(w, r, c, tms, parts[1], parts[2], parts[3])
	default:
		writeErr(w, 404, "ressource tuile inconnue: "+rest)
	}
}

// renderTile parse {z}/{y}/{x} et rend la tuile.
func (s *Server) renderTile(w http.ResponseWriter, r *http.Request, c *Collection, tms, zs, ys, xs string) {
	z, err1 := strconv.Atoi(zs)
	row, err2 := strconv.Atoi(ys)
	col, err3 := strconv.Atoi(xs)
	if err1 != nil || err2 != nil || err3 != nil {
		writeErr(w, 400, "indices de tuile invalides (attendu {z}/{y}/{x} entiers)")
		return
	}
	q := r.URL.Query()
	dt, err := s.parseDatetimeParam(q.Get("datetime"), c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	zlev, err := parseZ(q.Get("elevation"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	o := MapOptions{Palette: mapPalette(q), Datetime: dt, Z: zlev}
	if props := parseList(q.Get("properties")); len(props) > 0 {
		o.Field = props[0]
	}
	if cr := strings.TrimSpace(q.Get("colorscalerange")); cr != "" {
		mm, err := parseFloats(cr, 2)
		if err != nil || mm[1] <= mm[0] {
			writeErr(w, 400, "colorscalerange invalide (attendu min,max avec min<max)")
			return
		}
		o.VMin, o.VMax = &mm[0], &mm[1]
	}
	img, err := c.RenderTile(o, tms, z, col, row)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeImage(w, r, img)
}
