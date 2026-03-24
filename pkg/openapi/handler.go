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

func SwaggerUIHandler(specURL string) http.HandlerFunc {
	page := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>TVProxy API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
SwaggerUIBundle({url:"` + specURL + `",dom_id:"#swagger-ui",deepLinking:true,presets:[SwaggerUIBundle.presets.apis,SwaggerUIBundle.SwaggerUIStandalonePreset],layout:"BaseLayout"});
</script>
</body>
</html>`

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
	}
}
