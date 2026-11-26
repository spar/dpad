package tui

import "testing"

func TestEscClosesOnlyTheTopScreen(t *testing.T) {
	a := newTestApp()
	a.client = &fakeClient{}
	a.networkDetails, _ = NewNetworkDetailsView(a.ctx, a.client, "n1", "net", a.width, a.bodyHeight())
	a.details, _ = NewDetailsView(a.ctx, a.client, "c1", "web", a.width, a.bodyHeight())
	a.logs, _ = NewLogsView(a.ctx, a.client, "c1", "web", a.width, a.bodyHeight())

	if got := a.screen(); got != screen(a.logs) {
		t.Fatalf("top screen = %T, want logs", got)
	}
	press(a, "esc")
	if a.logs != nil || a.details == nil || a.networkDetails == nil {
		t.Fatal("esc should close logs and keep the panes under it")
	}
	press(a, "esc")
	if a.details != nil || a.networkDetails == nil {
		t.Fatal("esc should close container details and keep network details")
	}
	press(a, "esc")
	if a.screen() != nil {
		t.Fatalf("screen still open: %T", a.screen())
	}
}
