// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "reflect"

// deepCopy returns a copy of v that shares no pointer, slice, map or
// interface value with it. State and every packet type are plain data with
// only exported fields, which is all it handles.
func deepCopy[T any](v T) T {
	return copyValue(reflect.ValueOf(&v).Elem()).Interface().(T)
}

func copyValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		p := reflect.New(v.Type().Elem())
		p.Elem().Set(copyValue(v.Elem()))
		return p
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(copyValue(v.Elem()))
		return c
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			c.Index(i).Set(copyValue(v.Index(i)))
		}
		return c
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			c.SetMapIndex(copyValue(it.Key()), copyValue(it.Value()))
		}
		return c
	case reflect.Struct:
		c := reflect.New(v.Type()).Elem()
		for i := range v.NumField() {
			c.Field(i).Set(copyValue(v.Field(i)))
		}
		return c
	case reflect.Array:
		c := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			c.Index(i).Set(copyValue(v.Index(i)))
		}
		return c
	}
	c := reflect.New(v.Type()).Elem()
	c.Set(v)
	return c
}
