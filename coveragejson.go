package gocoverage

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/benedictemarty/xarray"
)

// ErrSelectLevel signale qu'un export CoverageJSON est impossible tant qu'un
// niveau vertical unique n'a pas été sélectionné (paramètre EDR z=…). C'est une
// erreur corrigible par le client : le serveur la traduit en HTTP 400.
var ErrSelectLevel = errors.New("sélection d'un niveau vertical requise")

// Structures CoverageJSON reproduisant fidèlement gen_covjson de pygeoapi
// (XarrayProvider) : domaine Grid (axes réguliers start/stop/num) ou PointSeries
// (1×1), axe temporel optionnel, paramètres multiples.

type covJSON struct {
	Type       string              `json:"type"` // "Coverage"
	Domain     covDomain           `json:"domain"`
	Parameters map[string]covParam `json:"parameters"`
	Ranges     map[string]covNdArr `json:"ranges"`
}

type covDomain struct {
	Type        string                 `json:"type"`       // "Domain"
	DomainType  string                 `json:"domainType"` // "Grid" | "PointSeries"
	Axes        map[string]interface{} `json:"axes"`
	Referencing []covReferencing       `json:"referencing"`
}

// covRegularAxis : axe régulier {start, stop, num} (cas Grid).
type covRegularAxis struct {
	Start float64 `json:"start"`
	Stop  float64 `json:"stop"`
	Num   int     `json:"num"`
}

// covValuesAxis : axe par valeurs explicites (cas PointSeries et axe temporel).
type covValuesAxis struct {
	Values []interface{} `json:"values"`
}

type covReferencing struct {
	Coordinates []string  `json:"coordinates"`
	System      covSystem `json:"system"`
}

type covSystem struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Calendar string `json:"calendar,omitempty"`
}

type covParam struct {
	ID               string      `json:"id"`
	Type             string      `json:"type"` // "Parameter"
	Name             string      `json:"name,omitempty"`
	ObservedProperty covObserved `json:"observedProperty"`
	Unit             *covUnit    `json:"unit,omitempty"`
}

type covObserved struct {
	ID    string            `json:"id"`
	Label map[string]string `json:"label"`
}

type covUnit struct {
	Symbol covSymbol `json:"symbol"`
}

type covSymbol struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}

type covNdArr struct {
	Type      string        `json:"type"` // "NdArray"
	DataType  string        `json:"dataType"`
	AxisNames []string      `json:"axisNames"`
	Shape     []int         `json:"shape"`
	Values    []interface{} `json:"values"` // null pour NaN
}

const (
	crs84   = "http://www.opengis.net/def/crs/OGC/1.3/CRS84"
	uomUCUM = "http://www.opengis.net/def/uom/UCUM/"
)

// CoverageJSON produit un document CoverageJSON à partir d'un Dataset — pendant
// Go de gen_covjson. Chaque variable devient un paramètre et une portée (range).
// Reproduit les conventions pygeoapi : axes réguliers, bascule PointSeries pour
// une grille 1×1, axe temporel en valeurs.
func (c *Collection) CoverageJSON(ds *xarray.Dataset[float64]) ([]byte, error) {
	names := ds.VarNames()
	if len(names) == 0 {
		return nil, fmt.Errorf("aucun paramètre à exporter")
	}
	// Le domaine Grid ne modélise que x/y/(t) : un axe vertical à plusieurs
	// niveaux n'est pas représentable. Sélectionner un niveau (paramètre z EDR).
	if c.ZDim != "" {
		if n, ok := ds.Dims()[c.ZDim]; ok && n > 1 {
			return nil, fmt.Errorf("axe vertical %q à %d niveaux non représentable en CoverageJSON : sélectionnez un niveau (z=…): %w", c.ZDim, n, ErrSelectLevel)
		}
	}

	xv, err := ds.Coord(c.XDim)
	if err != nil {
		return nil, fmt.Errorf("coordonnée X %q: %w", c.XDim, err)
	}
	yv, err := ds.Coord(c.YDim)
	if err != nil {
		return nil, fmt.Errorf("coordonnée Y %q: %w", c.YDim, err)
	}
	width, height := len(xv), len(yv)

	axes := map[string]interface{}{}
	domainType := "Grid"
	if width == 1 && height == 1 {
		// PointSeries : x et y donnés par leur unique valeur.
		domainType = "PointSeries"
		axes["x"] = covValuesAxis{Values: []interface{}{xv[0]}}
		axes["y"] = covValuesAxis{Values: []interface{}{yv[0]}}
	} else {
		// Grille régulière → axe {start, stop, num} ; sinon axe par valeurs
		// explicites (remarque C : ne pas prétendre un pas constant inexistant).
		axes["x"] = regularOrValuesAxis(xv)
		axes["y"] = regularOrValuesAxis(yv)
	}

	referencing := []covReferencing{{
		Coordinates: []string{"x", "y"},
		System:      covSystem{Type: c.CRS.typ(), ID: c.CRS.id()},
	}}

	// Axe temporel éventuel (valeurs sous forme de chaînes, comme pygeoapi).
	timeSteps := 0
	if c.TDim != "" {
		if tv, err := ds.Coord(c.TDim); err == nil && len(tv) > 0 {
			timeSteps = len(tv)
			iso := allEpochSeconds(tv)
			vals := make([]interface{}, len(tv))
			for i, t := range tv {
				if iso {
					vals[i] = time.Unix(int64(t), 0).UTC().Format(time.RFC3339)
				} else {
					vals[i] = strconv.FormatFloat(t, 'g', -1, 64)
				}
			}
			axes["t"] = covValuesAxis{Values: vals}
			referencing = append(referencing, covReferencing{
				Coordinates: []string{"t"},
				System:      covSystem{Type: "TemporalRS", Calendar: "Gregorian"},
			})
		}
	}

	params := map[string]covParam{}
	ranges := map[string]covNdArr{}
	fieldsByName := c.fieldsMap()
	for _, name := range names {
		da, err := ds.Get(name)
		if err != nil {
			return nil, err
		}
		f := fieldsByName[name]
		params[name] = covParam{
			ID:               name,
			Type:             "Parameter",
			Name:             f.Title,
			ObservedProperty: covObserved{ID: name, Label: map[string]string{"en": labelOr(f.Title, name)}},
			Unit:             unitOf(f.Unit),
		}

		axisNames := []string{"y", "x"}
		shape := []int{height, width}
		if timeSteps > 0 && da.HasDim(c.TDim) {
			axisNames = append([]string{"t"}, axisNames...)
			shape = append([]int{timeSteps}, shape...)
		}
		ranges[name] = covNdArr{
			Type:      "NdArray",
			DataType:  fieldType(f),
			AxisNames: axisNames,
			Shape:     shape,
			Values:    withNaNAsNull(da.Data()),
		}
	}

	doc := covJSON{
		Type: "Coverage",
		Domain: covDomain{
			Type:        "Domain",
			DomainType:  domainType,
			Axes:        axes,
			Referencing: referencing,
		},
		Parameters: params,
		Ranges:     ranges,
	}
	return json.MarshalIndent(doc, "", "  ")
}

// isRegularAxis indique si les valeurs sont régulièrement espacées (pas constant
// à une tolérance relative près). Sert à décider entre axe régulier
// {start, stop, num} et axe par valeurs explicites (remarque C).
func isRegularAxis(v []float64) bool {
	if len(v) < 3 {
		return true // 1 ou 2 points : toujours « réguliers »
	}
	step := v[1] - v[0]
	if step == 0 {
		return false
	}
	tol := math.Abs(step) * 1e-6
	for i := 2; i < len(v); i++ {
		if math.Abs((v[i]-v[i-1])-step) > tol {
			return false
		}
	}
	return true
}

// regularOrValuesAxis renvoie un axe régulier si le pas est constant, sinon un
// axe par valeurs explicites.
func regularOrValuesAxis(v []float64) interface{} {
	if isRegularAxis(v) {
		return covRegularAxis{Start: v[0], Stop: v[len(v)-1], Num: len(v)}
	}
	vals := make([]interface{}, len(v))
	for i, x := range v {
		vals[i] = x
	}
	return covValuesAxis{Values: vals}
}

// withNaNAsNull convertit un slice float64 en []interface{} avec null pour NaN
// (JSON ne représente pas NaN), comme le fait pygeoapi.
func withNaNAsNull(data []float64) []interface{} {
	out := make([]interface{}, len(data))
	for i, v := range data {
		if math.IsNaN(v) {
			out[i] = nil
		} else {
			out[i] = v
		}
	}
	return out
}

func unitOf(u string) *covUnit {
	if u == "" {
		return nil
	}
	return &covUnit{Symbol: covSymbol{Value: u, Type: uomUCUM}}
}

func labelOr(title, name string) string {
	if title != "" {
		return title
	}
	return name
}

func fieldType(f Field) string {
	if f.Type != "" {
		return f.Type
	}
	return "float"
}

// allEpochSeconds indique si toutes les valeurs temporelles tombent dans une
// plage plausible de secondes epoch (≈ 1973 à 2100) — auquel cas l'axe est
// formaté en ISO 8601 dans le CoverageJSON. Les axes temps synthétiques (petits
// entiers) restent numériques.
func allEpochSeconds(tv []float64) bool {
	const lo, hi = 1e8, 4.1e9
	for _, t := range tv {
		if t < lo || t > hi {
			return false
		}
	}
	return len(tv) > 0
}
