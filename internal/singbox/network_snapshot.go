package singbox

import (
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/network"
)

// RouterNetworkSnapshot is one collected view used by every part of a candidate.
// Collection does not freeze the OS; subsequent network changes require a new candidate.
type RouterNetworkSnapshot struct {
	SystemVPNUp     bool
	SystemInterface string
	SystemDNS       string
	WorkRoutes      []string
	EndpointCIDRs   []string
	MainInterface   string
}

type routerNetworkSources struct {
	vpn           func(config.Settings) (bool, string)
	dns           func(string) string
	routes        func(string) []string
	endpoints     func(config.Settings) []string
	mainInterface func() string
}

// CaptureRouterNetwork only observes the host; it does not connect/disconnect VPNs.
func CaptureRouterNetwork(st config.Settings) RouterNetworkSnapshot {
	return captureRouterNetwork(st, routerNetworkSources{
		vpn:           readSystemVPNState,
		dns:           systemVPNDNS,
		routes:        WorkRoutesFromOS,
		endpoints:     systemVPNEndpointCIDRs,
		mainInterface: network.DefaultMainIface,
	})
}

func captureRouterNetwork(st config.Settings, sources routerNetworkSources) RouterNetworkSnapshot {
	snapshot := RouterNetworkSnapshot{MainInterface: st.MainInterface}
	if snapshot.MainInterface == "" {
		snapshot.MainInterface = sources.mainInterface()
	}
	snapshot.SystemVPNUp, snapshot.SystemInterface = sources.vpn(st)
	if snapshot.SystemInterface == "" {
		snapshot.SystemInterface = "tun0"
	}
	if snapshot.SystemVPNUp {
		snapshot.SystemDNS = sources.dns(snapshot.SystemInterface)
		snapshot.WorkRoutes = append([]string(nil), sources.routes(snapshot.SystemInterface)...)
		snapshot.EndpointCIDRs = append([]string(nil), sources.endpoints(st)...)
	}
	return snapshot
}
