package gocoverage

import (
	"encoding/json"
	"fmt"
)

// Requête EDR « locations » : liste de points nommés prédéfinis (ex. aéroports,
// stations) et extraction de la donnée à l'un d'eux. Utile en aviation pour
// interroger la météo à un aérodrome par son code plutôt que par coordonnées.

// LocationsGeoJSON renvoie la liste des locations de la collection sous forme de
// FeatureCollection GeoJSON (chaque Feature = un point nommé).
func (c *Collection) LocationsGeoJSON() ([]byte, error) {
	features := make([]map[string]interface{}, 0, len(c.Locations))
	for _, loc := range c.Locations {
		features = append(features, map[string]interface{}{
			"type": "Feature",
			"id":   loc.ID,
			"geometry": map[string]interface{}{
				"type":        "Point",
				"coordinates": []float64{loc.Lon, loc.Lat},
			},
			"properties": map[string]interface{}{"name": nameOr(loc.Name, loc.ID)},
		})
	}
	return json.MarshalIndent(map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	}, "", "  ")
}

// LocationByID retrouve une location nommée par son identifiant.
func (c *Collection) LocationByID(id string) (NamedLocation, bool) {
	for _, loc := range c.Locations {
		if loc.ID == id {
			return loc, true
		}
	}
	return NamedLocation{}, false
}

// LocationCoverageJSON extrait la donnée au point nommé locID (au plus proche
// voisin, via Position) et la renvoie en CoverageJSON.
func (c *Collection) LocationCoverageJSON(locID string, p EDRParams) ([]byte, error) {
	loc, ok := c.LocationByID(locID)
	if !ok {
		return nil, fmt.Errorf("location inconnue: %s", locID)
	}
	ds, err := c.Position(loc.Lon, loc.Lat, p)
	if err != nil {
		return nil, err
	}
	return c.CoverageJSON(ds)
}

func nameOr(name, id string) string {
	if name != "" {
		return name
	}
	return id
}
