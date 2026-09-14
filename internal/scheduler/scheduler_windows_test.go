package scheduler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskStatusPreservesExecutionDetails(t *testing.T) {
	// Given a task with a failed run, Unicode launcher, and battery restrictions,
	// when its PowerShell status is decoded, all user-visible details survive.
	// These are synthetic objects; this test never accesses Task Scheduler.
	script := `
$task = [PSCustomObject]@{
    Name = 'HMBAMailSync'
    Enabled = $true
    LastRunTime = [datetime]'2026-09-13T12:34:56'
    NextRunTime = [datetime]'2026-09-14T12:34:56'
    LastTaskResult = 2
    Definition = [PSCustomObject]@{
        Settings = [PSCustomObject]@{
            DisallowStartIfOnBatteries = $true
            StopIfGoingOnBatteries = $true
        }
    }
}
$stateStr = 'Ready'
$trigger = [PSCustomObject]@{ Delay = 'PT2M' }
$action = [PSCustomObject]@{
    Path = 'wscript.exe'
    Arguments = '//B //Nologo "C:\Users\Renée 李\run-task.vbs"'
}
`
	out, err := runPowerShell(context.Background(), script+taskStatusJSONScript)
	if err != nil {
		t.Fatal(err)
	}
	var status TaskStatus
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatal(err)
	}
	want := TaskStatus{
		Installed: true, Name: TaskName, Enabled: true, State: "Ready",
		LastRunTime: "2026-09-13T12:34:56.0000000", NextRunTime: "2026-09-14T12:34:56.0000000",
		LastExitCode: 2, Delay: "PT2M", DisallowStartIfOnBatteries: true, StopIfGoingOnBatteries: true,
		ActionCommand: "wscript.exe", ActionArguments: `//B //Nologo "C:\Users\Renée 李\run-task.vbs"`,
	}
	if status != want {
		t.Fatalf("status details lost: got %+v, want %+v (JSON: %s)", status, want, out)
	}
}

func TestPowerShellPathRoundTrip(t *testing.T) {
	paths := []string{
		`C:\Users\Test User\run-task.vbs`,
		`C:\Users\O'Brien $env:USERNAME $(1+2) ` + "`" + `n\run-task.vbs`,
		`C:\Users\Renée 李\OneDrive - School [mail]\run-task.vbs`,
		`\\server\shared folder\run-task.vbs`,
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			out, err := runPowerShell(context.Background(), "$launcher = "+powerShellLiteral(path)+"\n[Console]::Write($launcher)")
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != path {
				t.Fatalf("path changed: got %q, want %q", out, path)
			}
		})
	}
}

func TestPowerShellUnicodeJSON(t *testing.T) {
	out, err := runPowerShell(context.Background(), "[PSCustomObject]@{ Path = 'Renée 李' } | ConvertTo-Json -Compress")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "Renée 李") {
		t.Fatalf("Unicode JSON corrupted: %s", out)
	}
}
