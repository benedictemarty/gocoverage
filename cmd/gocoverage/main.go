// Commande gocoverage : démarre un serveur OGC API Coverages minimal avec une
// collection de démonstration (grille synthétique), adossé à xarray-go.
//
//	go run ./cmd/gocoverage   # écoute sur :8080
package main

import (
	"log"
	"net/http"

	"github.com/bmarty/gocoverage"
	"github.com/bmarty/xarray"
)

func main() {
	// Grille de démonstration 5×8 (latitude × longitude).
	ny, nx := 5, 8
	data := make([]float64, ny*nx)
	lats := make([]float64, ny)
	lons := make([]float64, nx)
	for j := 0; j < ny; j++ {
		lats[j] = 45 - float64(j) // 45 -> 41 (nord vers sud)
	}
	for i := 0; i < nx; i++ {
		lons[i] = float64(i) // 0 -> 7
	}
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			data[j*nx+i] = float64(j*10 + i) // valeurs lisibles
		}
	}
	da, err := xarray.NewDataArray(
		[]string{"latitude", "longitude"}, []int{ny, nx}, data,
		map[string][]float64{"latitude": lats, "longitude": lons}, "t2m")
	if err != nil {
		log.Fatal(err)
	}

	prov := gocoverage.NewMemProvider()
	prov.Add(&gocoverage.Collection{
		ID: "demo", Title: "Température 2 m (démo)",
		Param: "t2m", XDim: "longitude", YDim: "latitude", Data: da,
	})

	srv := gocoverage.NewServer(prov)
	log.Println("gocoverage à l'écoute sur http://localhost:8080")
	log.Println("  GET /collections")
	log.Println("  GET /collections/demo/coverage?bbox=1,42,4,45")
	log.Println("  GET /collections/demo/position?coords=2,43")
	log.Fatal(http.ListenAndServe(":8080", srv.Handler()))
}
