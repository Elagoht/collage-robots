// Package robots is a collage plugin that serves /robots.txt.
//
//	app, err := collage.New(&collage.Config{
//		Plugins: []collage.Plugin{robots.New(robots.Options{
//			Sitemaps: []string{"https://example.com/sitemap.xml"},
//		})},
//	})
//
// With no rules it allows everything. A staging or preview deployment sets
// DisallowAll — from its own plugin configuration, so the same binary is open in
// production and closed elsewhere — and every page is then also served with
// X-Robots-Tag: noindex, for the crawlers that index what robots.txt only asks
// them not to fetch.
package robots

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Elagoht/collage/pkg/collage"
)

// Name is the plugin's name, and the key its configuration is found under.
const Name = "elagoht/robots"

// Options configures the plugin.
type Options struct {
	// Rules are the groups of robots.txt, in order. Empty allows every crawler
	// everything.
	Rules []Rule `json:"rules"`
	// Sitemaps are absolute URLs, or paths made absolute against the request's
	// origin.
	Sitemaps []string `json:"sitemaps"`
	// DisallowAll closes the site to every crawler, whatever Rules say, and
	// serves every response with X-Robots-Tag: noindex, nofollow. For a staging
	// or preview deployment.
	DisallowAll bool `json:"disallowAll"`
}

// Rule is one group: the user agents it is for, and the paths they may and may
// not fetch.
type Rule struct {
	// UserAgents the group applies to; empty is "*".
	UserAgents []string `json:"userAgents"`
	Allow      []string `json:"allow"`
	Disallow   []string `json:"disallow"`
	// CrawlDelay asks for this many seconds between requests; zero leaves it out.
	// Not every crawler honours it.
	CrawlDelay int `json:"crawlDelay"`
}

// ErrNoBaseURL is returned by Init for a sitemap given as a path when collage
// can name no origin to make it absolute with.
var ErrNoBaseURL = errors.New("robots: a sitemap given as a path needs Config.BaseURL, or a plugin resolving each host's origin")

// Plugin serves robots.txt.
type Plugin struct{ opts Options }

// New returns a plugin with opts as its starting point, which the application's
// own configuration is then decoded over.
func New(opts Options) *Plugin { return &Plugin{opts: opts} }

func (p *Plugin) Name() string                   { return Name }
func (p *Plugin) Version() string                { return "0.2.1" }
func (p *Plugin) Shutdown(context.Context) error { return nil }

// Init reads the configuration and registers /robots.txt, and the header when the
// site is closed.
func (p *Plugin) Init(_ context.Context, host collage.Host) error {
	cfg, err := collage.PluginConfig(host, p.opts)
	if err != nil {
		return err
	}
	p.opts = cfg
	for _, rule := range p.opts.Rules {
		for _, path := range append(append([]string(nil), rule.Allow...), rule.Disallow...) {
			if path != "" && !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "*") {
				return fmt.Errorf("robots: path %q must begin with / or *", path)
			}
		}
	}
	relative := false
	for _, s := range p.opts.Sitemaps {
		switch u, err := url.Parse(s); {
		case strings.HasPrefix(s, "//"):
			// Scheme-relative: another host's URL, not a path on this one.
			return fmt.Errorf("robots: sitemap %q must be an absolute URL or a path", s)
		case strings.HasPrefix(s, "/"):
			relative = true
		case err != nil || !u.IsAbs():
			return fmt.Errorf("robots: sitemap %q must be an absolute URL or a path", s)
		}
	}
	if relative && !canResolve(host) {
		return ErrNoBaseURL
	}
	b := collage.NewDocument(Name, "text/plain; charset=utf-8").AtRoot("/robots.txt")
	if relative {
		b = b.WithHandler(func(_ context.Context, rc *collage.RenderContext) ([]byte, []string, error) {
			origin := collage.BaseURL(rc)
			if origin == "" {
				return nil, nil, ErrNoBaseURL
			}
			return p.render(origin), nil, nil
		}).Static()
	} else {
		b = b.WithBody(p.Render())
	}
	doc := b.Build()
	if err := host.RegisterDocument(doc); err != nil {
		return fmt.Errorf("robots: %w", err)
	}
	if p.opts.DisallowAll {
		return host.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Robots-Tag", "noindex, nofollow")
				next.ServeHTTP(w, r)
			})
		})
	}
	return nil
}

// Render returns robots.txt as the plugin serves it.
func (p *Plugin) Render() []byte { return p.render("") }

// render is Render with sitemap paths made absolute against origin.
func (p *Plugin) render(origin string) []byte {
	var b bytes.Buffer
	rules := p.opts.Rules
	if p.opts.DisallowAll {
		rules = []Rule{{Disallow: []string{"/"}}}
	} else if len(rules) == 0 {
		rules = []Rule{{Allow: []string{"/"}}}
	}
	for i, rule := range rules {
		if i > 0 {
			b.WriteByte('\n')
		}
		agents := rule.UserAgents
		if len(agents) == 0 {
			agents = []string{"*"}
		}
		for _, agent := range agents {
			fmt.Fprintf(&b, "User-agent: %s\n", clean(agent))
		}
		for _, path := range rule.Allow {
			fmt.Fprintf(&b, "Allow: %s\n", clean(path))
		}
		for _, path := range rule.Disallow {
			fmt.Fprintf(&b, "Disallow: %s\n", clean(path))
		}
		if len(rule.Allow) == 0 && len(rule.Disallow) == 0 {
			// A group with no line allows everything, but some parsers read a
			// bare group as malformed; an empty Disallow says it outright.
			b.WriteString("Disallow:\n")
		}
		if rule.CrawlDelay > 0 {
			fmt.Fprintf(&b, "Crawl-delay: %d\n", rule.CrawlDelay)
		}
	}
	if len(p.opts.Sitemaps) > 0 && !p.opts.DisallowAll {
		b.WriteByte('\n')
		for _, sitemap := range p.opts.Sitemaps {
			if strings.HasPrefix(sitemap, "/") {
				sitemap = origin + sitemap
			}
			fmt.Fprintf(&b, "Sitemap: %s\n", clean(sitemap))
		}
	}
	return b.Bytes()
}

// clean keeps a value on its own line: a newline inside one would start a
// directive nobody wrote.
func clean(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(s))
}

// canResolve reports whether collage can name an origin without the plugin's
// own BaseURL: from Config.BaseURL, or per host from a plugin implementing
// collage.OriginResolver.
func canResolve(host collage.Host) bool {
	if host.BaseURL() != "" {
		return true
	}
	origins, ok := host.(collage.Origins)
	return ok && origins.Dynamic()
}
