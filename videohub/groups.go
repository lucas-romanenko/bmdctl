// SPDX-License-Identifier: MIT

package videohub

import (
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Groups are the Videohub's command groups: what bmdctl mcp makes one
// tool of each (docs/PORTING.md, "mcp").
var Groups = []table.Group{
	{Name: "routing", Summary: "Routing and labels: route an input to an output, rename an input or an output."},
	{Name: "session", Summary: "The session and reads: status (the routing, with labels and locks), the inputs, the outputs, the raw state, watch changes, connect, disconnect."},
}

var groupOf = map[string]string{
	"route": "routing", "input": "routing", "output": "routing",
	"status": "session", "outputs": "session", "inputs": "session", "state": "session", "watch": "session",
	"connect": "session", "disconnect": "session",
}

// GroupOf is the group a Videohub command belongs to; "" for none.
func GroupOf(path string) string {
	first, _, _ := strings.Cut(path, " ")
	return groupOf[first]
}

// RegisterCommands adds the Videohub's groups to r. Its commands are all
// operations the engine adds (a port is named by its number or label,
// which only the hub's state resolves), so there are no wire entries.
func RegisterCommands(r *table.Registry) error { return r.AddGroups("videohub", Groups...) }
