package gocoverage

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestConformance vérifie que /conformance déclare les classes OGC API - Coverages.
func TestConformance(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/conformance")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc struct {
		ConformsTo []string `json:"conformsTo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	want := "http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/core"
	found := false
	for _, c := range doc.ConformsTo {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Errorf("classe %q absente de conformsTo=%v", want, doc.ConformsTo)
	}
}

// TestLandingConformanceLink : la page d'accueil pointe vers /conformance.
func TestLandingConformanceLink(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/")
	if !strings.Contains(rec.Body.String(), "/conformance") {
		t.Errorf("lien conformance absent de la landing page: %s", rec.Body.String())
	}
}

// TestDomainSet vérifie la description du domaine (axes réguliers x/y).
func TestDomainSet(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage/domainset")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc["type"] != "DomainSet" {
		t.Fatalf("type=%v, attendu DomainSet", doc["type"])
	}
	grid := doc["generalGrid"].(map[string]interface{})
	labels := grid["axisLabels"].([]interface{})
	if len(labels) != 2 || labels[0] != "x" || labels[1] != "y" {
		t.Errorf("axisLabels=%v, attendu [x y]", labels)
	}
	axes := grid["axis"].([]interface{})
	x := axes[0].(map[string]interface{})
	// longitude {0,1,2,3} : borne 0→3, résolution 1.
	if x["lowerBound"].(float64) != 0 || x["upperBound"].(float64) != 3 || x["resolution"].(float64) != 1 {
		t.Errorf("axe x=%v, attendu lower=0 upper=3 res=1", x)
	}
}

// TestRangeType vérifie la description des champs (SWE DataRecord + unités).
func TestRangeType(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage/rangetype")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc["type"] != "DataRecord" {
		t.Fatalf("type=%v, attendu DataRecord", doc["type"])
	}
	fields := doc["field"].([]interface{})
	units := map[string]string{}
	for _, f := range fields {
		fm := f.(map[string]interface{})
		q := fm["quantity"].(map[string]interface{})
		if uom, ok := q["uom"].(map[string]interface{}); ok {
			units[fm["name"].(string)] = uom["code"].(string)
		}
	}
	if units["t2m"] != "K" || units["uwind"] != "m/s" {
		t.Errorf("unités=%v, attendu t2m=K uwind=m/s", units)
	}
}

// TestCoverageScaling vérifie le sous-échantillonnage par scale-factor.
func TestCoverageScaling(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// Grille 3×4 (lat×lon). scale-factor=2 → environ moitié de résolution.
	rec := doGET(t, srv, "/collections/demo/coverage?scale-factor=2")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	axes := doc["domain"].(map[string]interface{})["axes"].(map[string]interface{})
	xnum := int(axes["x"].(map[string]interface{})["num"].(float64))
	ynum := int(axes["y"].(map[string]interface{})["num"].(float64))
	// La grille scalée doit être strictement plus petite que 4×3.
	if xnum >= 4 || ynum >= 3 {
		t.Errorf("grille scalée x=%d y=%d, attendu plus petit que 4×3", xnum, ynum)
	}
}

// TestScaleAxes vérifie scale-axes(Long(n)) : scaling par axe uniquement.
func TestScaleAxes(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?scale-axes=Long(2)")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	axes := doc["domain"].(map[string]interface{})["axes"].(map[string]interface{})
	xnum := int(axes["x"].(map[string]interface{})["num"].(float64))
	ynum := int(axes["y"].(map[string]interface{})["num"].(float64))
	if xnum >= 4 {
		t.Errorf("x=%d, attendu réduit par scale-axes Long(2)", xnum)
	}
	if ynum != 3 {
		t.Errorf("y=%d, attendu inchangé (3) — scaling limité à Long", ynum)
	}
}

// TestScaleFactorInvalid : scale-factor non entier → 400.
func TestScaleFactorInvalid(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?scale-factor=abc")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 pour scale-factor invalide", rec.Code)
	}
}
