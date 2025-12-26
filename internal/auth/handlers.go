package auth

import (
	"crypto/rand"
	"net/http"
	"net/url"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
)

var (
	Store *sessions.CookieStore
)

func InitStore() {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		panic(err)
	}
	Store = sessions.NewCookieStore(key)
}

// Handler holds the dependencies for the auth handlers.
type Handler struct {
	Authenticator *Authenticator
}

// NewHandler creates a new Handler.
func NewHandler(auth *Authenticator) *Handler {
	return &Handler{
		Authenticator: auth,
	}
}

// LoginHandler handles the login redirect.
func (h *Handler) LoginHandler(c *gin.Context) {
	state, err := GenerateRandomState()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to generate state: "+err.Error())
		return
	}

	// Generate PKCE verifier and challenge
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to generate code verifier: "+err.Error())
		return
	}
	challenge := GenerateCodeChallenge(verifier)

	// Store state and verifier in session
	session, _ := Store.Get(c.Request, "auth-session")
	session.Values["state"] = state
	session.Values["code_verifier"] = verifier
	if err := session.Save(c.Request, c.Writer); err != nil {
		c.String(http.StatusInternalServerError, "Failed to save session: "+err.Error())
		return
	}

	// Redirect to Auth0
	url := h.Authenticator.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// CallbackHandler handles the callback from Auth0.
func (h *Handler) CallbackHandler(c *gin.Context) {
	session, _ := Store.Get(c.Request, "auth-session")

	// Validate state
	expectedState, ok := session.Values["state"].(string)
	if !ok || c.Query("state") != expectedState {
		c.String(http.StatusBadRequest, "Invalid state parameter")
		return
	}

	// Retrieve code verifier
	verifier, ok := session.Values["code_verifier"].(string)
	if !ok {
		c.String(http.StatusBadRequest, "Code verifier not found in session")
		return
	}

	// Exchange code for token
	token, err := h.Authenticator.Exchange(
		c.Request.Context(),
		c.Query("code"),
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		c.String(http.StatusUnauthorized, "Failed to exchange an authorization code for a token: "+err.Error())
		return
	}

	idToken, ok := token.Extra("id_token").(string)
	if !ok {
		c.String(http.StatusInternalServerError, "Failed to generate ID token")
		return
	}

	// Store ID token in session (or use access token depending on needs)
	session.Values["id_token"] = idToken
	session.Values["access_token"] = token.AccessToken
	if err := session.Save(c.Request, c.Writer); err != nil {
		c.String(http.StatusInternalServerError, "Failed to save session: "+err.Error())
		return
	}

	c.Redirect(http.StatusTemporaryRedirect, "/user")
}

// LogoutHandler handles the logout.
func (h *Handler) LogoutHandler(c *gin.Context) {
	session, _ := Store.Get(c.Request, "auth-session")
	session.Options.MaxAge = -1
	session.Save(c.Request, c.Writer)

	logoutUrl, err := url.Parse("https://" + os.Getenv("AUTH0_DOMAIN") + "/v2/logout")
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	parameters := url.Values{}
	parameters.Add("returnTo", os.Getenv("AUTH0_CALLBACK_URL")) // Or a specific return URL
	parameters.Add("client_id", os.Getenv("AUTH0_CLIENT_ID"))
	logoutUrl.RawQuery = parameters.Encode()

	c.Redirect(http.StatusTemporaryRedirect, logoutUrl.String())
}
