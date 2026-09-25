package hook

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

// GenerateHookScript returns the shell integration hook string for bash, zsh, or fish.
func GenerateHookScript(shellName string, binName string) (string, error) {
	if binName == "" {
		binName = "cron-person"
	}

	switch shellName {
	case "bash":
		return fmt.Sprintf(`_cron_person_hook() {
  local previous_exit_status=$?;
  trap -- '' SIGINT;
  eval "$("%s" hook export bash 2>/dev/null)";
  trap - SIGINT;
  return $previous_exit_status;
};
if [[ ";${PROMPT_COMMAND:-};" != *";_cron_person_hook;"* ]]; then
  PROMPT_COMMAND="_cron_person_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
fi
`, binName), nil

	case "zsh":
		return fmt.Sprintf(`_cron_person_hook() {
  trap -- '' SIGINT;
  eval "$("%s" hook export zsh 2>/dev/null)";
  trap - SIGINT;
}
typeset -ag precmd_functions;
if (( ! ${precmd_functions[(I)_cron_person_hook]} )); then
  precmd_functions=(_cron_person_hook $precmd_functions);
fi
typeset -ag chpwd_functions;
if (( ! ${chpwd_functions[(I)_cron_person_hook]} )); then
  chpwd_functions=(_cron_person_hook $chpwd_functions);
fi
`, binName), nil

	case "fish":
		return fmt.Sprintf(`function __cron_person_export_eval --on-event fish_prompt;
  "%s" hook export fish 2>/dev/null | source;
end
`, binName), nil

	default:
		return "", fmt.Errorf("unsupported shell %q: supported shells are bash, zsh, fish", shellName)
	}
}

// HandleHookExport is executed on every directory change or prompt hook.
// It checks if a .cronrc exists in the current hierarchy and warns if it is unauthorized or changed.
func HandleHookExport(st *store.Store, dir string, isAgent bool, stderr io.Writer) error {
	foundCronrc, err := cronrc.FindCronrc(dir)
	if err != nil {
		return err
	}
	if foundCronrc == "" {
		return nil
	}

	cronrcDir := filepath.Dir(foundCronrc)
	status, _, err := st.Status(cronrcDir)
	if err != nil {
		return err
	}

	switch status {
	case store.StatusUnauthorized:
		if isAgent {
			fmt.Fprintf(stderr, "WARN: unauthorized .cronrc at %s\n", foundCronrc)
		} else {
			fmt.Fprintf(stderr, "[WARN] cron-person: unauthorized .cronrc detected at %s (run 'cron-person allow' to approve)\n", foundCronrc)
		}
	case store.StatusChanged:
		if isAgent {
			fmt.Fprintf(stderr, "WARN: modified .cronrc at %s\n", foundCronrc)
		} else {
			fmt.Fprintf(stderr, "[WARN] cron-person: modified .cronrc detected at %s (run 'cron-person allow' to approve changes)\n", foundCronrc)
		}
	case store.StatusAllowed, store.StatusMissingCronrc, store.StatusOrphan:
		// No warning needed
	}

	return nil
}
