package engine

import (
	"reflect"
	"testing"
)

func TestExemptionSharedUIDAndUnavailablePackages(t *testing.T) {
	listing := "package:com.app.one uid:10001\npackage:com.app.two uid:10001\npackage:com.app.other uid:10002\npackage:android uid:1000\n"
	ids, warnings, err := compileExemptionIDs([]string{"com.app.one", "com.missing.app"}, listing)
	if err != nil || len(ids) != 0 || len(warnings) != 2 {
		t.Fatal(ids, warnings, err)
	}
	ids, warnings, err = compileExemptionIDs([]string{"com.app.one", "com.app.two"}, listing)
	if err != nil || !reflect.DeepEqual(ids, []int{10001}) || len(warnings) != 0 {
		t.Fatal(ids, warnings, err)
	}
}

func TestExemptionValidationAndDefault(t *testing.T) {
	for _, name := range []string{"", "com..app", "com.app:service", "../com.app", "com.app\nuid=10000", "com.1app"} {
		if validateExemptions([]string{name}) == nil {
			t.Fatal("accepted", name)
		}
	}
	if validateExemptions([]string{"com.app.one", "com.app.one"}) == nil {
		t.Fatal("duplicates accepted")
	}
	profile := DefaultProfile()
	if len(profile.ExemptPackages) != 0 || !profile.IsSystem() {
		t.Fatal("default changed")
	}
	profile.ExemptPackages = []string{"com.app.one"}
	if !profile.IsSystem() {
		t.Fatal("exemptions force global mounts")
	}
	_, _, err := compileExemptionIDs(profile.ExemptPackages, "garbled output")
	if err == nil {
		t.Fatal("unreadable inventory accepted")
	}
}

func TestExemptionsPersistInGeneration(t *testing.T) {
	s, asset, _ := fixtureStore(t)
	p := variableProfile(asset)
	p.ExemptPackages = []string{"com.taobao.taobao", "com.android.settings"}
	g, err := s.Stage(p)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.generation("pending")
	if err != nil || saved == nil || !reflect.DeepEqual(saved.Profile.ExemptPackages, p.ExemptPackages) || g.ID != saved.ID {
		t.Fatal("lost exemptions", err)
	}
}
