# OGC API - Coverages dans gocoverage

**OGC API - Coverages** est la génération OpenAPI/REST qui succède au vieux
**WCS** (Web Coverage Service, XML/KVP). gocoverage l'implémente au-dessus du
même provider xarray que la partie EDR.

Principe conservé dans tout gocoverage : le CRS est **décrit**, jamais
**reprojeté** (voir `Collection.CRS`). Pas de dépendance type pyproj.

## WCS vs EDR — pourquoi les deux

| | WCS / OGC API - Coverages | EDR |
|---|---|---|
| Question | « donne-moi **la donnée** (ce pavé de la couverture) » | « donne-moi **une valeur/série** ici, maintenant » |
| Découpe | rectangulaire (bbox / subset / échelle) | géométrique (point, aire, trajectoire, corridor) |
| Sortie | raster (CoverageJSON, netCDF, Zarr) | CoverageJSON |
| Endpoints | `/coverage`, `/coverage/domainset`, `/coverage/rangetype` | `/position`, `/area`, `/cube`, `/trajectory`, `/corridor` |

Les deux familles cohabitent dans gocoverage et partagent le provider, le
CoverageJSON et la négociation de format (`f=json|netcdf|zarr`).

## Endpoints Coverages

### `GET /conformance`
Déclare les classes de conformité `ogcapi-coverages-1/1.0` : `core`, `coverage`,
`coverage-subset`, `coverage-bbox`, `coverage-datetime`, `coverage-rangesubset`,
`coverage-scaling`, `coverage-domainset`, `coverage-rangetype`, ainsi que les
encodages `covjson`/`netcdf`/`zarr`.

### `GET /collections/{id}/coverage`
Récupération de la couverture (déjà présent). Paramètres :

- `bbox=minx,miny,maxx,maxy` — emprise spatiale ;
- `subset=Axe(lo:hi),…` — sous-ensembles par axe nommé (Lat/Long/time/z) ;
- `datetime=lo/hi` — plage temporelle (ISO 8601, bornes ouvertes `..`) ;
- `properties=var1,var2` — sélection de champs (**range-subset**) ;
- `scale-factor=N` / `scale-axes=Axe(n),…` — **scaling** (voir plus bas) ;
- `f=json|netcdf|zarr` — format de sortie.

`bbox` et un `subset` de coordonnées sont exclusifs ; `datetime` et un `subset`
temporel aussi (repris de pygeoapi).

### `GET /collections/{id}/coverage/domainset`
Description du **domaine** au format CIS 1.1 `GeneralGridCoverage` : axes
réguliers `x`/`y` (`lowerBound`/`upperBound`/`resolution`), axes irréguliers
`z`/`t` (liste de coordonnées), et `gridLimits` (bornes d'index `i`/`j`/…).
Pendant du `DomainSet` de WCS `DescribeCoverage`.

### `GET /collections/{id}/coverage/rangetype`
Description des **champs** au format SWE Common `DataRecord` : un `Field` par
paramètre, avec une `Quantity` (`description`, `encodingInfo.dataType`, et `uom`
si l'unité est connue). Pendant du `RangeType` de WCS.

## Scaling (classe « scaling »)

Sous-échantillonnage de la grille par **moyennage de blocs** (agrégation
« average » de WCS), via `DataArray.Coarsen(dim, factor).Mean()` :

- `scale-factor=2` — applique le facteur 2 aux axes `x` **et** `y` ;
- `scale-axes=Long(2),Lat(3)` — facteur par axe (raffine/écrase `scale-factor`).

Un facteur `1` laisse l'axe intact. Le scaling s'applique **après** subset/bbox,
avant la sérialisation. C'est une réduction de résolution par agrégation, pas une
interpolation.

## Ce qui n'est pas (encore) couvert

- **Reprojection** (`crs`/`subset-crs` effectifs) : le CRS est décrit, non
  reprojeté — cohérent avec le reste de gocoverage et avec pygeoapi.
- **OpenAPI 3.0 servie** (`/api`) : non exposée (les classes `oas30` ne sont pas
  déclarées).
- **Scaling par `scale-size`** (taille cible absolue) : seul le facteur (pas
  entier) est géré ; `scale-size` (nombre de cellules visé) reste à faire.
