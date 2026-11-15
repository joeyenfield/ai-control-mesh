package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// ErrServerChanged prevents a pinned process or old socket using another server after login.
var ErrServerChanged = errors.New("the shared sign-in selects another server")

// Tests can exercise migration across volumes without requiring a second mounted volume.
var migrateRename = replaceFile

// ExistingLogin lets either login command reuse a usable shared sign-in, including refresh.
// A different account on the same server requires logout first.
func ExistingLogin(server string) (*ServerEntry, error) {
	entry := LoadCredentials().DefaultEntry()
	if entry == nil || Normalize(entry.URL) != Normalize(server) {
		return nil, nil
	}
	if _, err := (Upstream{}).AccessToken(server); errors.Is(err, ErrLoginRequired) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	credentials := LoadCredentials()
	if err := credentials.CheckServer(server); err != nil {
		return nil, err
	}
	return credentials.Servers[Normalize(server)], nil
}

// EnvironmentToken accepts the old machine variable as an alias for both programs.
func EnvironmentToken() string {
	if token := os.Getenv("AI_PROXY_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("AI_PROXY_MACHINE_TOKEN")
}

func (c *Credentials) CheckServer(server string) error {
	if entry := c.DefaultEntry(); entry != nil && entry.URL != "" && Normalize(entry.URL) != Normalize(server) {
		return fmt.Errorf("%w: %s; run `agent login %s` to switch both programs", ErrServerChanged, entry.URL, server)
	}
	// An injected token belongs to its explicitly configured URL. Never silently send it
	// to a saved default instead when an old environment and the shared login disagree.
	if configured := os.Getenv("AI_PROXY_URL"); EnvironmentToken() != "" && configured != "" && Normalize(configured) != Normalize(server) {
		return fmt.Errorf("%w: AI_PROXY_URL conflicts with the token's destination; clear the inherited token/server variables or switch the shared login", ErrServerChanged)
	}
	return nil
}

func legacyCredentialsPath() string {
	// An explicit profile must never import credentials from the user's real profile.
	if os.Getenv("AI_PROXY_CREDENTIALS") != "" {
		return ""
	}
	if home := os.Getenv("AI_PROXY_MACHINE_HOME"); home != "" {
		return filepath.Join(expandHome(home), "credentials.json")
	}
	return filepath.Join(configDir(), "agent-machine", "credentials.json")
}

func credentialData() []byte {
	data, err := os.ReadFile(CredentialsPath())
	if os.IsNotExist(err) {
		if legacy := legacyCredentialsPath(); legacy != "" {
			data, _ = os.ReadFile(legacy)
		}
	}
	return data
}

// migrateCredentials runs only with the shared lock held. Move the old machine-only file,
// rather than leaving a second copy of its rotating refresh token in use.
func migrateCredentials() error {
	legacy := legacyCredentialsPath()
	if legacy == "" {
		return nil
	}
	if _, err := os.Stat(CredentialsPath()); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	err := migrateRename(legacy, CredentialsPath())
	if err == nil || !(errors.Is(err, syscall.EXDEV) || (runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(17)))) {
		return err
	}
	// A custom machine home can be on another volume. Publish a private copy atomically,
	// then remove the old file, while neither updated program can refresh outside this lock.
	data, err := os.ReadFile(legacy)
	if err != nil {
		return err
	}
	temporary := CredentialsPath() + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := replaceFile(temporary, CredentialsPath()); err != nil {
		return err
	}
	return os.Remove(legacy)
}
