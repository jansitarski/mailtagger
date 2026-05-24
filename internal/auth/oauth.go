package auth

import (
	"golang.org/x/oauth2"
)

// Gmail OAuth 2.0 scopes
const (
	// ScopeGmailModify allows read/write access to mailbox, including sending.
	// Required for applying labels to messages.
	ScopeGmailModify = "https://www.googleapis.com/auth/gmail.modify"

	// ScopeUserInfoEmail allows reading the user's email address.
	ScopeUserInfoEmail = "https://www.googleapis.com/auth/userinfo.email"
)

// OAuthConfig creates an oauth2.Config from ClientSecret credentials.
// The redirect URI is typically "http://localhost" with a dynamic port,
// or "urn:ietf:wg:oauth:2.0:oob" for manual copy-paste flow.
func (cs *ClientSecret) OAuthConfig(redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cs.ClientID,
		ClientSecret: cs.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cs.AuthURI,
			TokenURL: cs.TokenURI,
		},
		RedirectURL: redirectURI,
		Scopes:      []string{ScopeGmailModify, ScopeUserInfoEmail},
	}
}

// AuthCodeURL generates the OAuth authorization URL that the user should visit.
// It uses access_type=offline to get a refresh token and prompt=consent to
// always show the consent screen (ensuring we get a refresh token even if
// the user previously granted access).
func (cs *ClientSecret) AuthCodeURL(redirectURI, state string) string {
	cfg := cs.OAuthConfig(redirectURI)
	return cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
}
