package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"loadbalancer/internal/admin"
	"loadbalancer/internal/spec"
)

func registerConfigAPI(mux *http.ServeMux, service admin.Service) {
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			configView, err := service.GetConfig(r.Context())
			if err != nil {
				writeAPIError(w, err)
				return
			}

			writeJSON(w, configView)
		case http.MethodPut:
			var cfg spec.Config
			if err := decodeJSONBody(r, &cfg); err != nil {
				writeAPIError(w, err)
				return
			}

			updated, err := service.ReplaceConfig(r.Context(), cfg)
			if err != nil {
				writeAPIError(w, err)
				return
			}
			writeJSON(w, updated)
		default:
			writeAPIError(w, newMethodNotAllowedError())
		}
	})
}

func decodeJSONBody(r *http.Request, target interface{}) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &admin.APIError{
			StatusCode: http.StatusBadRequest,
			Message:    "invalid JSON request body",
			Err:        err,
		}
	}
	if err := decoder.Decode(&struct{}{}); err != nil && !errors.Is(err, io.EOF) {
		return &admin.APIError{
			StatusCode: http.StatusBadRequest,
			Message:    "request body must contain a single JSON object",
		}
	}
	return nil
}
