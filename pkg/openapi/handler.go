package openapi

import (
	"embed"
	"net/http"
)

//go:embed spec.yaml
var specFS embed.FS

func SpecHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := specFS.ReadFile("spec.yaml")
		if err != nil {
			http.Error(w, "spec not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(data)
	}
}
