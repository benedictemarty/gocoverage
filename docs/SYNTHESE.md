# Synthèse — parité du provider xarray de pygeoapi

Document de clôture du sprint « parité pygeoapi ». Objectif initial : *couvrir,
dans un premier temps, les fonctions de pygeoapi en se basant sur la
bibliothèque `~/xarray` (xarray-go)*.

## Résultat

Toute la surface **réelle** du provider xarray de pygeoapi (`XarrayProvider` +
`XarrayEDRProvider`) est reproduite. Elle a été établie en **lisant le code
source de pygeoapi** (pas de mémoire) : le provider n'implémente pas
`domainset`/`rangetype` (absents du dépôt) ; la surface réelle est `get_fields`,
`_get_coverage_properties`, `query`, `gen_covjson`, `_parse_storage_crs`, et côté
EDR `position`/`cube`.

| pygeoapi | gocoverage / xarray-go | État |
|---|---|---|
| `get_fields` | `Collection.Fields` | ✅ |
| `_get_coverage_properties` (+ `restime`/`time_duration`) | `Collection.Properties` | ✅ |
| `_parse_storage_crs` (`crs_type`/`bbox_crs`) | `Collection.CRS` + `detectCRS` | ✅ description (pas de reprojection) |
| `query` (properties, subsets, bbox, datetime, format_) | `Collection.Query` + négociation `f` | ✅ |
| `gen_covjson` (Grid/PointSeries, axe t, CRS) | `Collection.CoverageJSON` | ✅ |
| EDR `position` / `cube` (+ z, datetime) | `Collection.Position` / `Cube` | ✅ |
| décodage CF (packing, `_FillValue`, temps) | `xarray.DecodeCF` / `DecodeTime` | ✅ |
| ouverture netCDF/Zarr (`open_dataset`/`open_zarr`) | `LoadNetCDF` / `LoadZarr` / `OpenNetCDFFile` | ✅ (voir Limites I/O) |
| formats natifs (`format_=netcdf`/`zarr`) | `f=netcdf` / `f=zarr` (ZIP) | ✅ |

## Parcours par versions (gocoverage)

| Version | Apport |
|---|---|
| 0.1.0 | Serveur OGC API Coverages/EDR minimal (1 collection = 1 `DataArray` 2D) |
| 0.2.0 | Cœur du provider : `Dataset` multi-paramètres, `Fields`, `Properties`, `Query` (properties/bbox/subset/datetime), `CoverageJSON`, EDR `position`/`cube`, ouverture fichiers |
| 0.3.0 | Décodage CF à l'ouverture (packing, `_FillValue`, temps, `units`) |
| 0.4.0 | Ouverture NetCDF-4/HDF5 & CDF-2/5 via convertisseur externe (`OpenNetCDFFile`) |
| 0.5.0 | `datetime` ISO 8601 (entrée + sortie) |
| 0.6.0 | Axe vertical `z` (EDR) |
| 0.7.0 | Format de sortie natif netCDF (`f=netcdf`) |
| 0.8.0 | Format de sortie natif Zarr zippé (`f=zarr`) |
| 0.9.0 | Description du CRS (`Collection.CRS` + détection CF), sans reprojection |
| 0.10.0 | Résolution/durée temporelles ISO 8601 (`restime`/`time_duration`) |

## Sprints xarray-go associés

- **57–58** : paquet `geoapi` (CoverageJSON mono-`DataArray`, `SubsetBBox`, `Position`).
- **59** : `SelNearestKeep`/`SelNearestMany` (nearest conservant la dimension) — requis par CoverageJSON/EDR.
- **60** : attributs netCDF (lecture/écriture), `DecodeCF`/`DecodeTime`, dimension d'enregistrement illimitée.
- **61** : `OpenNetCDFFile` (NetCDF-4/HDF5 & CDF-2/5 via `nccopy`/`cdo`).

## Écarts assumés (documentés, non masqués)

1. **Temps** : axe interne en **secondes epoch** (`float64`), pas `datetime64`.
   Entrée/sortie ISO 8601 gérées ; `datetime` accepte aussi le numérique.
2. **CRS** : **décrit**, jamais **reprojeté** — comme pygeoapi (`bbox_crs` non
   géré). Détection limitée aux conteneurs CF portant un code EPSG.
3. **I/O** : CDF-1 lu nativement (y compris attributs CF + dimension illimitée) ;
   **NetCDF-4/HDF5 & CDF-2/5 via convertisseur externe** (`nccopy`/`cdo`) — pas
   de lecteur HDF5 en Go. Voir le tableau « Limitations I/O » du README.
4. **Champs sans `units`** : conservés (unité vide) au lieu d'être ignorés
   (pygeoapi les masque).
5. Pas de dask/chunking distribué ; sortie Zarr non compressée.

## Validation

- **Tests** : suite verte dans les deux dépôts (`go test ./...`), 28 tests
  gocoverage (`go vet` + `gofmt` propres).
- **Empirique** : chargement de vrais fichiers écrits par **Python xarray**
  (`NETCDF3_CLASSIC` avec packing int16, `time` illimité, `units` CF) et d'un
  **NetCDF-4/HDF5** réel converti par `nccopy` (netCDF 4.9.3).

## Au-delà de la parité (non réalisé, par choix)

- Reprojection effective (nécessiterait un équivalent pyproj en Go ; pygeoapi ne
  reprojette pas non plus).
- Alignement de `geoapi.ToCoverageJSON` (xarray-go) sur `PointSeries` — cosmétique,
  cet encodeur n'étant pas utilisé par gocoverage.
