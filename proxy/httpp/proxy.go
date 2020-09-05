package httpp

import (
	"github.com/enfabrica/enkit/lib/config/marshal"
	"github.com/enfabrica/enkit/lib/kflags"
	"github.com/enfabrica/enkit/lib/logger"
	"github.com/enfabrica/enkit/lib/oauth"
	"github.com/kataras/muxie"

	"fmt"
	"net/http"
	"net/url"
)

type Flags struct {
	Name   string
	Config []byte
}

func DefaultFlags() *Flags {
	return &Flags{}
}

func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.ByteFileVar(&f.Config, "config", f.Name, "Default config file location.", kflags.WithFilename(&f.Name))
	return f
}

type Proxy struct {
	// Public, as it provides the ServeHTTP method, needed to serve the proxy.
	*muxie.Mux
	// List of domains for which an SSL certificate would be needed.
	Domains []string

	authenticator oauth.Authenticate
	config    *Config
	log       logger.Logger

	cachedir     string
	httpAddress  string
	httpsAddress string
	authURL      *url.URL
}

type AuthenticatedProxy struct {
	Proxy     http.Handler
	Authenticator oauth.Authenticate
	AuthURL   *url.URL
}

func (as *AuthenticatedProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target, err := oauth.CreateRedirectURL(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	creds, err := as.Authenticator(w, r, target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if creds == nil {
		return
	}

	as.Proxy.ServeHTTP(w, r.WithContext(oauth.SetCredentials(r.Context(), creds)))
}

func (p *Proxy) CreateServer(mapping *Mapping) (http.Handler, error) {
	proxy, err := NewProxy(mapping.From.Path, mapping.To, mapping.Transform)
	if err != nil {
		return nil, err
	}
	if mapping.Auth == MappingPublic {
		return proxy, nil
	}
	if p.authenticator == nil {
		return nil, fmt.Errorf("proxy for mapping %v requires authentication - but no authentication configured", *mapping)
	}

	return &AuthenticatedProxy{AuthURL: p.authURL, Proxy: proxy, Authenticator: p.authenticator}, nil
}

type Modifier func(*Proxy) error

type Modifiers []Modifier

func (mods Modifiers) Apply(p *Proxy) error {
	for _, m := range mods {
		if err := m(p); err != nil {
			return err
		}
	}
	return nil
}

func WithConfig(config *Config) Modifier {
	return func(p *Proxy) error {
		p.config = config
		return nil
	}
}

func WithLogging(log logger.Logger) Modifier {
	return func(p *Proxy) error {
		p.log = log
		return nil
	}
}

func WithAuthURL(u *url.URL) Modifier {
	return func(p *Proxy) error {
		p.authURL = u
		return nil
	}
}

func WithAuthenticator(authenticator oauth.Authenticate) Modifier {
	return func(p *Proxy) error {
		p.authenticator = authenticator
		return nil
	}
}

func FromFlags(fl *Flags) Modifier {
	return func(p *Proxy) error {
		var config Config
		if err := marshal.UnmarshalDefault(fl.Name, fl.Config, marshal.Json, &config); err != nil {
			return kflags.NewUsageError(err)
		}

		if len(config.Mapping) <= 0 {
			return kflags.NewUsageError(fmt.Errorf("invalid config: it has no mappings"))
		}

		mods := Modifiers{
			WithConfig(&config),
		}
		return mods.Apply(p)
	}
}

func New(mod ...Modifier) (*Proxy, error) {
	p := &Proxy{
		log: logger.Nil,
	}
	if err := Modifiers(mod).Apply(p); err != nil {
		return nil, err
	}

	p.log.Infof("Config is: %v", *p.config)
	mux, domains, err := BuildMux(nil, p.log, p.config.Mapping, p.CreateServer)
	if err != nil {
		return nil, err
	}

	p.Mux = mux
	p.Domains = append(domains, p.config.Domains...)

	return p, nil
}