//go:build integration && windows

package integration

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsPowerShellModuleDiscovery(t *testing.T) {
	// Model the module path inherited through Go from a different PowerShell edition.
	t.Setenv("PSModulePath", t.TempDir())
	data := []byte("fs-clip checksum regression\n")
	path := filepath.Join(t.TempDir(), "checksum.txt")
	writeFile(t, path, data, 0600)
	output := powershell(t,
		"$null=Get-Command Get-FileHash, Set-Clipboard, Get-Clipboard -ErrorAction Stop; "+
			"[Console]::Write((Get-FileHash -LiteralPath "+psQuote(path)+" -Algorithm SHA256).Hash)")
	expected := fmt.Sprintf("%x", sha256.Sum256(data))
	if !strings.EqualFold(strings.TrimSpace(string(output)), expected) {
		t.Fatalf("Unexpected checksum from Windows PowerShell: %q", output)
	}
}

func TestWindowsUserIdentity(t *testing.T) {
	powershell(t, windowsUserSID+`
$current=[Security.Principal.WindowsIdentity]::GetCurrent();
foreach ($identity in @($current.User.Value, $current.Name)) {
    if ((ConvertTo-UserSid $identity) -ne $current.User.Value) {
        throw "Account identity did not resolve to the current user: $identity";
    }
};
$otherSid='S-1-1-0';
if ((ConvertTo-UserSid $otherSid) -eq $current.User.Value) {
    throw 'A different user was accepted as the current user';
}`)
}
