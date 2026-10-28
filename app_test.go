package main

import (
	"context"
	"testing"
)

func TestCloseRequestsDoNotExitUntilTrayApproves(t *testing.T) {
	app := &App{}
	if err := app.HideToTray(); err == nil {
		t.Fatal("hiding without a notification icon must fail safely")
	}
	for i := 0; i < 3; i++ {
		if !app.beforeClose(context.Background()) {
			t.Fatal("ordinary close request was allowed to exit")
		}
	}
	app.exiting.Store(true)
	if app.beforeClose(context.Background()) {
		t.Fatal("tray-approved shutdown was blocked")
	}
}
