package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/node"
)

// remoteProxy forwards /api/servers/{nodeId~serverId}/… to the node's local API.
func (h *Handler) remoteProxy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.Nodes == nil || !strings.HasPrefix(r.URL.Path, "/api/servers/") {
			next.ServeHTTP(w, r)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/servers/")
		seg, suffix, _ := strings.Cut(rest, "/")
		if seg == "" || seg == "from-backup" || seg == "import" || seg == "from-template" {
			next.ServeHTTP(w, r)
			return
		}
		nodeID, serverID, ok := node.ParseCompositeID(seg)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		n, found := h.Nodes.Lookup(nodeID)
		if !found {
			apiError(w, http.StatusNotFound, "not_found", "node not found")
			return
		}
		if !n.Online || n.ApiURL == "" || n.Token == "" {
			apiError(w, http.StatusBadGateway, "node_unreachable",
				"node is offline or has no control API (re-enroll / update the node agent)")
			return
		}
		target, err := url.Parse(n.ApiURL)
		if err != nil || target.Scheme == "" || target.Host == "" {
			apiError(w, http.StatusBadGateway, "node_unreachable", "invalid node API URL")
			return
		}
		path := "/api/servers/" + serverID
		if suffix != "" {
			path += "/" + suffix
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.FlushInterval = 100 * time.Millisecond
		origDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			origDirector(req)
			req.URL.Path = path
			req.URL.RawPath = path
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = target.Host
			req.Header.Set("Authorization", "Bearer "+n.Token)
			req.Header.Del("Cookie")
			req.Header.Del(csrfHeader)
		}
		proxy.ModifyResponse = func(resp *http.Response) error {
			// Don't buffer SSE / binary streams.
			ct := resp.Header.Get("Content-Type")
			if strings.Contains(ct, "text/event-stream") ||
				strings.Contains(ct, "application/octet-stream") ||
				strings.Contains(ct, "application/zip") {
				return nil
			}
			return rewriteRemoteJSON(resp, nodeID, n.Name)
		}
		proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, err error) {
			apiError(rw, http.StatusBadGateway, "node_unreachable", err.Error())
		}
		proxy.ServeHTTP(w, r)
	})
}

func rewriteRemoteJSON(resp *http.Response, nodeID, nodeName string) error {
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") || resp.Body == nil {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	rewritten, changed := rewriteCompositeIDs(body, nodeID, nodeName)
	if !changed {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", itoa(len(body)))
		return nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", itoa(len(rewritten)))
	return nil
}

func rewriteCompositeIDs(body []byte, nodeID, nodeName string) ([]byte, bool) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return body, false
	}
	changed := stampRemoteIDs(v, nodeID, nodeName)
	if !changed {
		return body, false
	}
	out, err := json.Marshal(v)
	if err != nil {
		return body, false
	}
	return out, true
}

func stampRemoteIDs(v any, nodeID, nodeName string) bool {
	changed := false
	switch t := v.(type) {
	case map[string]any:
		if id, ok := t["id"].(string); ok && id != "" && !strings.Contains(id, node.CompositeSep) {
			// Only rewrite server-like objects (have type/status) or bare id views.
			if _, hasType := t["type"]; hasType {
				t["id"] = node.CompositeID(nodeID, id)
				t["nodeId"] = nodeID
				t["nodeName"] = nodeName
				t["nodeOnline"] = true
				t["apiReady"] = true
				changed = true
			}
		}
		for _, child := range t {
			if stampRemoteIDs(child, nodeID, nodeName) {
				changed = true
			}
		}
	case []any:
		for _, child := range t {
			if stampRemoteIDs(child, nodeID, nodeName) {
				changed = true
			}
		}
	}
	return changed
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// proxyCreateToNode forwards POST /api/servers with nodeId to the node agent.
func (h *Handler) proxyCreateToNode(w http.ResponseWriter, r *http.Request, nodeID string, body []byte) {
	n, found := h.Nodes.Lookup(nodeID)
	if !found {
		apiError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}
	if !n.Online || n.ApiURL == "" || n.Token == "" {
		apiError(w, http.StatusBadGateway, "node_unreachable",
			"node is offline or has no control API")
		return
	}
	// Strip nodeId from the payload before forwarding.
	var m map[string]any
	if err := json.Unmarshal(body, &m); err == nil {
		delete(m, "nodeId")
		body, _ = json.Marshal(m)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(n.ApiURL, "/")+"/api/servers", bytes.NewReader(body))
	if err != nil {
		apiError(w, http.StatusBadGateway, "node_unreachable", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		apiError(w, http.StatusBadGateway, "node_unreachable", err.Error())
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		if rewritten, ok := rewriteCompositeIDs(respBody, nodeID, n.Name); ok {
			respBody = rewritten
		}
	}
	for k, vals := range resp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}
