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

// cqlExpr est un nœud de l'arbre de filtre : il évalue une entité (ses propriétés)
// à vrai/faux.
type cqlExpr interface {
	eval(props map[string]interface{}) bool
}

type cqlAnd struct{ l, r cqlExpr }

func (e cqlAnd) eval(p map[string]interface{}) bool { return e.l.eval(p) && e.r.eval(p) }

type cqlOr struct{ l, r cqlExpr }

func (e cqlOr) eval(p map[string]interface{}) bool { return e.l.eval(p) || e.r.eval(p) }

// cqlCmp compare la propriété prop à une valeur (numérique ou chaîne).
type cqlCmp struct {
	prop  string
	op    string
	num   float64
	str   string
	isNum bool
}

func (e cqlCmp) eval(p map[string]interface{}) bool {
	v, ok := p[e.prop]
	if !ok || v == nil {
		return false // propriété absente ou nulle (NaN) → non satisfaite
	}
	if e.isNum {
		f, ok := v.(float64)
		if !ok {
			return false
		}
		return cmpNum(f, e.op, e.num)
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return cmpStr(s, e.op, e.str)
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
		case c == '(' || c == ')':
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
				if d == ' ' || d == '\t' || d == '\n' || d == '(' || d == ')' ||
					d == '<' || d == '>' || d == '=' || d == '!' || d == '\'' {
					break
				}
				j++
			}
			if j == i {
				return nil, fmt.Errorf("CQL2 : caractère inattendu %q", string(c))
			}
			toks = append(toks, s[i:j])
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
	if p.pos+3 > len(p.toks) {
		return nil, fmt.Errorf("CQL2 : comparaison incomplète")
	}
	prop := p.toks[p.pos]
	op := p.toks[p.pos+1]
	val := p.toks[p.pos+2]
	if isCQLReserved(prop) || !cqlOps[op] {
		return nil, fmt.Errorf("CQL2 : comparaison invalide près de %q", prop)
	}
	p.pos += 3
	cmp := cqlCmp{prop: prop, op: op}
	if strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'") {
		cmp.str = strings.Trim(val, "'")
	} else if f, err := strconv.ParseFloat(val, 64); err == nil {
		cmp.num, cmp.isNum = f, true
	} else {
		return nil, fmt.Errorf("CQL2 : littéral invalide %q", val)
	}
	return cmp, nil
}

// isCQLReserved indique si un jeton est un mot réservé (ne peut pas être un nom
// de propriété en tête de comparaison).
func isCQLReserved(t string) bool {
	return t == "(" || t == ")" || cqlOps[t] || strings.EqualFold(t, "AND") || strings.EqualFold(t, "OR")
}
