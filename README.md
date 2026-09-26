# elagoht/robots

A collage plugin that serves `/robots.txt`.

```go
app, err := collage.New(&collage.Config{
	Plugins: []collage.Plugin{robots.New(robots.Options{
		Sitemaps: []string{"https://example.com/sitemap.xml"},
	})},
})
```

Requires collage v0.21.0 or later.

With no rules it allows every crawler everything:

```
User-agent: *
Allow: /

Sitemap: https://example.com/sitemap.xml
```

## Rules

```go
robots.New(robots.Options{
	Rules: []robots.Rule{
		{Disallow: []string{"/admin", "/search"}},
		{UserAgents: []string{"GPTBot", "CCBot"}, Disallow: []string{"/"}},
	},
	Sitemaps: []string{"https://example.com/sitemap.xml"},
})
```

A rule with no user agents is for `*`. `CrawlDelay` asks for seconds between
requests, which not every crawler honours. A path must begin with `/` or `*`, and a
value cannot break onto a line of its own.

## Staging and previews

`disallowAll` closes the site to every crawler, whatever the rules say. Set it in
the plugin configuration of the deployments that should not be indexed, so one
binary is open in production and closed elsewhere:

```json
{ "elagoht/robots": { "disallowAll": true } }
```

A closed site also serves every response with `X-Robots-Tag: noindex, nofollow`.
`robots.txt` only asks a crawler not to fetch; a page linked from elsewhere can
still be indexed without being fetched, and the header is what keeps it out.

## Configuration

```json
{
  "elagoht/robots": {
    "rules": [{ "userAgents": ["*"], "disallow": ["/admin"] }],
    "sitemaps": ["https://example.com/sitemap.xml"],
    "disallowAll": false
  }
}
```

The body is fixed when the application starts, and a static build writes it to
`robots.txt`.
