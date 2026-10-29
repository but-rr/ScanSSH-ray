package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanssh-ray/backend/config"
	"scanssh-ray/backend/internal/testssh"
	sshclient "scanssh-ray/backend/ssh"
)

func adminManager(t *testing.T, response string, status uint32) (*sshclient.Manager, *testssh.Server) {
	t.Helper()
	server := testssh.Start(t, func(testssh.Execution) (string, uint32) { return response, status })
	manager, err := sshclient.New(&config.Config{User: "tester", SSHPort: server.Port, KeyPath: server.KeyPath, KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), ConnectTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager, server
}

func TestPasswordTravelsOnlyThroughSSHStdin(t *testing.T) {
	manager, server := adminManager(t, "SCANSSH_ADMIN:ok\r\n", 0)
	user, password := "Local Admin", `S3cret'"$(); & пароль\42`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ChangePassword(ctx, manager, "127.0.0.1", user, password); err != nil {
		t.Fatal(err)
	}
	execution := <-server.Executions
	if strings.Contains(execution.Command, password) || strings.Contains(execution.Command, user) || strings.Contains(execution.Command, base64.StdEncoding.EncodeToString([]byte(password))) {
		t.Fatal("account data leaked into command arguments")
	}
	data, err := base64.StdEncoding.DecodeString(execution.Input)
	if err != nil {
		t.Fatal(err)
	}
	var request struct{ User, Password string }
	if err := json.Unmarshal(data, &request); err != nil || request.User != user || request.Password != password {
		t.Fatal("SSH stdin did not preserve account data")
	}
}

func TestRemoteErrorsAreTranslatedWithoutReturningRemoteOutput(t *testing.T) {
	for _, code := range []string{"not_found", "not_admin", "denied", "rejected", "failed", "unexpected-sensitive-output"} {
		t.Run(code, func(t *testing.T) {
			manager, _ := adminManager(t, "SCANSSH_ADMIN:"+code, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := ChangePassword(ctx, manager, "127.0.0.1", "admin", "private-password")
			if err == nil || strings.Contains(err.Error(), "private-password") || strings.Contains(err.Error(), "sensitive-output") {
				t.Fatal("remote failure was accepted or leaked input/output")
			}
		})
	}
}

func TestInvalidPasswordRequestDoesNotConnect(t *testing.T) {
	for _, request := range [][2]string{{"", "secret"}, {`domain\admin`, "secret"}, {"admin\n", "secret"}, {"admin", ""}, {"admin", "bad\x00secret"}, {"admin", strings.Repeat("x", 257)}} {
		// A nil manager is intentional: invalid input must fail before SSH access.
		if err := ChangePassword(context.Background(), nil, "127.0.0.1", request[0], request[1]); err == nil {
			t.Fatal("accepted an invalid password request")
		}
	}
}
