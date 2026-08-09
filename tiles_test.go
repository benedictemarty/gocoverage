package gocoverage

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"testing"
)

// opaquePixels compte les pixels non transparents d'une image PNG encodée.
func opaquePixels(t *testing.T, body []byte) (int, image.Image) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("décodage png: %v", err)
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				n++
			}
		}
	}
	return n, img
}

// TestTileMatrixSets : liste et définition d'un TMS.
func TestTileMatrixSets(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/tileMatrixSets")
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var body struct {
		TileMatrixSets []struct {
			ID string `json:"id"`
		} `json:"tileMatrixSets"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.TileMatrixSets) != 2 {
		t.Fatalf("attendu 2 TMS, %d", len(body.TileMatrixSets))
	}
	if rec := doGET(t, srv, "/tileMatrixSets/WebMercatorQuad"); rec.Code != 200 {
		t.Fatalf("définition TMS : code = %d", rec.Code)
	}
	if rec := doGET(t, srv, "/tileMatrixSets/Inconnu"); rec.Code != 404 {
		t.Fatalf("TMS inconnu : code = %d, attendu 404", rec.Code)
	}
}

// TestTilesetList : /map/tiles liste un tileset par TMS.
func TestTilesetList(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/map/tiles")
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var body struct {
		Tilesets []map[string]interface{} `json:"tilesets"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Tilesets) != 2 {
		t.Fatalf("attendu 2 tilesets, %d", len(body.Tilesets))
	}
}

// TestTileRenderCRS84 : une tuile WorldCRS84Quad couvrant la donnée est opaque.
func TestTileRenderCRS84(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// z=5, row=8, col=32 → bbox ≈ [0, 39.375, 5.625, 45], recouvre la démo.
	rec := doGET(t, srv, "/collections/demo/map/tiles/WorldCRS84Quad/5/8/32?properties=t2m")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q", ct)
	}
	n, img := opaquePixels(t, rec.Body.Bytes())
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("dimensions = %dx%d, attendu 256x256", b.Dx(), b.Dy())
	}
	if n == 0 {
		t.Fatal("tuile entièrement transparente, attendu des pixels de donnée")
	}
}

// TestTileRenderMercator : une tuile WebMercatorQuad couvrant la donnée est opaque.
func TestTileRenderMercator(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// z=5, row=11, col=16 → recouvre lon 0..11°, lat ~40..47°.
	rec := doGET(t, srv, "/collections/demo/map/tiles/WebMercatorQuad/5/11/16?properties=t2m")
	if rec.Code != 200 {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	n, _ := opaquePixels(t, rec.Body.Bytes())
	if n == 0 {
		t.Fatal("tuile Mercator entièrement transparente, attendu des pixels de donnée")
	}
}

// TestTileOutsideTransparent : une tuile hors couverture est transparente (200).
func TestTileOutsideTransparent(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// z=2, row=0, col=0 → bbox lon [-180,-135] : aucune donnée.
	rec := doGET(t, srv, "/collections/demo/map/tiles/WorldCRS84Quad/2/0/0")
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	if n, _ := opaquePixels(t, rec.Body.Bytes()); n != 0 {
		t.Fatalf("attendu tuile transparente, %d pixels opaques", n)
	}
}

// TestTileErrors : TMS inconnu (404), tuile hors grille et indices non entiers (400).
func TestTileErrors(t *testing.T) {
	srv := NewServer(demoProvider(t))
	if rec := doGET(t, srv, "/collections/demo/map/tiles/Bidon/0/0/0"); rec.Code != 404 {
		t.Errorf("TMS inconnu : code = %d, attendu 404", rec.Code)
	}
	// z=0 → 2 colonnes (WorldCRS84Quad) : col 5 hors grille.
	if rec := doGET(t, srv, "/collections/demo/map/tiles/WorldCRS84Quad/0/0/5"); rec.Code != 400 {
		t.Errorf("tuile hors grille : code = %d, attendu 400", rec.Code)
	}
	if rec := doGET(t, srv, "/collections/demo/map/tiles/WorldCRS84Quad/a/b/c"); rec.Code != 400 {
		t.Errorf("indices non entiers : code = %d, attendu 400", rec.Code)
	}
}

// TestConformanceTiles : la classe Tiles core est annoncée.
func TestConformanceTiles(t *testing.T) {
	found := false
	for _, cl := range conformanceClasses() {
		if cl == "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/core" {
			found = true
		}
	}
	if !found {
		t.Fatal("classe de conformité Tiles core absente")
	}
}
