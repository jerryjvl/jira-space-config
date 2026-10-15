package spec

import (
	"slices"
	"strings"
	"testing"
)

// TestHolderTypesMatchTheValidator: the kinds the table can write are
// exactly the kinds validate accepts as holders of a scheme; "space" and
// "logged_in" are for filters only.
func TestHolderTypesMatchTheValidator(t *testing.T) {
	var accepted []string
	accepted = append(accepted, holderWithValue...)
	accepted = append(accepted, holderWithoutValue...)
	accepted = append(accepted, holderEitherValue...)
	var table []string
	for kind := range HolderTypes {
		table = append(table, kind)
	}
	slices.Sort(accepted)
	slices.Sort(table)
	if !slices.Equal(accepted, table) {
		t.Errorf("validate accepts %v\nHolderTypes has %v", accepted, table)
	}
	for kind, ty := range HolderTypes {
		if ty.Permission == "" && ty.Notification == "" && ty.Security == "" {
			t.Errorf("%s has no spelling in any place", kind)
		}
		if ty.In(PlacePermission) != ty.Permission || ty.In(PlaceNotification) != ty.Notification || ty.In(PlaceSecurity) != ty.Security {
			t.Errorf("%s: In does not give the places' spellings", kind)
		}
		withValue := slices.Contains(holderWithValue, kind) || slices.Contains(holderEitherValue, kind)
		if ty.Value != withValue {
			t.Errorf("%s: the table says takes a value is %v, validate says %v", kind, ty.Value, withValue)
		}
	}
	// No two kinds may share a spelling, or reading it back would be ambiguous.
	seen := map[string]string{}
	for kind, ty := range HolderTypes {
		for _, name := range []string{ty.Permission, ty.Notification, ty.Security} {
			if name == "" {
				continue
			}
			if other, ok := seen[strings.ToLower(name)]; ok && other != kind {
				t.Errorf("%q is the spelling of both %s and %s", name, other, kind)
			}
			seen[strings.ToLower(name)] = kind
		}
	}
}
