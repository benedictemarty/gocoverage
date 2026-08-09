package gocoverage

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// TestMapRenderPNG : /map produit une image PNG aux dimensions demandées.
func TestMapRenderPNG(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/map?width=40&height=30&properties=t2m")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q", ct)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("décodage png: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Fatalf("dimensions = %dx%d, attendu 40x30", b.Dx(), b.Dy())
	}
}

// TestMapDefaultSize : sans width/height, l'image adopte 512 × rapport d'emprise.
func TestMapDefaultSize(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/map")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("décodage png: %v", err)
	}
	// bbox démo = [0,43,3,45] → dlon=3, dlat=2 → hauteur = 512*2/3 ≈ 341.
	if b := img.Bounds(); b.Dx() != 512 || b.Dy() != 341 {
		t.Fatalf("dimensions par défaut = %dx%d, attendu 512x341", b.Dx(), b.Dy())
	}
}

// TestMapNoDataTransparent : hors emprise source → pixels transparents.
func TestMapNoDataTransparent(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// bbox largement au sud de la collection (43..45) : aucune donnée.
	rec := doGET(t, srv, "/collections/demo/map?bbox=0,0,3,5&width=10&height=10")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	img, _ := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("type image inattendu %T", img)
	}
	for _, a := range nrgba.Pix[3:4] { // premier pixel alpha
		_ = a
	}
	// Tous les pixels doivent être transparents.
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if _, _, _, a := nrgba.At(x, y).RGBA(); a != 0 {
				t.Fatalf("pixel (%d,%d) opaque (alpha=%d), attendu transparent", x, y, a)
			}
		}
	}
}

// TestMapColorScaleRange : colorscalerange fixe les bornes de la rampe ; deux
// valeurs extrêmes doivent produire des couleurs distinctes.
func TestMapColorScaleRange(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/map?width=4&height=3&properties=t2m&colorscalerange=0,23")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	img, _ := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	// coin haut-gauche (val 0) vs bas-droite (val 23) → couleurs différentes.
	r0, g0, b0, _ := img.At(0, 0).RGBA()
	r1, g1, b1, _ := img.At(3, 2).RGBA()
	if r0 == r1 && g0 == g1 && b0 == b1 {
		t.Fatalf("couleurs identiques aux extrêmes de la rampe")
	}
}

// TestMapBadParams : paramètres invalides → 400.
func TestMapBadParams(t *testing.T) {
	srv := NewServer(demoProvider(t))
	cases := []string{
		"/collections/demo/map?bbox=1,2,3", // 3 valeurs
		"/collections/demo/map?width=0",
		"/collections/demo/map?colorscalerange=5,5",
		"/collections/demo/map?f=gif",
	}
	for _, p := range cases {
		if rec := doGET(t, srv, p); rec.Code != 400 {
			t.Errorf("%s : code = %d, attendu 400", p, rec.Code)
		}
	}
}

// TestMapBilinear : interpolation=bilinear produit une image lissée valide.
func TestMapBilinear(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/map?width=40&height=30&properties=t2m&interpolation=bilinear")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("décodage png: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Fatalf("dimensions = %dx%d, attendu 40x30", b.Dx(), b.Dy())
	}
}

// TestBracketAxis : encadrement d'une cible sur axe croissant et décroissant.
func TestBracketAxis(t *testing.T) {
	asc := []float64{0, 1, 2, 3}
	b := bracketAxis(asc, 1.5)
	if !b.ok || b.i0 != 1 || b.i1 != 2 || b.f < 0.49 || b.f > 0.51 {
		t.Fatalf("asc: %+v", b)
	}
	desc := []float64{45, 44, 43}
	b = bracketAxis(desc, 43.5)
	if !b.ok || b.i0 != 1 || b.i1 != 2 || b.f < 0.49 || b.f > 0.51 {
		t.Fatalf("desc: %+v", b)
	}
	if bracketAxis(asc, 9).ok {
		t.Fatal("cible hors axe devrait être ok=false")
	}
}

// TestPalettes : chaque palette nommée rend une image valide.
func TestPalettes(t *testing.T) {
	srv := NewServer(demoProvider(t))
	for _, p := range []string{"viridis", "plasma", "magma", "inferno", "turbo", "coolwarm", "grayscale"} {
		rec := doGET(t, srv, "/collections/demo/map?width=8&height=6&properties=t2m&palette="+p)
		if rec.Code != 200 {
			t.Errorf("palette %s : code = %d", p, rec.Code)
			continue
		}
		if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
			t.Errorf("palette %s : png invalide (%v)", p, err)
		}
	}
}

// TestConformanceMaps : la classe Maps core est annoncée.
func TestConformanceMaps(t *testing.T) {
	found := false
	for _, cl := range conformanceClasses() {
		if cl == "http://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/core" {
			found = true
		}
	}
	if !found {
		t.Fatal("classe de conformité Maps core absente")
	}
}
