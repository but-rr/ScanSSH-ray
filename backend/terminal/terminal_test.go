package terminal

import (
	"reflect"
	"testing"
)

func TestConsoleArgumentsKeepIdentityAndHostVerification(t *testing.T) {
	o := Options{IP: "10.0.0.5", Port: 2222, User: `domain\admin & whoami`, KeyPath: `C:\SSH keys\admin & key`, KnownHosts: `C:\SSH profile\known_hosts`}
	args, err := o.arguments()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-F", "NUL", "-t", "-o", "StrictHostKeyChecking=accept-new", "-o", `UserKnownHostsFile="C:/SSH profile/known_hosts"`, "-o", "IdentitiesOnly=yes", "-p", "2222", "-l", o.User, "-i", o.KeyPath, o.IP, "cmd.exe /d"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("console arguments differ: %#v", args)
	}
}

func TestConsoleRejectsInvalidTargets(t *testing.T) {
	for _, o := range []Options{
		{IP: "10.0.0.5 & calc.exe", Port: 22, User: "admin", KnownHosts: "known_hosts"},
		{IP: "10.0.0.5", Port: 0, User: "admin", KnownHosts: "known_hosts"},
		{IP: "10.0.0.5", Port: 22, User: "admin\x00", KnownHosts: "known_hosts"},
		{IP: "10.0.0.5", Port: 22, User: "admin", KnownHosts: ""},
	} {
		if _, err := o.arguments(); err == nil {
			t.Fatal("accepted an invalid target")
		}
	}
}
