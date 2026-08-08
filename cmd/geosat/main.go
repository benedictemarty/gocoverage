// Démo : chaîne satellite géostationnaire (type MTG FCI) -> reprojection en
// lon/lat -> subset par emprise via gocoverage.
//
// Point clé : une scène géostationnaire n'est PAS une grille lon/lat régulière ;
// on la reprojette d'abord (xarray.ReprojectFromGeos) avant tout subset bbox.
//
//	go run ./cmd/geosat
package main

import (
	"fmt"
	"math"

	"github.com/benedictemarty/gocoverage"
	"github.com/benedictemarty/xarray"
)

func main() {
	geos := xarray.MTGGeos()

	// --- 1) Scène "FCI" synthétique dans le plan géostationnaire (mètres) ---
	// Emprise géostationnaire couvrant l'Europe (coins lon/lat -> geos).
	minx, miny, maxx, maxy := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, ll := range [][2]float64{{-10, 60}, {12, 60}, {-10, 41}, {12, 41}} {
		gx, gy, ok := geos.Forward(ll[0], ll[1])
		if ok {
			minx, maxx = math.Min(minx, gx), math.Max(maxx, gx)
			miny, maxy = math.Min(miny, gy), math.Max(maxy, gy)
		}
	}
	sw, sh := 48, 48
	resx, resy := (maxx-minx)/float64(sw), (maxy-miny)/float64(sh)
	srcT := xarray.Affine{A: resx, C: minx, E: -resy, F: maxy}
	// Motif : un "système nuageux" (valeurs élevées) au centre de la scène.
	src := make([]float64, sw*sh)
	for r := 0; r < sh; r++ {
		for c := 0; c < sw; c++ {
			dr, dc := float64(r)-24, float64(c)-24
			if dr*dr+dc*dc < 120 {
				src[r*sw+c] = 1
			}
		}
	}
	fmt.Printf("Scène géostationnaire %d×%d (plan satellite, mètres)\n", sh, sw)
	fmt.Printf("  emprise geos: x[%.0f,%.0f] y[%.0f,%.0f]\n", minx, maxx, miny, maxy)

	// --- 2) Reprojection géostationnaire -> lon/lat (EPSG:4326) ---
	dstT := xarray.Affine{A: 0.5, C: -10, E: -0.5, F: 60} // lon -10..12, lat 60..41
	dw, dh := 44, 38
	ll, err := xarray.ReprojectFromGeos(src, sw, sh, srcT, geos, dstT, dw, dh, xarray.Bilinear)
	must(err)
	fmt.Printf("\nReprojeté en lon/lat %d×%d :\n", dh, dw)
	printGrid(ll, dh, dw)

	// --- 3) Provider gocoverage : subset par emprise (France ~ [-5,42,8,51]) ---
	xs, ys, _ := xarray.GeoCoords(dstT, dw, dh)
	da, _ := xarray.NewDataArray([]string{"latitude", "longitude"}, []int{dh, dw}, ll,
		map[string][]float64{"latitude": ys, "longitude": xs}, "cloud")
	ds, _ := xarray.NewDataset(map[string]*xarray.DataArray[float64]{"cloud": da})
	prov := gocoverage.NewMemProvider()
	must(prov.Add(&gocoverage.Collection{
		ID: "fci", Title: "FCI reprojeté", XDim: "longitude", YDim: "latitude", Data: ds,
	}))
	c, _ := prov.Get("fci")
	bbox := [4]float64{-5, 42, 8, 51}
	sub, err := c.Query(gocoverage.QueryParams{BBox: &bbox})
	must(err)
	fmt.Printf("\nSubset bbox France %v -> dims %v\n", bbox, sub.Dims())
	cloud, _ := sub.Get("cloud")
	fmt.Printf("  pixels 'nuage' dans l'emprise : %d\n", countCloud(cloud.Data()))
	fmt.Println("\nChaîne : géostationnaire -> ReprojectFromGeos -> Collection -> Query(bbox). OK.")
}

func printGrid(d []float64, h, w int) {
	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			v := d[r*w+c]
			switch {
			case math.IsNaN(v):
				fmt.Print(" ")
			case v < 0.5:
				fmt.Print(".")
			default:
				fmt.Print("#")
			}
		}
		fmt.Println()
	}
}

func countCloud(d []float64) int {
	n := 0
	for _, v := range d {
		if v >= 0.5 {
			n++
		}
	}
	return n
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
