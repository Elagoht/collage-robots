package robots_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	robots "github.com/Elagoht/collage-robots"
	"github.com/Elagoht/collage/pkg/collage"
)

func serve(t *testing.T, p *robots.Plugin, path string) *httptest.ResponseRecorder {
	t.Helper()
	app, err := collage.New(&collage.Config{
		Server:   collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{"t/p.html": {Data: []byte(`<p>x</p>`)}}, Root: "t"},
		Plugins:  []collage.Plugin{p},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterPage(collage.NewPage("home").WithContent(collage.NewFragment("home", "p.html").Build()).WithPath("en", "/").Build()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestDefaultAllowsEverything(t *testing.T) {
	rec := serve(t, robots.New(robots.Options{}), "/robots.txt")
	if rec.Code != http.StatusOK || rec.Body.String() != "User-agent: *\nAllow: /\n" {
		t.Errorf("robots.txt = %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestRulesAndSitemaps(t *testing.T) {
	rec := serve(t, robots.New(robots.Options{
		Rules: []robots.Rule{
			{Disallow: []string{"/admin", "/search"}},
			{UserAgents: []string{"GPTBot", "CCBot"}, Disallow: []string{"/"}, CrawlDelay: 10},
		},
		Sitemaps: []string{"https://example.com/sitemap.xml"},
	}), "/robots.txt")
	want := "User-agent: *\nDisallow: /admin\nDisallow: /search\n\n" +
		"User-agent: GPTBot\nUser-agent: CCBot\nDisallow: /\nCrawl-delay: 10\n\n" +
		"Sitemap: https://example.com/sitemap.xml\n"
	if rec.Body.String() != want {
		t.Errorf("robots.txt =\n%s\nwant\n%s", rec.Body.String(), want)
	}
}

// A closed site says so to crawlers twice: in robots.txt, and on every response,
// for the crawlers that index what they were only asked not to fetch.
func TestDisallowAll(t *testing.T) {
	p := robots.New(robots.Options{DisallowAll: true, Sitemaps: []string{"https://example.com/sitemap.xml"}, Rules: []robots.Rule{{Allow: []string{"/"}}}})
	rec := serve(t, p, "/robots.txt")
	if rec.Body.String() != "User-agent: *\nDisallow: /\n" {
		t.Errorf("robots.txt = %q", rec.Body.String())
	}
	if got := serve(t, p, "/").Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Errorf("X-Robots-Tag = %q", got)
	}
	if got := serve(t, robots.New(robots.Options{}), "/").Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("an open site sends X-Robots-Tag %q", got)
	}
}

// A newline in a value cannot start a directive of its own.
func TestValuesStayOnTheirLine(t *testing.T) {
	rec := serve(t, robots.New(robots.Options{Rules: []robots.Rule{{Disallow: []string{"/a\nAllow: /secret"}}}}), "/robots.txt")
	if rec.Body.String() != "User-agent: *\nDisallow: /aAllow: /secret\n" {
		t.Errorf("robots.txt = %q", rec.Body.String())
	}
}

// origins resolves two hosts, as elagoht/tenant would.
type origins struct{}

func (origins) Name() string                             { return "test/origins" }
func (origins) Version() string                          { return "0" }
func (origins) Init(context.Context, collage.Host) error { return nil }
func (origins) Shutdown(context.Context) error           { return nil }
func (origins) Origin(_ context.Context, host string) (string, bool) {
	switch host {
	case "a.test":
		return "https://a.example", true
	case "b.test":
		return "https://b.example", true
	}
	return "", false
}

// A sitemap given as a path is made absolute against the request's origin.
func TestRelativeSitemapFollowsHost(t *testing.T) {
	app, err := collage.New(&collage.Config{
		Server:   collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{"t/p.html": {Data: []byte(`x`)}}, Root: "t"},
		Cache:    collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour},
		Plugins:  []collage.Plugin{origins{}, robots.New(robots.Options{Sitemaps: []string{"/sitemap.xml", "https://cdn.example/s.xml"}})},
	})
	if err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]string{"a.test": "Sitemap: https://a.example/sitemap.xml\n", "b.test": "Sitemap: https://b.example/sitemap.xml\n"} {
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/robots.txt", nil))
		body := rec.Body.String()
		if !strings.Contains(body, want) || !strings.Contains(body, "Sitemap: https://cdn.example/s.xml\n") {
			t.Errorf("%s robots.txt = %q, want %q and the absolute one", host, body, want)
		}
	}
}

// A relative sitemap with no way to make it absolute refuses to start; one that is
// neither absolute nor a path is refused too. Init runs on Start.
func TestSitemapValidation(t *testing.T) {
	for _, tc := range []struct {
		sitemap string
		noBase  bool
		base    string
	}{
		{"/sitemap.xml", true, ""},
		{"sitemap.xml", false, ""},
		// A scheme-relative URL is another host's, not a path on this one.
		{"//evil.com/sitemap.xml", false, "https://example.com"},
	} {
		app, err := collage.New(&collage.Config{
			BaseURL:  tc.base,
			Server:   collage.ServerConfig{Host: "localhost", Port: 3000},
			Template: collage.TemplateConfig{FS: fstest.MapFS{"t/p.html": {Data: []byte(`x`)}}, Root: "t"},
			Plugins:  []collage.Plugin{robots.New(robots.Options{Sitemaps: []string{tc.sitemap}})},
		})
		if err != nil {
			t.Fatal(err)
		}
		err = app.Start()
		if err == nil {
			t.Errorf("Sitemaps %q started, want an error", tc.sitemap)
			continue
		}
		if tc.noBase && !errors.Is(err, robots.ErrNoBaseURL) {
			t.Errorf("Sitemaps %q: err = %v, want ErrNoBaseURL", tc.sitemap, err)
		}
		if !tc.noBase && !strings.Contains(err.Error(), "must be an absolute URL or a path") {
			t.Errorf("Sitemaps %q: err = %v, want \"must be an absolute URL or a path\"", tc.sitemap, err)
		}
	}
}
