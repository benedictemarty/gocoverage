# gocoverage

Serveur **OGC API - Coverages / EDR** en Go, adossé à
[`xarray-go`](../xarray) comme couche de données (« provider »).

Il reproduit les fonctions du **provider xarray de pygeoapi**
(`XarrayProvider` + `XarrayEDRProvider`) — voir
[`docs/parite-pygeoapi.md`](docs/parite-pygeoapi.md) pour la cartographie
fonction par fonction.

## Positionnement

```
gocoverage (serveur HTTP, endpoints OGC)  ──►  xarray-go (données : Dataset, subset, coords)
```

C'est le pendant Go de `pygeoapi` (serveur Python) + `xarray` (données). Pour un
serveur OGC API complet et productisé, voir des projets dédiés (ex. **gogeoapi**,
GoKoala, GOAF) ; `gocoverage` est une démonstration compacte et autonome.

## Fonctions couvertes (parité pygeoapi)

| pygeoapi | gocoverage |
|----------|------------|
| `get_fields` | `Collection.Fields` |
| `_get_coverage_properties` | `Collection.Properties` |
| `query` (properties, subsets, bbox, datetime) | `Collection.Query` |
| `gen_covjson` | `Collection.CoverageJSON` |
| EDR `position` / `cube` | `Collection.Position` / `Collection.Cube` |
| ouverture netCDF / Zarr | `LoadNetCDF` / `LoadZarr` |

## Lancer

```bash
go run ./cmd/gocoverage      # écoute sur :8080 avec une collection de démo
```

## Endpoints

| Route | Rôle |
|-------|------|
| `GET /` | landing page |
| `GET /collections` | liste des couvertures (id, titre, bbox, paramètres) |
| `GET /collections/{id}` | description : champs (`Fields`) + propriétés (`Properties`) |
| `GET /collections/{id}/coverage` | requête → **CoverageJSON** |
| `GET /collections/{id}/position?coords=x,y` | point le plus proche (EDR, PointSeries) |
| `GET /collections/{id}/cube?bbox=…` | sous-cube (EDR) |

Paramètres de `coverage` : `properties=t2m,uwind`, `bbox=minx,miny,maxx,maxy`,
`subset=Lat(43:45),Long(0:2)`, `datetime=lo/hi`.
Paramètres de `position`/`cube` : `parameter-name=…`, `datetime=…`.

### Exemples

```bash
curl "http://localhost:8080/collections"
# {"collections":[{"id":"demo","title":"...","bbox":[0,41,7,45],"parameters":["t2m","uwind"]}]}

# Sélection de paramètre + emprise
curl "http://localhost:8080/collections/demo/coverage?properties=t2m&bbox=1,42,4,45"

# Sous-ensemble par axe nommé
curl "http://localhost:8080/collections/demo/coverage?subset=Lat(43:45),Long(0:2)"

# EDR position (au point le plus proche) → domaine PointSeries
curl "http://localhost:8080/collections/demo/position?coords=2,43&parameter-name=t2m"
```

## Architecture

- `provider.go` : `Collection` (= un `xarray.Dataset[float64]`), `Provider`, `MemProvider`.
- `fields.go` : `Fields` (get_fields) et `Properties` (coverage_properties).
- `query.go` : logique de requête (`Query`), parsing `subset`/`datetime`, subset de Dataset.
- `coveragejson.go` : export CoverageJSON multi-paramètres (gen_covjson).
- `edr.go` : EDR `Position` / `Cube`.
- `fileprovider.go` : chargement netCDF/Zarr (`LoadNetCDF`, `LoadZarr`).
- `server.go` : routeur `net/http`.
- `cmd/gocoverage` : binaire de démonstration.

## Limites connues

- Axe temporel numérique (pas encore de dates ISO 8601), CRS84 uniquement.
- Une variable sans `units` est conservée (unité vide) — divergence assumée avec
  pygeoapi qui l'ignore.
- Pas encore de formats natifs en sortie (zarr/netcdf), ni d'axe vertical `z`.
- Sous-ensemble d'OGC API (pas d'OpenAPI/conformance/HTML).

Voir [`CHANGELOG.md`](CHANGELOG.md) et [`docs/parite-pygeoapi.md`](docs/parite-pygeoapi.md).
