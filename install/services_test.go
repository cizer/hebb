package install

import (
	"errors"
	"reflect"
	"testing"
)

func TestIsHebbWebLabel(t *testing.T) {
	cases := map[string]bool{
		"local.hebb.personal.web":          true,
		"local.hebb.work-vault.web":        true,
		"local.hebb.personal.daily-digest": false, // scheduled, not restarted
		"local.hebb.personal.update-check": false,
		"com.apple.something.web":          false, // not hebb
		"Label":                            false,
	}
	for label, want := range cases {
		if got := isHebbWebLabel(label); got != want {
			t.Errorf("isHebbWebLabel(%q) = %v, want %v", label, got, want)
		}
	}
}

func TestParseHebbWebLabels(t *testing.T) {
	out := "PID\tStatus\tLabel\n" +
		"1234\t0\tlocal.hebb.personal.web\n" +
		"-\t0\tlocal.hebb.personal.daily-digest\n" +
		"5678\t0\tlocal.hebb.work.web\n" +
		"4321\t0\tcom.apple.Finder\n"
	got := parseHebbWebLabels(out)
	want := []string{"local.hebb.personal.web", "local.hebb.work.web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseHebbWebLabels = %v, want %v", got, want)
	}
}

func TestRestartServices(t *testing.T) {
	list := func() (string, error) {
		return "PID\tStatus\tLabel\n" +
			"1\t0\tlocal.hebb.a.web\n" +
			"2\t0\tlocal.hebb.a.daily-digest\n" + // must NOT be restarted
			"3\t0\tlocal.hebb.b.web\n", nil
	}
	var restarted []string
	restart := func(label string) error {
		restarted = append(restarted, label)
		return nil
	}
	got, err := restartServices(list, restart)
	if err != nil {
		t.Fatalf("restartServices: %v", err)
	}
	want := []string{"local.hebb.a.web", "local.hebb.b.web"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(restarted, want) {
		t.Errorf("restarted = %v (returned %v), want %v (web jobs only)", restarted, got, want)
	}
}

func TestRestartServicesStopsOnError(t *testing.T) {
	list := func() (string, error) {
		return "1\t0\tlocal.hebb.a.web\n2\t0\tlocal.hebb.b.web\n", nil
	}
	restart := func(label string) error { return errors.New("boom") }
	if _, err := restartServices(list, restart); err == nil {
		t.Fatal("expected an error when a restart fails")
	}
}
