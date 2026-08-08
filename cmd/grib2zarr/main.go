// Commande grib2zarr : convertit un fichier GRIB2 (grille lat/lon, simple packing)
// en store Zarr chunké/compressé, prêt à être servi en lecture élaguée par
// gocoverage (LoadChunkedZarr).
//
// Usage :
//
//	grib2zarr [-chunk N] [-comp none|zlib|zstd] <entrée.grib> <sortie.zarr>
//
// Limites : GRIB édition 2, grille lat/lon régulière, simple (5.0) ou complex
// (5.2/5.3) packing (voir gocoverage.LoadGrib). Pour les formats non gérés
// (JPEG2000/PNG, gaussienne réduite, GRIB1), convertir en amont via wgrib2/cdo/
// eccodes.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/benedictemarty/gocoverage"
	"github.com/benedictemarty/xarray"
)

func main() {
	chunk := flag.Int("chunk", 256, "taille de chunk (cellules) sur latitude et longitude ; 0 = un seul chunk")
	compName := flag.String("comp", "zstd", "compression des chunks : none|zlib|zstd")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: grib2zarr [-chunk N] [-comp none|zlib|zstd] <entrée.grib> <sortie.zarr>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	comp, err := parseComp(*compName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erreur:", err)
		os.Exit(2)
	}
	var chunks map[string]int
	if *chunk > 0 {
		chunks = map[string]int{"latitude": *chunk, "longitude": *chunk}
	}
	in, out := flag.Arg(0), flag.Arg(1)
	if err := gocoverage.ConvertGribToZarr(in, out, chunks, comp); err != nil {
		fmt.Fprintln(os.Stderr, "erreur:", err)
		os.Exit(1)
	}
	fmt.Printf("GRIB → Zarr : %s → %s (chunk=%d, comp=%s)\n", in, out, *chunk, *compName)
}

func parseComp(s string) (xarray.ZarrCompression, error) {
	switch s {
	case "none", "":
		return xarray.ZarrNone, nil
	case "zlib":
		return xarray.ZarrZlib, nil
	case "zstd":
		return xarray.ZarrZstd, nil
	default:
		return xarray.ZarrNone, fmt.Errorf("compression inconnue %q (none|zlib|zstd)", s)
	}
}
