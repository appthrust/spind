package config

import (
	"os"
	"os/exec"
)

func FirstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func FindExecutable(name string) string {
	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}
	return ""
}
