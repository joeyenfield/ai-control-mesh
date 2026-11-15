package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func sharedProfile(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	for _, name := range []string{"AI_PROXY_CREDENTIALS", "AI_PROXY_MACHINE_HOME", "AI_PROXY_TOKEN", "AI_PROXY_MACHINE_TOKEN", "AI_PROXY_URL"} {
		t.Setenv(name, "")
	}
	t.Setenv("APPDATA", base)
	t.Setenv("XDG_CONFIG_HOME", base)
	return base
}

func TestBothProgramsUseTheSameAuthProfile(t *testing.T) {
	base := sharedProfile(t)
	if got := CredentialsPath(); got != filepath.Join(base, "agent-proxy", "credentials.json") {
		t.Fatal(got)
	}
	t.Setenv("AI_PROXY_MACHINE_HOME", filepath.Join(base, "machine"))
	if got := CredentialsPath(); got != filepath.Join(base, "agent-proxy", "credentials.json") {
		t.Fatal(got)
	}
	t.Setenv("AI_PROXY_CREDENTIALS", filepath.Join(base, "explicit.json"))
	if got := CredentialsPath(); got != filepath.Join(base, "explicit.json") {
		t.Fatal(got)
	}
}

func TestMachineOnlyLoginMovesUnderTheSharedLock(t *testing.T) {
	base := sharedProfile(t)
	server := "https://mesh.example"
	legacy := filepath.Join(base, "agent-machine", "credentials.json")
	credentials := &Credentials{Default: Normalize(server), Servers: map[string]*ServerEntry{
		Normalize(server): {URL: server, User: "alice", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Unix() + 3600, Issuer: server, ClientID: "mesh-cli"},
	}}
	data, _ := json.Marshal(credentials)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if DefaultServerURL() != server {
		t.Fatal("legacy server not discovered before the first refresh")
	}
	if token, err := (Upstream{}).AccessToken(server); err != nil || token != "access" {
		t.Fatalf("%q %v", token, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("old rotating credentials remain in a separate file", err)
	}
	if LoadCredentials().DefaultEntry().RefreshToken != "refresh" {
		t.Fatal("migration lost the refresh token")
	}
	if err := (&Credentials{Servers: map[string]*ServerEntry{}}).Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if LoadCredentials().DefaultEntry() != nil {
		t.Fatal("logout restored a stale legacy sign-in")
	}
}

func TestSharedSelectionWinsAndOtherServersCannotUseCredentials(t *testing.T) {
	sharedProfile(t)
	server, other := "https://mesh.example", "https://other.example"
	credentials := &Credentials{Default: Normalize(server), Servers: map[string]*ServerEntry{
		Normalize(server): {URL: server}, Normalize(other): {URL: other, AccessToken: "old-other-token"},
	}}
	if err := credentials.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_PROXY_URL", other)
	t.Setenv("AI_PROXY_TOKEN", "shared-token")
	t.Setenv("AI_PROXY_MACHINE_TOKEN", "legacy-token")
	if DefaultServerURL() != server {
		t.Fatal("an inherited server override split the shared sign-in")
	}
	if token, err := (Upstream{}).AccessToken(other); !errors.Is(err, ErrServerChanged) || token != "" {
		t.Fatalf("credentials sent to the old server: %q %v", token, err)
	}
	if token, err := (Upstream{}).AccessToken(server); !errors.Is(err, ErrServerChanged) || token != "" {
		t.Fatalf("an injected token was sent somewhere other than its configured URL: %q %v", token, err)
	}
	t.Setenv("AI_PROXY_URL", server)
	if token, err := (Upstream{}).AccessToken(server); err != nil || token != "shared-token" {
		t.Fatalf("%q %v", token, err)
	}
	t.Setenv("AI_PROXY_TOKEN", "")
	if EnvironmentToken() != "legacy-token" {
		t.Fatal("the old machine token alias was lost")
	}
}

func TestMigrationFromAnotherVolumeRemovesTheOldTokenCopy(t *testing.T) {
	base := sharedProfile(t)
	home := filepath.Join(base, "custom-machine-home")
	t.Setenv("AI_PROXY_MACHINE_HOME", home)
	legacy := filepath.Join(home, "credentials.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"default":"https://mesh.example","servers":{"https://mesh.example":{"url":"https://mesh.example","refresh_token":"secret"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	original := migrateRename
	migrateRename = func(string, string) error { return &os.LinkError{Err: syscall.EXDEV} }
	t.Cleanup(func() { migrateRename = original })
	unlock, err := LockCredentials()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if LoadCredentials().DefaultEntry().RefreshToken != "secret" {
		t.Fatal("migration lost the token")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("a separate token copy remains", err)
	}
}
