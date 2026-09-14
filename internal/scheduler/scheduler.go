// Package scheduler manages the Windows Task Scheduler background logon task.
package scheduler

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"
)

// TaskName is the identifier registered in Windows Task Scheduler.
const TaskName = "HMBAMailSync"

// ErrSchedulerUnavailable indicates the Windows Task Scheduler service is disabled or inaccessible on the host.
var ErrSchedulerUnavailable = errors.New("windows task scheduler service unavailable")

// TaskStatus represents the current state and configuration of the scheduled task.
type TaskStatus struct {
	Installed                  bool   `json:"installed"`
	Name                       string `json:"name,omitempty"`
	Enabled                    bool   `json:"enabled,omitempty"`
	State                      string `json:"state,omitempty"`
	LastRunTime                string `json:"last_run_time,omitempty"`
	NextRunTime                string `json:"next_run_time,omitempty"`
	LastExitCode               int    `json:"last_exit_code,omitempty"`
	Delay                      string `json:"delay,omitempty"`
	DisallowStartIfOnBatteries bool   `json:"disallow_start_if_on_batteries,omitempty"`
	StopIfGoingOnBatteries     bool   `json:"stop_if_going_on_batteries,omitempty"`
	ActionCommand              string `json:"action_command,omitempty"`
	ActionArguments            string `json:"action_arguments,omitempty"`
	Error                      string `json:"error,omitempty"`
}

// GenerateLauncherScript generates the VBScript content for windowless execution.
func GenerateLauncherScript(exePath, configPath string) string {
	return fmt.Sprintf(`' HMBA Mail Sync - Silent Scheduled Task Launcher
' Generated automatically by hmba-mail schedule install
Set WshShell = CreateObject("WScript.Shell")
exitCode = WshShell.Run("""%s"" run --config ""%s""", 0, True)
WScript.Quit(exitCode)
`, exePath, configPath)
}

// LauncherPath returns the standard launcher script path adjacent to the config file.
func LauncherPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "run-task.vbs")
}

// WriteLauncher creates or overwrites the silent launcher script.
func WriteLauncher(exePath, configPath string) (string, error) {
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		return "", fmt.Errorf("resolve exe path: %w", err)
	}
	absCfg, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	if _, err := os.Stat(absExe); err != nil {
		return "", fmt.Errorf("executable not found at %s: %w", absExe, err)
	}
	if _, err := os.Stat(absCfg); err != nil {
		return "", fmt.Errorf("config not found at %s: %w", absCfg, err)
	}

	lPath := LauncherPath(absCfg)
	content := GenerateLauncherScript(absExe, absCfg)
	// Windows Script Host recognizes UTF-16LE with a BOM, not UTF-8.
	// Preserve non-ASCII user and OneDrive folder names in the launcher.
	encoded := utf16.Encode([]rune(content))
	data := make([]byte, 2+2*len(encoded))
	binary.LittleEndian.PutUint16(data, 0xfeff)
	for i, unit := range encoded {
		binary.LittleEndian.PutUint16(data[2+i*2:], unit)
	}
	if err := os.WriteFile(lPath, data, 0600); err != nil {
		return "", fmt.Errorf("write launcher script: %w", err)
	}
	return lPath, nil
}

// Install registers the scheduled task in Windows Task Scheduler.
func Install(ctx context.Context, configPath, exePath string) (*TaskStatus, error) {
	if configPath == "" {
		return nil, errors.New("config path required")
	}
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable: %w", err)
		}
	}
	launcherPath, err := WriteLauncher(exePath, configPath)
	if err != nil {
		return nil, err
	}
	return installTask(ctx, launcherPath)
}

// Status queries the current Task Scheduler status.
func Status(ctx context.Context) (*TaskStatus, error) {
	return queryStatus(ctx)
}

// RunNow immediately triggers the scheduled task via Task Scheduler.
func RunNow(ctx context.Context) error {
	return runTask(ctx)
}

// Remove unregisters the scheduled task and removes the launcher if present.
func Remove(ctx context.Context, configPath string) error {
	return removeWith(ctx, configPath, removeTask)
}

func removeWith(ctx context.Context, configPath string, unregister func(context.Context) error) error {
	if err := unregister(ctx); err != nil {
		return err
	}
	if configPath != "" {
		lPath := LauncherPath(configPath)
		if err := os.Remove(lPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove launcher script: %w", err)
		}
	}
	return nil
}
