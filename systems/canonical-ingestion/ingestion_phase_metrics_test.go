package canonicalingestion

import (
	"reflect"
	"sync/atomic"
	"testing"
)

func TestIngestionPhaseSnapshotIncludesEveryCounter(t *testing.T) {
	m := &AccessMetrics{}
	fields := reflect.ValueOf(&m.IngestionPhases).Elem()
	for i := 0; i < fields.NumField(); i++ {
		fields.Field(i).Addr().Interface().(*atomic.Uint64).Store(uint64(i + 1))
	}
	s := reflect.ValueOf(m.Snapshot().IngestionPhases)
	for i := 0; i < fields.NumField(); i++ {
		name := fields.Type().Field(i).Name
		v := s.FieldByName(name)
		if !v.IsValid() || v.Uint() != uint64(i+1) {
			t.Fatal("missing/miswired phase counter", name)
		}
	}
}
