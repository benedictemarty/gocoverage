package gocoverage

import (
	"encoding/json"
	"fmt"
	"math"
)

// Requête EDR « trajectory » : échantillonne les paramètres le long d'une
// polyligne (points lon/lat), au plus proche voisin. Sortie CoverageJSON de
// domaine Trajectory (axe « composite » de tuples de coordonnées). Utile pour
// extraire un profil météo le long d'une route (ex. aviation).

// covCompositeAxis : axe « composite » d'un domaine Trajectory — chaque valeur
// est un tuple de coordonnées ([x, y]).
type covCompositeAxis struct {
	DataType    string          `json:"dataType"` // "tuple"
	Coordinates []string        `json:"coordinates"`
	Values      [][]interface{} `json:"values"`
}

// Trajectory échantillonne chaque paramètre le long de la polyligne coords
// (points {lon, lat}), au plus proche voisin, en appliquant p (datetime/z).
// Renvoie une valeur par point et par paramètre (la première valeur si l'axe
// temps/vertical n'est pas entièrement réduit par p).
func (c *Collection) Trajectory(coords [][2]float64, p EDRParams) (map[string][]float64, []string, error) {
	if len(coords) < 2 {
		return nil, nil, fmt.Errorf("trajectory: au moins 2 points requis")
	}
	values := map[string][]float64{}
	var names []string
	for idx, xy := range coords {
		ds, err := c.Position(xy[0], xy[1], p)
		if err != nil {
			return nil, nil, err
		}
		if idx == 0 {
			names = ds.VarNames()
		}
		for _, name := range names {
			v, err := ds.Get(name)
			val := math.NaN()
			if err == nil && len(v.Data()) > 0 {
				val = v.Data()[0]
			}
			values[name] = append(values[name], val)
		}
	}
	return values, names, nil
}

// TrajectoryCoverageJSON produit un CoverageJSON de domaine Trajectory le long de
// la polyligne coords.
func (c *Collection) TrajectoryCoverageJSON(coords [][2]float64, p EDRParams) ([]byte, error) {
	values, names, err := c.Trajectory(coords, p)
	if err != nil {
		return nil, err
	}
	comp := make([][]interface{}, len(coords))
	for i, xy := range coords {
		comp[i] = []interface{}{xy[0], xy[1]}
	}
	axes := map[string]interface{}{
		"composite": covCompositeAxis{DataType: "tuple", Coordinates: []string{"x", "y"}, Values: comp},
	}
	referencing := []covReferencing{{
		Coordinates: []string{"x", "y"},
		System:      covSystem{Type: c.CRS.typ(), ID: c.CRS.id()},
	}}
	params := map[string]covParam{}
	ranges := map[string]covNdArr{}
	fields := c.fieldsMap()
	for _, name := range names {
		f := fields[name]
		params[name] = covParam{
			ID: name, Type: "Parameter", Name: f.Title,
			ObservedProperty: covObserved{ID: name, Label: map[string]string{"en": labelOr(f.Title, name)}},
			Unit:             unitOf(f.Unit),
		}
		ranges[name] = covNdArr{
			Type: "NdArray", DataType: fieldType(f),
			AxisNames: []string{"composite"}, Shape: []int{len(coords)},
			Values: withNaNAsNull(values[name]),
		}
	}
	doc := covJSON{
		Type:       "Coverage",
		Domain:     covDomain{Type: "Domain", DomainType: "Trajectory", Axes: axes, Referencing: referencing},
		Parameters: params,
		Ranges:     ranges,
	}
	return json.MarshalIndent(doc, "", "  ")
}
