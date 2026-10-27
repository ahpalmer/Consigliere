package mail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"

	"github.com/ahpalmer/consigliere/internal/config"
)

// Authorize runs the one-time consent for the dedicated mailbox: it opens a browser, asks for read-only Gmail access
// (desktop-app loopback flow with PKCE), and returns the refresh token. The caller saves it to secrets; in the cloud,
// copy it into Key Vault instead.
func Authorize(ctx context.Context, cfg config.Mail, printf func(string, ...any)) (string, error) {
	if cfg.ClientId == "" || cfg.ClientSecret == "" {
		return "", errors.New("set Mail:ClientId and Mail:ClientSecret first (consigliere secrets set ...)")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()

	oauthCfg := OAuthConfig(cfg)
	oauthCfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d/", listener.Addr().(*net.TCPAddr).Port)
	state := randomHex(16)
	verifier := oauth2.GenerateVerifier()
	// Offline + forced consent so Google always returns a refresh token.
	authURL := oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))

	codes := make(chan string, 1)
	failures := make(chan error, 1)
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		case q.Get("error") != "":
			fmt.Fprintln(w, "Authorization failed. You can close this tab.")
			failures <- fmt.Errorf("authorization failed: %s", q.Get("error"))
		default:
			fmt.Fprintln(w, "Consigliere is authorized. You can close this tab.")
			codes <- q.Get("code")
		}
	})}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	printf("Opening your browser. Sign in with the DEDICATED mailbox account.\nIf it doesn't open, visit:\n%s\n", authURL)
	openBrowser(authURL)

	var code string
	select {
	case code = <-codes:
	case err := <-failures:
		return "", err
	case <-time.After(5 * time.Minute):
		return "", errors.New("timed out waiting for authorization")
	case <-ctx.Done():
		return "", ctx.Err()
	}
	token, err := oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", err
	}
	if token.RefreshToken == "" {
		return "", errors.New("Google returned no refresh token; remove the app's access at myaccount.google.com/permissions and retry")
	}
	return token.RefreshToken, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
