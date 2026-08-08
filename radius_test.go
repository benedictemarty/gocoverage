package gocoverage

import (
	"math"
	"net/url"
	"testing"
)

func TestLengthMeters(t *testing.T) {
	if m, metric, _ := lengthMeters(2, ""); metric || m != 0 {
		t.Errorf("degrés : metric=%v m=%v", metric, m)
	}
	if m, metric, _ := lengthMeters(5, "km"); !metric || math.Abs(m-5000) > 1e-6 {
		t.Errorf("5 km = %v m (metric=%v)", m, metric)
	}
	if m, metric, _ := lengthMeters(250, "m"); !metric || m != 250 {
		t.Errorf("250 m = %v (metric=%v)", m, metric)
	}
	if _, _, err := lengthMeters(1, "lightyears"); err == nil {
		t.Error("erreur attendue : unité inconnue")
	}
}

// TestRadiusMetric : un rayon métrique masque par distance réelle (mètres).
func TestRadiusMetric(t *testing.T) {
	c := gridColl(t, 7) // lon 0..6, lat 6..0, pas 1°
	// ~250 km ≈ 2,25° : la bbox capte les coins (dist ≈ 314 km > 250) -> exclus.
	res, err := c.Radius(3, 3, 250, "km", EDRParams{})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := res.Get("v")
	inside, outside := 0, 0
	for _, x := range v.Data() {
		if math.IsNaN(x) {
			outside++
		} else {
			inside++
		}
	}
	if inside == 0 || outside == 0 {
		t.Errorf("disque métrique devrait partitionner : %d dedans, %d hors", inside, outside)
	}
	// Unité inconnue -> erreur.
	if _, err := c.Radius(3, 3, 10, "furlongs", EDRParams{}); err == nil {
		t.Error("erreur attendue : unité inconnue")
	}
}

// TestRadiusMask : un disque plus petit que sa bbox laisse les coins hors (NaN).
func TestRadiusMask(t *testing.T) {
	c := gridColl(t, 7) // lon 0..6, lat 6..0 (helper de corridor_test)
	// centre (3,3), rayon 2 : bbox [1,5]² = 25 cellules ; coins (1,1)/(5,5)…
	// à distance 2.83 > 2 -> exclus.
	res, err := c.Radius(3, 3, 2, "", EDRParams{})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := res.Get("v")
	inside, outside := 0, 0
	for _, x := range v.Data() {
		if math.IsNaN(x) {
			outside++
		} else {
			inside++
		}
	}
	if inside == 0 || outside == 0 {
		t.Errorf("le disque devrait exclure les coins : %d dedans, %d hors", inside, outside)
	}
	// Rayon nul -> erreur.
	if _, err := c.Radius(3, 3, 0, "", EDRParams{}); err == nil {
		t.Error("erreur attendue : rayon nul")
	}
}

func TestParsePoint(t *testing.T) {
	if lon, lat, err := parsePoint("POINT(2 48)"); err != nil || lon != 2 || lat != 48 {
		t.Errorf("WKT POINT = %v,%v,%v", lon, lat, err)
	}
	if lon, lat, err := parsePoint("2,48"); err != nil || lon != 2 || lat != 48 {
		t.Errorf("repli = %v,%v,%v", lon, lat, err)
	}
	if _, _, err := parsePoint("nope"); err == nil {
		t.Error("erreur attendue : point invalide")
	}
}

// TestRadiusHTTP : endpoint EDR radius (+ within-units km).
func TestRadiusHTTP(t *testing.T) {
	p := NewMemProvider()
	if err := p.Add(gridColl(t, 7)); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(p)
	u := "/collections/c/radius?coords=" + url.QueryEscape("POINT(3 3)") + "&within=200&within-units=km"
	if rec := doGET(t, srv, u); rec.Code != 200 {
		t.Fatalf("code %d : %s", rec.Code, rec.Body.String())
	}
	// within manquant -> 400.
	if bad := doGET(t, srv, "/collections/c/radius?coords="+url.QueryEscape("POINT(3 3)")); bad.Code != 400 {
		t.Errorf("sans within : code %d, attendu 400", bad.Code)
	}
}
