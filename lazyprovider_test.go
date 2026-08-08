package gocoverage

import (
	"path/filepath"
	"testing"

	"github.com/benedictemarty/xarray"
)

// writeZarrColl écrit un petit Dataset lon/lat en Zarr dans dir.
func writeZarrColl(t *testing.T, dir string, base float64) {
	t.Helper()
	coords := map[string][]float64{"latitude": {45, 44, 43}, "longitude": {0, 1, 2, 3}}
	data := make([]float64, 12)
	for i := range data {
		data[i] = base + float64(i)
	}
	da, err := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{3, 4}, data, coords, "t2m")
	if err != nil {
		t.Fatal(err)
	}
	da.Variable().SetAttr("units", "K")
	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": da})
	if err != nil {
		t.Fatal(err)
	}
	if err := xarray.WriteDatasetZarr(dir, ds, xarray.ZarrNone); err != nil {
		t.Fatal(err)
	}
}

func TestLazyProviderLRUEviction(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	writeZarrColl(t, dirA, 0)
	writeZarrColl(t, dirB, 100)

	p := NewLazyFileProvider(1) // au plus 1 collection résidente
	p.AddZarr(dirA, "a", "A", "", "", "")
	p.AddZarr(dirB, "b", "B", "", "", "")

	// Rien n'est chargé tant qu'on ne demande rien... mais Collections() capture
	// les métadonnées (chargement transitoire).
	if infos := p.Collections(); len(infos) != 2 {
		t.Fatalf("Collections()=%d, attendu 2", len(infos))
	}

	if _, ok := p.Get("a"); !ok {
		t.Fatal("Get(a) a échoué")
	}
	if _, ok := p.Get("b"); !ok {
		t.Fatal("Get(b) a échoué")
	}
	// Avec maxResident=1, une seule collection reste résidente après deux Get.
	if r := p.Resident(); r != 1 {
		t.Errorf("Resident()=%d, attendu 1 (LRU borné)", r)
	}
}

func TestLazyProviderServesRequests(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "c")
	writeZarrColl(t, dir, 0)
	p := NewLazyFileProvider(4)
	p.AddZarr(dir, "c", "C", "", "", "")

	srv := NewServer(p)
	// La collection est chargée à la demande à la première requête.
	rec := doGET(t, srv, "/collections/c/coverage?bbox=0,43,1,45")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	// describe conforme via le provider lazy.
	rec = doGET(t, srv, "/collections/c")
	if rec.Code != 200 {
		t.Fatalf("describe code=%d", rec.Code)
	}
}

func TestLazyProviderUnknown(t *testing.T) {
	p := NewLazyFileProvider(2)
	if _, ok := p.Get("absente"); ok {
		t.Error("Get d'une source inconnue devrait échouer")
	}
}
