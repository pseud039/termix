package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	zspotify "github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
)

// redirectURI must be an EXACT match (including trailing slash or lack
// thereof) of a Redirect URI registered on your app at
// https://developer.spotify.com/dashboard. If yours is different,
// change this constant to match.
const redirectURI = "http://127.0.0.1:8080/callback"

const authState = "termix-auth"

// Credentials identify the Spotify app registered at
// https://developer.spotify.com/dashboard. They come from config.toml.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// oauthConfig builds the OAuth2 config directly against Spotify's
// documented endpoints.
func oauthConfig(creds Credentials) (*oauth2.Config, error) {
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, errors.New("spotify.client_id and spotify.client_secret are not set in config.toml")
	}
	return &oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		RedirectURL:  redirectURI,
		Scopes: []string{
			"user-read-playback-state",
			"user-modify-playback-state",
			"user-read-currently-playing",
			// Library tab: Liked Songs and the user's playlists. A token
			// saved before these were added lacks them, so the tab asks
			// for `termix auth` to be run again.
			"user-library-read",
			"playlist-read-private",
			"playlist-read-collaborative",
		},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.spotify.com/authorize",
			TokenURL: "https://accounts.spotify.com/api/token",
		},
	}, nil
}

func tokenPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "termix")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "spotify_token.json"), nil
}

func saveToken(tok *oauth2.Token) error {
	path, err := tokenPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadToken() (*oauth2.Token, error) {
	path, err := tokenPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no saved Spotify login — run `termix auth` first: %w", err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// Login runs the one-time browser OAuth flow (authorization code grant)
// and saves the resulting token to disk. Call this from `termix auth`.
// It blocks until the browser redirect hits our local callback server,
// or times out after 3 minutes.
func Login(ctx context.Context, creds Credentials) error {
	cfg, err := oauthConfig(creds)
	if err != nil {
		return err
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if state := r.URL.Query().Get("state"); state != authState {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- errors.New("oauth state mismatch")
			return
		}
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			http.Error(w, "spotify denied access: "+errParam, http.StatusForbidden)
			errCh <- fmt.Errorf("spotify denied access: %s", errParam)
			return
		}
		code := r.URL.Query().Get("code")
		fmt.Fprintln(w, "Logged in — you can close this tab and return to termix.")
		codeCh <- code
	})

	srv := &http.Server{Addr: "127.0.0.1:8080", Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer srv.Close()

	url := cfg.AuthCodeURL(authState, oauth2.AccessTypeOffline)
	fmt.Println("Open this URL in your browser to log in to Spotify:")
	fmt.Println(url)

	select {
	case code := <-codeCh:
		tok, err := cfg.Exchange(ctx, code)
		if err != nil {
			return fmt.Errorf("exchanging code for token: %w", err)
		}
		if err := saveToken(tok); err != nil {
			return fmt.Errorf("saving token: %w", err)
		}
		fmt.Println("Saved credentials — you're good to go.")
		return nil
	case err := <-errCh:
		return err
	case <-time.After(3 * time.Minute):
		return errors.New("timed out waiting for browser login")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// persistingTokenSource wraps an oauth2.TokenSource and writes the token
// to disk every time the underlying source refreshes it, so termix
// doesn't need a fresh browser login every time the ~1hr access token
// expires — only the very first time (or if the refresh token itself
// is revoked).
type persistingTokenSource struct {
	base oauth2.TokenSource
	last string
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != p.last {
		p.last = tok.AccessToken
		_ = saveToken(tok) // best-effort; don't fail playback over a disk write
	}
	return tok, nil
}

// NewClient loads the cached token from disk (saved by Login) and
// returns a ready-to-use Spotify Web API client that refreshes and
// persists its own token silently.
func NewClient(ctx context.Context, creds Credentials) (*zspotify.Client, error) {
	cfg, err := oauthConfig(creds)
	if err != nil {
		return nil, err
	}
	tok, err := loadToken()
	if err != nil {
		return nil, err
	}

	ts := &persistingTokenSource{base: cfg.TokenSource(ctx, tok)}
	httpClient := oauth2.NewClient(ctx, ts)
	return zspotify.New(httpClient), nil
}
