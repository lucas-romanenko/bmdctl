// SPDX-License-Identifier: MIT

package ultimatte

import (
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Groups are the Ultimatte's command groups: what bmdctl mcp makes one
// tool of each (docs/PORTING.md, "mcp").
var Groups = []table.Group{
	{Name: "profile", Summary: "Archive and restore: the presets and settings to a Smart Remote 4 zip, and back."},
	{Name: "network", Summary: "The network interface: read it, or set its address, mask, gateway, DNS or DHCP."},
	{Name: "session", Summary: "Reads: status (label, presets, network), the presets, the control values, the prelude, connect."},
}

var groupOf = map[string]string{
	"archive": "profile", "restore": "profile", "network": "network",
	"status": "session", "presets": "session", "controls": "session", "prelude": "session", "connect": "session",
}

// GroupOf is the group an Ultimatte command belongs to; "" for none.
func GroupOf(path string) string {
	first, _, _ := strings.Cut(path, " ")
	return groupOf[first]
}

// RegisterCommands adds the Ultimatte's groups to r. Its commands are
// all operations the engine adds (each opens its own connections, as
// ultimattewire's calls do), so there are no wire entries.
func RegisterCommands(r *table.Registry) error { return r.AddGroups("ultimatte", Groups...) }
