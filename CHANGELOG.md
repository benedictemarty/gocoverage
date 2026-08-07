# Changelog

Toutes les modifications notables de gocoverage sont consignées dans ce fichier.

Le format s'inspire de [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/)
et le projet suit un versionnement sémantique.

## [0.6.0] - 2026-08-07

### Sprint « parité pygeoapi #5 » — axe vertical z (EDR)

- **`Collection.ZDim`** : axe vertical optionnel (ex. `z`/`level`/`height`/
  `pressure`), détecté par nom à l'ouverture de fichier.
- **Paramètre EDR `z`** dans `position` et `cube` : sélection du **niveau le plus
  proche** (comme `XarrayEDRProvider`), qui réduit la dimension verticale. `z`
  fourni à une collection sans axe vertical est ignoré (comme pygeoapi).
- **`subset`/`resolveAxis`** : alias d'axe vertical (`z`, `level`, `height`,
  `depth`, `elevation`, `vertical`).
- **Garde CoverageJSON** : un axe vertical à plusieurs niveaux n'étant pas
  représentable dans le domaine Grid (x/y/t), l'export renvoie une erreur
  explicite invitant à sélectionner un niveau (`z=…`).
- Tests : `TestPositionWithZ`, `TestCubeWithZ`, `TestCoverageZMultiLevelRejected`,
  `TestZOnCollectionWithoutZ`.

## [0.5.0] - 2026-08-07

### Sprint « parité pygeoapi #4 » — datetime ISO 8601

- **`datetime` accepte l'ISO 8601 en entrée** (comme pygeoapi) : dates
  (`2020-01-01`), instants (`2020-01-01T06:00:00Z`), intervalles `a/b` et bornes
  ouvertes `..`, converties vers le temps interne (secondes epoch). Les valeurs
  numériques restent acceptées (rétro-compatibilité, axes temps synthétiques).
- **Sortie CoverageJSON** : l'axe temporel `t` est formaté en **ISO 8601**
  lorsque les valeurs sont des secondes epoch plausibles (sinon numérique),
  au lieu d'un nombre brut.
- `subset=time(...)` accepte les dates seules (l'heure `hh:mm:ss` entre en
  conflit avec le séparateur de plage `:` ; utiliser `datetime` pour un instant).
- Tests : `TestParseDatetimeISO`, `TestCoverageJSONTimeISO` + validation HTTP
  bout en bout (`datetime` ISO filtre l'axe temps, sortie ISO).

## [0.4.0] - 2026-08-07

### Sprint « parité pygeoapi #3 » — ouverture NetCDF-4/HDF5 par conversion

S'appuie sur le sprint 61 de xarray-go (`OpenNetCDFFile`).

- **`LoadNetCDF` ouvre désormais les fichiers NetCDF-4/HDF5 et CDF-2/5** en
  déléguant à un convertisseur externe (`nccopy` ou `cdo`) qui les réécrit en
  CDF-1, puis en appliquant la lecture + décodage CF habituels. Le CDF-1 reste lu
  directement, sans outil externe.
- Si aucun convertisseur n'est présent, un fichier binaire échoue par une erreur
  explicite (jamais de panic). Pas de lecteur HDF5 en Go : dépendance à
  `nccopy`/`cdo` assumée pour ces formats.
- README : tableau « Limitations I/O » mis à jour (lignes 🔄 via convertisseur).

## [0.3.0] - 2026-08-07

### Sprint « parité pygeoapi #2 » — décodage CF à l'ouverture netCDF

S'appuie sur le sprint 60 de xarray-go (attributs netCDF + `DecodeCF`/`DecodeTime`).

### Ajouté / Modifié
- **`LoadNetCDF` décode désormais les conventions CF** (comme `decode_cf=True`
  de pygeoapi) :
  - *packing* : `scale_factor`/`add_offset` appliqués, `_FillValue`/
    `missing_value` → NaN (via `xarray.DecodeCF`) ;
  - *temps* : axe `time` en « `<unité> since <date>` » converti en secondes
    depuis l'epoch (via `xarray.DecodeTime`), quand l'axe T est détecté ;
  - les **attributs de variable** (`units`, `long_name`) sont maintenant lus
    depuis le fichier et exposés par `Fields()` (`get_fields`).
- Test d'intégration `TestLoadNetCDFCF` : écriture d'un netCDF CF → `LoadNetCDF`
  → vérification du dépacking, du `_FillValue`→NaN et des `units`.

### Corrigé (frontière I/O, cf. tableau README)
- Les netCDF **avec attributs CF** se chargent désormais (auparavant :
  `unexpected EOF`).
- Les netCDF à **dimension `time` illimitée** (cas climato le plus courant) se
  chargent désormais (auparavant : panic, puis rejet). Variables d'enregistrement
  désentrelacées côté xarray-go (sprint 60).
- Lignes ✅ du tableau I/O du README **vérifiées empiriquement** sur des fichiers
  écrits par Python xarray.

### Reste hors périmètre
- NetCDF-4/HDF5, CDF-2/5, dask, CF avancé.

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
  - ⚠️ **Portée réelle limitée** (mesurée empiriquement, voir README) :
    `LoadNetCDF` ne lit fiablement que du **CDF-1 classique sans attributs**
    (typiquement un aller-retour depuis xarray-go). Les fichiers réels — NetCDF-4/
    HDF5, CDF-2/5, **attributs CF** (`units`, `long_name`), **dimension `time`
    illimitée**, **packing** `scale_factor`/`add_offset` — ne se chargent PAS.
    Zarr couvre le v2 non compressé / blosc selon l'implémentation xarray-go.
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
