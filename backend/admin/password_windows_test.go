//go:build windows

package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanssh-ray/backend/proxy"
)

// Exercise the actual PowerShell script, with every account-management cmdlet
// replaced by a local function. No local or remote account is modified.
func TestPowerShellPasswordScriptResolvesExactLocalAdministrator(t *testing.T) {
	t.Run("default_environment", testPasswordScriptScenarios)
	t.Run("inherited_PowerShell_7_modules", func(t *testing.T) {
		// Reproduce pwsh -> go test -> powershell.exe without requiring an
		// installed PowerShell 7. Its incompatible Security module would shadow
		// Windows PowerShell's own ConvertTo-SecureString command.
		root := t.TempDir()
		module := filepath.Join(root, "Microsoft.PowerShell.Security")
		if err := os.Mkdir(module, 0700); err != nil {
			t.Fatal(err)
		}
		manifest := `@{
ModuleVersion='7.0.0'
GUID='A94C8C7E-9810-47C0-B8AF-65089C13A35A'
PowerShellVersion='7.0'
CompatiblePSEditions=@('Core')
RootModule='Microsoft.PowerShell.Security.dll'
CmdletsToExport=@('ConvertTo-SecureString')
FunctionsToExport=@()
AliasesToExport=@()
}`
		if err := os.WriteFile(filepath.Join(module, "Microsoft.PowerShell.Security.psd1"), []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PSModulePath", root+string(os.PathListSeparator)+os.Getenv("PSModulePath"))
		testPasswordScriptScenarios(t)
	})
}

// Starting Windows PowerShell through an intermediate Go process bypasses
// pwsh's automatic removal of Core module paths. Let powershell.exe construct
// its own defaults, without modifying the parent process's environment.
func windowsPowerShellTestEnv() []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "PSModulePath") {
			environment = append(environment, entry)
		}
	}
	return environment
}

func testPasswordScriptScenarios(t *testing.T) {
	t.Helper()
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
			cmd.Env = windowsPowerShellTestEnv()
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
