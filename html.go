package gocoverage

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

// Rendu HTML navigable (classe de conformité OGC API - Common/Features « html »).
// Négociation : `?f=html` ou en-tête `Accept: text/html`. Les pages restent
// minimales mais permettent d'explorer le service au navigateur ; les liens
// internes conservent `f=html` pour poursuivre la navigation en HTML.

// wantsHTML décide si la réponse doit être en HTML : `f=html` explicite, ou (à
// défaut de `f`) un en-tête Accept mentionnant text/html (cas d'un navigateur).
func wantsHTML(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("f"))) {
	case "html":
		return true
	case "":
		return strings.Contains(r.Header.Get("Accept"), "text/html")
	default:
		return false
	}
}

// htmlLink est un lien affiché dans une page HTML.
type htmlLink struct {
	Rel  string
	Href string
}

// pageTmpl est le gabarit d'enveloppe commun à toutes les pages HTML.
var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} — gocoverage</title>
<style>
 body{font-family:system-ui,sans-serif;margin:2rem;max-width:70rem;color:#222}
 h1{font-size:1.4rem} table{border-collapse:collapse;width:100%}
 th,td{border:1px solid #ddd;padding:.35rem .6rem;text-align:left;font-size:.9rem}
 th{background:#f4f4f4} a{color:#06c;text-decoration:none} a:hover{text-decoration:underline}
 nav a{margin-right:1rem} .muted{color:#777;font-size:.85rem}
 img.preview{max-width:100%;border:1px solid #ddd;margin-top:1rem}
</style>
</head>
<body>
<nav><a href="/?f=html">Accueil</a><a href="/collections?f=html">Collections</a><a href="/conformance?f=html">Conformance</a></nav>
<h1>{{.Title}}</h1>
{{.Body}}
<p class="muted">gocoverage — OGC API Coverages / EDR / Maps / Tiles / Features</p>
</body>
</html>`))

// writeHTML rend une page HTML complète (enveloppe + corps).
func writeHTML(w http.ResponseWriter, title string, body template.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	_ = pageTmpl.Execute(w, struct {
		Title string
		Body  template.HTML
	}{title, body})
}

// renderBody exécute un gabarit de corps vers une chaîne HTML sûre.
func renderBody(t *template.Template, data interface{}) template.HTML {
	var b strings.Builder
	_ = t.Execute(&b, data)
	return template.HTML(b.String())
}

var landingBodyTmpl = template.Must(template.New("landing").Parse(`
<p>{{.Description}}</p>
<ul>{{range .Links}}<li><a href="{{.Href}}">{{.Rel}}</a> <span class="muted">{{.Href}}</span></li>{{end}}</ul>`))

// landingHTML rend la page d'accueil.
func landingHTML(w http.ResponseWriter, description string, links []htmlLink) {
	body := renderBody(landingBodyTmpl, struct {
		Description string
		Links       []htmlLink
	}{description, links})
	writeHTML(w, "Accueil", body)
}

var collectionsBodyTmpl = template.Must(template.New("collections").Parse(`
<table><thead><tr><th>Identifiant</th><th>Titre</th><th>Emprise (bbox)</th><th>Paramètres</th></tr></thead>
<tbody>{{range .}}<tr>
<td><a href="/collections/{{.ID}}?f=html">{{.ID}}</a></td>
<td>{{.Title}}</td>
<td class="muted">{{range $i,$v := .BBox}}{{if $i}}, {{end}}{{$v}}{{end}}</td>
<td>{{range $i,$p := .Parameters}}{{if $i}}, {{end}}{{$p}}{{end}}</td>
</tr>{{end}}</tbody></table>`))

// collectionsHTML rend la liste des collections.
func collectionsHTML(w http.ResponseWriter, cols []CollectionInfo) {
	writeHTML(w, "Collections", renderBody(collectionsBodyTmpl, cols))
}

var collectionBodyTmpl = template.Must(template.New("collection").Parse(`
<p>{{.Title}}</p>
<p class="muted">Emprise : {{range $i,$v := .BBox}}{{if $i}}, {{end}}{{$v}}{{end}}</p>
<p>Paramètres : {{range $i,$p := .Params}}{{if $i}}, {{end}}<code>{{$p}}</code>{{end}}</p>
<h2 style="font-size:1.1rem">Liens</h2>
<ul>{{range .Links}}<li><a href="{{.Href}}">{{.Rel}}</a> <span class="muted">{{.Href}}</span></li>{{end}}</ul>
{{if .MapHref}}<img class="preview" src="{{.MapHref}}" alt="aperçu carte">{{end}}`))

// collectionHTML rend la description d'une collection.
func collectionHTML(w http.ResponseWriter, c *Collection) {
	links := []htmlLink{
		{"entités (items)", "/collections/" + c.ID + "/items?f=html"},
		{"couverture (CoverageJSON)", "/collections/" + c.ID + "/coverage"},
		{"carte (PNG)", "/collections/" + c.ID + "/map"},
		{"tuiles", "/collections/" + c.ID + "/map/tiles"},
		{"queryables", "/collections/" + c.ID + "/queryables"},
	}
	body := renderBody(collectionBodyTmpl, struct {
		Title   string
		BBox    [4]float64
		Params  []string
		Links   []htmlLink
		MapHref string
	}{c.Title, c.BBox(), c.Params(), links, "/collections/" + c.ID + "/map?width=480"})
	writeHTML(w, "Collection "+c.ID, body)
}

var conformanceBodyTmpl = template.Must(template.New("conf").Parse(`
<ul>{{range .}}<li><a href="{{.}}">{{.}}</a></li>{{end}}</ul>`))

// conformanceHTML rend la liste des classes de conformité.
func conformanceHTML(w http.ResponseWriter, classes []string) {
	writeHTML(w, "Conformance", renderBody(conformanceBodyTmpl, classes))
}

// htmlFeature est la ligne d'une entité dans la table HTML de /items.
type htmlFeature struct {
	ID    interface{}
	Lon   float64
	Lat   float64
	Props string
}

var itemsBodyTmpl = template.Must(template.New("items").Parse(`
<p class="muted">{{.Returned}} entité(s) affichée(s) sur {{.Matched}} correspondante(s).</p>
<nav>{{range .Nav}}<a href="{{.Href}}">{{.Rel}}</a>{{end}}</nav>
<table><thead><tr><th>Id</th><th>Lon</th><th>Lat</th><th>Propriétés</th></tr></thead>
<tbody>{{range .Features}}<tr>
<td><a href="/collections/{{$.CollID}}/items/{{.ID}}?f=html">{{.ID}}</a></td>
<td>{{.Lon}}</td><td>{{.Lat}}</td><td class="muted">{{.Props}}</td>
</tr>{{end}}</tbody></table>`))

// itemsHTML rend une FeatureCollection sous forme de table.
func itemsHTML(w http.ResponseWriter, collID string, features []map[string]interface{}, matched int, links []htmlLink) {
	rows := make([]htmlFeature, 0, len(features))
	for _, f := range features {
		hf := htmlFeature{ID: f["id"]}
		if g, ok := f["geometry"].(map[string]interface{}); ok {
			if coords, ok := g["coordinates"].([]float64); ok && len(coords) == 2 {
				hf.Lon, hf.Lat = coords[0], coords[1]
			}
		}
		if pr, ok := f["properties"].(map[string]interface{}); ok {
			var parts []string
			for k, v := range pr {
				parts = append(parts, k+"="+formatProp(v))
			}
			hf.Props = strings.Join(parts, ", ")
		}
		rows = append(rows, hf)
	}
	var nav []htmlLink
	for _, l := range links {
		if l.Rel == "prev" || l.Rel == "next" {
			nav = append(nav, htmlLink{Rel: l.Rel, Href: htmlify(l.Href)})
		}
	}
	body := renderBody(itemsBodyTmpl, struct {
		CollID   string
		Matched  int
		Returned int
		Features []htmlFeature
		Nav      []htmlLink
	}{collID, matched, len(features), rows, nav})
	writeHTML(w, "Entités — "+collID, body)
}

// htmlify ajoute f=html à un lien interne (pour poursuivre la navigation HTML).
func htmlify(href string) string {
	if strings.Contains(href, "?") {
		return href + "&f=html"
	}
	return href + "?f=html"
}

// formatProp formate une valeur de propriété pour l'affichage.
func formatProp(v interface{}) string {
	if v == nil {
		return "—"
	}
	if t, ok := v.(float64); ok {
		return fmt.Sprintf("%g", t)
	}
	return fmt.Sprint(v)
}
