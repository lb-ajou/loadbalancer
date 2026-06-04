package dashboard

import (
	"net/http"

	"reverseproxy-poc/internal/runtime"
)

func registerRuntimeAPI(
	mux *http.ServeMux,
	state *runtime.State,
	clusterProvider ClusterStatusProvider,
	vipProvider VIPStatusProvider,
) {
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, newMethodNotAllowedError())
			return
		}

		writeJSON(w, buildStatusView(state.Snapshot(), clusterStatus(r, clusterProvider), vipStatus(state, vipProvider)))
	})
	mux.HandleFunc("/api/runtime", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, newMethodNotAllowedError())
			return
		}

		writeJSON(w, buildRuntimeView(state.Snapshot()))
	})
	mux.HandleFunc("/api/cluster", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, newMethodNotAllowedError())
			return
		}

		writeJSON(w, clusterStatus(r, clusterProvider))
	})
}

func clusterStatus(r *http.Request, provider ClusterStatusProvider) ClusterView {
	if provider == nil {
		return disabledClusterView()
	}
	return provider.ClusterStatus(r.Context())
}

func vipStatus(state *runtime.State, provider VIPStatusProvider) VIPStatusView {
	if provider != nil {
		return provider.VIPStatus()
	}
	return vipStatusFromSnapshot(state.Snapshot())
}
