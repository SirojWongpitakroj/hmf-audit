package durable

import (
	"blas/bitmap"
	"encoding/gob"
	"reflect"
)

type countingWriter struct{ bytes uint64 }

func (w *countingWriter) Write(b []byte) (int, error) { w.bytes += uint64(len(b)); return len(b), nil }
func evidenceSize(value any) (uint64, error) {
	var out countingWriter
	err := gob.NewEncoder(&out).Encode(value)
	return out.bytes, err
}
func bitmapVolume(value any) uint64 { return bitmapValue(reflect.ValueOf(value)) }
func bitmapValue(v reflect.Value) uint64 {
	if !v.IsValid() {
		return 0
	}
	if v.CanInterface() {
		if b, ok := v.Interface().(bitmap.Bitmap); ok {
			return (b.Len() + 7) / 8
		}
	}
	var out uint64
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			out = bitmapValue(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanInterface() {
				out += bitmapValue(v.Field(i))
			}
		}
	case reflect.Array, reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			out += bitmapValue(v.Index(i))
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			out += bitmapValue(iter.Value())
		}
	}
	return out
}
