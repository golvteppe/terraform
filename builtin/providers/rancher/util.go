package rancher

import (
	"strings"

	"github.com/golvteppe/go-rancher/v2"
)

const (
	stateRemoved = "removed"
	statePurged  = "purged"
)

func removed(state string) bool {
	return state == stateRemoved || state == statePurged
}

func splitID(id string) (envID, resourceID string) {
	if strings.Contains(id, "/") {
		return id[0:strings.Index(id, "/")], id[strings.Index(id, "/")+1:]
	}
	return "", id
}

// NewListOpts wraps around client.NewListOpts()
func NewListOpts() *client.ListOpts {
	return client.NewListOpts()
}
