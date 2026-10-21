// SPDX-License-Identifier: MIT

package videohub

import (
	"maps"
	"strconv"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// State is what a session has read from the hub: videohubwire's six
// attributes, indices as the wire has them (0-based).
type State struct {
	ProtocolVersion string            `json:"protocol_version"`
	Device          map[string]string `json:"device"` // keys lower-cased: "model name"
	InputLabels     map[int]string    `json:"input_labels"`
	OutputLabels    map[int]string    `json:"output_labels"`
	Routing         map[int]int       `json:"routing"` // output → input
	Locks           map[int]string    `json:"locks"`   // output → U, O or L
}

func newState() State {
	return State{Device: map[string]string{}, InputLabels: map[int]string{}, OutputLabels: map[int]string{},
		Routing: map[int]int{}, Locks: map[int]string{}}
}

func (s State) clone() State {
	return State{ProtocolVersion: s.ProtocolVersion, Device: maps.Clone(s.Device), InputLabels: maps.Clone(s.InputLabels),
		OutputLabels: maps.Clone(s.OutputLabels), Routing: maps.Clone(s.Routing), Locks: maps.Clone(s.Locks)}
}

// VideoInputs is the video_inputs property: int() of the device block's
// "video inputs", 0 when it is absent or empty. A count int() refuses is
// its ValueError.
func (s State) VideoInputs() (int, error) { return count(s.Device["video inputs"]) }

// VideoOutputs is the video_outputs property.
func (s State) VideoOutputs() (int, error) { return count(s.Device["video outputs"]) }

func count(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := pytext.Int(v)
	return int(n), err
}

// Model is the device block's model name; Name its friendly name, else
// the model.
func (s State) Model() string { return s.Device["model name"] }

func (s State) Name() string {
	if n := s.Device["friendly name"]; n != "" {
		return n
	}
	return s.Model()
}

// Snapshot is state()'s dict.
type Snapshot struct {
	Device  SnapshotDevice `json:"device"`
	Inputs  []Port         `json:"inputs"`
	Outputs []Output       `json:"outputs"`
}

// SnapshotDevice is state()'s device: the counts with their fallback.
type SnapshotDevice struct {
	ModelName       string `json:"model_name"`
	FriendlyName    string `json:"friendly_name"`
	VideoInputs     int    `json:"video_inputs"`
	VideoOutputs    int    `json:"video_outputs"`
	ProtocolVersion string `json:"protocol_version"`
}

// Port is an input: its index and label.
type Port struct {
	Index int    `json:"index"`
	Label string `json:"label"`
}

// Output is an output: its index, label, the input routed to it (nil
// where the hub has said none) and its lock (U where it has said none).
type Output struct {
	Index  int    `json:"index"`
	Label  string `json:"label"`
	Source *int   `json:"source"`
	Lock   string `json:"lock"`
}

// Snapshot is state() without its drain (the session has taken every
// block the hub has sent): the device, then each input and output from
// 0 to the count, a count of 0 falling back to one past the highest label
// index. A port without a label is "Input N" / "Output N", N 1-based. A
// count int() refuses is its ValueError.
func (s State) Snapshot() (Snapshot, error) {
	in, err := s.VideoInputs()
	if err != nil {
		return Snapshot{}, err
	}
	out, err := s.VideoOutputs()
	if err != nil {
		return Snapshot{}, err
	}
	if in == 0 {
		in = maxKey(s.InputLabels) + 1
	}
	if out == 0 {
		out = maxKey(s.OutputLabels) + 1
	}
	snap := Snapshot{Device: SnapshotDevice{ModelName: s.Device["model name"], FriendlyName: s.Device["friendly name"],
		VideoInputs: in, VideoOutputs: out, ProtocolVersion: s.ProtocolVersion}, Inputs: []Port{}, Outputs: []Output{}}
	for i := 0; i < in; i++ {
		snap.Inputs = append(snap.Inputs, Port{Index: i, Label: label(s.InputLabels[i], "Input", i)})
	}
	for i := 0; i < out; i++ {
		o := Output{Index: i, Label: label(s.OutputLabels[i], "Output", i), Lock: "U"}
		if src, ok := s.Routing[i]; ok {
			o.Source = &src
		}
		if l, ok := s.Locks[i]; ok {
			o.Lock = l
		}
		snap.Outputs = append(snap.Outputs, o)
	}
	return snap, nil
}

func label(l, kind string, i int) string {
	if l != "" {
		return l
	}
	return kind + " " + strconv.Itoa(i+1)
}

// maxKey is max(m, default=-1).
func maxKey(m map[int]string) int {
	n := -1
	for k := range m {
		n = max(n, k)
	}
	return n
}

func itoa(i int) string { return strconv.Itoa(i) }
