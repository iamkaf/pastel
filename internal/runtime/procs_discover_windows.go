//go:build windows

package runtime

import (
	"os/exec"
	"strconv"
	"strings"
)

func findServerProcessesWindows(absRoot string) []ProcInfo {
	// Prefer PowerShell CIM for command lines; fall back to WMIC.
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`Get-CimInstance Win32_Process -Filter "name = 'java.exe'" | ForEach-Object { $_.ProcessId.ToString() + '|' + $_.CommandLine }`).Output()
	if err != nil {
		out, err = exec.Command("wmic", "process", "where", "name='java.exe'", "get", "ProcessId,CommandLine", "/FORMAT:LIST").Output()
		if err != nil {
			return nil
		}
		return parseWMICJava(absRoot, string(out))
	}
	var infos []ProcInfo
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pidStr, cmd, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
		if err != nil || pid <= 1 {
			continue
		}
		cmd = strings.TrimSpace(cmd)
		if isMinecraftServerProcess(cmd, absRoot, "") {
			infos = append(infos, ProcInfo{PID: pid, Cmdline: cmd})
		}
	}
	return infos
}

func parseWMICJava(absRoot, raw string) []ProcInfo {
	blocks := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n\n")
	var infos []ProcInfo
	for _, block := range blocks {
		var pid int
		var cmd string
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "ProcessId="); ok {
				pid, _ = strconv.Atoi(strings.TrimSpace(after))
			}
			if after, ok := strings.CutPrefix(line, "CommandLine="); ok {
				cmd = strings.TrimSpace(after)
			}
		}
		if pid <= 1 || cmd == "" {
			continue
		}
		if isMinecraftServerProcess(cmd, absRoot, "") {
			infos = append(infos, ProcInfo{PID: pid, Cmdline: cmd})
		}
	}
	return infos
}
