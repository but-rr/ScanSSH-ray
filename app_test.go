package main

import (
	"context"
	"testing"

	"scanssh-ray/backend/config"
	"scanssh-ray/backend/scanner"
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

func TestAdministrationRejectsHostsOutsideConfiguredPools(t *testing.T) {
	app := &App{engine: &scanner.Engine{Config: &config.Config{IPs: []string{"10.0.0.5"}}}}
	for _, ip := range []string{"", "10.0.0.6", "10.0.0.5 & calc.exe"} {
		if err := app.OpenHostConsole(ip); err == nil {
			t.Fatal("console accepted a host outside the pool")
		}
		if err := app.SetAdministratorPassword(ip, "admin", "test-password"); err == nil {
			t.Fatal("password change accepted a host outside the pool")
		}
	}
}
