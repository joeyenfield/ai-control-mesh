package login

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-control-mesh/client/internal/settings"
)

func TestActiveLogoutCannotRestoreAHistoricalServer(t *testing.T) {
	t.Setenv("AI_PROXY_CREDENTIALS", t.TempDir()+"/credentials.json")
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/revoke" {
			t.Error(r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer issuer.Close()
	credentials := &settings.Credentials{Default: settings.Normalize(issuer.URL), Servers: map[string]*settings.ServerEntry{
		settings.Normalize(issuer.URL): {URL: issuer.URL, RefreshToken: "active", Issuer: issuer.URL, ClientID: "mesh-cli", RevocationEndpoint: issuer.URL + "/revoke"},
		"https://old.example":          {URL: "https://old.example", AccessToken: "old-access", RefreshToken: "old-refresh"},
	}}
	if err := credentials.Save(); err != nil {
		t.Fatal(err)
	}
	if code := Logout(nil); code != 0 {
		t.Fatal(code)
	}
	if saved := settings.LoadCredentials(); saved.Default != "" || len(saved.Servers) != 0 {
		t.Fatal("a historical login can become active after logout")
	}
}
