// Package hookreference exposes the production hook generator for migration checks.
package hookreference

import (
	"encoding/hex"
	"errors"

	"github.com/alexgorbatchev/cron-person-cli/internal/hook"
)

// GenerateHookScript calls the actual production generator.
func GenerateHookScript(shell, binary string) (string, error) {
	return hook.GenerateHookScript(shell, binary)
}

// GenerateHookBytes preserves arbitrary Go string bytes across the JSON boundary.
func GenerateHookBytes(shell, binary string) (string, error) {
	shellBytes, err := hex.DecodeString(shell)
	if err != nil {
		return "", errors.New("invalid shell hex")
	}
	binaryBytes, err := hex.DecodeString(binary)
	if err != nil {
		return "", errors.New("invalid binary hex")
	}
	script, err := hook.GenerateHookScript(string(shellBytes), string(binaryBytes))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString([]byte(script)), nil
}
