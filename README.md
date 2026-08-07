# gocoverage

Serveur **OGC API - Coverages / EDR** minimal, écrit en Go, adossé à
[`xarray-go`](../xarray) comme couche de données (« provider »).

## Positionnement

Ce projet illustre l'architecture d'un service géospatial en Go :

```
gocoverage (serveur HTTP, endpoints OGC)  ──►  xarray-go (données : lecture, subset, CoverageJSON)
```

C'est le pendant Go de `pygeoapi` (serveur Python) + `xarray` (données). Pour un
serveur OGC API complet et productisé, voir des projets dédiés (ex. **gogeoapi**,
GoKoala, GOAF) ; `gocoverage` est une démonstration compacte et autonome.

L'analyse du code source du provider xarray de pygeoapi montre qu'il n'utilise
qu'un petit sous-ensemble de xarray (ouverture, `sel` nearest/slice, `.values`,
`.attrs`, `min`/`max`, export) — **entièrement couvert par xarray-go**.

## Lancer

```bash
go run ./cmd/gocoverage      # écoute sur :8080 avec une collection de démo
```

## Endpoints

| Route | Rôle |
|-------|------|
| `GET /` | landing page |
| `GET /collections` | liste des couvertures (id, titre, bbox) |
| `GET /collections/{id}` | métadonnées d'une couverture |
| `GET /collections/{id}/coverage?bbox=minx,miny,maxx,maxy` | sous-cube en **CoverageJSON** |
| `GET /collections/{id}/position?coords=x,y` | valeur au point le plus proche (EDR) |

### Exemples

```bash
curl "http://localhost:8080/collections"
# {"collections":[{"id":"demo","title":"...","bbox":[0,41,7,45]}]}

curl "http://localhost:8080/collections/demo/position?coords=2,43"
# {"parameter":"t2m","value":22,"x":2,"y":43}

curl "http://localhost:8080/collections/demo/coverage?bbox=1,42,3,45"
# document CoverageJSON (domaine Grid, CRS84)
```

## Architecture

- `provider.go` : interface `Provider` + `MemProvider` (collections en mémoire,
  chaque collection = un `xarray.DataArray[float64]` 2D latitude × longitude).
- `server.go` : routeur `net/http` ; les handlers délèguent le travail de données
  au paquet `xarray/geoapi` (`SubsetBBox`, `Position`, `ToCoverageJSON`).
- `cmd/gocoverage` : binaire de démonstration.

## Limites (démonstration)

- Une collection = une grille 2D lat/lon en mémoire (pas encore de chargement
  netCDF/Zarr/GRIB depuis disque, ni d'axes temps/z, ni de CRS autre que CRS84).
- Sous-ensemble d'OGC API (pas d'OpenAPI/conformance/HTML).

Ces limites relèvent du **serveur** ; la **couche de données** (xarray-go) est,
elle, déjà suffisante pour un provider type pygeoapi.
