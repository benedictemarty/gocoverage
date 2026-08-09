package gocoverage

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// getHTML effectue un GET avec l'en-tête Accept: text/html.
func getHTML(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// TestHTMLNegotiation : f=html et Accept:text/html renvoient du HTML ; sinon JSON.
func TestHTMLNegotiation(t *testing.T) {
	srv := NewServer(demoProvider(t))

	// Accept: text/html → HTML.
	rec := getHTML(t, srv, "/collections")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, attendu text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<table") {
		t.Error("table des collections absente")
	}

	// f=html explicite → HTML.
	if rec := doGET(t, srv, "/collections?f=html"); !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Error("f=html devrait produire du HTML")
	}

	// Sans Accept ni f → JSON (comportement inchangé).
	rec = doGET(t, srv, "/collections")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type par défaut = %q, attendu application/json", ct)
	}
}

// TestHTMLPages : les pages principales rendent du HTML valide et navigable.
func TestHTMLPages(t *testing.T) {
	srv := NewServer(demoProvider(t))
	cases := map[string]string{
		"/":                       "Accueil",
		"/conformance":            "Conformance",
		"/collections/demo":       "Collection demo",
		"/collections/demo/items": "Entités",
	}
	for path, marker := range cases {
		rec := getHTML(t, srv, path)
		if rec.Code != 200 {
			t.Errorf("%s : code = %d", path, rec.Code)
			continue
		}
		body := rec.Body.String()
		if !strings.HasPrefix(body, "<!doctype html>") {
			t.Errorf("%s : pas une page HTML", path)
		}
		if !strings.Contains(body, marker) {
			t.Errorf("%s : marqueur %q absent", path, marker)
		}
	}
}

// TestHTMLCollectionLinks : la page collection renvoie un aperçu carte et des liens.
func TestHTMLCollectionLinks(t *testing.T) {
	srv := NewServer(demoProvider(t))
	body := getHTML(t, srv, "/collections/demo").Body.String()
	if !strings.Contains(body, "/collections/demo/map?width=480") {
		t.Error("aperçu carte absent")
	}
	if !strings.Contains(body, "/collections/demo/items?f=html") {
		t.Error("lien items (HTML) absent")
	}
}

// TestHTMLItemsTable : la table d'entités liste les mailles avec leurs propriétés.
func TestHTMLItemsTable(t *testing.T) {
	srv := NewServer(demoProvider(t))
	body := getHTML(t, srv, "/collections/demo/items?limit=100").Body.String()
	if !strings.Contains(body, "sur 12 correspondante") {
		t.Error("compte d'entités absent")
	}
	if !strings.Contains(body, "t2m=") {
		t.Error("propriété t2m absente de la table")
	}
}

// TestConformanceHTMLClass : la classe de conformité html est annoncée.
func TestConformanceHTMLClass(t *testing.T) {
	found := false
	for _, cl := range conformanceClasses() {
		if cl == "http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/html" {
			found = true
		}
	}
	if !found {
		t.Fatal("classe de conformité html absente")
	}
}
