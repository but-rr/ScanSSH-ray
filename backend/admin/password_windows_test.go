//go:build windows

package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"scanssh-ray/backend/proxy"
)

// Exercise the actual PowerShell script, with every account-management cmdlet
// replaced by a local function. No local or remote account is modified.
func TestPowerShellPasswordScriptResolvesExactLocalAdministrator(t *testing.T) {
	for _, scenario := range []string{"ok", "missing", "ordinary", "policy"} {
		t.Run(scenario, func(t *testing.T) {
			user, password := "Admin [exact]", `S3cret'"$(); & пароль\42`
			data, _ := json.Marshal(map[string]string{"user": user, "password": password})
			payload := base64.StdEncoding.EncodeToString(data)
			prefix := `$expected=([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + payload + `')) | ConvertFrom-Json)
$accountSID='S-1-5-21-111-222-333-1001'
function Get-LocalUser { [pscustomobject]@{ Name='` + user + `'; SID=[pscustomobject]@{ Value=$accountSID } } }
function Get-LocalGroupMember { param($SID) if ($SID -ne 'S-1-5-32-544') { throw 'wrong group' }; [pscustomobject]@{ SID=[pscustomobject]@{ Value=$accountSID } } }
function Set-LocalUser {
    param($SID,$Password)
    if ($SID.Value -ne $accountSID) { throw 'wrong account' }
    $pointer=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($Password)
    try { $actual=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer); if ($actual -cne $expected.password) { throw 'wrong password bytes' } }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
}
`
			want := "SCANSSH_ADMIN:ok"
			switch scenario {
			case "missing":
				prefix += "function Get-LocalUser { [pscustomobject]@{ Name='Other Admin'; SID=$null } }; function Set-LocalUser { exit 98 }; "
				want = "SCANSSH_ADMIN:not_found"
			case "ordinary":
				prefix += "function Get-LocalGroupMember { param($SID) }; function Set-LocalUser { exit 98 }; "
				want = "SCANSSH_ADMIN:not_admin"
			case "policy":
				prefix += "function Set-LocalUser { param($SID,$Password) throw 'private-policy-error' }; "
				want = "SCANSSH_ADMIN:rejected"
			}
			command := proxy.EncodedPowerShell(prefix + passwordScript)
			encoded := command[strings.LastIndex(command, " ")+1:]
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
			cmd.WaitDelay = 5 * time.Second
			cmd.Stdin = strings.NewReader(payload)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil || !strings.Contains(string(output), want) || (scenario == "ok" && err != nil) {
				t.Fatalf("password script scenario %s failed (context: %v): %v %s", scenario, ctx.Err(), err, output)
			}
			if strings.Contains(string(output), password) || strings.Contains(string(output), "private-policy-error") {
				t.Fatal("script leaked sensitive details")
			}
		})
	}
}
