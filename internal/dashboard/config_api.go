package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"reverseproxy-poc/internal/admin"
	"reverseproxy-poc/internal/spec"
)

type namespaceRequest struct {
	Namespace string `json:"namespace"`
}

func registerConfigAPI(mux *http.ServeMux, service admin.Service) {
	mux.HandleFunc("/api/namespaces", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := service.ListNamespaces(r.Context())
			if err != nil {
				writeAPIError(w, err)
				return
			}

			writeJSON(w, admin.NamespaceListView{
				Items:            items,
				DefaultNamespace: admin.DefaultNamespace,
			})
		case http.MethodPost:
			var request namespaceRequest
			if err := decodeJSONBody(r, &request); err != nil {
				writeAPIError(w, err)
				return
			}

			created, err := service.CreateNamespace(r.Context(), request.Namespace)
			if err != nil {
				writeAPIError(w, err)
				return
			}

			writeJSONStatus(w, http.StatusCreated, created)
		default:
			writeAPIError(w, newMethodNotAllowedError())
		}
	})
	mux.HandleFunc("/api/namespaces/", func(w http.ResponseWriter, r *http.Request) {
		namespace, rest, ok := namespacePathParts(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if rest == "" {
			handleNamespaceRoot(w, r, service, namespace)
			return
		}

		switch {
		case rest == "config":
			handleNamespaceConfig(w, r, service, namespace)
		default:
			http.NotFound(w, r)
		}
	})
}

func handleNamespaceRoot(w http.ResponseWriter, r *http.Request, service admin.Service, namespace string) {
	switch r.Method {
	case http.MethodDelete:
		if err := service.DeleteNamespace(r.Context(), namespace); err != nil {
			writeAPIError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, newMethodNotAllowedError())
	}
}

func handleNamespaceConfig(w http.ResponseWriter, r *http.Request, service admin.Service, namespace string) {
	switch r.Method {
	case http.MethodGet:
		configView, err := service.GetNamespaceConfig(r.Context(), namespace)
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

		updated, err := service.ReplaceNamespaceConfig(r.Context(), namespace, cfg)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, updated)
	default:
		writeAPIError(w, newMethodNotAllowedError())
	}
}

func namespacePathParts(path string) (namespace, rest string, ok bool) {
	const prefix = "/api/namespaces/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}

	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	namespace, err := url.PathUnescape(parts[0])
	if err != nil || namespace == "" || strings.Contains(namespace, "/") {
		return "", "", false
	}

	if len(parts) == 1 {
		return namespace, "", true
	}

	for _, part := range parts[1:] {
		if part == "" {
			return "", "", false
		}
	}

	return namespace, strings.Join(parts[1:], "/"), true
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
