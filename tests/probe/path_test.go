package probe_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/probe"
)

func TestBestAvailablePath(t *testing.T) {
	report := probe.SiteReport{
		Results: []probe.PathResult{
			{Path: config.RouteDirect, Available: false},
			{Path: config.RouteWork, Available: true},
			{Path: config.RoutePersonal, Available: true},
		},
	}
	path, ok := probe.BestAvailablePath(report)
	if !ok || path != config.RouteWork {
		t.Fatalf("got %s %v", path, ok)
	}

	empty := probe.SiteReport{Results: []probe.PathResult{
		{Path: config.RouteDirect, Available: false},
	}}
	if _, ok := probe.BestAvailablePath(empty); ok {
		t.Fatal("expected no path")
	}
}

func TestResultForPath(t *testing.T) {
	report := probe.SiteReport{
		Results: []probe.PathResult{{Path: config.RoutePersonal, StatusCode: 200}},
	}
	r := probe.ResultForPath(report, config.RoutePersonal)
	if r == nil || r.StatusCode != 200 {
		t.Fatalf("%+v", r)
	}
	if probe.ResultForPath(report, config.RouteWork) != nil {
		t.Fatal("expected nil")
	}
}

func TestBindMap(t *testing.T) {
	m := probe.BindMap("wlan0", "tun0", "http://127.0.0.1:47894")
	if m[config.RouteDirect] != "wlan0" || m[config.RouteWork] != "tun0" {
		t.Fatalf("%v", m)
	}
}
