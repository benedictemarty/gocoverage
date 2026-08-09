package gocoverage

import (
	"fmt"
	"strconv"
	"strings"
)

// Sous-ensemble de CQL2-text (OGC API - Features Part 3 : Filtering) évalué sur
// les propriétés d'une entité. Gère les comparaisons de base
// (`=`, `<>`/`!=`, `<`, `<=`, `>`, `>=`) sur des littéraux numériques ou chaînes,
// combinées par `AND`/`OR` (insensible à la casse) et regroupées par parenthèses.
// Exemples : `t2m > 20`, `t2m >= 10 AND uwind < 5`, `(a = 1 OR b = 2) AND c > 0`.

// cqlFeat est l'entité évaluée par un filtre : ses propriétés et la position de
// son point (pour les prédicats spatiaux).
type cqlFeat struct {
	props    map[string]interface{}
	lon, lat float64
}

// cqlExpr est un nœud de l'arbre de filtre : il évalue une entité à vrai/faux.
type cqlExpr interface {
	eval(f cqlFeat) bool
}

type cqlAnd struct{ l, r cqlExpr }

func (e cqlAnd) eval(f cqlFeat) bool { return e.l.eval(f) && e.r.eval(f) }

type cqlOr struct{ l, r cqlExpr }

func (e cqlOr) eval(f cqlFeat) bool { return e.l.eval(f) || e.r.eval(f) }

// cqlCmp compare la propriété prop à une valeur (numérique ou chaîne).
type cqlCmp struct {
	prop  string
	op    string
	num   float64
	str   string
	isNum bool
}

func (e cqlCmp) eval(f cqlFeat) bool {
	v, ok := f.props[e.prop]
	if !ok || v == nil {
		return false // propriété absente ou nulle (NaN) → non satisfaite
	}
	if e.isNum {
		x, ok := v.(float64)
		if !ok {
			return false
		}
		return cmpNum(x, e.op, e.num)
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return cmpStr(s, e.op, e.str)
}

// cqlIn : appartenance à une liste (IN / NOT IN). Selon le type de la valeur de
// la propriété, on teste l'appartenance à nums ou strs.
type cqlIn struct {
	prop   string
	nums   []float64
	strs   []string
	negate bool
}

func (e cqlIn) eval(f cqlFeat) bool {
	v, ok := f.props[e.prop]
	if !ok || v == nil {
		return false
	}
	in := false
	switch t := v.(type) {
	case float64:
		for _, n := range e.nums {
			if t == n {
				in = true
				break
			}
		}
	case string:
		for _, s := range e.strs {
			if t == s {
				in = true
				break
			}
		}
	}
	return in != e.negate
}

// cqlLike : correspondance de motif (LIKE / NOT LIKE) avec `%` (toute séquence)
// et `_` (un caractère).
type cqlLike struct {
	prop    string
	pattern string
	negate  bool
}

func (e cqlLike) eval(f cqlFeat) bool {
	v, ok := f.props[e.prop]
	if !ok || v == nil {
		return false
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return likeMatch(e.pattern, s) != e.negate
}

// cqlBetween : intervalle fermé (BETWEEN lo AND hi / NOT BETWEEN).
type cqlBetween struct {
	prop   string
	lo, hi float64
	negate bool
}

func (e cqlBetween) eval(f cqlFeat) bool {
	v, ok := f.props[e.prop]
	if !ok || v == nil {
		return false
	}
	x, ok := v.(float64)
	if !ok {
		return false
	}
	within := x >= e.lo && x <= e.hi
	return within != e.negate
}

// cqlNull : test de nullité (IS NULL / IS NOT NULL). negate=true → IS NOT NULL.
type cqlNull struct {
	prop   string
	negate bool
}

func (e cqlNull) eval(f cqlFeat) bool {
	v, ok := f.props[e.prop]
	isNull := !ok || v == nil
	return isNull != e.negate
}

// likeMatch teste un motif SQL LIKE (`%`, `_`) contre s (correspondance totale).
func likeMatch(pattern, s string) bool {
	// Programmation dynamique classique sur les octets (motifs ASCII attendus).
	m, n := len(pattern), len(s)
	dp := make([][]bool, m+1)
	for i := range dp {
		dp[i] = make([]bool, n+1)
	}
	dp[0][0] = true
	for i := 1; i <= m; i++ {
		if pattern[i-1] == '%' {
			dp[i][0] = dp[i-1][0]
		}
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			switch pattern[i-1] {
			case '%':
				dp[i][j] = dp[i-1][j] || dp[i][j-1]
			case '_':
				dp[i][j] = dp[i-1][j-1]
			default:
				dp[i][j] = dp[i-1][j-1] && pattern[i-1] == s[j-1]
			}
		}
	}
	return dp[m][n]
}

// cqlSpatial : prédicat spatial (S_INTERSECTS / S_WITHIN) entre le point de
// l'entité et un polygone (avec trous). negate → S_DISJOINT.
type cqlSpatial struct {
	rings  [][][2]float64
	negate bool
}

func (e cqlSpatial) eval(f cqlFeat) bool {
	in := pointInRings(f.lon, f.lat, e.rings)
	return in != e.negate
}

// isGeomKeyword reconnaît les mots-clés de géométrie WKT.
func isGeomKeyword(w string) bool {
	switch strings.ToUpper(w) {
	case "POLYGON", "MULTIPOLYGON", "POINT", "LINESTRING", "MULTILINESTRING", "MULTIPOINT":
		return true
	}
	return false
}

// balancedParen renvoie l'indice juste après la parenthèse fermante équilibrant
// celle en position open dans s.
func balancedParen(s string, open int) (int, error) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("CQL2 : parenthèse de géométrie non fermée")
}

// isSpatialFunc reconnaît un nom de prédicat spatial géré.
func isSpatialFunc(w string) bool {
	switch strings.ToUpper(w) {
	case "S_INTERSECTS", "S_WITHIN", "S_CONTAINS", "S_DISJOINT":
		return true
	}
	return false
}

func cmpNum(a float64, op string, b float64) bool {
	switch op {
	case "=":
		return a == b
	case "<>", "!=":
		return a != b
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	}
	return false
}

func cmpStr(a, op, b string) bool {
	switch op {
	case "=":
		return a == b
	case "<>", "!=":
		return a != b
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	}
	return false
}

// ParseCQL2Text analyse une expression CQL2-text en un arbre de filtre.
func ParseCQL2Text(s string) (cqlExpr, error) {
	toks, err := cqlTokenize(s)
	if err != nil {
		return nil, err
	}
	p := &cqlParser{toks: toks}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("CQL2 : jeton inattendu %q", p.toks[p.pos])
	}
	return e, nil
}

// cqlTokenize découpe l'expression en jetons (opérateurs, parenthèses, chaînes,
// identifiants, nombres, mots-clés AND/OR).
func cqlTokenize(s string) ([]string, error) {
	var toks []string
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '(' || c == ')' || c == ',':
			toks = append(toks, string(c))
			i++
		case c == '<' || c == '>' || c == '=' || c == '!':
			j := i + 1
			if j < n && (s[j] == '=' || s[j] == '>') { // <=, >=, <>, !=
				toks = append(toks, s[i:j+1])
				i = j + 1
			} else {
				toks = append(toks, s[i:i+1])
				i++
			}
		case c == '\'':
			j := i + 1
			for j < n && s[j] != '\'' {
				j++
			}
			if j >= n {
				return nil, fmt.Errorf("CQL2 : chaîne non terminée")
			}
			toks = append(toks, s[i:j+1]) // conserve les quotes pour la classification
			i = j + 1
		default:
			j := i
			for j < n {
				d := s[j]
				if d == ' ' || d == '\t' || d == '\n' || d == '(' || d == ')' || d == ',' ||
					d == '<' || d == '>' || d == '=' || d == '!' || d == '\'' {
					break
				}
				j++
			}
			if j == i {
				return nil, fmt.Errorf("CQL2 : caractère inattendu %q", string(c))
			}
			word := s[i:j]
			// Géométrie WKT (POLYGON/POINT/…) : capturer le mot-clé et sa
			// parenthèse équilibrée en un seul jeton (sinon le WKT serait éclaté).
			if isGeomKeyword(word) {
				k := j
				for k < n && (s[k] == ' ' || s[k] == '\t') {
					k++
				}
				if k < n && s[k] == '(' {
					end, err := balancedParen(s, k)
					if err != nil {
						return nil, err
					}
					toks = append(toks, s[i:end])
					i = end
					continue
				}
			}
			toks = append(toks, word)
			i = j
		}
	}
	return toks, nil
}

type cqlParser struct {
	toks []string
	pos  int
}

func (p *cqlParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *cqlParser) parseOr() (cqlExpr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for strings.EqualFold(p.peek(), "OR") {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = cqlOr{left, right}
	}
	return left, nil
}

func (p *cqlParser) parseAnd() (cqlExpr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for strings.EqualFold(p.peek(), "AND") {
		p.pos++
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = cqlAnd{left, right}
	}
	return left, nil
}

func (p *cqlParser) parsePrimary() (cqlExpr, error) {
	if p.peek() == "(" {
		p.pos++
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("CQL2 : parenthèse fermante attendue")
		}
		p.pos++
		return e, nil
	}
	return p.parseComparison()
}

// cqlOps liste les opérateurs de comparaison reconnus.
var cqlOps = map[string]bool{"=": true, "<>": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

func (p *cqlParser) parseComparison() (cqlExpr, error) {
	if p.pos >= len(p.toks) {
		return nil, fmt.Errorf("CQL2 : comparaison incomplète")
	}
	// Prédicat spatial : S_INTERSECTS(geom, <WKT>) etc.
	if isSpatialFunc(p.toks[p.pos]) && p.pos+1 < len(p.toks) && p.toks[p.pos+1] == "(" {
		return p.parseSpatial()
	}

	prop := p.toks[p.pos]
	if isCQLReserved(prop) {
		return nil, fmt.Errorf("CQL2 : nom de propriété attendu près de %q", prop)
	}
	p.pos++

	// Prédicat éventuel : [NOT] IN|LIKE|BETWEEN, ou IS [NOT] NULL.
	negate := false
	kw := strings.ToUpper(p.peek())
	if kw == "NOT" {
		negate = true
		p.pos++
		kw = strings.ToUpper(p.peek())
	}
	switch kw {
	case "IN":
		p.pos++
		return p.parseIn(prop, negate)
	case "LIKE":
		p.pos++
		return p.parseLike(prop, negate)
	case "BETWEEN":
		p.pos++
		return p.parseBetween(prop, negate)
	case "IS":
		if negate {
			return nil, fmt.Errorf("CQL2 : NOT inattendu avant IS")
		}
		p.pos++
		return p.parseIsNull(prop)
	}
	if negate {
		return nil, fmt.Errorf("CQL2 : NOT inattendu près de %q", prop)
	}

	// Comparaison classique : op littéral.
	op := p.peek()
	if !cqlOps[op] {
		return nil, fmt.Errorf("CQL2 : opérateur attendu près de %q", prop)
	}
	p.pos++
	num, str, isNum, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return cqlCmp{prop: prop, op: op, num: num, str: str, isNum: isNum}, nil
}

// parseLiteral consomme un littéral numérique ou chaîne (`'…'`).
func (p *cqlParser) parseLiteral() (num float64, str string, isNum bool, err error) {
	if p.pos >= len(p.toks) {
		return 0, "", false, fmt.Errorf("CQL2 : littéral attendu")
	}
	val := p.toks[p.pos]
	p.pos++
	if strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'") && len(val) >= 2 {
		return 0, strings.Trim(val, "'"), false, nil
	}
	f, e := strconv.ParseFloat(val, 64)
	if e != nil {
		return 0, "", false, fmt.Errorf("CQL2 : littéral invalide %q", val)
	}
	return f, "", true, nil
}

// parseIn analyse `IN (v1, v2, …)`.
func (p *cqlParser) parseIn(prop string, negate bool) (cqlExpr, error) {
	if p.peek() != "(" {
		return nil, fmt.Errorf("CQL2 : '(' attendu après IN")
	}
	p.pos++
	e := cqlIn{prop: prop, negate: negate}
	for {
		num, str, isNum, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		if isNum {
			e.nums = append(e.nums, num)
		} else {
			e.strs = append(e.strs, str)
		}
		switch p.peek() {
		case ",":
			p.pos++
		case ")":
			p.pos++
			return e, nil
		default:
			return nil, fmt.Errorf("CQL2 : ',' ou ')' attendu dans la liste IN")
		}
	}
}

// parseLike analyse `LIKE '<motif>'`.
func (p *cqlParser) parseLike(prop string, negate bool) (cqlExpr, error) {
	val := p.peek()
	if !strings.HasPrefix(val, "'") || !strings.HasSuffix(val, "'") || len(val) < 2 {
		return nil, fmt.Errorf("CQL2 : motif entre quotes attendu après LIKE")
	}
	p.pos++
	return cqlLike{prop: prop, pattern: strings.Trim(val, "'"), negate: negate}, nil
}

// parseBetween analyse `BETWEEN <lo> AND <hi>`.
func (p *cqlParser) parseBetween(prop string, negate bool) (cqlExpr, error) {
	lo, _, isNum, err := p.parseLiteral()
	if err != nil || !isNum {
		return nil, fmt.Errorf("CQL2 : borne basse numérique attendue après BETWEEN")
	}
	if !strings.EqualFold(p.peek(), "AND") {
		return nil, fmt.Errorf("CQL2 : AND attendu dans BETWEEN")
	}
	p.pos++
	hi, _, isNum, err := p.parseLiteral()
	if err != nil || !isNum {
		return nil, fmt.Errorf("CQL2 : borne haute numérique attendue dans BETWEEN")
	}
	return cqlBetween{prop: prop, lo: lo, hi: hi, negate: negate}, nil
}

// parseIsNull analyse `IS [NOT] NULL`.
func (p *cqlParser) parseIsNull(prop string) (cqlExpr, error) {
	negate := false
	if strings.EqualFold(p.peek(), "NOT") {
		negate = true
		p.pos++
	}
	if !strings.EqualFold(p.peek(), "NULL") {
		return nil, fmt.Errorf("CQL2 : NULL attendu après IS")
	}
	p.pos++
	return cqlNull{prop: prop, negate: negate}, nil
}

// parseSpatial analyse `S_INTERSECTS|S_WITHIN|S_CONTAINS|S_DISJOINT(geom, <WKT>)`.
// L'entité étant un point, S_INTERSECTS/S_WITHIN/S_CONTAINS se ramènent au test
// point-dans-polygone ; S_DISJOINT en est la négation.
func (p *cqlParser) parseSpatial() (cqlExpr, error) {
	fn := strings.ToUpper(p.toks[p.pos])
	p.pos++ // nom de fonction
	if p.peek() != "(" {
		return nil, fmt.Errorf("CQL2 : '(' attendu après %s", fn)
	}
	p.pos++
	// Premier argument : référence à la géométrie de l'entité.
	if !strings.EqualFold(p.peek(), "geom") && !strings.EqualFold(p.peek(), "geometry") {
		return nil, fmt.Errorf("CQL2 : premier argument 'geom' attendu dans %s", fn)
	}
	p.pos++
	if p.peek() != "," {
		return nil, fmt.Errorf("CQL2 : ',' attendu dans %s", fn)
	}
	p.pos++
	// Second argument : littéral WKT (capturé en un seul jeton).
	wkt := p.peek()
	if !isGeomKeyword(strings.TrimSpace(strings.SplitN(wkt, "(", 2)[0])) {
		return nil, fmt.Errorf("CQL2 : géométrie WKT attendue dans %s", fn)
	}
	rings, err := parsePolygonRings(wkt)
	if err != nil {
		return nil, fmt.Errorf("CQL2 : géométrie invalide dans %s: %w", fn, err)
	}
	p.pos++
	if p.peek() != ")" {
		return nil, fmt.Errorf("CQL2 : ')' attendu pour clore %s", fn)
	}
	p.pos++
	return cqlSpatial{rings: rings, negate: fn == "S_DISJOINT"}, nil
}

// isCQLReserved indique si un jeton est un mot réservé (ne peut pas être un nom
// de propriété en tête de comparaison).
func isCQLReserved(t string) bool {
	if t == "(" || t == ")" || t == "," || cqlOps[t] {
		return true
	}
	switch strings.ToUpper(t) {
	case "AND", "OR", "NOT", "IN", "LIKE", "BETWEEN", "IS", "NULL":
		return true
	}
	return false
}
