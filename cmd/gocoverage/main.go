// Commande gocoverage : démarre un serveur OGC API Coverages / EDR reproduisant
// le provider xarray de pygeoapi, avec une collection de démonstration
// (grille synthétique multi-paramètres), adossée à xarray-go.
//
//	go run ./cmd/gocoverage   # écoute sur :8080
package main

import (
	"log"
	"net/http"

	"github.com/benedictemarty/gocoverage"
	"github.com/benedictemarty/xarray"
)

func main() {
	// Grille de démonstration 5×8 (latitude × longitude), deux paramètres.
	ny, nx := 5, 8
	lats := make([]float64, ny)
	lons := make([]float64, nx)
	for j := 0; j < ny; j++ {
		lats[j] = 45 - float64(j) // 45 -> 41 (nord vers sud)
	}
	for i := 0; i < nx; i++ {
		lons[i] = float64(i) // 0 -> 7
	}
	t2m := make([]float64, ny*nx)
	uwind := make([]float64, ny*nx)
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			t2m[j*nx+i] = float64(j*10 + i)
			uwind[j*nx+i] = float64(i) - float64(j)
		}
	}
	coords := map[string][]float64{"latitude": lats, "longitude": lons}
	daT, err := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{ny, nx}, t2m, coords, "t2m")
	if err != nil {
		log.Fatal(err)
	}
	daT.Variable().SetAttr("units", "K")
	daT.Variable().SetAttr("long_name", "Température à 2 m")
	daU, err := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{ny, nx}, uwind, coords, "uwind")
	if err != nil {
		log.Fatal(err)
	}
	daU.Variable().SetAttr("units", "m/s")
	daU.Variable().SetAttr("long_name", "Vent zonal")

	ds, err := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"t2m": daT, "uwind": daU})
	if err != nil {
		log.Fatal(err)
	}

	prov := gocoverage.NewMemProvider()
	if err := prov.Add(&gocoverage.Collection{
		ID: "demo", Title: "Champ météo (démo)",
		XDim: "longitude", YDim: "latitude", Data: ds,
	}); err != nil {
		log.Fatal(err)
	}

	srv := gocoverage.NewServer(prov)
	log.Println("gocoverage à l'écoute sur http://localhost:8080")
	log.Println("  GET /collections")
	log.Println("  GET /collections/demo")
	log.Println("  GET /collections/demo/coverage?properties=t2m&bbox=1,42,4,45")
	log.Println("  GET /collections/demo/coverage?subset=Lat(43:45),Long(0:2)")
	log.Println("  GET /collections/demo/position?coords=2,43&parameter-name=t2m")
	log.Println("  GET /collections/demo/cube?bbox=1,42,4,45")
	log.Fatal(http.ListenAndServe(":8080", srv.Handler()))
}
