# Parité avec le provider xarray de pygeoapi

Ce document cartographie les fonctions du provider xarray de pygeoapi
(`pygeoapi/provider/xarray_.py` et `xarray_edr.py`) et leur équivalent dans
gocoverage. La référence est le code source réel de pygeoapi (branche `master`).

> ⚠️ Constat d'analyse : pygeoapi **n'implémente pas** de fonctions
> `domainset` / `rangetype` dans son provider xarray (le terme est absent du
> dépôt). La surface réelle est décrite ci-dessous. gocoverage s'y aligne.

## Correspondance des fonctions

| pygeoapi (`XarrayProvider`)      | gocoverage                          | État |
|----------------------------------|-------------------------------------|------|
| `__init__` (open_dataset/zarr)   | `LoadNetCDF`, `LoadZarr`            | ✅ CDF-1 + décodage CF (packing, temps) ; pas de HDF5/CDF-2 |
| `get_fields`                     | `Collection.Fields`                 | ✅ |
| `_get_coverage_properties`       | `Collection.Properties`             | ✅ |
| `query` (+ `format_`)            | `Collection.Query` + négociation `f` | ✅ (json + netcdf + zarr) |
| `gen_covjson`                    | `Collection.CoverageJSON`           | ✅ |
| `_get_parameter_metadata`        | intégré à `CoverageJSON`/`Fields`   | ✅ |
| `get_time_resolution` / `get_time_coverage_duration` | `Properties` (`restime`/`time_duration`, ISO 8601) | ✅ (temps epoch) |
| formats natifs (`format_=netcdf`/`zarr`) | négociation `f=netcdf`/`zarr` | ✅ netCDF + Zarr (ZIP) |
| CRS de stockage (`_parse_storage_crs`, `crs_type`/`bbox_crs`) | `Collection.CRS` + `detectCRS` | ✅ **description** (EPSG explicite ou détecté) ; ❌ **reprojection** (comme pygeoapi) |

| pygeoapi (`XarrayEDRProvider`)   | gocoverage                          | État |
|----------------------------------|-------------------------------------|------|
| `position`                       | `Collection.Position`               | ✅ (+ `z`) |
| `cube`                           | `Collection.Cube`                   | ✅ (+ `z`) |
| `get_parameters` (base EDR)      | intégré à `CoverageJSON`            | ✅ |
| `_make_datetime`                 | `parseDatetime`                     | ✅ (ISO 8601 + numérique) |
| axe vertical `z`                 | `Collection.ZDim` + param `z`       | ✅ (niveau unique, au plus proche) |
| `area` (polygone)                | —                                   | ❌ (non présent dans xarray_edr non plus) |

## Détails de fidélité (`gen_covjson`)

- Domaine `Grid` avec axes **réguliers** `{start, stop, num}`.
- Bascule en `PointSeries` lorsque la grille est 1×1 (axes `x`/`y` en `values`).
- Axe temporel `t` en valeurs, `referencing` `TemporalRS`/`Gregorian`.
- Paramètres : `id`, `type: Parameter`, `name`, `observedProperty`, `unit.symbol`
  (échelle UCUM).
- Portées (`ranges`) : `NdArray`, `axisNames` `[t?, y, x]`, `null` pour les NaN.

## Requête (`query`) — exclusivités reproduites

- `bbox` et un `subset` de coordonnées (X ou Y) sont **exclusifs**.
- `datetime` et un `subset` sur l'axe temporel sont **exclusifs**.

## Divergences assumées

1. **Variables sans `units`** : pygeoapi les ignore (`get_fields`). gocoverage
   les conserve avec une unité vide, pour rester exploitable avec des jeux de
   données xarray-go qui ne portent pas toujours cette métadonnée.
2. **Temps** : l'axe temporel interne est un `float64` en **secondes depuis
   l'epoch Unix** (décodé depuis les `units` CF « `<unité> since <date>` » à
   l'ouverture, via `xarray.DecodeTime`). Côté **entrée**, `datetime` accepte
   désormais les **dates ISO 8601** (`2020-01-01`, `2020-01-01T06:00:00Z`,
   intervalles `a/b`, bornes ouvertes `..`) *et* les valeurs numériques. Côté
   **sortie**, l'axe `t` du CoverageJSON est formaté en ISO 8601 quand les
   valeurs sont des secondes epoch plausibles (sinon numérique). Les subsets
   `subset=time(...)` acceptent les dates seules (l'heure `hh:mm:ss` entre en
   conflit avec le séparateur `:` — utiliser `datetime` pour un instant précis).
3. **CRS** : le CRS est **décrit** (géographique/projeté, id EPSG) mais les
   données ne sont **pas reprojetées** — exactement comme pygeoapi, dont
   `query(bbox_crs=…)` n'est pas géré. Le CRS peut être fixé explicitement
   (`Collection.CRS`, pendant de l'override `storage_crs`) ou détecté depuis une
   variable de conteneur CF (`grid_mapping_name`/`epsg_code`). Sans pyproj en Go,
   la détection couvre les cas où l'EPSG est présent ; sinon CRS84 par défaut.

## Prochains incréments possibles

- Reprojection effective (nécessiterait un équivalent pyproj en Go).
- Détection CRS élargie (mapping `grid_mapping_name` → EPSG sans code explicite).
