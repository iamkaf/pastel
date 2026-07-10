package jre

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var versionRE = regexp.MustCompile(`version "([0-9]+)(?:\.([0-9]+))?`)

// DetectMajor runs `java -version` (or path) and returns the major version.
func DetectMajor(javaBin string) (int, string, error) {
	if javaBin == "" {
		javaBin = "java"
	}
	cmd := exec.Command(javaBin, "-version")
	var stderr bytes.Buffer
	cmd.Stdout = &stderr // some JVMs print to stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, "", fmt.Errorf("couldn't run %s -version: %w", javaBin, err)
	}
	out := stderr.String()
	m := versionRE.FindStringSubmatch(out)
	if m == nil {
		return 0, out, fmt.Errorf("couldn't parse Java version from: %s", strings.TrimSpace(out))
	}
	major, _ := strconv.Atoi(m[1])
	// Legacy "1.8.0_xxx" style
	if major == 1 && len(m) > 2 && m[2] != "" {
		minor, _ := strconv.Atoi(m[2])
		if minor > 0 {
			major = minor
		}
	}
	return major, strings.TrimSpace(out), nil
}

// Meets reports whether javaBin provides at least requiredMajor.
func Meets(javaBin string, requiredMajor int) (bool, int, error) {
	got, _, err := DetectMajor(javaBin)
	if err != nil {
		return false, 0, err
	}
	return got >= requiredMajor, got, nil
}
