package router

import (
	"database/sql"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/modelcontextprotocol/registry/internal/api/handler"
)

const indexHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Model Context Registry</title>
</head>
<body>
    <h1>Model Context Registry</h1>
    <p>Welcome to the Model Context Registry. This is a server for managing and distributing model contexts.</p>
    <p>For more information, see the <a href="https://github.com/modelcontextprotocol/registry">GitHub repository</a>.</p>
</body>
</html>`

// Parse the template once at startup.
var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

func New(db *sql.DB, s3Client *s3.Client, s3Bucket string) http.Handler {
	h := handler.New(db, s3Client, s3Bucket)
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		err := indexTmpl.Execute(w, nil)
		if err != nil {
			slog.Error("failed to execute index template", "err", err)
		}
	})

	r.Route("/v1", func(r chi.Router) {
		r.Get("/contexts/{namespace}/{name}", h.GetContextHandler)
		r.Get("/contexts/{namespace}/{name}/blobs/{digest}", h.GetBlobHandler)
		r.Head("/contexts/{namespace}/{name}/blobs/{digest}", h.HeadBlobHandler)
		r.Post("/contexts/{namespace}/{name}/blobs", h.PostBlobHandler)
		r.Put("/contexts/{namespace}/{name}/manifests/{reference}", h.PutManifestHandler)
	})

	return r
}
