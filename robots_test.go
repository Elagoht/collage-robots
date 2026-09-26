package robots_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

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
