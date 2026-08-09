# gocoverage

Serveur **OGC API - Coverages / EDR** en Go, adossé à
[`xarray-go`](https://github.com/benedictemarty/xarray) comme couche de données
(« provider »).

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

## Capacités

Au-delà de la parité pygeoapi initiale, gocoverage couvre désormais un
sous-ensemble étendu d'OGC API - Coverages et EDR, avec lecture partielle.

**OGC API - Coverages** : `/coverage` (bbox, subset, datetime, properties,
scaling `scale-factor`/`scale-axes`/`scale-size`), `/coverage/domainset` (CIS
GeneralGrid), `/coverage/rangetype` (SWE DataRecord), `/conformance`, `/api`
(squelette OpenAPI). CoverageJSON, netCDF, Zarr, GeoJSON en sortie (`f=` ou `Accept`).

**OGC API - EDR** : `position`, `cube`, `area` (polygone à trous), `corridor`,
`radius`, `trajectory`, `locations`, `instances` — avec `parameter-name`,
`datetime`, `z`, `interpolation=bilinear`, distances métriques (`km`/`m`).

**OGC API - Maps** (successeur de WMS) : `/map` rend une variable en **image**
(`png`/`jpeg`) pour une emprise et une taille données — `bbox`, `width`/`height`,
`datetime`, `z`, `properties=<variable>`, `colorscalerange=min,max`,
`style`/`palette` (`viridis` par défaut, ou `grayscale`). Échantillonnage plus
proche voisin, NaN/hors-emprise transparents.

**Entrées** : `LoadNetCDF`, `LoadZarr`, `LoadChunkedZarr` (lecture élaguée par
chunks), `LoadPyramidZarr` (aperçus multi-résolution), `LoadGrib` (GRIB2) +
`ConvertGribToZarr` / `cmd/grib2zarr`.

**Passage à l'échelle** : `MemProvider` (tout en mémoire) ou `LazyFileProvider`
(résidence RAM bornée par LRU) ; les collections chunkées ne lisent que les
chunks recouvrant l'emprise/plage temporelle demandée, métadonnées comprises
(voir *Lecture élaguée*).

## Build & lancer

`gocoverage` dépend de `xarray-go`, résolu directement depuis GitHub (aucune
directive `replace`). Cloner puis lancer :

```bash
git clone https://github.com/benedictemarty/gocoverage
cd gocoverage
go run ./cmd/gocoverage      # écoute sur :8080 avec une collection de démo
```

Ou installer le binaire de démonstration sans cloner :

```bash
go install github.com/benedictemarty/gocoverage/cmd/gocoverage@latest
```

## Endpoints

| Route | Rôle |
|-------|------|
| `GET /` | landing page (liens data/conformance/api) |
| `GET /conformance` | classes de conformité (OGC API - Coverages/EDR) |
| `GET /api` | squelette OpenAPI 3.0 |
| `GET /collections` | liste des couvertures (id, titre, bbox, paramètres) |
| `GET /collections/{id}` | description conforme : `extent`, `crs`, `parameter_names`, `data_queries` |
| `GET /collections/{id}/coverage` | requête → **CoverageJSON** (+ scaling) |
| `GET /collections/{id}/coverage/domainset` | domaine CIS GeneralGrid |
| `GET /collections/{id}/coverage/rangetype` | champs SWE DataRecord |
| `GET /collections/{id}/map?bbox=…&width=…&height=…` | rendu **image** (OGC API - Maps, `f=png`/`jpeg`) |
| `GET /collections/{id}/position?coords=x,y` | point le plus proche (EDR, PointSeries) |
| `GET /collections/{id}/cube?bbox=…` | sous-cube (EDR) |
| `GET /collections/{id}/area?coords=POLYGON((…))` | découpe par polygone (à trous) |
| `GET /collections/{id}/corridor?coords=LINESTRING(…)&corridor-width=…` | tube autour d'une route |
| `GET /collections/{id}/radius?coords=POINT(…)&within=…&within-units=km` | disque autour d'un point |
| `GET /collections/{id}/trajectory?coords=LINESTRING(…)` | profil le long d'une polyligne |
| `GET /collections/{id}/locations` / `…/locations/{id}` | points nommés (ex. aéroports) |
| `GET /collections/{id}/instances` / `…/instances/{id}/…` | versions temporelles |

Paramètres de `coverage` : `properties=t2m,uwind`, `bbox=minx,miny,maxx,maxy`,
`subset=Lat(43:45),Long(0:2)`, `datetime=lo/hi`.
Paramètres de `position`/`cube` : `parameter-name=…`, `datetime=…`, `z=…`
(niveau vertical unique, sélectionné au plus proche si la collection a un axe Z).

`datetime` accepte l'**ISO 8601** (`2020-01-01`, `2020-01-01T06:00:00Z`,
intervalles `a/b`, bornes ouvertes `..`) ou des valeurs numériques (epoch). L'axe
temporel du CoverageJSON ressort en ISO 8601 quand les temps sont des secondes
epoch.

**Format de sortie** (`f=` ou en-tête `Accept`) : `json` (CoverageJSON, défaut),
`geojson` (FeatureCollection, refusé si multi-pas), `netcdf`/`nc` (netCDF natif)
ou `zarr` (archive ZIP du `.zarr`). Ex. `.../coverage?bbox=1,42,4,45&f=netcdf`.
Un `Accept` non satisfiable → 406 ; un `bbox-crs`/`crs` non supporté → 400 (pas
de reprojection).

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

- `provider.go` : `Collection`, `Provider`, `MemProvider`, `LazyFileProvider`, accès paresseux `grid()`.
- `fields.go` : `Fields` (get_fields) et `Properties` (coverage_properties).
- `query.go` : requête (`Query`), parsing `subset`/`datetime`, antiméridien, hook de lecture élaguée.
- `coveragejson.go` / `geojson.go` : sorties CoverageJSON / GeoJSON.
- `coverages.go` / `metadata.go` : OGC API - Coverages (conformance, domainset, rangetype, scaling) et description conforme (extent, data_queries).
- `edr.go`, `area.go`, `corridor.go`, `radius.go`, `trajectory.go`, `locations.go` : requêtes EDR.
- `fileprovider.go` : `LoadNetCDF` / `LoadZarr` ; `chunkzarr.go` : lecture Zarr élaguée par chunks (`LoadChunkedZarr`) ; `pyramid.go` : aperçus multi-résolution (`LoadPyramidZarr`) ; `grib.go` : GRIB2 (`LoadGrib`, `ConvertGribToZarr`).
- `crs.go`, `geodist.go`, `zarrout.go` : CRS (décrit), distances métriques, export Zarr.
- `server.go` : routeur `net/http`.
- `cmd/gocoverage` : démo ; `cmd/grib2zarr` : conversion GRIB2 → Zarr.

## Lecture élaguée (passage à l'échelle)

Une collection ouverte par `LoadChunkedZarr` (ou `LoadPyramidZarr`) n'est **jamais
chargée entièrement** : une requête à emprise (`bbox`) et/ou plage temporelle
(`datetime`) ne lit que les fichiers-chunks Zarr qui recouvrent la fenêtre — y
compris pour un cube 3D `[t, lat, lon]` (élagage sur les trois axes). Les
**métadonnées** (bbox, champs, domainset) sont servies depuis des indices légers
(coordonnées + schéma) sans matérialiser les données. `LoadPyramidZarr` sert en
plus un **niveau grossier** pour les grandes emprises. Périmètre du lecteur
élagué : Zarr v2, `<f8`, ordre C, compression none/zlib/zstd (blosc non géré →
repli sur `LoadZarr`). Combiné à `LazyFileProvider`, le nombre de collections
servies est découplé de la mémoire résidente.

## Limitations I/O (frontière réelle, mesurée)

L'**API et la sémantique** de xarray-go couvrent les besoins du provider pygeoapi
(`sel` slice/nearest, `.values`, `.attrs`, coords, `min`/`max`). L'**ouverture de
fichiers** reste plus étroite que xarray Python, mais le sprint « attributs CF »
a élargi le périmètre. État mesuré (fichiers générés puis passés à `LoadNetCDF`) :

| Fichier de test | Cas | Résultat `LoadNetCDF` |
|---|---|---|
| CDF-1, sans attributs | aller-retour xarray-go | ✅ chargé |
| CDF-1 **+ attributs** (`units`, `long_name`) | métadonnées CF | ✅ chargé, `units` exposé dans `Fields` |
| CDF-1 **+ `scale_factor`/`add_offset`/`_FillValue`** | packing | ✅ **dépacké** (`DecodeCF`), fill → NaN |
| CDF-1 **+ `time: "hours since…"`** | axe temporel CF | ✅ **décodé** en epoch (`DecodeTime`) |
| CDF-1 **+ `time` illimitée** (`numrecs`>0) | climato typique | ✅ **chargé** (variables d'enregistrement désentrelacées) |
| **CDF-2** (64-bit offset) / **CDF-5** | gros fichiers | 🔄 **via convertisseur** (`nccopy`/`cdo` → CDF-1) sinon erreur explicite |
| **NetCDF-4 / HDF5** | défaut de xarray/CDO | 🔄 **via convertisseur** (`nccopy`/`cdo` → CDF-1) sinon erreur explicite |

Toutes les lignes ✅ ci-dessus sont **vérifiées empiriquement** sur des fichiers
écrits par Python xarray (`NETCDF3_CLASSIC`). Les lignes 🔄 passent par
`xarray.OpenNetCDFFile`, qui délègue à un convertisseur externe détecté dans le
PATH ; **validées de bout en bout** avec `nccopy` (netCDF 4.9.3) sur un vrai
NetCDF-4/HDF5 (superblock v2) : `LoadNetCDF` bâtit la collection attendue.

**Conclusion honnête :** `LoadNetCDF` lit nativement le **CDF-1 classique** (avec
attributs, décodage CF packing + temps, dimension illimitée). Les formats binaires
**NetCDF-4/HDF5** et **CDF-2/5** sont pris en charge **par conversion externe**
(`nccopy` ou `cdo` requis dans le PATH) — il n'y a **pas** de lecteur HDF5 en Go.
Pour l'écosystème complet (dask, CF avancé, lecture HDF5 sans outil externe),
xarray-go n'est **pas** un substitut à xarray Python ; il l'est pour l'API, les
opérations sur tableaux numériques, l'échange CDF-1 + CF, et l'ouverture des
formats binaires via un convertisseur.

## Autres limites connues

- **CRS décrit, non reprojeté** (comme pygeoapi) : CRS84 par défaut ; un CRS
  EPSG (géographique/projeté) peut être fixé via `Collection.CRS` ou détecté
  depuis une variable de conteneur CF (`grid_mapping_name`/`epsg_code`). Les
  données ne sont pas reprojetées ; `bbox`/`coords` sont dans le CRS de stockage.
- Une variable sans `units` est conservée (unité vide) — divergence assumée avec
  pygeoapi qui l'ignore.
- Sorties natives disponibles : **netCDF** (`f=netcdf`) et **Zarr** zippé
  (`f=zarr`).
- **Axe vertical `z`** : pris en charge en **sélection** EDR (`z=…`, niveau unique
  au plus proche) ; un axe à plusieurs niveaux n'est pas représentable en
  CoverageJSON (sélectionnez un niveau → sinon HTTP 400).
- `/conformance` et `/api` (squelette OpenAPI) sont exposés ; il n'y a **pas** de
  représentation **HTML**, ni de reprojection effective, ni de rendu de carte
  (OGC API - Maps/Tiles hors périmètre).

Voir [`CHANGELOG.md`](CHANGELOG.md), [`docs/parite-pygeoapi.md`](docs/parite-pygeoapi.md)
(cartographie fonction par fonction) et [`docs/SYNTHESE.md`](docs/SYNTHESE.md)
(document de clôture : parcours 0.1→0.10, parité, écarts assumés, validation).
