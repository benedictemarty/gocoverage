package gocoverage

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
			Type:  "float", // Dataset[float64]
			Title: attrs["long_name"],
			Unit:  attrs["units"],
		})
	}
	return out
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
	Axes       []string    `json:"axes"`
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
	}
	return p
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
