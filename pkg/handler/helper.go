package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
)

func respondJSON(w http.ResponseWriter, status int, data any) {
	if data != nil {
		v := reflect.ValueOf(data)
		if v.Kind() == reflect.Slice && v.IsNil() {
			data = reflect.MakeSlice(v.Type(), 0, 0).Interface()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func RespondJSONPublic(w http.ResponseWriter, status int, data any) {
	respondJSON(w, status, data)
}

func respondCacheable(w http.ResponseWriter, r *http.Request, etag string, status int, data any) {
	if etag != "" {
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	respondJSON(w, status, data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
