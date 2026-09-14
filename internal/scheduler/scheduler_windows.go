//go:build windows

package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func runPowerShell(ctx context.Context, script string) ([]byte, error) {
	// Windows PowerShell otherwise encodes redirected output using a legacy
	// code page, corrupting Unicode paths returned in task status JSON.
	script = "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)\n" + script
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errStr := strings.TrimSpace(stderr.String())
		if errStr == "" {
			errStr = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("powershell failed: %w: %s", err, errStr)
	}
	return stdout.Bytes(), nil
}

func powerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func isSchedulerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Task Scheduler service unavailable") ||
		strings.Contains(msg, "Schedule.Service") ||
		strings.Contains(msg, "0x80070003")
}

func installTask(ctx context.Context, launcherPath string) (*TaskStatus, error) {
	psScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$launcher = %s
try {
    $service = New-Object -ComObject("Schedule.Service")
    $service.Connect()
    $root = $service.GetFolder("\")
} catch {
    Write-Error "Task Scheduler service unavailable: $_"
    exit 10
}

$taskDef = $service.NewTask(0)
$taskDef.RegistrationInfo.Description = "HMBA Email Automation - local EML conversion, archive and OneDrive sync"

$taskDef.Settings.Enabled = $true
$taskDef.Settings.DisallowStartIfOnBatteries = $false
$taskDef.Settings.StopIfGoingOnBatteries = $false
$taskDef.Settings.ExecutionTimeLimit = "PT15M"
$taskDef.Settings.MultipleInstances = 2 # TASK_INSTANCES_IGNORE_NEW
$taskDef.Settings.StartWhenAvailable = $true

$taskDef.Principal.UserId = "$env:USERDOMAIN\$env:USERNAME"
$taskDef.Principal.LogonType = 3 # TASK_LOGON_INTERACTIVE_TOKEN
$taskDef.Principal.RunLevel = 0  # TASK_RUNLEVEL_LUA (Least privilege)

$trigger = $taskDef.Triggers.Create(9) # TASK_TRIGGER_LOGON
$trigger.UserId = "$env:USERDOMAIN\$env:USERNAME"
$trigger.Delay = "PT2M"
$trigger.Enabled = $true

$action = $taskDef.Actions.Create(0) # TASK_ACTION_EXEC
$action.Path = "wscript.exe"
$action.Arguments = '//B //Nologo "' + $launcher + '"'

$reg = $root.RegisterTaskDefinition(%s, $taskDef, 6, $null, $null, 3)
`, powerShellLiteral(launcherPath), powerShellLiteral(TaskName))

	if _, err := runPowerShell(ctx, psScript); err != nil {
		if isSchedulerUnavailable(err) {
			return nil, fmt.Errorf("%w: %v", ErrSchedulerUnavailable, err)
		}
		return nil, fmt.Errorf("register task definition: %w", err)
	}

	return queryStatus(ctx)
}

// Keep the PowerShell projection aligned with TaskStatus's JSON contract.
// It is shared with the synthetic status test so no installed task is needed.
const taskStatusJSONScript = `
$res = [PSCustomObject]@{
    installed = $true
    name = $task.Name
    enabled = $task.Enabled
    state = $stateStr
    last_run_time = $task.LastRunTime.ToString("o")
    next_run_time = $task.NextRunTime.ToString("o")
    last_exit_code = $task.LastTaskResult
    delay = if ($trigger) { $trigger.Delay } else { "" }
    disallow_start_if_on_batteries = $task.Definition.Settings.DisallowStartIfOnBatteries
    stop_if_going_on_batteries = $task.Definition.Settings.StopIfGoingOnBatteries
    action_command = if ($action) { $action.Path } else { "" }
    action_arguments = if ($action) { $action.Arguments } else { "" }
}
$res | ConvertTo-Json -Compress
`

func queryStatus(ctx context.Context) (*TaskStatus, error) {
	psScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$taskName = %s
try {
    $service = New-Object -ComObject("Schedule.Service")
    $service.Connect()
    $root = $service.GetFolder("\")
} catch {
    Write-Error "Task Scheduler service unavailable: $_"
    exit 10
}
try {
    $task = $root.GetTask($taskName)
} catch {
    $out = [PSCustomObject]@{ Installed = $false }
    $out | ConvertTo-Json -Compress
    exit 0
}

$stateMap = @{
    0 = "Unknown"; 1 = "Disabled"; 2 = "Queued"; 3 = "Ready"; 4 = "Running"
}
$stateStr = $stateMap[[int]$task.State]
if (-not $stateStr) { $stateStr = "Code_$($task.State)" }

$trigger = $null
if ($task.Definition.Triggers.Count -ge 1) {
    $trigger = $task.Definition.Triggers.Item(1)
}

$action = $null
if ($task.Definition.Actions.Count -ge 1) {
    $action = $task.Definition.Actions.Item(1)
}

`, powerShellLiteral(TaskName))
	psScript += taskStatusJSONScript

	out, err := runPowerShell(ctx, psScript)
	if err != nil {
		if isSchedulerUnavailable(err) {
			return nil, fmt.Errorf("%w: %v", ErrSchedulerUnavailable, err)
		}
		return nil, fmt.Errorf("query task status: %w", err)
	}

	var status TaskStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, fmt.Errorf("decode task status JSON: %w", err)
	}
	return &status, nil
}

func runTask(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "schtasks.exe", "/Run", "/TN", TaskName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("schtasks run: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Shared with synthetic COM tests so removal can be checked without touching
// the user's installed task.
const taskRemovalScript = `
$ErrorActionPreference = 'Stop'
try {
    $service = New-Object -ComObject("Schedule.Service")
    $service.Connect()
    $root = $service.GetFolder("\")
} catch {
    Write-Error "Task Scheduler service unavailable: $_"
    exit 10
}
try {
    $root.DeleteTask($taskName, 0)
} catch {
    # COM invocation errors may wrap the actual HRESULT. Only a missing task
    # is an idempotent success; access denied and service errors must surface.
    $exception = $_.Exception
    while ($null -ne $exception) {
        if ($exception.HResult -eq -2147024894) { exit 0 } # 0x80070002
        $exception = $exception.InnerException
    }
    throw
}
`

func removeTask(ctx context.Context) error {
	psScript := "$taskName = " + powerShellLiteral(TaskName) + "\n" + taskRemovalScript

	_, err := runPowerShell(ctx, psScript)
	if err != nil {
		if isSchedulerUnavailable(err) {
			return fmt.Errorf("%w: %v", ErrSchedulerUnavailable, err)
		}
		return fmt.Errorf("remove scheduled task: %w", err)
	}
	return nil
}
