// Package admin performs explicit administration of a selected Windows host.
package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

	"scanssh-ray/backend/proxy"
	sshclient "scanssh-ray/backend/ssh"
)

func ValidatePasswordRequest(user, password string) error {
	if strings.TrimSpace(user) == "" || user != strings.TrimSpace(user) || strings.ContainsAny(user, "\x00\r\n\\/") {
		return fmt.Errorf("укажите имя локальной учётной записи без домена")
	}
	if password == "" || strings.ContainsAny(password, "\x00\r\n") || len(utf16.Encode([]rune(password))) > 256 {
		return fmt.Errorf("укажите непустой пароль до 256 символов без переносов строк")
	}
	return nil
}

// User and password are data on SSH stdin, never interpolated into a command.
// Resolve an exact local name and then operate by SID to avoid wildcard matches
// or accidentally changing a domain account.
const passwordScript = `$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
try {
    $request=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd())) | ConvertFrom-Json
    $account=Get-LocalUser | Where-Object { $_.Name -eq $request.user } | Select-Object -First 1
    if ($null -eq $account) { Write-Output 'SCANSSH_ADMIN:not_found'; exit 11 }
    $administrators=Get-LocalGroupMember -SID 'S-1-5-32-544'
    if (-not ($administrators | Where-Object { $_.SID.Value -eq $account.SID.Value })) { Write-Output 'SCANSSH_ADMIN:not_admin'; exit 12 }
    $secure=ConvertTo-SecureString -String $request.password -AsPlainText -Force
    try {
        Set-LocalUser -SID $account.SID -Password $secure
    } catch {
        if ($_.CategoryInfo.Category -eq 'PermissionDenied') { Write-Output 'SCANSSH_ADMIN:denied'; exit 13 }
        Write-Output 'SCANSSH_ADMIN:rejected'; exit 14
    } finally {
        if ($null -ne $secure) { $secure.Dispose() }
        $request.password=$null
    }
    Write-Output 'SCANSSH_ADMIN:ok'
} catch {
    Write-Output 'SCANSSH_ADMIN:failed'; exit 15
}`

func ChangePassword(ctx context.Context, manager *sshclient.Manager, ip, user, password string) error {
	if err := ValidatePasswordRequest(user, password); err != nil {
		return err
	}
	client, err := manager.Connect(ctx, ip, false)
	if err != nil {
		return err
	}
	defer client.Close()
	data, err := json.Marshal(struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}{user, password})
	if err != nil {
		return fmt.Errorf("не удалось подготовить запрос смены пароля")
	}
	output, err := sshclient.Run(ctx, client, proxy.EncodedPowerShell(passwordScript), base64.StdEncoding.EncodeToString(data))
	if ctx.Err() != nil {
		return fmt.Errorf("истёк таймаут: результат смены пароля не подтверждён")
	}
	code := strings.TrimSpace(strings.TrimPrefix(output, "\uFEFF"))
	if code == "SCANSSH_ADMIN:ok" && err == nil {
		return nil
	}
	switch code {
	case "SCANSSH_ADMIN:not_found":
		return fmt.Errorf("локальная учётная запись не найдена на выбранном ПК")
	case "SCANSSH_ADMIN:not_admin":
		return fmt.Errorf("выбранная учётная запись не входит в локальную группу администраторов")
	case "SCANSSH_ADMIN:denied":
		return fmt.Errorf("недостаточно прав: для смены пароля подключитесь по SSH с правами администратора")
	case "SCANSSH_ADMIN:rejected":
		return fmt.Errorf("Windows отклонила новый пароль: проверьте политику сложности, историю паролей и права SSH-пользователя")
	case "SCANSSH_ADMIN:failed":
		return fmt.Errorf("не удалось выполнить запрос: проверьте административные права и модуль LocalAccounts в PowerShell удалённого ПК")
	default:
		return fmt.Errorf("результат смены пароля не подтверждён: проверьте соединение с выбранным ПК")
	}
}
