package gocoverage

import (
	"path/filepath"
	"testing"

	"github.com/benedictemarty/xarray"
)

// TestLoadZarr : aller-retour Zarr → LoadZarr (comble le trou de couverture).
func TestLoadZarr(t *testing.T) {
	coords := map[string][]float64{"latitude": {45, 44}, "longitude": {0, 1}}
	da, err := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{2, 2},
		[]float64{0, 1, 2, 3}, coords, "t2m")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("units", "K")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "demo.zarr")
	if err := xarray.WriteDatasetZarr(dir, ds, xarray.ZarrNone); err != nil {
		t.Fatal(err)
	}

	c, err := LoadZarr(dir, "z", "Démo Zarr", "", "", "")
	if err != nil {
		t.Fatalf("LoadZarr: %v", err)
	}
	if c.XDim != "longitude" || c.YDim != "latitude" {
		t.Errorf("axes = %s/%s", c.XDim, c.YDim)
	}
	if len(c.Params()) != 1 || c.Params()[0] != "t2m" {
		t.Errorf("params = %v", c.Params())
	}
	arr, _ := c.Data.Get("t2m")
	if arr.Data()[3] != 3 {
		t.Errorf("valeur = %v, attendu 3", arr.Data()[3])
	}
}

func TestLoadNetCDFFichierAbsent(t *testing.T) {
	if _, err := LoadNetCDF("/nexiste/pas.nc", "x", "x", "", "", ""); err == nil {
		t.Error("attendu une erreur pour fichier absent")
	}
}

func TestCollectionFromDatasetAxesIntrouvables(t *testing.T) {
	// Dataset sans dimensions lat/lon → détection d'axes échoue (exerce keysOf).
	da, _ := xarray.NewDataArray([]string{"a", "b"}, []int{2, 2},
		[]float64{1, 2, 3, 4}, map[string][]float64{"a": {0, 1}, "b": {0, 1}}, "v")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"v": da})
	if _, err := collectionFromDataset(ds, "id", "t", "", "", ""); err == nil {
		t.Error("attendu une erreur (axes X/Y introuvables)")
	}
}

func TestAddDimensionsAbsentes(t *testing.T) {
	ds := demoDataset(t) // dims latitude, longitude
	p := NewMemProvider()
	cases := []Collection{
		{ID: "x", XDim: "nope", YDim: "latitude", Data: ds},
		{ID: "y", XDim: "longitude", YDim: "nope", Data: ds},
		{ID: "t", XDim: "longitude", YDim: "latitude", TDim: "nope", Data: ds},
		{ID: "z", XDim: "longitude", YDim: "latitude", ZDim: "nope", Data: ds},
	}
	for _, c := range cases {
		cc := c
		if err := p.Add(&cc); err == nil {
			t.Errorf("Add(%s): attendu une erreur (dimension absente)", c.ID)
		}
	}
}

func TestPositionDatetimeSansAxeTemporel(t *testing.T) {
	// Méthode Position directe : datetime fourni sans axe temporel → erreur.
	c := &Collection{ID: "d", XDim: "longitude", YDim: "latitude", Data: demoDataset(t)}
	if _, err := c.Position(1, 44, EDRParams{Datetime: &[2]float64{0, 1}}); err == nil {
		t.Error("attendu une erreur (datetime sans axe temporel)")
	}
}

func TestQuerySubsetAxeInconnu(t *testing.T) {
	// subset sur un axe inexistant → resolveAxis échoue.
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?subset=Bogus(1:2)")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 (axe inconnu)", rec.Code)
	}
}

func TestSelectVarsAucunValide(t *testing.T) {
	// properties ne référençant aucun paramètre existant → erreur.
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/collections/demo/coverage?properties=inexistant")
	if rec.Code != 400 {
		t.Errorf("code=%d, attendu 400 (aucun paramètre valide)", rec.Code)
	}
}

func TestLandingPage(t *testing.T) {
	srv := NewServer(demoProvider(t))
	rec := doGET(t, srv, "/")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	if len(rec.Body.Bytes()) == 0 {
		t.Error("landing vide")
	}
	// Chemin inconnu sous la racine → 404.
	if r := doGET(t, srv, "/inconnu"); r.Code != 404 {
		t.Errorf("chemin inconnu: code=%d, attendu 404", r.Code)
	}
}

func TestHandlersEntreesInvalides(t *testing.T) {
	srv := NewServer(demoProvider(t))
	cases := []struct {
		path string
	}{
		{"/collections/demo/position?coords=abc"},       // coords invalide
		{"/collections/demo/position?coords=1,44&z=xx"}, // z invalide
		{"/collections/demo/cube?bbox=1,2"},             // bbox incomplète
		{"/collections/demo/coverage?bbox=1,2,3"},       // bbox invalide
		{"/collections/demo/coverage?subset=Lat(a:b)"},  // subset invalide
		{"/collections/demo/foo"},                       // action inconnue
	}
	for _, c := range cases {
		rec := doGET(t, srv, c.path)
		if rec.Code < 400 {
			t.Errorf("%s: code=%d, attendu ≥400", c.path, rec.Code)
		}
	}
}

func TestDetectCRSGeographiqueEtProjeteSansEPSG(t *testing.T) {
	mk := func(gm string) *xarray.Dataset[float64] {
		coords := map[string][]float64{"latitude": {45, 44}, "longitude": {0, 1}}
		t2m, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{2, 2},
			[]float64{0, 1, 2, 3}, coords, "t2m")
		crsVar, _ := xarray.NewDataArray([]string{"crs"}, []int{1}, []float64{0},
			map[string][]float64{"crs": {0}}, "crs")
		crsVar.Variable().SetAttr("grid_mapping_name", gm)
		ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": t2m, "crs": crsVar})
		return ds
	}
	// grid_mapping géographique, sans EPSG → CRS84.
	if crs, name := detectCRS(mk("latitude_longitude")); name != "crs" || crs.typ() != "GeographicCRS" {
		t.Errorf("géographique: crs=%+v var=%q", crs, name)
	}
	// grid_mapping projeté, sans EPSG → ProjectedCRS sans id.
	if crs, name := detectCRS(mk("transverse_mercator")); name != "crs" || crs.Type != "ProjectedCRS" {
		t.Errorf("projeté sans EPSG: crs=%+v var=%q", crs, name)
	}
}

func TestFieldSansUnite(t *testing.T) {
	// Variable sans units → unitOf renvoie nil (branche non couverte).
	coords := map[string][]float64{"latitude": {45, 44}, "longitude": {0, 1}}
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{2, 2},
		[]float64{0, 1, 2, 3}, coords, "raw")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"raw": da})
	c := &Collection{ID: "n", XDim: "longitude", YDim: "latitude", Data: ds}
	b, err := c.CoverageJSON(ds)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Error("CoverageJSON vide")
	}
	// Le champ sans units reste exposé (divergence assumée avec pygeoapi).
	if c.Fields()[0].Unit != "" {
		t.Errorf("unit attendu vide, obtenu %q", c.Fields()[0].Unit)
	}
}
