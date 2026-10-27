// SPDX-License-Identifier: MIT

package hyperdeck

import (
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Groups are the HyperDeck's command groups: what bmdctl mcp makes one
// tool of each (docs/PORTING.md, "mcp").
var Groups = []table.Group{
	{Name: "transport", Summary: "The transport: play, pause, stop, record, loop, next and previous clip, goto a clip or a timecode, the active slot."},
	{Name: "session", Summary: "The session and reads: status (transport, timecode, clip, slots, time left), the timeline's or a disk's clips and editing the timeline, the raw state, watch changes, connect, disconnect."},
}

var groupOf = map[string]string{
	"play": "transport", "pause": "transport", "stop": "transport", "record": "transport", "loop": "transport", "next": "transport",
	"prev": "transport", "goto": "transport", "slot": "transport",
	"status": "session", "clips": "session", "timeline": "session", "state": "session", "watch": "session", "connect": "session",
	"disconnect": "session",
}

// GroupOf is the group a HyperDeck command belongs to; "" for none.
func GroupOf(path string) string {
	first, _, _ := strings.Cut(path, " ")
	return groupOf[first]
}
