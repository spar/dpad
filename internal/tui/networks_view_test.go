package tui

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

func networkContainers() []container.Summary {
	return []container.Summary{
		{ID: "web-id", Names: []string{"/web"}, NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{"app": {NetworkID: "net-id", EndpointID: "endpoint-id"}}}},
		{ID: "old-id", Names: []string{"/old-web"}, NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{"app": {NetworkID: "net-id"}}}},
	}
}
func networkFake() *fakeClient {
	return &fakeClient{networks: []network.Summary{{Network: network.Network{ID: "net-id", Name: "app", Driver: "bridge", Scope: "local", Labels: map[string]string{composeProjectLabel: "shop"}}}, {Network: network.Network{ID: "empty-id", Name: "empty", Driver: "bridge"}}}, networkInfo: network.Inspect{Network: network.Network{ID: "net-id", Name: "app", Driver: "bridge", Scope: "local"}, Containers: map[string]network.EndpointResource{
		"web-id":    {Name: "web", EndpointID: "endpoint-id", IPv4Address: netip.MustParsePrefix("172.30.0.2/24"), IPv6Address: netip.MustParsePrefix("fd00:1234::2/64"), MacAddress: network.HardwareAddr{2, 66, 172, 30, 0, 2}},
		"worker-id": {Name: "worker", IPv4Address: netip.MustParsePrefix("172.30.0.3/24")},
	}}}
}
func TestNetworksLazySharedMembershipAndFailures(t *testing.T) {
	f := networkFake()
	v := NewNetworksView(t.Context(), f)
	if v.Init() != nil || v.Update(refreshMsg{}) != nil {
		t.Fatal("unopened tab fetched networks")
	}
	v.Update(containersMsg{items: networkContainers()})
	if len(v.usage) != 0 {
		t.Fatal("indexed unopened tab")
	}
	v.Update(v.Focus()())
	if len(f.actions) != 1 || f.actions[0] != "networks " {
		t.Fatal("unexpected calls", f.actions)
	}
	for _, filter := range []string{"app", "net-id", "BRIDGE", "local", "shop", "web", "old-id"} {
		v.SetFilter(filter)
		rows, _ := v.visible()
		if len(rows) == 0 {
			t.Fatal("missing filter", filter)
		}
	}
	v.SetFilter("app")
	if !strings.Contains(v.View(), "1 / 2") {
		t.Fatal(v.View())
	}
	v.Update(containersMsg{err: errors.New("lookup failed")})
	if !strings.Contains(v.View(), "unknown") || strings.Contains(v.View(), "0 / 0") {
		t.Fatal("failure reported zero attachments", v.View())
	}
	v.Update(containersMsg{items: networkContainers()})
	if !strings.Contains(v.View(), "1 / 2") {
		t.Fatal("usage did not recover")
	}
	v.SetSize(80, 12)
	if !strings.Contains(ansi.Strip(v.View()), "ATTACHED / CONFIGURED") || strings.Contains(v.View(), "PROJECT") {
		t.Fatal(v.View())
	}
}
func TestNetworksCoalescePreserveSelectionAndPinIDs(t *testing.T) {
	f := networkFake()
	v := NewNetworksView(t.Context(), f)
	v.Update(v.Focus()())
	v.cur.key = "net-id"
	v.SetFilter("bridge")
	v.HandleKey("r")
	first := v.Focus()
	for range 100 {
		if v.Update(refreshMsg{}) != nil {
			t.Fatal("overlapping list request")
		}
	}
	next := v.Update(first())
	if next == nil {
		t.Fatal("pending refresh lost")
	}
	v.Update(next())
	if id, _, ok := v.SelectedNetwork(); !ok || id != "net-id" || v.filter != "bridge" || !v.reverse {
		t.Fatal("view state lost")
	}
	f.networkErr = errors.New("offline")
	v.Update(v.Focus()())
	if _, _, ok := v.SelectedNetwork(); ok {
		t.Fatal("failed list offered details")
	}
	f.networkErr = nil
	v.Update(v.Focus()())
	if id, _, _ := v.SelectedNetwork(); id != "net-id" {
		t.Fatal("recovery lost selection")
	}
	f.networks = f.networks[1:]
	v.Update(v.Focus()())
	if id, _, ok := v.SelectedNetwork(); !ok || id != "empty-id" {
		t.Fatal("removed row did not fall back")
	}
	v.Update(networksMsg{v: NewNetworksView(t.Context(), f), err: errors.New("foreign")})
	if v.err != nil {
		t.Fatal("foreign result applied")
	}
}
func TestNetworkEndpointNavigationAndReturn(t *testing.T) {
	a := newTestApp()
	f := networkFake()
	a.client = f
	source := a.tabs[6].(*NetworksView)
	source.client = f
	a.Update(containersMsg{items: networkContainers()})
	a.Update(a.switchTab(6)())
	source.SetFilter("shop")
	a.filter.SetValue("shop")
	source.HandleKey("r")
	for _, k := range []string{"S", "x", "R", "e"} {
		press(a, k)
		if a.confirm != nil || a.shell != nil {
			t.Fatal("network list offered mutations")
		}
	}
	a.Update(a.handleKey(key("enter"))())
	v := a.networkDetails
	if v == nil || !strings.Contains(v.View(), "Scope") || f.actions[len(f.actions)-1] != "network inspect net-id" {
		t.Fatal("network details missing or unpinned")
	}
	for _, k := range []string{"S", "x", "R", "e"} {
		press(a, k)
		if a.confirm != nil || a.shell != nil {
			t.Fatal("network metadata offered mutations")
		}
	}
	press(a, "tab")
	if !v.showEndpoints || !strings.Contains(v.View(), "172.30.0.2/24") || !strings.Contains(v.View(), "fd00:1234::2/64") || !strings.Contains(v.View(), "02:42:ac:1e:00:02") {
		t.Fatal(v.View())
	}
	press(a, "j")
	id, _, _ := v.SelectedEndpoint()
	cmd := a.handleKey(key("enter"))
	if cmd == nil || a.details == nil || a.details.id != id {
		t.Fatal("wrong endpoint opened")
	}
	a.Update(cmd())
	containerDetails := a.details
	a.resize(90, 12)
	if containerDetails.vp.Width() != 90 || v.vp.Width() != 90 || v.endpoints.width != 90 {
		t.Fatal("nested details did not resize together")
	}
	press(a, "enter")
	if a.logs == nil || a.details != containerDetails || a.networkDetails != v {
		t.Fatal("opening endpoint logs discarded parent panes")
	}
	logsClosed := false
	cancelLogs := a.logs.cancel
	a.logs.cancel = func() {
		logsClosed = true
		cancelLogs()
	}
	press(a, "esc")
	if a.logs != nil || !logsClosed || a.details != containerDetails || v.ctx.Err() != nil {
		t.Fatal("closing logs lost or cancelled its parent panes")
	}
	press(a, "esc")
	if containerDetails.ctx.Err() == nil || v.ctx.Err() != nil {
		t.Fatal("closing container details cancelled the wrong context")
	}
	if a.details != nil || a.networkDetails != v || !v.showEndpoints {
		t.Fatal("container return lost network pane")
	}
	if got, _, _ := v.SelectedEndpoint(); got != id {
		t.Fatal("endpoint selection lost")
	}
	a.resize(80, 24)
	if !strings.Contains(v.View(), "IPv6:") {
		t.Fatal("narrow layout hides IPv6")
	}
	press(a, "esc")
	if a.networkDetails != nil || v.ctx.Err() == nil || source.filter != "shop" || !source.reverse || a.active != 6 {
		t.Fatal("return lost list state or leaked context")
	}
	if strings.Contains(ansi.Strip(a.footer()), "S/x/R") || strings.Contains(ansi.Strip(a.helpView()), "start (or unpause)") {
		t.Fatal("network view advertises mutations")
	}
	if !strings.Contains(ansi.Strip(a.tabBar()), "7 Networks") {
		t.Fatal(a.tabBar())
	}
}
func TestNetworkInspectStaleFailureAndCancellation(t *testing.T) {
	f := networkFake()
	v, first := NewNetworkDetailsView(t.Context(), f, "net-id", "app", 120, 25)
	second := v.load()
	v.Update(first())
	if !v.loading || v.loaded {
		t.Fatal("obsolete inspection applied")
	}
	v.Update(second())
	v.showEndpoints = true
	if _, _, ok := v.SelectedEndpoint(); !ok {
		t.Fatal("no endpoints")
	}
	v.endpoints.cur.key = "web-id"
	f.networkErr = errors.New("network removed")
	v.Update(v.load()())
	if _, _, ok := v.SelectedEndpoint(); ok || !strings.Contains(v.View(), "network removed") {
		t.Fatal("failed inspection offered stale endpoints")
	}
	f.networkErr = nil
	v.Update(v.load()())
	if id, _, ok := v.SelectedEndpoint(); !ok || id != "web-id" {
		t.Fatal("refresh lost endpoint selection")
	}
	f.networkInfo.Containers = nil
	v.Update(v.load()())
	if _, _, ok := v.SelectedEndpoint(); ok || !strings.Contains(v.View(), "no container endpoints") {
		t.Fatal("removed endpoints remained selectable")
	}
	pending := v.load()
	v.Close()
	v.Update(pending())
	if v.load() != nil {
		t.Fatal("inspect after close")
	}
	ctx, cancel := context.WithCancel(t.Context())
	list := NewNetworksView(ctx, f)
	cancel()
	if list.Focus() != nil {
		t.Fatal("list after cancellation")
	}
}
func BenchmarkNetworksUsageAndView(b *testing.B) {
	f := networkFake()
	f.networks = make([]network.Summary, 1000)
	items := make([]container.Summary, 1000)
	for i := range f.networks {
		id := fmt.Sprint(i)
		name := "net-" + id
		f.networks[i] = network.Summary{Network: network.Network{ID: id, Name: name, Driver: "bridge", Scope: "local"}}
		items[i] = container.Summary{ID: "web-" + id, NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{name: {NetworkID: id, EndpointID: "ep-" + id}}}}
	}
	v := NewNetworksView(b.Context(), f)
	v.SetSize(160, 35)
	v.Update(v.Focus()())
	b.ReportAllocs()
	for b.Loop() {
		v.Update(containersMsg{items: items})
		v.View()
	}
}
