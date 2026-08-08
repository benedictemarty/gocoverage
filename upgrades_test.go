package gocoverage

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/benedictemarty/xarray"
)

// irregularProvider : longitude à pas non constant (0,1,3,7), latitude régulière.
func irregularProvider(t *testing.T) *MemProvider {
	t.Helper()
	coords := map[string][]float64{"latitude": {45, 44, 43}, "longitude": {0, 1, 3, 7}}
	da, err := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{3, 4},
		[]float64{0, 1, 2, 3, 10, 11, 12, 13, 20, 21, 22, 23}, coords, "t2m")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("units", "K")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err != nil {
		t.Fatal(err)
	}
	p := NewMemProvider()
	if err := p.Add(&Collection{ID: "irr", XDim: "longitude", YDim: "latitude", Data: ds}); err != nil {
		t.Fatal(err)
	}
	return p
}

// --- Remarque A : description conforme (extent, parameter_names, data_queries) ---

func TestDescribeConformant(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)

	dq, ok := doc["data_queries"].(map[string]interface{})
	if !ok || dq["position"] == nil || dq["cube"] == nil {
		t.Errorf("data_queries incomplet: %v", doc["data_queries"])
	}
	ext, ok := doc["extent"].(map[string]interface{})
	if !ok || ext["spatial"] == nil {
		t.Errorf("extent.spatial absent: %v", doc["extent"])
	}
	pn, ok := doc["parameter_names"].(map[string]interface{})
	if !ok || pn["t2m"] == nil {
		t.Errorf("parameter_names.t2m absent: %v", doc["parameter_names"])
	}
}

func TestDescribeTemporalExtent(t *testing.T) {
	srv := NewServer(timeProvider(t))
	rec := doGET(t, srv, "/collections/meteo")
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	ext := doc["extent"].(map[string]interface{})
	if ext["temporal"] == nil {
		t.Errorf("extent.temporal attendu pour une collection avec axe temps")
	}
}

// --- Remarque B : rejet d'un CRS de requête non supporté ---

func TestBBoxCRSUnsupportedRejected(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?bbox=0,43,3,45&bbox-crs=EPSG:3857")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 pour bbox-crs non supporté", rec.Code)
	}
}

func TestBBoxCRSSynonymAccepted(t *testing.T) {
	srv := NewServer(demoProvider(t))
	// Synonymes CRS84 (lon/lat) acceptés.
	for _, crs := range []string{"CRS84", "http://www.opengis.net/def/crs/OGC/1.3/CRS84"} {
		rec := doGET(t, srv, "/collections/demo/coverage?bbox=0,43,3,45&bbox-crs="+crs)
		if rec.Code != 200 {
			t.Errorf("crs=%q: code=%d, attendu 200 (synonyme CRS84)", crs, rec.Code)
		}
	}
	// EPSG:4326 (ordre lat/lon) volontairement rejeté sans reprojection (remarque L).
	rec := doGET(t, srv, "/collections/demo/coverage?bbox=0,43,3,45&bbox-crs=EPSG:4326")
	if rec.Code != 400 {
		t.Errorf("EPSG:4326: code=%d, attendu 400 (ordre d'axes ≠ CRS84)", rec.Code)
	}
}

// --- Remarque C : grille irrégulière décrite comme telle ---

func TestCoverageJSONIrregularAxis(t *testing.T) {
	srv := NewServer(irregularProvider(t))
	rec := doGET(t, srv, "/collections/irr/coverage")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	axes := doc["domain"].(map[string]interface{})["axes"].(map[string]interface{})
	x := axes["x"].(map[string]interface{})
	if x["values"] == nil {
		t.Errorf("axe x irrégulier attendu par valeurs, obtenu %v", x)
	}
	y := axes["y"].(map[string]interface{})
	if y["start"] == nil {
		t.Errorf("axe y régulier attendu {start,stop,num}, obtenu %v", y)
	}
}

func TestDomainSetIrregularAxis(t *testing.T) {
	srv := NewServer(irregularProvider(t))
	rec := doGET(t, srv, "/collections/irr/coverage/domainset")
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	axes := doc["generalGrid"].(map[string]interface{})["axis"].([]interface{})
	x := axes[0].(map[string]interface{})
	if x["type"] != "IrregularAxis" {
		t.Errorf("axe x=%v, attendu IrregularAxis", x["type"])
	}
	y := axes[1].(map[string]interface{})
	if y["type"] != "RegularAxis" {
		t.Errorf("axe y=%v, attendu RegularAxis", y["type"])
	}
}

// --- Remarque D : GeoJSON refuse le multi-pas au lieu de tronquer ---

func TestGeoJSONMultiStepRejected(t *testing.T) {
	srv := NewServer(timeProvider(t)) // 2 pas de temps
	rec := doGET(t, srv, "/collections/meteo/coverage?f=geojson")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 (GeoJSON multi-pas)", rec.Code)
	}
}

func TestGeoJSONSingleStepOK(t *testing.T) {
	srv := NewServer(timeProvider(t))
	rec := doGET(t, srv, "/collections/meteo/coverage?f=geojson&datetime=0")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "FeatureCollection") {
		t.Errorf("sortie GeoJSON attendue")
	}
}

// --- Remarque G : paramètre inconnu → 400 ---

func TestUnknownPropertiesRejected(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?properties=inexistant")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 pour properties inconnu", rec.Code)
	}
}

func TestUnknownParameterNameRejected(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/position?coords=1,44&parameter-name=inexistant")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 pour parameter-name inconnu", rec.Code)
	}
}

// --- Remarque H : négociation via l'en-tête Accept ---

func TestAcceptHeaderNegotiation(t *testing.T) {
	srv := NewServer(demoProvider(t)) // sans axe temps → GeoJSON mono-pas OK
	req := httptest.NewRequest("GET", "/collections/demo/coverage", nil)
	req.Header.Set("Accept", "application/geo+json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "FeatureCollection") {
		t.Errorf("Accept: geo+json → GeoJSON attendu, code=%d body=%.60s", rec.Code, rec.Body.String())
	}
}

func TestErrorBodyStandard(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/inconnue")
	var doc map[string]string
	json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc["code"] == "" || doc["description"] == "" {
		t.Errorf("corps d'erreur non standard: %v", doc)
	}
}

// --- Remarque I : instances dérivées de l'axe temporel ---

func TestInstancesFromTime(t *testing.T) {
	c, _ := timeProvider(t).Get("meteo")
	insts, err := c.InstancesFromTime()
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 2 {
		t.Fatalf("len=%d, attendu 2 instances (2 pas de temps)", len(insts))
	}
	if insts[0].TDim != "" {
		t.Errorf("une instance ne doit plus porter d'axe temps, TDim=%q", insts[0].TDim)
	}
	if _, ok := insts[0].Data.Dims()["time"]; ok {
		t.Errorf("l'axe time devrait être réduit dans l'instance")
	}
}

// --- Remarque J : type de champ entier déclaré via l'attribut dtype ---

func TestFieldIntegerType(t *testing.T) {
	coords := map[string][]float64{"latitude": {45, 44}, "longitude": {0, 1}}
	da, err := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{2, 2},
		[]float64{1, 2, 3, 4}, coords, "flag")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("dtype", "int32")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"flag": da})
	c := &Collection{ID: "f", XDim: "longitude", YDim: "latitude", Data: ds}
	if c.Fields()[0].Type != "integer" {
		t.Errorf("Type=%q, attendu integer (dtype=int32)", c.Fields()[0].Type)
	}
}

// --- Remarque N : bbox traversant l'antiméridien (±180°) ---

func TestAntimeridianBBox(t *testing.T) {
	coords := map[string][]float64{"latitude": {0, 1}, "longitude": {-180, -90, 0, 90, 175}}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{2, 5},
		[]float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, coords, "t2m")
	da.Variable().SetAttr("units", "K")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	c := &Collection{ID: "am", XDim: "longitude", YDim: "latitude", Data: ds}
	// bbox de 170°E à -170°E (traverse 180°) : doit retenir 175 et -180.
	out, err := c.Query(QueryParams{BBox: &[4]float64{170, -1, -170, 2}})
	if err != nil {
		t.Fatal(err)
	}
	xs, _ := out.Coord("longitude")
	if len(xs) != 2 {
		t.Fatalf("longitudes=%v, attendu 2 (175 et -180)", xs)
	}
	got := map[float64]bool{xs[0]: true, xs[1]: true}
	if !got[175] || !got[-180] {
		t.Errorf("longitudes=%v, attendu {175, -180}", xs)
	}
}

// --- Remarque O : polygone à trou ---

func TestAreaWithHole(t *testing.T) {
	coords := map[string][]float64{"latitude": {0, 1, 2, 3, 4}, "longitude": {0, 1, 2, 3, 4}}
	data := make([]float64, 25)
	for i := range data {
		data[i] = float64(i)
	}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{5, 5}, data, coords, "t2m")
	da.Variable().SetAttr("units", "K")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	c := &Collection{ID: "h", XDim: "longitude", YDim: "latitude", Data: ds}

	rings, err := parsePolygonRings("POLYGON((0 0, 4 0, 4 4, 0 4, 0 0),(1 1, 3 1, 3 3, 1 3, 1 1))")
	if err != nil {
		t.Fatal(err)
	}
	if len(rings) != 2 {
		t.Fatalf("anneaux=%d, attendu 2 (extérieur + trou)", len(rings))
	}
	out, err := c.AreaRings(rings, EDRParams{})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := out.Get("t2m")
	xs, _ := out.Coord("longitude")
	ys, _ := out.Coord("latitude")
	// Cellule centrale (2,2) : dans le trou → NaN. Coin (0,0) : conservé.
	ix2, iy2 := indexOf2(xs, 2), indexOf2(ys, 2)
	ix0, iy0 := indexOf2(xs, 0), indexOf2(ys, 0)
	st := cStrides(v.Shape())
	if !mathIsNaN(v.Data()[iy2*st[0]+ix2*st[1]]) {
		t.Errorf("cellule (2,2) dans le trou devrait être NaN")
	}
	if mathIsNaN(v.Data()[iy0*st[0]+ix0*st[1]]) {
		t.Errorf("cellule (0,0) hors trou devrait être conservée")
	}
}

func indexOf2(s []float64, v float64) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func mathIsNaN(f float64) bool { return f != f }

// --- Remarque K : marge de longitude corrigée par cos(lat) ---

func TestDegMarginsHighLatitude(t *testing.T) {
	// À 60°, 1° de longitude ≈ 55 km : la marge lon doit être ~2× la marge lat.
	mLon, mLat := degMargins(111320, 60)
	if mLon < mLat*1.9 {
		t.Errorf("mLon=%.4f mLat=%.4f : marge longitude insuffisante à 60° (remarque K)", mLon, mLat)
	}
}

// --- Remarque M : scaling recentre les coordonnées sur le milieu du bloc ---

func TestScalingRecenter(t *testing.T) {
	srv := NewServer(demoProvider(t)) // longitude {0,1,2,3}
	rec := doGET(t, srv, "/collections/demo/coverage?scale-factor=2")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	x := doc["domain"].(map[string]interface{})["axes"].(map[string]interface{})["x"].(map[string]interface{})
	// Bloc {0,1} → centre 0.5 (et non 0, borne gauche).
	if start, _ := x["start"].(float64); start != 0.5 {
		t.Errorf("x.start=%v, attendu 0.5 (centre de bloc, pas borne gauche)", x["start"])
	}
}

// --- Remarque Q : garde-fou de taille de réponse ---

func TestDatasetCellsCount(t *testing.T) {
	c, _ := demoProvider(t).Get("demo")
	if got := datasetCells(c.Data); got != 12 { // 3×4
		t.Errorf("datasetCells=%d, attendu 12", got)
	}
}

// --- Remarque H : Accept non satisfiable → 406 ---

func TestAcceptUnsupported406(t *testing.T) {
	srv := NewServer(demoProvider(t))
	req := httptest.NewRequest("GET", "/collections/demo/coverage", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 406 {
		t.Errorf("code=%d, attendu 406 (Accept: text/html non supporté)", rec.Code)
	}
}

// --- scale-size : taille cible absolue ---

func TestScaleSize(t *testing.T) {
	srv := NewServer(demoProvider(t)) // longitude {0,1,2,3} → 4 cellules
	rec := doGET(t, srv, "/collections/demo/coverage?scale-size=Long(2)")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	x := doc["domain"].(map[string]interface{})["axes"].(map[string]interface{})["x"].(map[string]interface{})
	if int(x["num"].(float64)) != 2 {
		t.Errorf("x.num=%v, attendu 2 (scale-size Long(2))", x["num"])
	}
}

// --- /api (oas30) ---

func TestOpenAPI(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/api")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc["openapi"] == nil || doc["paths"] == nil {
		t.Errorf("document OpenAPI incomplet: %v", doc)
	}
	// La classe oas30 doit être déclarée.
	crec := doGET(t, srv, "/conformance")
	if !strings.Contains(crec.Body.String(), "oas30") {
		t.Errorf("classe oas30 absente de /conformance")
	}
}

// --- Override d'interprétation du temps (fragilité epoch) ---

func TestTimeEpochOverride(t *testing.T) {
	ts := []float64{0, 1, 2} // petits entiers : heuristique → non-epoch
	yes, no := true, false
	c := &Collection{TimeEpoch: &yes}
	if !c.timeIsEpoch(ts) {
		t.Errorf("override true ignoré")
	}
	c = &Collection{TimeEpoch: &no}
	if c.timeIsEpoch([]float64{1e9, 1e9 + 3600}) { // grands : heuristique → epoch
		t.Errorf("override false ignoré")
	}
	c = &Collection{} // auto
	if c.timeIsEpoch(ts) {
		t.Errorf("auto: petits entiers ne sont pas de l'epoch")
	}
}

// --- Remarque F : bbox correct sur axes ascendants ET descendants ---

func TestBBoxAscendingAndDescendingLat(t *testing.T) {
	mk := func(lat []float64) *Collection {
		coords := map[string][]float64{"latitude": lat, "longitude": {0, 1, 2}}
		da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{3, 3},
			[]float64{0, 1, 2, 3, 4, 5, 6, 7, 8}, coords, "t2m")
		da.Variable().SetAttr("units", "K")
		ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
		return &Collection{ID: "c", XDim: "longitude", YDim: "latitude", Data: ds}
	}
	for _, tc := range []struct {
		name string
		lat  []float64
	}{
		{"descendant", []float64{45, 44, 43}},
		{"ascendant", []float64{43, 44, 45}},
	} {
		c := mk(tc.lat)
		// bbox couvrant uniquement la latitude médiane 44.
		ds, err := c.Query(QueryParams{BBox: &[4]float64{0, 43.5, 2, 44.5}})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		ys, _ := ds.Coord("latitude")
		if len(ys) != 1 || ys[0] != 44 {
			t.Errorf("%s: latitudes sélectionnées=%v, attendu [44]", tc.name, ys)
		}
	}
}
