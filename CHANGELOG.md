# Changelog

Toutes les modifications notables de gocoverage sont consignées dans ce fichier.

Le format s'inspire de [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/)
et le projet suit un versionnement sémantique.

## [0.2.0] - 2026-08-07

### Sprint « parité pygeoapi #1 » — cœur du provider xarray + ouverture fichiers

Objectif : couvrir les fonctions réelles du provider xarray de pygeoapi
(`XarrayProvider` + `XarrayEDRProvider`), en se basant sur xarray-go.

Note d'analyse : la lecture du code source de pygeoapi montre que le provider
xarray **n'implémente pas** `domainset`/`rangetype` (absents du dépôt) ; la
surface réelle est `get_fields`, `_get_coverage_properties`, `query`,
`gen_covjson`, et côté EDR `position`/`cube`. C'est cette surface qui est
reproduite.

### Ajouté
- **Modèle multi-paramètres** : une `Collection` porte désormais un
  `xarray.Dataset[float64]` (plusieurs variables) au lieu d'un seul `DataArray`.
- **`Collection.Fields()`** — pendant de `get_fields` (type, `long_name`,
  `units`/`x-ogc-unit`).
- **`Collection.Properties()`** — pendant de `_get_coverage_properties` (bbox,
  libellés d'axes X/Y/T, width/height, résolution, étendue temporelle).
- **`Collection.Query(QueryParams)`** — pendant de `query` : sélection de champs
  (`properties`), emprise (`bbox`), sous-ensembles par axe nommé
  (`subset=Lat(43:45),Long(0:2)`), plage temporelle (`datetime`), avec les
  exclusivités de pygeoapi (bbox/subset de coordonnées, datetime/subset temporel).
- **`Collection.CoverageJSON(ds)`** — pendant de `gen_covjson` : domaine Grid
  (axes réguliers `start/stop/num`), bascule `PointSeries` pour une grille 1×1,
  axe temporel `t`, paramètres au format CoverageJSON (`unit.symbol`, UCUM),
  `null` pour les valeurs NaN.
- **EDR `Collection.Position()` et `Collection.Cube()`** — pendants de
  `XarrayEDRProvider.position`/`cube` (sélection au plus proche / sous-cube,
  avec `parameter-name` et `datetime`).
- **Ouverture depuis fichiers** : `LoadNetCDF` et `LoadZarr` construisent une
  collection depuis un fichier netCDF / répertoire Zarr, avec détection
  automatique des axes X/Y/T par nom.
- **Routes HTTP** : `/collections/{id}` (description : champs + propriétés),
  `/collections/{id}/coverage`, `/collections/{id}/position`,
  `/collections/{id}/cube`.
- **Tests** : couverture des collections, description, position (PointSeries),
  coverage bbox/subset, cube, datetime, exclusivités, parsing `subset`/`datetime`.

### Modifié
- `MemProvider.Add` renvoie désormais une erreur si les dimensions X/Y/T
  déclarées sont absentes du Dataset.
- Binaire de démonstration : grille synthétique à deux paramètres (`t2m`,
  `uwind`) avec unités et `long_name`.

### Limites connues (documentées)
- Contrairement à pygeoapi, une variable sans attribut `units` est conservée
  (unité vide) plutôt qu'ignorée.
- Axe temporel numérique (pas encore de dates ISO 8601 ni de CRS autre que CRS84).
- Pas encore de formats natifs en sortie (zarr/netcdf), ni de sous-cube EDR sur
  un axe vertical `z`.

## [0.1.0] - 2026-08-07

### Ajouté
- Serveur OGC API - Coverages / EDR minimal adossé à xarray-go.
- Provider mémoire (une collection = un `DataArray` 2D lat/lon).
- Endpoints `landing`, `/collections`, `/collections/{id}`,
  `/collections/{id}/coverage?bbox=…`, `/collections/{id}/position?coords=…`.
- Export CoverageJSON (domaine Grid, CRS84) mono-paramètre via `xarray/geoapi`.
