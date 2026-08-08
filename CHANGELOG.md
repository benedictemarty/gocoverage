# Changelog

Toutes les modifications notables de gocoverage sont consignées dans ce fichier.

Le format s'inspire de [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/)
et le projet suit un versionnement sémantique.

## [0.18.0] - 2026-08-08

### Ajouté — Raffinements EDR (métrique, interpolation, GeoJSON)

- **Distances métriques** pour `radius` et `corridor` : `within-units` /
  `corridor-width-units` acceptent désormais `km`/`m` (en plus de `deg`). La
  distance est alors calculée en **mètres** (projection équirectangulaire locale,
  correcte à haute latitude via cos(lat)), et non plus en degrés euclidiens.
  `area` reste géométrique (inclusion), non concernée. Helpers `geodist.go`.
- **Interpolation bilinéaire au point** : `position?interpolation=bilinear`
  échantillonne la valeur au point **exact** (via `xarray.InterpBilinear`) au lieu
  du plus proche voisin ; le point interpolé est conservé comme coordonnée x/y
  (PointSeries). Défaut inchangé (plus proche voisin).
- **Négociation de contenu `f=geojson`** sur les endpoints passant par
  `writeCoverage` (coverage, position, cube, area, corridor, radius) : sortie
  **FeatureCollection GeoJSON** (un Point par cellule + valeurs en propriétés).
  Complète `json`/`netcdf`/`zarr`. `Collection.GeoJSON`.
- Tests : `TestPositionBilinear`, `TestGeoJSONOutput`, `TestCorridorMetric`,
  `TestRadiusMetric`, `TestLengthMeters`, `TestBadFormat`.

## [0.17.0] - 2026-08-08

### Ajouté — EDR `instances`

- **Requête EDR `instances`** : versions temporelles d'une collection (ex. runs
  de modèle successifs 00Z/06Z/12Z). Champ optionnel `Collection.Instances`
  (`[]*Collection`), chaque instance étant une (sous-)collection complète.
  - `GET /collections/{id}/instances` → liste des instances (id, titre, bbox).
  - `GET /collections/{id}/instances/{instanceId}[/…]` → **toutes les requêtes**
    (describe, coverage, position, cube, trajectory, area, corridor, radius,
    locations) s'exécutent sur la sous-collection. Instance inconnue → 404 ;
    instances imbriquées refusées.
- Refactor : le routage d'action est factorisé (`dispatchAction`) et réutilisé
  pour les instances. `Collection.InstancesInfo`, `InstanceByID`. Tests : liste,
  routage d'une requête vers la bonne instance, 404, imbrication.

## [0.16.0] - 2026-08-08

### Ajouté — EDR `locations`

- **Requête EDR `locations`** : points nommés prédéfinis (aéroports, stations…).
  Champ optionnel `Collection.Locations` (`[]NamedLocation{ID, Name, Lon, Lat}`).
  - `GET /collections/{id}/locations` → **FeatureCollection GeoJSON** listant les
    points nommés.
  - `GET /collections/{id}/locations/{locationId}` → donnée au point (au plus
    proche voisin, via `Position`) en **CoverageJSON** ; location inconnue → 404.
    Options `datetime`/`z`/`parameter-name`.
- Cas aviation : interroger la météo d'un aérodrome par son **code OACI** plutôt
  que par coordonnées. `Collection.LocationsGeoJSON`, `LocationByID`,
  `LocationCoverageJSON`. Lien ajouté à la description. Tests : liste, extraction,
  404, HTTP.

## [0.15.0] - 2026-08-08

### Ajouté — EDR `radius`

- **Requête EDR `radius`** : sous-ensemble dans un **disque** autour d'un point
  (`coords=POINT(lon lat)`, `within=<rayon>`, `within-units=deg|km|m`). Restreint
  à l'emprise carrée du disque puis masque (→ null) les cellules à plus de `within`
  du centre. Sortie CoverageJSON.
- Conversion d'unités **approximative** (1° ≈ 111,32 km, sans correction de
  latitude — assumé). Endpoint `GET /collections/{id}/radius` + lien dans la
  description. `Collection.Radius`, `parsePoint`, `radiusInDegrees` ;
  masquage factorisé (`maskDataset`). Tests : conversion, masquage, parsing, HTTP.

## [0.14.0] - 2026-08-08

### Ajouté — OGC API - Coverages (successeur moderne de WCS)

Complète le point d'accès `/coverage` existant pour couvrir OGC API - Coverages
(l'API OpenAPI qui remplace WCS ; le CRS est **décrit**, jamais reprojeté).

- **`GET /conformance`** : déclaration des classes de conformité
  (`ogcapi-coverages-1` : core, coverage, coverage-subset, -bbox, -datetime,
  -rangesubset, -scaling, -domainset, -rangetype, covjson/netcdf/zarr). Lien
  ajouté à la landing page.
- **`GET /collections/{id}/coverage/domainset`** : description du domaine (CIS 1.1
  `GeneralGridCoverage`) — axes réguliers `x`/`y` (bornes + résolution), axes
  irréguliers `z`/`t` (coordonnées), `gridLimits` en index. Pendant du DomainSet
  de WCS `DescribeCoverage`. `Collection.DomainSet`.
- **`GET /collections/{id}/coverage/rangetype`** : description des champs (SWE
  Common `DataRecord` : `Quantity` par champ, `uom`). Pendant du RangeType de
  WCS. `Collection.RangeType`. Liens ajoutés à la description de collection.
- **Scaling** (classe « scaling ») sur `/coverage` : `scale-factor` (global x/y)
  et `scale-axes=Axe(n),…` (par axe) — **sous-échantillonnage par moyennage de
  blocs** (`Coarsen().Mean()`, agrégation « average » de WCS). `parseScaling`,
  `applyScaling`.
- Tests : `TestConformance`, `TestLandingConformanceLink`, `TestDomainSet`,
  `TestRangeType`, `TestCoverageScaling`, `TestScaleAxes`, `TestScaleFactorInvalid`.

## [0.13.0] - 2026-08-08

### Ajouté — EDR `corridor`

- **Requête EDR `corridor`** : sous-ensemble dans un **tube** (buffer) autour
  d'une polyligne (route), de largeur `corridor-width` (degrés). Restreint à
  l'emprise élargie puis masque (→ null) les cellules à plus de `width/2` de la
  polyligne (distance point↔segment). Sortie CoverageJSON. **Cas aviation** :
  couloir autour d'une route de vol.
- Endpoint `GET /collections/{id}/corridor?coords=LINESTRING(…)&corridor-width=…`
  + lien dans la description. `Collection.Corridor`, `distToPolyline`. Masquage
  factorisé (`maskDataset`) partagé avec `area`. Tests : distances, tube diagonal,
  HTTP.

## [0.12.0] - 2026-08-08

### Ajouté — EDR `area`

- **Requête EDR `area`** : sous-ensemble par **polygone** arbitraire (WKT
  `POLYGON((lon lat, …))`). Restreint à l'emprise du polygone puis masque (→ null)
  les cellules dont le centre est **hors du polygone** (point-in-polygon, lancer
  de rayon). Sortie CoverageJSON (grille). Options `datetime`/`parameter-name`.
- Endpoint `GET /collections/{id}/area?coords=POLYGON((…))` + lien dans la
  description. `Collection.Area`, `pointInPolygon`, `parsePolygon`. Tests :
  point-in-polygon, masquage triangulaire, HTTP, parsing WKT.

## [0.11.0] - 2026-08-08

### Ajouté — EDR `trajectory`

- **Requête EDR `trajectory`** : échantillonne les paramètres le long d'une
  **polyligne** (route), au plus proche voisin, avec `datetime`/`z`/`parameter-name`
  optionnels. Sortie **CoverageJSON de domaine `Trajectory`** (axe `composite` de
  tuples `[x, y]`). Utile pour un profil météo le long d'une route (aviation).
- Endpoint `GET /collections/{id}/trajectory?coords=LINESTRING(lon lat, …)`
  (WKT LINESTRING ou repli `lon,lat;lon,lat`). Lien ajouté à la description de
  collection.
- `Collection.Trajectory` (échantillonnage) et `Collection.TrajectoryCoverageJSON`.
  Tests : échantillonnage diagonal, HTTP, parsing WKT/repli.

## [0.10.0] - 2026-08-07

### Sprint « parité pygeoapi #9 » — résolution et durée temporelles (ISO 8601)

- **`Properties()`** renseigne désormais `restime` (résolution) et
  `time_duration` (durée) en **ISO 8601** — pendants de `get_time_resolution` et
  `get_time_coverage_duration` de pygeoapi — quand l'axe temporel est en secondes
  epoch. Ex. pas de 6 h → `restime: "PT6H"`, span de 12 h → `time_duration: "PT12H"`.
- Helper `iso8601Duration(secondes)` (jours/heures/minutes/secondes).
- Tests : `TestISO8601Duration`, `TestPropertiesTimeResolution`.

## [0.9.0] - 2026-08-07

### Sprint « parité pygeoapi #8 » — description du CRS (sans reprojection)

Reproduit `_parse_storage_crs` + le `crs_type`/`bbox_crs` de pygeoapi : le CRS
est **décrit**, jamais **reprojeté** (pygeoapi ne gère pas non plus `bbox_crs`).

- **`Collection.CRS`** (type `CRS` : id URI, type Geographic/Projected, EPSG) —
  zéro-valeur = **CRS84**. Constructeurs `CRS84()` et `EPSGCRS(code, projected)`.
  Pendant de l'override `provider_def['storage_crs']`.
- **`CoverageJSON` et `Properties`** exposent désormais le CRS de la collection
  (`referencing.system.type`/`id`, `bbox_crs`, `crs_type`) au lieu de CRS84 figé.
- **`detectCRS`** : détection best-effort du CRS de stockage depuis une variable
  de conteneur CF (`grid_mapping_name`/`epsg_code`, ou nom usuel `crs`/
  `spatial_ref`…). La variable de conteneur est retirée des paramètres exposés.
- Sans pyproj en Go : pas de reprojection ; détection limitée aux cas portant un
  code EPSG (sinon CRS84, ou type `ProjectedCRS` sans id si projeté inconnu).
- Tests : `TestCoverageJSONProjectedCRS`, `TestDefaultCRS84`, `TestDetectCRSFromVar`.

## [0.8.0] - 2026-08-07

### Sprint « parité pygeoapi #7 » — sortie native Zarr (ZIP)

- **`f=zarr`** : export du sous-cube en **archive ZIP d'un répertoire `.zarr`**
  (via `xarray.WriteDatasetZarr`), `Content-Type: application/zip` — pendant Go de
  `_get_zarr_data` de pygeoapi. Complète `f=netcdf` (0.7.0).
- Correction d'une affirmation erronée des notes précédentes : xarray-go **possède
  bien** un writer Zarr (`WriteDatasetZarr`) ; la sortie Zarr est donc disponible.
- Test `TestCoverageFormatZarr` : requête `f=zarr` → dézip → relecture via
  `ReadDatasetZarr` (round-trip complet).

## [0.7.0] - 2026-08-07

### Sprint « parité pygeoapi #6 » — format de sortie natif (netCDF)

- **Négociation de format via `f`** — pendant de `query(format_=…)` de pygeoapi.
  `f=json`/`covjson` (défaut) → CoverageJSON ; `f=netcdf`/`nc` → export **netCDF
  natif** du sous-cube (via `xarray.WriteNetCDF`), `Content-Type:
  application/x-netcdf` + `Content-Disposition`.
- `f=zarr` renvoie une erreur explicite (pas d'écriture Zarr en xarray-go) ; un
  format inconnu → 400.
- Un axe vertical multi-niveaux à l'export CoverageJSON renvoie désormais **400**
  (erreur client-corrigible `ErrSelectLevel`, « sélectionnez un niveau »), au lieu
  de 500.
- Tests : `TestCoverageFormatNetCDF` (round-trip requête → netCDF → relecture),
  `TestCoverageFormatInconnu`, `TestCoverageFormatZarrIndisponible`.

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
