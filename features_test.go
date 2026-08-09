package gocoverage

import (
	"encoding/json"
	"testing"
)

// featureCollection décode une réponse /items.
type featureCollection struct {
	Type           string `json:"type"`
	NumberMatched  int    `json:"numberMatched"`
	NumberReturned int    `json:"numberReturned"`
	Features       []struct {
		ID       int `json:"id"`
		Geometry struct {
			Type        string    `json:"type"`
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties map[string]interface{} `json:"properties"`
	} `json:"features"`
	Links []struct {
		Rel  string `json:"rel"`
		Href string `json:"href"`
	} `json:"links"`
}

// TestItemsAll : /items énumère les mailles (démo = 3×4 = 12 cellules non vides).
func TestItemsAll(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/items?limit=100")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/geo+json" {
		t.Fatalf("content-type = %q", ct)
	}
	var fc featureCollection
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatal(err)
	}
	if fc.Type != "FeatureCollection" {
		t.Fatalf("type = %q", fc.Type)
	}
	if fc.NumberMatched != 12 || fc.NumberReturned != 12 {
		t.Fatalf("matched=%d returned=%d, attendu 12/12", fc.NumberMatched, fc.NumberReturned)
	}
	// La première maille (iy=0, ix=0) : coin haut-gauche (lon 0, lat 45), t2m=0.
	f0 := fc.Features[0]
	if f0.ID != 0 || f0.Geometry.Coordinates[0] != 0 || f0.Geometry.Coordinates[1] != 45 {
		t.Fatalf("feature 0 = id %d coords %v", f0.ID, f0.Geometry.Coordinates)
	}
}

// TestItemsPagination : limit/offset paginent et exposent les liens next/prev.
func TestItemsPagination(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/items?limit=5&offset=5")
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.NumberMatched != 12 || fc.NumberReturned != 5 {
		t.Fatalf("matched=%d returned=%d, attendu 12/5", fc.NumberMatched, fc.NumberReturned)
	}
	if fc.Features[0].ID != 5 {
		t.Fatalf("première entité id = %d, attendu 5", fc.Features[0].ID)
	}
	hasNext, hasPrev := false, false
	for _, l := range fc.Links {
		if l.Rel == "next" {
			hasNext = true
		}
		if l.Rel == "prev" {
			hasPrev = true
		}
	}
	if !hasNext || !hasPrev {
		t.Fatalf("liens next=%v prev=%v, attendu les deux", hasNext, hasPrev)
	}
}

// TestItemsBBox : le filtre bbox restreint les entités.
func TestItemsBBox(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// bbox lon [0,1], lat [44,45] → colonnes 0,1 × lignes 45,44 = 4 mailles.
	rec := doGET(t, srv, "/collections/demo/items?bbox=0,44,1,45&limit=100")
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.NumberMatched != 4 {
		t.Fatalf("matched = %d, attendu 4", fc.NumberMatched)
	}
}

// TestItemByID : /items/{fid} renvoie la maille attendue ; hors grille → 404.
func TestItemByID(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// fid = iy*nx+ix ; nx=4. Maille (iy=2, ix=1) → fid=9, lon=1, lat=43, t2m=21.
	rec := doGET(t, srv, "/collections/demo/items/9")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	var f struct {
		ID       int `json:"id"`
		Geometry struct {
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties map[string]interface{} `json:"properties"`
	}
	json.Unmarshal(rec.Body.Bytes(), &f)
	if f.ID != 9 || f.Geometry.Coordinates[0] != 1 || f.Geometry.Coordinates[1] != 43 {
		t.Fatalf("feature = id %d coords %v", f.ID, f.Geometry.Coordinates)
	}
	if v, _ := f.Properties["t2m"].(float64); v != 21 {
		t.Fatalf("t2m = %v, attendu 21", f.Properties["t2m"])
	}
	if rec := doGET(t, srv, "/collections/demo/items/9999"); rec.Code != 404 {
		t.Fatalf("hors grille : code = %d, attendu 404", rec.Code)
	}
	if rec := doGET(t, srv, "/collections/demo/items/abc"); rec.Code != 400 {
		t.Fatalf("id non entier : code = %d, attendu 400", rec.Code)
	}
}

// TestItemsProperties : properties sélectionne les variables exposées.
func TestItemsProperties(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/items?properties=t2m&limit=1")
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if len(fc.Features) != 1 {
		t.Fatalf("returned = %d", len(fc.Features))
	}
	props := fc.Features[0].Properties
	if _, ok := props["t2m"]; !ok {
		t.Fatal("t2m absent")
	}
	if _, ok := props["uwind"]; ok {
		t.Fatal("uwind présent alors que non demandé")
	}
}

// TestItemsBadParams : paramètres de pagination invalides → 400.
func TestItemsBadParams(t *testing.T) {
	srv := NewServer(demoProvider(t))
	for _, p := range []string{
		"/collections/demo/items?limit=0",
		"/collections/demo/items?offset=-1",
		"/collections/demo/items?bbox=1,2,3",
	} {
		if rec := doGET(t, srv, p); rec.Code != 400 {
			t.Errorf("%s : code = %d, attendu 400", p, rec.Code)
		}
	}
}

// TestItemsLazyPruned : sur une collection élaguée (Window), /items ne lit que
// les chunks recouvrant la bbox et conserve l'identifiant absolu.
func TestItemsLazyPruned(t *testing.T) {
	dir := t.TempDir() + "/g"
	writeChunkedZarr(t, dir) // grille 4×4, chunks 2×2, valeur = latidx*4 + lonidx
	r, err := OpenZarrWindow(dir, "longitude", "latitude")
	if err != nil {
		t.Fatal(err)
	}
	c := &Collection{
		ID: "c", XDim: "longitude", YDim: "latitude", Window: r.ReadWindow,
		coordHint: map[string][]float64{"longitude": {0, 1, 2, 3}, "latitude": {4, 3, 2, 1}},
	}

	// bbox = coin haut-gauche → un seul chunk (2×2).
	features, matched, err := c.Items(ItemsParams{BBox: &[4]float64{0, 3, 1, 4}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if r.ChunksRead() != 1 {
		t.Errorf("ChunksRead = %d, attendu 1 (élagage)", r.ChunksRead())
	}
	if c.Data != nil {
		t.Error("la grille complète ne doit pas être matérialisée")
	}
	if matched != 4 || len(features) != 4 {
		t.Fatalf("matched=%d returned=%d, attendu 4/4", matched, len(features))
	}
	// Maille lon1/lat3 → id absolu = iy(1)·nx(4) + ix(1) = 5, valeur = 5.
	var found bool
	for _, f := range features {
		if f["id"] == 5 {
			found = true
			if v, _ := f["properties"].(map[string]interface{})["t2m"].(float64); v != 5 {
				t.Errorf("t2m(id=5) = %v, attendu 5", v)
			}
		}
	}
	if !found {
		t.Fatal("entité id=5 absente")
	}

	// Item(15) : maille lon3/lat1 → bbox ponctuelle → un seul chunk lu
	// (ChunksRead reflète le dernier ReadWindow).
	f2, err := c.Item(15, ItemsParams{})
	if err != nil {
		t.Fatalf("Item(15): %v", err)
	}
	if f2["id"] != 15 {
		t.Errorf("Item(15).id = %v, attendu 15", f2["id"])
	}
	if v, _ := f2["properties"].(map[string]interface{})["t2m"].(float64); v != 15 {
		t.Errorf("t2m(id=15) = %v, attendu 15", v)
	}
	if r.ChunksRead() != 1 {
		t.Errorf("chunks lus par Item = %d, attendu 1 (élagage ponctuel)", r.ChunksRead())
	}
}

// TestConformanceFeatures : la classe Features core est annoncée.
func TestConformanceFeatures(t *testing.T) {
	found := false
	for _, cl := range conformanceClasses() {
		if cl == "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core" {
			found = true
		}
	}
	if !found {
		t.Fatal("classe de conformité Features core absente")
	}
}

// TestItemsSortByDesc : sortby=-t2m ordonne les entités par t2m décroissant.
func TestItemsSortByDesc(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/items?sortby=-t2m&limit=100")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.NumberReturned != 12 {
		t.Fatalf("returned = %d, attendu 12", fc.NumberReturned)
	}
	if v, _ := fc.Features[0].Properties["t2m"].(float64); v != 23 {
		t.Fatalf("première valeur = %v, attendu 23 (max)", v)
	}
	prev := 1e9
	for _, f := range fc.Features {
		v, _ := f.Properties["t2m"].(float64)
		if v > prev {
			t.Fatalf("tri décroissant rompu : %v après %v", v, prev)
		}
		prev = v
	}
}

// TestItemsSortByAsc : sortby=t2m (asc) + limit → N plus petites valeurs.
func TestItemsSortByAsc(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/items?sortby=t2m&limit=3")
	var fc featureCollection
	json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.NumberMatched != 12 || fc.NumberReturned != 3 {
		t.Fatalf("matched=%d returned=%d, attendu 12/3", fc.NumberMatched, fc.NumberReturned)
	}
	got := []float64{}
	for _, f := range fc.Features {
		v, _ := f.Properties["t2m"].(float64)
		got = append(got, v)
	}
	if got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("trois plus petites = %v, attendu [0 1 2]", got)
	}
}

// TestItemsSortPagination : le tri est cohérent avec offset (page 2 suit page 1).
func TestItemsSortPagination(t *testing.T) {
	srv := NewServer(demoProvider(t))
	var p1, p2 featureCollection
	json.Unmarshal(doGET(t, srv, "/collections/demo/items?sortby=-t2m&limit=3&offset=0").Body.Bytes(), &p1)
	json.Unmarshal(doGET(t, srv, "/collections/demo/items?sortby=-t2m&limit=3&offset=3").Body.Bytes(), &p2)
	last1, _ := p1.Features[2].Properties["t2m"].(float64)
	first2, _ := p2.Features[0].Properties["t2m"].(float64)
	if first2 > last1 {
		t.Fatalf("page 2 (%.0f) devrait suivre page 1 (%.0f)", first2, last1)
	}
}

// TestParseSortBy : analyse des critères de tri.
func TestParseSortBy(t *testing.T) {
	keys := parseSortBy("-t2m, +uwind , name")
	if len(keys) != 3 {
		t.Fatalf("keys = %d, attendu 3", len(keys))
	}
	if !keys[0].desc || keys[0].prop != "t2m" {
		t.Errorf("clé 0 = %+v", keys[0])
	}
	if keys[1].desc || keys[1].prop != "uwind" {
		t.Errorf("clé 1 = %+v", keys[1])
	}
	if keys[2].prop != "name" {
		t.Errorf("clé 2 = %+v", keys[2])
	}
	if parseSortBy("") != nil {
		t.Error("sortby vide devrait donner nil")
	}
}
