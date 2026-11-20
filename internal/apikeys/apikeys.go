package apikeys

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Debian/dcs/internal/httperr"
	"github.com/Debian/dcs/internal/version"
	"github.com/Debian/dcs/internal/web/common"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/securecookie"
	"golang.org/x/oauth2"
)

// See also https://docs.gitlab.com/ee/api/oauth2.html

// TODO: reference this code from https://wiki.debian.org/Salsa/SSO

type Decoder struct {
	SecureCookie *securecookie.SecureCookie
}

func (d *Decoder) Decode(apikey string) (*Key, error) {
	var k Key
	if err := d.SecureCookie.Decode("token", apikey, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// gitLabUserInfo represents the subset of user information that GitLab (as
// running on salsa.debian.org) shares via OpenID Connect, when only the openid
// scope is specified (e.g. the email field is not requested).
//
// See also
// https://docs.gitlab.com/ee/integration/openid_connect_provider.html#shared-information
type gitLabUserInfo struct {
	// The ID of the user, e.g. 1692.
	Sub string `json:"sub"`

	// The user’s full name, e.g. Michael Stapelberg.
	Name string `json:"name"`

	// The user’s GitLab username, e.g. stapelberg.
	//
	// This field is called “username” on contributors.debian.org.
	Nickname string `json:"nickname"`

	// URL for the user’s GitLab profile,
	// e.g. https://salsa.debian.org/stapelberg
	Profile string `json:"profile"`

	// URL for the user’s GitLab avatar, e.g.
	// https://seccdn.libravatar.org/avatar/51517bffba395c1ff6408f167c0293f1?s=80&d=identicon
	Picture string `json:"picture"`

	// Names of the groups the user is a member of, e.g. ["go-team", "i3-team"].
	Groups []string `json:"groups"`
}

type Key struct {
	// Subject is opaque (could be anything)
	// Usually salsa.debian.org!<nickname> for a user-registered one
	// Could also be github!<slug> for a tool
	Subject string `json:"s"`

	CreatedUnixTimestamp int64 `json:"c"`
}

type Options struct {
	HashKey      []byte
	BlockKey     []byte
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Prefix       string
}

func (o *Options) SecureCookie() *securecookie.SecureCookie {
	cookies := securecookie.New(o.HashKey, o.BlockKey)
	cookies.MaxAge(0) // do not restrict max age, our API keys should remain valid
	cookies.SetSerializer(securecookie.JSONEncoder{})
	return cookies
}

type session struct {
	UserInfo *gitLabUserInfo
	APIKey   string
}

func (s *session) GetUserInfo() *gitLabUserInfo {
	if s == nil || s.UserInfo == nil {
		return &gitLabUserInfo{}
	}
	return s.UserInfo
}

func (s *session) GetAPIKey() string {
	if s == nil {
		return ""
	}
	return s.APIKey
}

func (s *session) KeyFilename() string {
	return fmt.Sprintf("dcs-apikey-%s.txt", s.GetUserInfo().Nickname)
}

type server struct {
	opts    Options
	cookies *securecookie.SecureCookie
}

func (s *server) deriveSession(ui *gitLabUserInfo) (*session, error) {
	key := Key{
		Subject:              "salsa.debian.org!" + ui.Nickname,
		CreatedUnixTimestamp: time.Now().Unix(),
	}
	encoded, err := s.cookies.Encode("token", key)
	if err != nil {
		return nil, err
	}
	return &session{
		UserInfo: ui,
		APIKey:   encoded,
	}, nil
}

type state struct {
	LoginRequest int64 `json:"l"` // unix timestamp
}

func ServeOnMux(mux *http.ServeMux, opts Options) error {
	// Used both immediately, and also later (by the oidc.KeySet):
	longLived := context.Background()
	// https://salsa.debian.org/.well-known/openid-configuration
	const issuer = "https://salsa.debian.org"
	provider, err := oidc.NewProvider(longLived, issuer)
	if err != nil {
		return err
	}
	log.Printf("OpenID Connect issuer %s configured", issuer)

	oidcVerifier := provider.Verifier(&oidc.Config{ClientID: opts.ClientID})

	oauth2Config := &oauth2.Config{
		ClientID:     opts.ClientID,
		ClientSecret: opts.ClientSecret,
		RedirectURL:  opts.RedirectURL,
		// It is sufficient for the scope to be the openid scope (which always
		// must be specified). One could also specify email or profile, but we
		// can get enough info about the user from the UserInfo() request.
		Scopes:   []string{oidc.ScopeOpenID},
		Endpoint: provider.Endpoint(),
	}

	cookies := opts.SecureCookie()

	s := server{
		opts:    opts,
		cookies: cookies,
	}

	const cookieName = "dcs-session"
	mux.Handle("GET /", httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		var session session
		if cookie, err := r.Cookie(cookieName); err == nil {
			if err := cookies.Decode("dcs-session", cookie.Value, &session); err != nil {
				log.Printf("could not decode cookie: %v", err)
			}
		}
		return common.Templates.ExecuteTemplate(w, "apikeys.html", map[string]any{
			"criticalcss": common.CriticalCss,
			"version":     version.Read(),
			"host":        r.Host,
			"userinfo":    session.GetUserInfo(),
			"keyfilename": session.KeyFilename(),
			"apikey":      session.GetAPIKey(),
		})
	}))

	mux.Handle("GET /logout", httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		cookie := &http.Cookie{
			Name:     cookieName,
			Value:    "", // clear
			Path:     "/",
			HttpOnly: true,
		}
		cookie.Secure = strings.HasPrefix(opts.RedirectURL, "https://")
		http.SetCookie(w, cookie)
		http.Redirect(w, r, opts.Prefix+"/", http.StatusTemporaryRedirect)
		return nil
	}))

	mux.Handle("GET /login", httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		// To prevent CSRF attacks, state needs to contain a unique and
		// non-guessable value associated with each authentication request.  We
		// use the securecookie package to encrypt and later authenticate the
		// UNIX timestamp of when the request came in.
		state, err := s.cookies.Encode("state", &state{
			LoginRequest: time.Now().Unix(),
		})
		if err != nil {
			return err
		}
		url := oauth2Config.AuthCodeURL(state)
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
		return nil
	}))

	mux.Handle("GET /download/", httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		var session session
		if cookie, err := r.Cookie(cookieName); err == nil {
			if err := cookies.Decode("dcs-session", cookie.Value, &session); err != nil {
				log.Printf("could not decode cookie: %v", err)
			}
		}
		nickname := session.GetUserInfo().Nickname
		if nickname == "" {
			http.Redirect(w, r, opts.Prefix+"/", http.StatusTemporaryRedirect)
			return nil
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+session.KeyFilename()+`"`)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, session.GetAPIKey()+"\n")
		return nil
	}))

	var lastLogin atomic.Int64 // stores time.Now().Unix()
	mux.Handle("GET /redirect_uri", httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()

		var st state
		if err := s.cookies.Decode("state", r.FormValue("state"), &st); err != nil {
			return err
		}
		if time.Since(time.Unix(st.LoginRequest, 0)) > 5*time.Minute {
			return fmt.Errorf("/login state expired, please start over")
		}

		// Rate-limit logins, because each verification is a request to salsa.
		if now := time.Now().Unix(); lastLogin.Swap(now) == now {
			return httperr.Error(http.StatusTooManyRequests, fmt.Errorf("too many concurrent logins, please try again in a few seconds"))
		}
		// Further restrict each login attempt to 10s.
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		code := r.FormValue("code")
		token, err := oauth2Config.Exchange(ctx, code)
		if err != nil {
			// Might fail with a 401 Unauthorized when the user revoked the app
			return err
		}

		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok {
			return fmt.Errorf("can't extract id token from access token")
		}

		if _, err := oidcVerifier.Verify(ctx, rawIDToken); err != nil {
			return err
		}

		userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err != nil {
			return err
		}

		var ui gitLabUserInfo
		if err := userInfo.Claims(&ui); err != nil {
			return err
		}

		session, err := s.deriveSession(&ui)
		if err != nil {
			return err
		}

		encoded, err := cookies.Encode(cookieName, session)
		if err != nil {
			return err
		}
		cookie := &http.Cookie{
			Name:     cookieName,
			Value:    encoded,
			Path:     "/",
			HttpOnly: true,
		}
		cookie.Secure = strings.HasPrefix(opts.RedirectURL, "https://")
		http.SetCookie(w, cookie)

		http.Redirect(w, r, opts.Prefix+"/", http.StatusTemporaryRedirect)
		return nil
	}))

	return nil
}
