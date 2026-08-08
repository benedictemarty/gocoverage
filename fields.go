package gocoverage

import (
	"fmt"
	"strings"
)

// Field décrit un paramètre (variable de données) exposé par une collection,
// à la manière de get_fields dans pygeoapi : type, libellé et unité.
type Field struct {
	Name  string `json:"name"`
	Type  string `json:"type"`            // "float", "integer", "string"
	Title string `json:"title,omitempty"` // long_name
	Unit  string `json:"x-ogc-unit,omitempty"`
}

// Fields renvoie les champs de la collection — pendant Go de get_fields.
//
// Divergence assumée avec pygeoapi : pygeoapi *ignore* les variables sans
// attribut `units`. Comme xarray-go ne porte pas toujours cette métadonnée,
// gocoverage conserve la variable avec une unité vide plutôt que de la masquer.
func (c *Collection) Fields() []Field {
	names := c.Data.VarNames()
	out := make([]Field, 0, len(names))
	for _, name := range names {
		da, err := c.Data.Get(name)
		if err != nil {
			continue
		}
		attrs := da.Variable().Attrs()
		out = append(out, Field{
			Name:  name,
			Type:  fieldTypeFromAttrs(attrs),
			Title: attrs["long_name"],
			Unit:  attrs["units"],
		})
	}
	return out
}

// fieldTypeFromAttrs déduit le type d'un champ. Le stockage est float64, mais un
// jeu de données peut déclarer un type logique via l'attribut `dtype`
// (remarque J : ne pas forcer « float » pour des champs entiers/catégoriels).
func fieldTypeFromAttrs(attrs map[string]string) string {
	switch strings.ToLower(strings.TrimSpace(attrs["dtype"])) {
	case "int", "integer", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return "integer"
	case "str", "string", "category", "categorical":
		return "string"
	default:
		return "float"
	}
}

// fieldsMap indexe les champs par nom.
func (c *Collection) fieldsMap() map[string]Field {
	m := map[string]Field{}
	for _, f := range c.Fields() {
		m[f.Name] = f
	}
	return m
}

// CoverageProperties reproduit _get_coverage_properties de pygeoapi : emprise,
// libellés d'axes, dimensions de grille, résolution et étendue temporelle.
type CoverageProperties struct {
	BBox       [4]float64  `json:"bbox"`
	BBoxCRS    string      `json:"bbox_crs"`
	CRSType    string      `json:"crs_type"`
	XAxisLabel string      `json:"x_axis_label"`
	YAxisLabel string      `json:"y_axis_label"`
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	ResX       float64     `json:"resx"`
	ResY       float64     `json:"resy"`
	TimeAxis   string      `json:"time_axis_label,omitempty"`
	TimeRange  *[2]float64 `json:"time_range,omitempty"`
	TimeSteps  int         `json:"time,omitempty"`
	// Résolution et durée temporelles en ISO 8601 (pendants de
	// get_time_resolution / get_time_coverage_duration de pygeoapi), renseignées
	// quand l'axe temporel est en secondes epoch.
	TimeResolution string   `json:"restime,omitempty"`
	TimeDuration   string   `json:"time_duration,omitempty"`
	Axes           []string `json:"axes"`
}

// Properties calcule les propriétés de couverture de la collection.
func (c *Collection) Properties() CoverageProperties {
	xs, _ := c.Data.Coord(c.XDim)
	ys, _ := c.Data.Coord(c.YDim)
	p := CoverageProperties{
		BBox:       [4]float64{xs[0], ys[0], xs[len(xs)-1], ys[len(ys)-1]},
		BBoxCRS:    c.CRS.id(),
		CRSType:    c.CRS.typ(),
		XAxisLabel: c.XDim,
		YAxisLabel: c.YDim,
		Width:      len(xs),
		Height:     len(ys),
		Axes:       []string{c.XDim, c.YDim},
	}
	if len(xs) > 1 {
		p.ResX = absf(xs[1] - xs[0])
	}
	if len(ys) > 1 {
		p.ResY = absf(ys[1] - ys[0])
	}
	if ext, ok := c.TimeExtent(); ok {
		ts, _ := c.Data.Coord(c.TDim)
		p.TimeAxis = c.TDim
		p.TimeRange = &ext
		p.TimeSteps = len(ts)
		p.Axes = append(p.Axes, c.TDim)
		// Résolution/durée en ISO 8601 uniquement si le temps est en secondes epoch.
		if c.timeIsEpoch(ts) {
			if len(ts) > 1 {
				p.TimeResolution = iso8601Duration(ts[1] - ts[0])
			}
			p.TimeDuration = iso8601Duration(ts[len(ts)-1] - ts[0])
		}
	}
	return p
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// iso8601Duration formate un nombre de secondes en durée ISO 8601
// (ex. 86400 → "P1D", 21600 → "PT6H", 90 → "PT1M30S"). 0 → "PT0S".
func iso8601Duration(sec float64) string {
	if sec < 0 {
		sec = -sec
	}
	total := int64(sec + 0.5)
	if total == 0 {
		return "PT0S"
	}
	days := total / 86400
	total %= 86400
	h := total / 3600
	total %= 3600
	m := total / 60
	s := total % 60

	var b strings.Builder
	b.WriteByte('P')
	if days > 0 {
		fmt.Fprintf(&b, "%dD", days)
	}
	if h > 0 || m > 0 || s > 0 {
		b.WriteByte('T')
		if h > 0 {
			fmt.Fprintf(&b, "%dH", h)
		}
		if m > 0 {
			fmt.Fprintf(&b, "%dM", m)
		}
		if s > 0 {
			fmt.Fprintf(&b, "%dS", s)
		}
	}
	return b.String()
}
