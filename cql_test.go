package gocoverage

import (
	"encoding/json"
	"net/url"
	"testing"
)

// TestCQLParseEval : analyse et évaluation d'expressions CQL2-text.
func TestCQLParseEval(t *testing.T) {
	props := map[string]interface{}{"t2m": 20.0, "uwind": 3.0, "name": "paris"}
	cases := []struct {
		expr string
		want bool
	}{
		{"t2m > 10", true},
		{"t2m > 20", false},
		{"t2m >= 20", true},
		{"t2m = 20", true},
		{"t2m <> 20", false},
		{"t2m != 21", true},
		{"t2m > 10 AND uwind < 5", true},
		{"t2m > 30 OR uwind < 5", true},
		{"t2m > 30 OR uwind > 5", false},
		{"(t2m = 20 OR t2m = 30) AND uwind = 3", true},
		{"name = 'paris'", true},
		{"name = 'lyon'", false},
	}
	for _, tc := range cases {
		e, err := ParseCQL2Text(tc.expr)
		if err != nil {
			t.Errorf("%q : erreur %v", tc.expr, err)
			continue
		}
		if got := e.eval(cqlFeat{props: props}); got != tc.want {
			t.Errorf("%q : eval = %v, attendu %v", tc.expr, got, tc.want)
		}
	}
}

// TestCQLAdvanced : opérateurs IN, LIKE, BETWEEN, IS NULL.
func TestCQLAdvanced(t *testing.T) {
	props := map[string]interface{}{"t2m": 20.0, "name": "paris", "miss": nil}
	cases := []struct {
		expr string
		want bool
	}{
		{"t2m IN (10, 20, 30)", true},
		{"t2m IN (10, 30)", false},
		{"t2m NOT IN (10, 30)", true},
		{"name IN ('lyon', 'paris')", true},
		{"name LIKE 'par%'", true},
		{"name LIKE 'p_ris'", true},
		{"name LIKE 'lyon%'", false},
		{"name NOT LIKE 'lyon%'", true},
		{"t2m BETWEEN 10 AND 30", true},
		{"t2m BETWEEN 21 AND 30", false},
		{"t2m NOT BETWEEN 21 AND 30", true},
		{"miss IS NULL", true},
		{"t2m IS NULL", false},
		{"t2m IS NOT NULL", true},
		{"absent IS NULL", true},
		{"t2m BETWEEN 10 AND 30 AND name LIKE 'par%'", true},
	}
	for _, tc := range cases {
		e, err := ParseCQL2Text(tc.expr)
		if err != nil {
			t.Errorf("%q : erreur %v", tc.expr, err)
			continue
		}
		if got := e.eval(cqlFeat{props: props}); got != tc.want {
			t.Errorf("%q : eval = %v, attendu %v", tc.expr, got, tc.want)
		}
	}
}

// TestCQLAdvancedErrors : formes avancées mal formées → erreur.
func TestCQLAdvancedErrors(t *testing.T) {
	for _, expr := range []string{
		"t2m IN 10", "t2m IN (10,", "t2m LIKE 5", "t2m BETWEEN 1", "t2m BETWEEN 1 5",
		"t2m IS", "t2m IS NOT", "NOT IS NULL",
	} {
		if _, err := ParseCQL2Text(expr); err == nil {
			t.Errorf("%q : attendu une erreur", expr)
		}
	}
}

// TestItemsFilterAdvanced : IN via HTTP restreint aux valeurs listées.
func TestItemsFilterAdvanced(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// t2m IN (0, 23) → deux mailles (coins).
	rec := doGET(t, srv, "/collections/demo/items?filter=t2m+IN+%280%2C+23%29&limit=100")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.NumberReturned != 2 {
		t.Fatalf("returned = %d, attendu 2", fc.NumberReturned)
	}
	for _, f := range fc.Features {
		v, _ := f.Properties["t2m"].(float64)
		if v != 0 && v != 23 {
			t.Errorf("t2m=%v hors de la liste (0, 23)", v)
		}
	}
}

// TestCQLSpatial : prédicats spatiaux point-dans-polygone.
func TestCQLSpatial(t *testing.T) {
	// Carré unité [0,1]×[0,1].
	poly := "S_INTERSECTS(geom, POLYGON((0 0, 1 0, 1 1, 0 1, 0 0)))"
	e, err := ParseCQL2Text(poly)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !e.eval(cqlFeat{lon: 0.5, lat: 0.5}) {
		t.Error("point (0.5,0.5) devrait être dans le carré")
	}
	if e.eval(cqlFeat{lon: 2, lat: 2}) {
		t.Error("point (2,2) ne devrait pas être dans le carré")
	}
	// S_DISJOINT = négation.
	ed, _ := ParseCQL2Text("S_DISJOINT(geom, POLYGON((0 0, 1 0, 1 1, 0 1, 0 0)))")
	if ed.eval(cqlFeat{lon: 0.5, lat: 0.5}) {
		t.Error("S_DISJOINT devrait être faux pour un point intérieur")
	}
	if !ed.eval(cqlFeat{lon: 2, lat: 2}) {
		t.Error("S_DISJOINT devrait être vrai pour un point extérieur")
	}
}

// TestCQLSpatialErrors : formes spatiales mal formées → erreur.
func TestCQLSpatialErrors(t *testing.T) {
	for _, expr := range []string{
		"S_INTERSECTS(geom)",
		"S_INTERSECTS(t2m, POLYGON((0 0,1 0,1 1,0 0)))",
		"S_INTERSECTS(geom, 5)",
		"S_INTERSECTS geom, POLYGON((0 0)))",
	} {
		if _, err := ParseCQL2Text(expr); err == nil {
			t.Errorf("%q : attendu une erreur", expr)
		}
	}
}

// TestItemsFilterSpatial : filtre spatial via HTTP restreint aux mailles dans le
// polygone.
func TestItemsFilterSpatial(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// Démo : lon 0..3, lat 43..45. Polygone couvrant lon[−0.5,1.5], lat[43.5,45.5]
	// → colonnes 0,1 × lignes lat 45,44 = 4 mailles.
	wkt := "S_INTERSECTS(geom, POLYGON((-0.5 43.5, 1.5 43.5, 1.5 45.5, -0.5 45.5, -0.5 43.5)))"
	rec := doGET(t, srv, "/collections/demo/items?limit=100&filter="+url.QueryEscape(wkt))
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.NumberReturned != 4 {
		t.Fatalf("returned = %d, attendu 4", fc.NumberReturned)
	}
	for _, f := range fc.Features {
		lon, lat := f.Geometry.Coordinates[0], f.Geometry.Coordinates[1]
		if lon < -0.5 || lon > 1.5 || lat < 43.5 || lat > 45.5 {
			t.Errorf("entité hors polygone: %v", f.Geometry.Coordinates)
		}
	}
}

// TestCQLParseErrors : expressions mal formées → erreur.
func TestCQLParseErrors(t *testing.T) {
	for _, expr := range []string{"t2m >", "t2m 20", "AND t2m = 1", "(t2m = 1", "t2m = 'x", "= 5"} {
		if _, err := ParseCQL2Text(expr); err == nil {
			t.Errorf("%q : attendu une erreur", expr)
		}
	}
}

// TestItemsFilter : le paramètre filter restreint les entités renvoyées.
func TestItemsFilter(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// t2m de la démo : 0..23 ; filtre t2m > 15 → mailles de valeur > 15.
	rec := doGET(t, srv, "/collections/demo/items?filter=t2m%20%3E%2015&limit=100")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.NumberReturned == 0 || fc.NumberReturned == 12 {
		t.Fatalf("returned = %d, attendu un sous-ensemble strict", fc.NumberReturned)
	}
	for _, f := range fc.Features {
		if v, _ := f.Properties["t2m"].(float64); v <= 15 {
			t.Errorf("entité avec t2m=%v ne satisfait pas t2m>15", v)
		}
	}
}

// TestItemsFilterCombined : AND combine deux conditions.
func TestItemsFilterCombined(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// t2m > 10 AND uwind < 8 (encodé).
	rec := doGET(t, srv, "/collections/demo/items?filter=t2m+%3E+10+AND+uwind+%3C+8&limit=100")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	for _, f := range fc.Features {
		tv, _ := f.Properties["t2m"].(float64)
		uv, _ := f.Properties["uwind"].(float64)
		if !(tv > 10 && uv < 8) {
			t.Errorf("entité t2m=%v uwind=%v ne satisfait pas le filtre", tv, uv)
		}
	}
}

// TestItemsFilterErrors : filtre invalide ou filter-lang non supporté → 400.
func TestItemsFilterErrors(t *testing.T) {
	srv := NewServer(demoProvider(t))
	if rec := doGET(t, srv, "/collections/demo/items?filter=t2m+%3E"); rec.Code != 400 {
		t.Errorf("filtre incomplet : code = %d, attendu 400", rec.Code)
	}
	if rec := doGET(t, srv, "/collections/demo/items?filter=t2m+%3E+1&filter-lang=cql2-json"); rec.Code != 400 {
		t.Errorf("filter-lang non supporté : code = %d, attendu 400", rec.Code)
	}
}

// TestQueryables : /queryables expose le schéma des propriétés.
func TestQueryables(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/queryables")
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var body struct {
		Type       string                            `json:"type"`
		Properties map[string]map[string]interface{} `json:"properties"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Type != "object" {
		t.Fatalf("type = %q", body.Type)
	}
	if _, ok := body.Properties["t2m"]; !ok {
		t.Fatal("propriété t2m absente des queryables")
	}
}

// TestConformanceFilter : la classe de filtrage CQL2 est annoncée.
func TestConformanceFilter(t *testing.T) {
	found := false
	for _, cl := range conformanceClasses() {
		if cl == "http://www.opengis.net/spec/cql2/1.0/conf/cql2-text" {
			found = true
		}
	}
	if !found {
		t.Fatal("classe de conformité cql2-text absente")
	}
}
