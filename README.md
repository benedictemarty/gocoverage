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
| ouverture netCDF / Zarr | `LoadNetCDF` / `LoadZarr` (⚠️ portée limitée, voir *Limitations I/O*) |

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

## Limitations I/O (frontière réelle, mesurée)

L'**API et la sémantique** de xarray-go couvrent les besoins du provider pygeoapi
(`sel` slice/nearest, `.values`, `.attrs`, coords, `min`/`max`). En revanche
l'**ouverture de fichiers réels** est nettement plus étroite que ce que suggère
« ouverture netCDF/Zarr ». Vérifié en générant des fichiers avec xarray Python
(`format=…`) et en les passant à `LoadNetCDF` :

| Fichier de test | Cas | Résultat `LoadNetCDF` |
|---|---|---|
| CDF-1, sans attributs | aller-retour xarray-go | ✅ chargé, valeurs correctes |
| CDF-1 **+ attributs** (`units`, `long_name`) | fichier réel minimal | ❌ échec (`unexpected EOF`) |
| **NetCDF-4 / HDF5** | défaut de xarray/CDO | ❌ rejet propre (signature) |
| **CDF-2** (64-bit offset) | gros fichiers | ❌ rejet propre (version 2) |
| CDF-1 **+ `time` illimitée** + CF | climato typique | ❌ **panic** (borne d'index) |
| CDF-1 **+ `scale_factor`/`add_offset`** (packing int16) | ERA5/MF | ❌ échec de parse |

**Conclusion honnête :** `LoadNetCDF` ne lit fiablement que du **CDF-1 classique
sans attributs** (typiquement produit par xarray-go lui-même). La quasi-totalité
des jeux de données climato réels — NetCDF-4/HDF5, ou CDF-1 avec attributs CF,
dimension `time` illimitée, encodage `units: "hours since…"`, packing
`scale_factor`/`add_offset` — **ne se chargent pas** en l'état.

> Deux défauts de robustesse identifiés : les fichiers *avec attributs* échouent
> avec un message trompeur, et un fichier à *dimension illimitée* **fait paniquer**
> le lecteur au lieu de renvoyer une erreur. À durcir côté `xarray/netcdf.go`.

Pour l'écosystème complet (I/O CF, dask, HDF5), xarray-go n'est **pas** un
substitut à xarray Python ; il l'est pour l'API et les opérations sur tableaux
numériques.

## Autres limites connues

- Axe temporel numérique (pas encore de dates ISO 8601), CRS84 uniquement.
- Une variable sans `units` est conservée (unité vide) — divergence assumée avec
  pygeoapi qui l'ignore.
- Pas encore de formats natifs en sortie (zarr/netcdf), ni d'axe vertical `z`.
- Sous-ensemble d'OGC API (pas d'OpenAPI/conformance/HTML).

Voir [`CHANGELOG.md`](CHANGELOG.md) et [`docs/parite-pygeoapi.md`](docs/parite-pygeoapi.md).
