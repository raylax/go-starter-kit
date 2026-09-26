// Package federation 校验受信第三方的 OAuth 身份。
package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/oauth2"
)

var ErrProof = errors.New("第三方证明无效")
var ErrUnavailable = errors.New("第三方服务不可用")

// Protocol 是当前支持的第三方身份协议；提供商 ID 仍由配置扩展。
type Protocol string

const (
	ProtocolGitHub Protocol = "github"
)

func (p Protocol) Valid() bool { return p == ProtocolGitHub }

const (
	maxAuthorizationCodeBytes = 8 << 10 // 8 KiB
	maxUserResponseBytes      = 1 << 20 // 1 MiB
	githubIdentityNamespace   = "https://api.github.com"
	githubUserEndpoint        = githubIdentityNamespace + "/user"
)

type Config struct {
	ID               string   `json:"id"`
	Protocol         Protocol `json:"protocol"`
	ClientID         string   `json:"client_id"`
	ClientSecret     string   `json:"-"`
	ClientSecretFile string   `json:"client_secret_file"`
	RedirectURI      string   `json:"redirect_uri"`
}
type Verified struct {
	Namespace, Subject, Name string
}

type githubUserResponse struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type provider struct {
	config  Config
	version string
}
type Registry struct {
	providers map[string]*provider
	client    *http.Client
}

func New(configs []Config) (*Registry, error) {
	r := &Registry{providers: map[string]*provider{}, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("第三方端点不允许重定向") }}}
	validID := regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	for _, c := range configs {
		if !validID.MatchString(c.ID) || c.ID == "credential" || c.ID == "callback" || r.providers[c.ID] != nil || c.ClientID == "" || c.ClientSecret == "" || !validURL(c.RedirectURI) {
			return nil, errors.New("提供商配置无效")
		}
		if !c.Protocol.Valid() {
			return nil, errors.New("提供商协议无效")
		}
		data, _ := json.Marshal(c)
		sum := sha256.Sum256(append(data, []byte(c.ClientSecret)...))
		r.providers[c.ID] = &provider{config: c, version: hex.EncodeToString(sum[:])}
	}
	return r, nil
}
func validURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Host != "" && u.User == nil && u.Fragment == "" && u.Scheme == "https"
}
func (r *Registry) Enabled(id string) bool { return r.providers[id] != nil }
func (r *Registry) Version(id string) string {
	if p := r.providers[id]; p != nil {
		return p.version
	}
	return ""
}
func (r *Registry) context(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, r.client)
}
func (r *Registry) configuration(id string) (*provider, oauth2.Config, error) {
	p := r.providers[id]
	if p == nil {
		return nil, oauth2.Config{}, ErrProof
	}
	c := oauth2.Config{ClientID: p.config.ClientID, ClientSecret: p.config.ClientSecret, RedirectURL: p.config.RedirectURI,
		Endpoint: oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token"}, Scopes: []string{"read:user"}}
	return p, c, nil
}
func (r *Registry) Start(ctx context.Context, id, state, verifier string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	p, c, e := r.configuration(id)
	if e != nil {
		return "", "", e
	}
	return c.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), p.version, nil
}
func (r *Registry) Verify(ctx context.Context, id, version, code, verifier string) (Verified, error) {
	p, c, e := r.configuration(id)
	if e != nil {
		return Verified{}, e
	}
	if p.version != version || code == "" || len(code) > maxAuthorizationCodeBytes {
		return Verified{}, ErrProof
	}
	token, e := c.Exchange(r.context(ctx), code, oauth2.VerifierOption(verifier))
	if e != nil {
		return Verified{}, exchangeError(ctx, e)
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, githubUserEndpoint, nil)
	if e != nil {
		return Verified{}, ErrProof
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, e := r.client.Do(req)
	if e != nil {
		return Verified{}, dependencyError(ctx, e)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return Verified{}, ErrProof
	}
	if res.StatusCode != http.StatusOK {
		return Verified{}, ErrUnavailable
	}
	var user githubUserResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, maxUserResponseBytes)).Decode(&user); err != nil {
		return Verified{}, dependencyError(ctx, err)
	}
	if user.ID <= 0 {
		return Verified{}, ErrUnavailable
	}
	return Verified{Namespace: githubIdentityNamespace, Subject: strconv.FormatInt(user.ID, 10), Name: user.Login}, nil
}

// unavailableCause 保留底层链用于分类，日志文本始终使用固定脱敏提示。
type unavailableCause struct{ cause error }

func (e *unavailableCause) Error() string   { return ErrUnavailable.Error() }
func (e *unavailableCause) Unwrap() []error { return []error{ErrUnavailable, e.cause} }
func dependencyError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &unavailableCause{cause: err}
}
func exchangeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var rejected *oauth2.RetrieveError
	if errors.As(err, &rejected) {
		if rejected.Response != nil && (rejected.Response.StatusCode >= http.StatusInternalServerError || rejected.Response.StatusCode == http.StatusTooManyRequests) {
			return dependencyError(ctx, err)
		}
		switch rejected.ErrorCode {
		case "invalid_grant", "bad_verification_code", "access_denied":
			return ErrProof
		}
	}
	return dependencyError(ctx, err)
}
