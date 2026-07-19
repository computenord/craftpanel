package node

import "strings"

// CompositeSep joins node id and local server id into one URL path segment.
const CompositeSep = "~"

// CompositeID builds a panel-visible server id for a remote instance.
func CompositeID(nodeID, serverID string) string {
	return nodeID + CompositeSep + serverID
}

// ParseCompositeID splits "nodeId~serverId" (also accepts legacy "nodeId/serverId").
func ParseCompositeID(id string) (nodeID, serverID string, ok bool) {
	for _, sep := range []byte{CompositeSep[0], '/'} {
		i := strings.IndexByte(id, sep)
		if i > 0 && i < len(id)-1 {
			return id[:i], id[i+1:], true
		}
	}
	return "", "", false
}
