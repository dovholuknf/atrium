package daemon

import "net/http"

// ContractVersions are the runner contract versions the room answers. v1 only ever gains optional fields.
var ContractVersions = []int{1}

// handleContract tells an adapter which contract versions this room speaks.
func handleContract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	writeJSONBody(w, map[string]any{"versions": ContractVersions})
}
