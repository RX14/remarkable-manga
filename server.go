// server.go: the subscription web UI — server-rendered HTML, no JavaScript.
package main

import (
	"html/template"
	"log/slog"
	"net/http"
	"strings"
)

type subView struct {
	ID   string
	Name string
}

type pageData struct {
	Back    string // where Add returns to
	Heading string
	Query   string
	Results []SearchResult
	Authors []AuthorResult
	Subs    []subView
}

const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>mdxrm</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: system-ui, sans-serif; max-width: 40rem; margin: 2rem auto; padding: 0 1rem; line-height: 1.5; }
  h1 { font-size: 1.1rem; font-weight: 600; }
  h2 { font-size: .95rem; font-weight: 600; color: GrayText; margin-top: 2rem; }
  form.search { display: flex; gap: .5rem; }
  input[name=q] { flex: 1; font: inherit; padding: .45rem .7rem; }
  button { font: inherit; font-size: .9rem; padding: .45rem .9rem; cursor: pointer; }
  ul { list-style: none; padding: 0; }
  li { display: flex; align-items: center; justify-content: space-between; gap: 1rem;
       border: 1px solid color-mix(in srgb, currentColor 18%, transparent); border-radius: .5rem;
       padding: .6rem .8rem; margin-bottom: .5rem; }
  .name { overflow-wrap: anywhere; }
  .meta { font-size: .85rem; color: GrayText; }
  form { margin-left: auto; }
</style>
</head>
<body>
<h1>mdxrm — subscriptions</h1>
<form class="search" action="/search" method="get">
  <input name="q" value="{{.Query}}" placeholder="Search MangaDex" required>
  <button name="mode" value="title">Titles</button>
  <button name="mode" value="author">Authors</button>
</form>
{{if .Heading}}<h2>{{.Heading}}</h2>{{end}}
{{if .Authors}}
<ul>
{{range .Authors}}  <li>
    <div class="name">{{.Name}}</div>
    <form action="/search" method="get">
      <input type="hidden" name="author" value="{{.ID}}">
      <input type="hidden" name="aname" value="{{.Name}}">
      <button>Works</button>
    </form>
  </li>
{{end}}</ul>
{{end}}
{{if .Results}}
<ul>
{{range .Results}}  <li>
    <div><div class="name">{{.Name}}</div>
    <div class="meta">{{.Year}} &middot; {{.OriginalLang}} &middot; {{.Author}}{{if not .HasEN}} &middot; no EN yet{{end}}</div></div>
    <form action="/add" method="post">
      <input type="hidden" name="id" value="{{.ID}}">
      <input type="hidden" name="name" value="{{.Name}}">
      <input type="hidden" name="back" value="{{$.Back}}">
      <button>Add</button>
    </form>
  </li>
{{end}}</ul>
{{end}}
{{if .Subs}}
<h2>Subscribed</h2>
<ul>
{{range .Subs}}  <li>
    <div class="name">{{.Name}}</div>
    <form action="/remove" method="post">
      <input type="hidden" name="id" value="{{.ID}}">
      <button>Remove</button>
    </form>
  </li>
{{end}}</ul>
{{end}}
</body>
</html>
`

// serve runs the subscription web UI. It is meant to sit behind a reverse proxy
// (caddy handles TLS and auth upstream).
func serve(addr string, mdx *Client, s *store) error {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	mux := http.NewServeMux()

	render := func(w http.ResponseWriter, data pageData) {
		subs, err := s.subscriptions()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data.Subs = make([]subView, len(subs))
		for i, sub := range subs {
			data.Subs[i] = subView{ID: sub.mangaID, Name: sub.name}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, data); err != nil {
			slog.Error("render failed", "err", err)
		}
	}

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, pageData{})
	})

	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		data := pageData{Query: q.Get("q"), Back: r.URL.RequestURI()}

		switch {
		case q.Get("author") != "":
			works, err := mdx.WorksByAuthor(r.Context(), q.Get("author"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			data.Heading = "Works by " + q.Get("aname")
			data.Results = works
		case q.Get("mode") == "author":
			authors, err := mdx.SearchAuthors(r.Context(), data.Query)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			data.Heading = "Authors"
			data.Authors = authors
		default:
			results, err := mdx.SearchManga(r.Context(), data.Query)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			data.Heading = "Results"
			data.Results = results
		}
		render(w, data)
	})

	mux.HandleFunc("POST /add", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id, name := r.PostFormValue("id"), r.PostFormValue("name")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		if err := s.addSubscription(id, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		back := r.PostFormValue("back")
		if back == "" || !strings.HasPrefix(back, "/") {
			back = "/"
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
	})

	mux.HandleFunc("POST /remove", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.removeSubscription(r.PostFormValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	slog.Info("web ui listening", "addr", addr)
	return http.ListenAndServe(addr, mux)
}
