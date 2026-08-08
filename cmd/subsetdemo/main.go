// Démo : le même code de subset (gocoverage) s'applique à une collection
// « satellite » (réflectance 2D) et à une collection « modèle » (température 4D
// temps × niveau × lat × lon). Illustre l'agnosticité du provider à la source.
//
//	go run ./cmd/subsetdemo
package main

import (
	"fmt"

	"github.com/benedictemarty/gocoverage"
	"github.com/benedictemarty/xarray"
)

func main() {
	prov := gocoverage.NewMemProvider()

	// --- Collection SATELLITE : réflectance 2D (lat × lon) ---
	lat := []float64{50, 49, 48, 47}
	lon := []float64{0, 1, 2, 3}
	refl := make([]float64, 4*4)
	for i := range refl {
		refl[i] = float64(i) / 100 // réflectance factice 0..0.15
	}
	sat, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{4, 4}, refl,
		map[string][]float64{"latitude": lat, "longitude": lon}, "reflectance")
	satDS, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"reflectance": sat})
	must(prov.Add(&gocoverage.Collection{
		ID: "sat", Title: "Réflectance (satellite)",
		XDim: "longitude", YDim: "latitude", Data: satDS,
	}))

	// --- Collection MODÈLE : température 4D (temps × niveau × lat × lon) ---
	tvals := []float64{0, 6, 12}        // heures
	levels := []float64{1000, 850, 500} // hPa
	nt, nz, ny, nx := 3, 3, 4, 4
	temp := make([]float64, nt*nz*ny*nx)
	for it := 0; it < nt; it++ {
		for iz := 0; iz < nz; iz++ {
			for iy := 0; iy < ny; iy++ {
				for ix := 0; ix < nx; ix++ {
					// valeur repérable : encode niveau et position
					temp[((it*nz+iz)*ny+iy)*nx+ix] = float64(1000*iz + 10*iy + ix + it)
				}
			}
		}
	}
	mdl, _ := xarray.NewDataArray(
		[]string{"time", "level", "latitude", "longitude"}, []int{nt, nz, ny, nx}, temp,
		map[string][]float64{"time": tvals, "level": levels, "latitude": lat, "longitude": lon}, "temperature")
	mdlDS, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"temperature": mdl})
	must(prov.Add(&gocoverage.Collection{
		ID: "model", Title: "Température (modèle)",
		XDim: "longitude", YDim: "latitude", TDim: "time", ZDim: "level", Data: mdlDS,
	}))

	// ===== Les MÊMES opérations de subset sur les deux collections =====
	bbox := [4]float64{1, 48, 2, 49} // lon 1..2, lat 48..49

	fmt.Println("=== 1) Subset par emprise (bbox lon 1..2, lat 48..49) ===")
	for _, id := range []string{"sat", "model"} {
		c, _ := prov.Get(id)
		ds, err := c.Query(gocoverage.QueryParams{BBox: &bbox})
		must(err)
		fmt.Printf("  %-6s -> dims %v\n", id, dims(ds))
	}

	fmt.Println("\n=== 2) Point (EDR position, lon=1.5 lat=48.5) ===")
	for _, id := range []string{"sat", "model"} {
		c, _ := prov.Get(id)
		ds, err := c.Position(1.5, 48.5, gocoverage.EDRParams{})
		must(err)
		// satellite -> 1 valeur ; modèle -> profil temps×niveau au point
		fmt.Printf("  %-6s -> dims %v (le modèle garde temps×niveau au point)\n", id, dims(ds))
	}

	fmt.Println("\n=== 3) Spécifique modèle : plage temporelle + niveau vertical ===")
	c, _ := prov.Get("model")
	z := 500.0
	dt := [2]float64{0, 6}
	cube, err := c.Cube(bbox, gocoverage.EDRParams{Datetime: &dt, Z: &z})
	must(err)
	fmt.Printf("  model cube (bbox + datetime 0..6h + z=500hPa) -> dims %v\n", dims(cube))

	fmt.Println("\n=== 4) Satellite : bbox OK ; datetime/z refusés proprement (axes absents) ===")
	sc, _ := prov.Get("sat")
	scube, err := sc.Cube(bbox, gocoverage.EDRParams{}) // bbox seul
	must(err)
	fmt.Printf("  sat cube (bbox) -> dims %v\n", dims(scube))
	if _, err := sc.Cube(bbox, gocoverage.EDRParams{Datetime: &dt}); err != nil {
		fmt.Printf("  sat cube (+datetime) -> refusé : %v\n", err)
	}

	fmt.Println("\nConclusion : le même provider gocoverage subsette satellite ET modèle,")
	fmt.Println("et refuse explicitement les axes qu'une collection ne possède pas.")
}

func dims(ds *xarray.Dataset[float64]) map[string]int { return ds.Dims() }

func must(err error) {
	if err != nil {
		panic(err)
	}
}
