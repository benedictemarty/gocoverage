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
| `query`                          | `Collection.Query`                  | ✅ |
| `gen_covjson`                    | `Collection.CoverageJSON`           | ✅ |
| `_get_parameter_metadata`        | intégré à `CoverageJSON`/`Fields`   | ✅ |
| `get_time_resolution` / duration | `Properties` (restime partiel)      | ⚠️ partiel |
| formats natifs (zarr/netcdf out) | —                                   | ❌ à venir |
| CRS non-CRS84 (`_parse_storage_crs`) | —                               | ❌ à venir |

| pygeoapi (`XarrayEDRProvider`)   | gocoverage                          | État |
|----------------------------------|-------------------------------------|------|
| `position`                       | `Collection.Position`               | ✅ |
| `cube`                           | `Collection.Cube`                   | ✅ |
| `get_parameters` (base EDR)      | intégré à `CoverageJSON`            | ✅ |
| `_make_datetime`                 | `parseDatetime`                     | ✅ (numérique) |
| axe vertical `z`                 | —                                   | ❌ à venir |
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
2. **Temps numérique** : l'axe temporel est un `float64` en **secondes depuis
   l'epoch Unix** (décodé depuis les `units` CF « `<unité> since <date>` » à
   l'ouverture, via `xarray.DecodeTime`). Le paramètre `datetime` attend donc
   des bornes numériques (epoch) et `..`, pas encore des dates ISO 8601.
3. **CRS** : CRS84 uniquement pour l'instant.

## Prochains incréments possibles

- Formats natifs en sortie (netcdf/zarr) via `xarray.WriteNetCDF` / Zarr.
- Axe vertical `z` (cube/position).
- Dates ISO 8601 et durées/résolutions temporelles complètes.
- CRS projeté (`storage_crs`).
