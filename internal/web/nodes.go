package web

import (
	"net/http"
	"strings"

	"github.com/computenord/craftpanel/internal/node"
)

func (h *Handler) listNodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if h.Nodes == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, h.Nodes.List())
}

func (h *Handler) enrollNode(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if h.Nodes == nil {
		apiError(w, http.StatusInternalServerError, "internal", "nodes not available")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := h.Nodes.Enroll(req.Name)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	panel := publicBaseFromRequest(r)
	join := JoinCommand(panel, view.Token)
	h.audit(r, "node.enroll", "", view.ID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          view.ID,
		"name":        view.Name,
		"createdAt":   view.CreatedAt,
		"token":       view.Token,
		"joinCommand": join,
	})
}

func (h *Handler) deleteNode(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if h.Nodes == nil {
		apiError(w, http.StatusInternalServerError, "internal", "nodes not available")
		return
	}
	if err := h.Nodes.Delete(r.PathValue("id")); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "node.delete", "", r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) nodeCommand(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if h.Nodes == nil {
		apiError(w, http.StatusInternalServerError, "internal", "nodes not available")
		return
	}
	var req struct {
		Op       string `json:"op"`
		ServerID string `json:"serverId"` // composite nodeId/serverId or just serverId with path node
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	nodeID := r.PathValue("id")
	sid := req.ServerID
	if n, s, ok := node.ParseCompositeID(sid); ok {
		nodeID, sid = n, s
	}
	cmd, err := h.Nodes.Enqueue(nodeID, req.Op, sid)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, cmd)
}

// nodeSync is called by remote agents with Bearer node_… token.
func (h *Handler) nodeSync(w http.ResponseWriter, r *http.Request) {
	if h.Nodes == nil {
		apiError(w, http.StatusInternalServerError, "internal", "nodes not available")
		return
	}
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		apiError(w, http.StatusUnauthorized, "unauthorized", "missing node token")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	n, ok := h.Nodes.Authenticate(token)
	if !ok {
		apiError(w, http.StatusUnauthorized, "unauthorized", "invalid node token")
		return
	}
	var req node.SyncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeJSON(w, http.StatusOK, h.Nodes.Sync(n.ID, req))
}
