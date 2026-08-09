package gocoverage

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// maxMapDimension borne une dimension de sortie demandée (width/height) pour
// éviter des requêtes déraisonnables avant même le garde-fou en pixels.
const maxMapDimension = 8192

// mapRender : GET /collections/{id}/map — OGC API - Maps (map par défaut).
// Paramètres : bbox=minx,miny,maxx,maxy (défaut : emprise de la collection),
// width, height (défaut : 512 × rapport d'emprise), datetime, z,
// properties=<variable>, colorscalerange=min,max, style|palette=viridis|grayscale,
// f=png|jpeg (défaut png).
func (s *Server) mapRender(w http.ResponseWriter, r *http.Request, c *Collection) {
	q := r.URL.Query()

	// Emprise : bbox explicite, sinon emprise native de la collection.
	bbox := c.BBox()
	if v := strings.TrimSpace(q.Get("bbox")); v != "" {
		bb, err := parseFloats(v, 4)
		if err != nil {
			writeErr(w, 400, "bbox invalide: "+err.Error())
			return
		}
		bbox = [4]float64{bb[0], bb[1], bb[2], bb[3]}
	}
	if bbox[2] <= bbox[0] || bbox[3] <= bbox[1] {
		writeErr(w, 400, "bbox invalide (minx<maxx et miny<maxy attendus)")
		return
	}

	width, height, err := parseMapSize(q.Get("width"), q.Get("height"), bbox)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	dt, err := s.parseDatetimeParam(q.Get("datetime"), c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	z, err := parseZ(q.Get("z"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	opts := MapOptions{
		BBox:     bbox,
		Width:    width,
		Height:   height,
		Palette:  mapPalette(q),
		Datetime: dt,
		Z:        z,
	}
	if props := parseList(q.Get("properties")); len(props) > 0 {
		opts.Field = props[0]
	}
	if cr := strings.TrimSpace(q.Get("colorscalerange")); cr != "" {
		mm, err := parseFloats(cr, 2)
		if err != nil {
			writeErr(w, 400, "colorscalerange invalide (attendu min,max): "+err.Error())
			return
		}
		if mm[1] <= mm[0] {
			writeErr(w, 400, "colorscalerange invalide (min<max attendu)")
			return
		}
		opts.VMin, opts.VMax = &mm[0], &mm[1]
	}

	img, err := c.RenderMap(opts)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeImage(w, r, img)
}

// mapPalette lit la palette demandée (paramètres `style` puis `palette`).
func mapPalette(q map[string][]string) string {
	get := func(k string) string {
		if v, ok := q[k]; ok && len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	if s := get("style"); s != "" && s != "default" {
		return s
	}
	return get("palette")
}

// parseMapSize résout width/height. Absents → 512 × rapport d'emprise ; l'un
// fourni → l'autre dérivé du rapport d'emprise ; les deux fournis → tels quels.
func parseMapSize(ws, hs string, bbox [4]float64) (int, int, error) {
	ws, hs = strings.TrimSpace(ws), strings.TrimSpace(hs)
	dlon, dlat := bbox[2]-bbox[0], bbox[3]-bbox[1]
	aspect := 1.0
	if dlon > 0 {
		aspect = dlat / dlon
	}
	parseDim := func(s, label string) (int, error) {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return 0, mapDimErr(label)
		}
		if n > maxMapDimension {
			return 0, mapDimErr(label)
		}
		return n, nil
	}
	switch {
	case ws == "" && hs == "":
		w := defaultMapWidth
		return w, clampDim(int(math.Round(float64(w) * aspect))), nil
	case hs == "":
		w, err := parseDim(ws, "width")
		if err != nil {
			return 0, 0, err
		}
		return w, clampDim(int(math.Round(float64(w) * aspect))), nil
	case ws == "":
		h, err := parseDim(hs, "height")
		if err != nil {
			return 0, 0, err
		}
		wv := float64(h)
		if aspect > 0 {
			wv = float64(h) / aspect
		}
		return clampDim(int(math.Round(wv))), h, nil
	default:
		wv, err := parseDim(ws, "width")
		if err != nil {
			return 0, 0, err
		}
		hv, err := parseDim(hs, "height")
		if err != nil {
			return 0, 0, err
		}
		return wv, hv, nil
	}
}

func clampDim(n int) int {
	if n < 1 {
		return 1
	}
	if n > maxMapDimension {
		return maxMapDimension
	}
	return n
}

func mapDimErr(label string) error {
	return errString(label + " invalide (entier entre 1 et " + strconv.Itoa(maxMapDimension) + " attendu)")
}

// errString : petite erreur constante (évite fmt pour un message simple).
type errString string

func (e errString) Error() string { return string(e) }

// writeImage encode l'image dans le format négocié (png par défaut, jpeg si
// f=jpeg/jpg ou Accept image/jpeg).
func writeImage(w http.ResponseWriter, r *http.Request, img image.Image) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("f")))
	if format == "" {
		if a := r.Header.Get("Accept"); strings.Contains(a, "image/jpeg") && !strings.Contains(a, "image/png") {
			format = "jpeg"
		}
	}
	var buf bytes.Buffer
	switch format {
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			writeErr(w, 500, "encodage jpeg: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
	case "", "png":
		if err := png.Encode(&buf, img); err != nil {
			writeErr(w, 500, "encodage png: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "image/png")
	default:
		writeErr(w, 400, "format image inconnu: "+format+" (png|jpeg)")
		return
	}
	w.WriteHeader(200)
	_, _ = w.Write(buf.Bytes())
}
