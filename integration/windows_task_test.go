//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const windowsTaskIdentity = `$userId=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value;
$taskName='dev.nils-silbernagel.fs-clip-' + $userId;
`

const windowsUserSID = `function ConvertTo-UserSid([string]$identity) {
    if ($identity -match '^S-\d-') {
        return ([Security.Principal.SecurityIdentifier]::new($identity)).Value;
    }
    return ([Security.Principal.NTAccount]::new($identity)).Translate([Security.Principal.SecurityIdentifier]).Value;
}
`

func prepareWindowsTask(t *testing.T) {
	t.Helper()
	powershell(t, windowsTaskIdentity+`
if (Get-ScheduledTask | Where-Object { $_.TaskPath -eq '\' -and $_.TaskName -eq $taskName }) {
    throw 'An FSClip task already exists. Run installer tests in a clean user session.'
}`)
	t.Cleanup(func() {
		powershell(t, windowsTaskIdentity+`
$task=Get-ScheduledTask | Where-Object { $_.TaskPath -eq '\' -and $_.TaskName -eq $taskName };
if ($task) {
    Stop-ScheduledTask -TaskName $taskName;
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false;
}`)
	})
}

func assertWindowsTask(t *testing.T, binary, watched string) {
	t.Helper()
	powershell(t, windowsTaskIdentity+windowsUserSID+`
$deadline=[DateTime]::UtcNow.AddSeconds(5);
while ((Get-ScheduledTask -TaskName $taskName).State -ne 'Running') {
    if ([DateTime]::UtcNow -ge $deadline) { throw 'Task was not started' };
    Start-Sleep -Milliseconds 100;
};
$tasks=@(Get-ScheduledTask | Where-Object { $_.TaskPath -eq '\' -and $_.TaskName -eq $taskName });
if ($tasks.Count -ne 1) { throw 'Missing or duplicated task' };
[xml]$xml=Export-ScheduledTask -TaskName $taskName;
$task=$xml.Task;
if ($task.Actions.Exec.Command -ne `+psQuote(binary)+`) { throw 'Incorrect executable' };
if ((ConvertTo-UserSid $task.Principals.Principal.UserId) -ne $userId -or
    $task.Principals.Principal.LogonType -ne 'InteractiveToken' -or
    $task.Principals.Principal.RunLevel -ne 'LeastPrivilege') {
    throw "Incorrect task principal (expected user SID $userId): $($task.Principals.Principal.OuterXml)";
};
if ((ConvertTo-UserSid $task.Triggers.LogonTrigger.UserId) -ne $userId) {
    throw "Incorrect login trigger (expected user SID $userId): $($task.Triggers.LogonTrigger.OuterXml)";
};
if ($task.Settings.ExecutionTimeLimit -ne 'PT0S' -or
    $task.Settings.MultipleInstancesPolicy -ne 'IgnoreNew' -or
    $task.Settings.RestartOnFailure.Count -ne '3' -or
    $task.Settings.RestartOnFailure.Interval -ne 'PT1M' -or
    $task.Settings.DisallowStartIfOnBatteries -ne 'false' -or
    $task.Settings.StopIfGoingOnBatteries -ne 'false') { throw 'Incorrect task lifecycle settings' };
if ($tasks[0].State -ne 'Running') { throw 'Task was not started' };
`)
	text := "Scheduled task clipboard — Grüße, 世界!"
	probe := 0
	waitFor(t, "scheduled task clipboard access", func() bool {
		// Use fresh paths so a probe created before watcher startup cannot mask readiness.
		path := filepath.Join(watched, fmt.Sprintf("task-probe-%d.txt", probe))
		probe++
		writeFile(t, path, []byte(text), 0600)
		time.Sleep(150 * time.Millisecond)
		_, err := os.Stat(path)
		return os.IsNotExist(err) && readClipboard(t) == text
	})
	var data bytes.Buffer
	requireSuccess(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 3))))
	path := filepath.Join(watched, "task-image.png")
	writeFile(t, path, data.Bytes(), 0600)
	waitFor(t, "scheduled task image source removal", func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	})
	if readClipboardImage(t).Bounds().Size() != image.Pt(2, 3) {
		t.Fatal("Scheduled task did not copy image contents")
	}
}
