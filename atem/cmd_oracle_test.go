// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// allCommands has the zero value of every command type, by Go name.
var allCommands = func() map[string]Command {
	m := map[string]Command{}
	for _, c := range []Command{
		ProgramInput{}, PreviewInput{}, Cut{}, Auto{}, AuxSource{},
		TransitionSettings{}, MixSettings{}, DipSettings{}, WipeSettings{}, DVESettings{}, StingerSettings{},
		TransitionPreview{}, TransitionPosition{},
		KeyOnAir{}, KeyFill{}, KeyCut{}, KeyType{}, KeyPropertiesLuma{}, KeyPropertiesAdvancedChroma{},
		KeyPropertiesAdvancedChromaColorpicker{}, KeyPropertiesPattern{}, KeyPropertiesMask{}, KeyPropertiesDVE{},
		KeyerKeyframeSet{}, KeyerKeyframeRun{},
		DkeyOnair{}, DkeyAuto{}, DkeyTie{}, DkeyRate{}, DkeySetFill{}, DkeySetKey{}, DkeyGain{}, DkeyMask{},
		FadeToBlack{}, FadeToBlackConfig{}, FadeToBlackEnable{}, ColorGenerator{},
		CaptureStill{}, ClearStill{}, MediaplayerSelect{}, MacroAction{}, MacroRecord{}, MacroSleep{},
		FairlightMasterProperties{}, FairlightStripProperties{}, FairlightEQBandProperties{},
		FairlightMasterEQBandProperties{}, FairlightCompressorProperties{}, FairlightLimiterProperties{},
		FairlightExpanderProperties{}, FairlightMasterCompressorProperties{}, FairlightMasterLimiterProperties{},
		SendFairlightLevels{},
		AudioInput{}, AudioMasterProperties{}, AudioMonitorProperties{}, SendAudioLevels{},
		CameraControl{}, HyperdeckSettings{}, MultiviewInput{}, MultiviewProperties{},
		SupersourceBoxProperties{}, SupersourceProperties{}, RecordingSettingsSet{}, RecorderStatus{},
		StreamingServiceSet{}, StreamingAudioBitrate{}, StreamingStatusSet{},
		InputProperties{}, VideoMode{}, AutoInputVideoMode{}, SaveStartupState{}, ClearStartupState{},
		SetTimeOfDay{}, TimeRequest{},
		Lock{}, PartialLock{}, TransferDownloadRequest{}, TransferUploadRequest{}, TransferData{},
		TransferFileData{}, TransferAck{},
	} {
		m[reflect.TypeOf(c).Name()] = c
	}
	return m
}()

type sendCase struct {
	Type   string         `json:"type"`
	Fields map[string]any `json:"fields"`
	Call   string         `json:"call"`
	Result string         `json:"result"` // bytes, error, none
	Bytes  string         `json:"bytes"`
}

// Every command encodes what bmdwire v1.3.0's operation (or Send class)
// sends for the same arguments, byte for byte, and refuses what atemwire
// raises on or declines to send.
func TestCommandsMatchAtemwire(t *testing.T) {
	var oracle struct {
		Bmdwire string     `json:"bmdwire"`
		Cases   []sendCase `json:"cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(fixtures.Bytes(t, "oracle/send.json")))
	dec.UseNumber() // 2^63-1 must survive
	if err := dec.Decode(&oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.Bmdwire != "v1.3.0" {
		t.Fatalf("oracle generated from bmdwire %s", oracle.Bmdwire)
	}
	types := map[string]int{}
	for i, c := range oracle.Cases {
		types[c.Type]++
		cmd, err := buildCommand(c.Type, c.Fields)
		if err != nil {
			t.Fatalf("case %d %s: %v", i, c.Call, err)
		}
		var verr error
		if v, ok := cmd.(Validator); ok {
			verr = v.Validate()
		}
		switch c.Result {
		case "bytes":
			if verr != nil {
				t.Errorf("case %d %s: Validate: %v", i, c.Call, verr)
				continue
			}
			got := cmd.Encode()
			if hex.EncodeToString(got) != c.Bytes {
				t.Errorf("case %d %s:\n got %x\nwant %s", i, c.Call, got, c.Bytes)
				continue
			}
			if string(got[4:8]) != cmd.Opcode() || int(got[0])<<8|int(got[1]) != len(got) {
				t.Errorf("case %d %s: header %x", i, c.Call, got[:8])
			}
		case "error", "none":
			if verr == nil {
				t.Errorf("case %d %s: atemwire %s, Go encodes %x", i, c.Call,
					map[string]string{"error": "raises", "none": "sends nothing"}[c.Result], cmd.Encode())
			}
		}
	}
	for name := range allCommands {
		if types[name] == 0 {
			t.Errorf("%s: no oracle cases", name)
		}
	}
	if len(allCommands) != 82 {
		t.Errorf("%d command types, want 82", len(allCommands))
	}
	t.Logf("%d cases over %d command types", len(oracle.Cases), len(types))
}

// buildCommand makes the named command and sets its fields from the
// oracle's JSON values.
func buildCommand(typeName string, fields map[string]any) (Command, error) {
	zero, ok := allCommands[typeName]
	if !ok {
		return nil, fmt.Errorf("no command type %s", typeName)
	}
	v := reflect.New(reflect.TypeOf(zero)).Elem()
	for name, val := range fields {
		f := v.FieldByName(name)
		if !f.IsValid() {
			return nil, fmt.Errorf("%s has no field %s", typeName, name)
		}
		if val == nil {
			continue
		}
		if f.Kind() == reflect.Pointer {
			f.Set(reflect.New(f.Type().Elem()))
			f = f.Elem()
		}
		if err := setValue(f, val); err != nil {
			return nil, fmt.Errorf("%s.%s: %w", typeName, name, err)
		}
	}
	return v.Interface().(Command), nil
}

func setValue(f reflect.Value, val any) error {
	switch f.Interface().(type) {
	case time.Time:
		n, err := val.(json.Number).Int64()
		f.Set(reflect.ValueOf(time.Unix(n, 0)))
		return err
	case MD5:
		var h MD5
		if err := h.UnmarshalText([]byte(val.(string))); err != nil {
			return err
		}
		f.Set(reflect.ValueOf(h))
		return nil
	case []byte:
		b, err := hex.DecodeString(val.(string))
		f.SetBytes(b)
		return err
	case []any:
		var data []any
		for _, e := range val.([]any) {
			for kind, x := range e.(map[string]any) {
				switch kind {
				case "bool", "str":
					data = append(data, x)
				case "int":
					n, err := x.(json.Number).Int64()
					if err != nil {
						return err
					}
					data = append(data, n)
				case "float":
					fv, err := float(x)
					if err != nil {
						return err
					}
					data = append(data, fv)
				}
			}
		}
		if data == nil {
			data = []any{}
		}
		f.Set(reflect.ValueOf(data))
		return nil
	}
	switch f.Kind() {
	case reflect.Bool:
		f.SetBool(val.(bool))
	case reflect.String:
		f.SetString(val.(string))
	case reflect.Float64:
		fv, err := float(val)
		if err != nil {
			return err
		}
		f.SetFloat(fv)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := val.(json.Number).Int64()
		if err != nil || f.OverflowInt(n) {
			return fmt.Errorf("%v does not fit %s", val, f.Type())
		}
		f.SetInt(n)
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		n, err := val.(json.Number).Int64()
		if err != nil || n < 0 || f.OverflowUint(uint64(n)) {
			return fmt.Errorf("%v does not fit %s", val, f.Type())
		}
		f.SetUint(uint64(n))
	default:
		return fmt.Errorf("can't set %s", f.Type())
	}
	return nil
}

// float reads a JSON number or the oracle's "NaN", "Infinity", "-Infinity".
func float(v any) (float64, error) {
	switch x := v.(type) {
	case json.Number:
		return x.Float64()
	case string:
		switch x {
		case "NaN":
			return math.NaN(), nil
		case "Infinity":
			return math.Inf(1), nil
		case "-Infinity":
			return math.Inf(-1), nil
		}
	}
	return 0, fmt.Errorf("not a float: %v", v)
}
