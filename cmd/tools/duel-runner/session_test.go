package main

import (
	"reflect"
	"sort"
	"testing"
)

func TestComputeFitActions(t *testing.T) {
	cur := []string{"mining_laser_i", "pulse_laser_i", "pulse_laser_i"}
	want := []string{"pulse_laser_i", "missile_launcher_i"}
	rem, inst := computeFitActions(cur, want)
	sort.Strings(rem)
	sort.Strings(inst)
	if !reflect.DeepEqual(rem, []string{"mining_laser_i", "pulse_laser_i"}) {
		t.Errorf("remove = %v", rem)
	}
	if !reflect.DeepEqual(inst, []string{"missile_launcher_i"}) {
		t.Errorf("install = %v", inst)
	}
	// Duplicates count: two pulse lasers wanted, one present → install one.
	rem2, inst2 := computeFitActions([]string{"pulse_laser_i"}, []string{"pulse_laser_i", "pulse_laser_i"})
	if len(rem2) != 0 || !reflect.DeepEqual(inst2, []string{"pulse_laser_i"}) {
		t.Errorf("dup case: rem=%v inst=%v", rem2, inst2)
	}
}
