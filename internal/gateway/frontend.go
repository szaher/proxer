package gateway

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
)

//go:embed static static/*
var embeddedStaticFS embed.FS

const (
	seoMarkerStart = "<!-- PROXER_SEO_START -->"
	seoMarkerEnd   = "<!-- PROXER_SEO_END -->"
)

type seoDocument struct {
	Title              string
	Description        string
	OpenGraphTitle     string
	OpenGraphDesc      string
	OpenGraphImage     string
	TwitterTitle       string
	TwitterDescription string
	TwitterImage       string
	TwitterImageAlt    string
	CanonicalURL       string
	Robots             string
	StructuredDataJSON []string
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	requestPath := path.Clean(strings.TrimSpace(r.URL.Path))
	if requestPath == "." {
		requestPath = "/"
	}
	if strings.HasPrefix(requestPath, "/api/") || strings.HasPrefix(requestPath, "/t/") {
		http.NotFound(w, r)
		return
	}

	switch requestPath {
	case "/robots.txt":
		s.serveRobotsTxt(w, r)
		return
	case "/sitemap.xml":
		s.serveSitemapXML(w, r)
		return
	}

	frontendFS, err := fs.Sub(embeddedStaticFS, "static")
	if err != nil {
		http.Error(w, "frontend not available", http.StatusInternalServerError)
		return
	}

	if requestPath == "/" {
		s.serveEmbeddedSPAIndex(w, r, frontendFS, requestPath)
		return
	}

	clean := strings.TrimPrefix(requestPath, "/")
	if hasEmbeddedFile(frontendFS, clean) {
		serveEmbeddedFile(w, r, frontendFS, clean)
		return
	}

	// SPA fallback.
	s.serveEmbeddedSPAIndex(w, r, frontendFS, requestPath)
}

func hasEmbeddedFile(fsys fs.FS, filename string) bool {
	info, err := fs.Stat(fsys, filename)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func (s *Server) serveEmbeddedSPAIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS, requestPath string) {
	content, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	baseURL := resolvePublicBaseURL(s.cfg.PublicBaseURL, r)
	seo := buildSEODocument(requestPath, baseURL)
	rendered := injectSEOBlock(string(content), buildSEOBlock(seo))

	contentType := "text/html; charset=utf-8"
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	writeBodyWithOptionalGzip(w, r, []byte(rendered), contentType)
}

func serveEmbeddedFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, filename string) {
	content, err := fs.ReadFile(fsys, filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := "application/octet-stream"
	switch {
	case strings.HasSuffix(filename, ".html"):
		contentType = "text/html; charset=utf-8"
	case strings.HasSuffix(filename, ".css"):
		contentType = "text/css; charset=utf-8"
	case strings.HasSuffix(filename, ".js"):
		contentType = "application/javascript; charset=utf-8"
	case strings.HasSuffix(filename, ".json"):
		contentType = "application/json; charset=utf-8"
	case strings.HasSuffix(filename, ".svg"):
		contentType = "image/svg+xml; charset=utf-8"
	case strings.HasSuffix(filename, ".xml"):
		contentType = "application/xml; charset=utf-8"
	case strings.HasSuffix(filename, ".txt"):
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	setStaticCacheControlHeader(w, filename)
	writeBodyWithOptionalGzip(w, r, content, contentType)
}

func (s *Server) serveRobotsTxt(w http.ResponseWriter, r *http.Request) {
	baseURL := resolvePublicBaseURL(s.cfg.PublicBaseURL, r)
	contentType := "text/plain; charset=utf-8"
	body := []byte(fmt.Sprintf(
		"User-agent: *\nAllow: /\nDisallow: /api/\nDisallow: /app\nDisallow: /login\nSitemap: %s\n",
		canonicalURL(baseURL, "/sitemap.xml"),
	))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeBodyWithOptionalGzip(w, r, body, contentType)
}

func (s *Server) serveSitemapXML(w http.ResponseWriter, r *http.Request) {
	baseURL := resolvePublicBaseURL(s.cfg.PublicBaseURL, r)
	urls := []sitemapURL{
		{Loc: canonicalURL(baseURL, "/")},
		{Loc: canonicalURL(baseURL, "/signup")},
	}

	payload, err := xml.MarshalIndent(sitemapURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}, "", "  ")
	if err != nil {
		http.Error(w, "failed to render sitemap", http.StatusInternalServerError)
		return
	}

	contentType := "application/xml; charset=utf-8"
	body := []byte(xml.Header + string(payload))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeBodyWithOptionalGzip(w, r, body, contentType)
}

func setStaticCacheControlHeader(w http.ResponseWriter, filename string) {
	filename = strings.TrimSpace(filename)
	switch {
	case strings.HasPrefix(filename, "assets/"), strings.HasPrefix(filename, "images/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case strings.HasSuffix(filename, ".html"):
		w.Header().Set("Cache-Control", "no-cache")
	case strings.HasSuffix(filename, ".json"), strings.HasSuffix(filename, ".xml"), strings.HasSuffix(filename, ".txt"):
		w.Header().Set("Cache-Control", "public, max-age=300")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

func writeBodyWithOptionalGzip(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	if !clientAcceptsGzip(r) || !isCompressibleContentType(contentType) || len(body) < 1024 {
		w.Header().Add("Vary", "Accept-Encoding")
		_, _ = w.Write(body)
		return
	}

	var compressed bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if err != nil {
		w.Header().Add("Vary", "Accept-Encoding")
		_, _ = w.Write(body)
		return
	}
	if _, err := gzipWriter.Write(body); err != nil {
		_ = gzipWriter.Close()
		w.Header().Add("Vary", "Accept-Encoding")
		_, _ = w.Write(body)
		return
	}
	if err := gzipWriter.Close(); err != nil {
		w.Header().Add("Vary", "Accept-Encoding")
		_, _ = w.Write(body)
		return
	}

	if compressed.Len() >= len(body) {
		w.Header().Add("Vary", "Accept-Encoding")
		_, _ = w.Write(body)
		return
	}

	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	_, _ = w.Write(compressed.Bytes())
}

func clientAcceptsGzip(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Encoding")))
	return strings.Contains(accept, "gzip")
}

func isCompressibleContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "text/"):
		return true
	case strings.Contains(contentType, "javascript"):
		return true
	case strings.Contains(contentType, "json"):
		return true
	case strings.Contains(contentType, "xml"):
		return true
	case strings.Contains(contentType, "svg"):
		return true
	default:
		return false
	}
}

func resolvePublicBaseURL(configBaseURL string, r *http.Request) string {
	candidate := strings.TrimSpace(configBaseURL)
	if candidate == "" {
		candidate = inferRequestBaseURL(r)
	}
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}

	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Host == "" {
		return inferRequestBaseURL(r)
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func inferRequestBaseURL(r *http.Request) string {
	scheme := "http"
	if proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func canonicalURL(baseURL, routePath string) string {
	cleanPath := path.Clean("/" + strings.TrimSpace(routePath))
	if cleanPath == "." {
		cleanPath = "/"
	}

	base, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		if cleanPath == "/" {
			return strings.TrimRight(baseURL, "/") + "/"
		}
		return strings.TrimRight(baseURL, "/") + cleanPath
	}

	if cleanPath == "/" {
		ref := &url.URL{Path: ""}
		return base.ResolveReference(ref).String()
	}
	ref := &url.URL{Path: strings.TrimPrefix(cleanPath, "/")}
	return base.ResolveReference(ref).String()
}

func buildSEODocument(requestPath, baseURL string) seoDocument {
	cleanPath := path.Clean("/" + strings.TrimSpace(requestPath))
	if cleanPath == "." {
		cleanPath = "/"
	}

	defaultImage := canonicalURL(baseURL, "/images/og-default.svg")
	baseDoc := seoDocument{
		Title:              "Proxer | Localhost Tunnels with SaaS Governance",
		Description:        "Proxer is an ngrok-style routing platform with connector pairing, tenant isolation, plan enforcement, TLS management, and super-admin observability.",
		OpenGraphTitle:     "Proxer | Localhost Tunnels with SaaS Governance",
		OpenGraphDesc:      "Route traffic to localhost apps with connector-based forwarding, RBAC, rate limits, and tenant-scoped controls.",
		OpenGraphImage:     defaultImage,
		TwitterTitle:       "Proxer | Localhost Tunnels with SaaS Governance",
		TwitterDescription: "Expose local apps publicly with enterprise controls, plan enforcement, and full request fidelity.",
		TwitterImage:       defaultImage,
		TwitterImageAlt:    "Proxer local tunnel platform overview",
		CanonicalURL:       canonicalURL(baseURL, cleanPath),
		Robots:             "index, follow",
	}

	baseDoc.StructuredDataJSON = buildStructuredData(cleanPath, baseURL)

	switch {
	case cleanPath == "/":
		return baseDoc
	case cleanPath == "/signup":
		baseDoc.Title = "Sign up for Proxer | Start Routing Localhost Securely"
		baseDoc.OpenGraphTitle = baseDoc.Title
		baseDoc.TwitterTitle = baseDoc.Title
		baseDoc.Description = "Create your Proxer workspace in minutes, pair a connector to your machine, and publish localhost apps with traffic controls and tenant isolation."
		baseDoc.OpenGraphDesc = "Sign up for Proxer and expose localhost apps through secure, plan-aware routing."
		baseDoc.TwitterDescription = "Create a workspace, pair your connector, and ship localhost routes with governance built in."
		baseDoc.OpenGraphImage = canonicalURL(baseURL, "/images/og-signup.svg")
		baseDoc.TwitterImage = canonicalURL(baseURL, "/images/og-signup.svg")
		baseDoc.TwitterImageAlt = "Proxer signup page preview"
		return baseDoc
	case cleanPath == "/login":
		baseDoc.Title = "Log in | Proxer Console"
		baseDoc.OpenGraphTitle = baseDoc.Title
		baseDoc.TwitterTitle = baseDoc.Title
		baseDoc.Description = "Access the Proxer console to manage routes, connectors, and traffic policies for your tenant environment."
		baseDoc.OpenGraphDesc = baseDoc.Description
		baseDoc.TwitterDescription = baseDoc.Description
		baseDoc.OpenGraphImage = canonicalURL(baseURL, "/images/og-console.svg")
		baseDoc.TwitterImage = canonicalURL(baseURL, "/images/og-console.svg")
		baseDoc.TwitterImageAlt = "Proxer console preview"
		baseDoc.Robots = "noindex, nofollow"
		baseDoc.StructuredDataJSON = nil
		return baseDoc
	case cleanPath == "/app" || strings.HasPrefix(cleanPath, "/app/"):
		baseDoc.Title = "Proxer Console"
		baseDoc.OpenGraphTitle = baseDoc.Title
		baseDoc.TwitterTitle = baseDoc.Title
		baseDoc.Description = "Authenticated Proxer workspace for route, connector, and plan operations."
		baseDoc.OpenGraphDesc = baseDoc.Description
		baseDoc.TwitterDescription = baseDoc.Description
		baseDoc.CanonicalURL = canonicalURL(baseURL, "/app")
		baseDoc.OpenGraphImage = canonicalURL(baseURL, "/images/og-console.svg")
		baseDoc.TwitterImage = canonicalURL(baseURL, "/images/og-console.svg")
		baseDoc.TwitterImageAlt = "Proxer console preview"
		baseDoc.Robots = "noindex, nofollow"
		baseDoc.StructuredDataJSON = nil
		return baseDoc
	default:
		baseDoc.Robots = "noindex, nofollow"
		baseDoc.StructuredDataJSON = nil
		return baseDoc
	}
}

func buildSEOBlock(doc seoDocument) string {
	var builder strings.Builder
	builder.WriteString(seoMarkerStart)
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("    <title>%s</title>\n", html.EscapeString(doc.Title)))
	builder.WriteString(fmt.Sprintf("    <meta name=\"description\" content=\"%s\" />\n", html.EscapeString(doc.Description)))
	builder.WriteString(fmt.Sprintf("    <meta name=\"robots\" content=\"%s\" />\n", html.EscapeString(doc.Robots)))
	builder.WriteString("    <meta property=\"og:type\" content=\"website\" />\n")
	builder.WriteString(fmt.Sprintf("    <meta property=\"og:title\" content=\"%s\" />\n", html.EscapeString(doc.OpenGraphTitle)))
	builder.WriteString(fmt.Sprintf("    <meta property=\"og:description\" content=\"%s\" />\n", html.EscapeString(doc.OpenGraphDesc)))
	builder.WriteString(fmt.Sprintf("    <meta property=\"og:url\" content=\"%s\" />\n", html.EscapeString(doc.CanonicalURL)))
	builder.WriteString(fmt.Sprintf("    <meta property=\"og:image\" content=\"%s\" />\n", html.EscapeString(doc.OpenGraphImage)))
	builder.WriteString("    <meta property=\"og:image:width\" content=\"1200\" />\n")
	builder.WriteString("    <meta property=\"og:image:height\" content=\"630\" />\n")
	builder.WriteString("    <meta name=\"twitter:card\" content=\"summary_large_image\" />\n")
	builder.WriteString(fmt.Sprintf("    <meta name=\"twitter:title\" content=\"%s\" />\n", html.EscapeString(doc.TwitterTitle)))
	builder.WriteString(fmt.Sprintf("    <meta name=\"twitter:description\" content=\"%s\" />\n", html.EscapeString(doc.TwitterDescription)))
	builder.WriteString(fmt.Sprintf("    <meta name=\"twitter:image\" content=\"%s\" />\n", html.EscapeString(doc.TwitterImage)))
	builder.WriteString(fmt.Sprintf("    <meta name=\"twitter:image:alt\" content=\"%s\" />\n", html.EscapeString(doc.TwitterImageAlt)))
	builder.WriteString(fmt.Sprintf("    <link rel=\"canonical\" href=\"%s\" />\n", html.EscapeString(doc.CanonicalURL)))
	for _, script := range doc.StructuredDataJSON {
		script = strings.TrimSpace(script)
		if script == "" {
			continue
		}
		builder.WriteString("    <script type=\"application/ld+json\">")
		builder.WriteString(script)
		builder.WriteString("</script>\n")
	}
	builder.WriteString("    ")
	builder.WriteString(seoMarkerEnd)
	return builder.String()
}

func buildStructuredData(requestPath, baseURL string) []string {
	switch requestPath {
	case "/":
		return []string{buildHomeStructuredData(baseURL)}
	case "/signup":
		return []string{buildSignupStructuredData(baseURL)}
	default:
		return nil
	}
}

func buildHomeStructuredData(baseURL string) string {
	graph := map[string]any{
		"@context": "https://schema.org",
		"@graph": []any{
			map[string]any{
				"@type": "WebSite",
				"@id":   canonicalURL(baseURL, "/") + "#website",
				"url":   canonicalURL(baseURL, "/"),
				"name":  "Proxer",
			},
			map[string]any{
				"@type":               "SoftwareApplication",
				"@id":                 canonicalURL(baseURL, "/") + "#software",
				"name":                "Proxer",
				"applicationCategory": "DeveloperApplication",
				"operatingSystem":     "macOS, Linux, Windows",
				"url":                 canonicalURL(baseURL, "/"),
				"description":         "Expose localhost apps with tenant-aware controls, rate limits, and TLS governance.",
				"offers": map[string]any{
					"@type":         "Offer",
					"price":         "0",
					"priceCurrency": "USD",
					"availability":  "https://schema.org/InStock",
				},
			},
			map[string]any{
				"@type":       "Product",
				"@id":         canonicalURL(baseURL, "/") + "#product",
				"name":        "Proxer Tunnel Platform",
				"description": "Route HTTP/HTTPS traffic to local machines with enterprise controls.",
				"brand": map[string]any{
					"@type": "Brand",
					"name":  "Proxer",
				},
				"offers": []any{
					map[string]any{
						"@type":         "Offer",
						"name":          "Free",
						"price":         "0",
						"priceCurrency": "USD",
					},
					map[string]any{
						"@type":         "Offer",
						"name":          "Pro",
						"price":         "20",
						"priceCurrency": "USD",
					},
					map[string]any{
						"@type":         "Offer",
						"name":          "Business",
						"price":         "100",
						"priceCurrency": "USD",
					},
				},
			},
			map[string]any{
				"@type": "FAQPage",
				"mainEntity": []any{
					map[string]any{
						"@type": "Question",
						"name":  "Does Proxer return responses from localhost back to callers?",
						"acceptedAnswer": map[string]any{
							"@type": "Answer",
							"text":  "Yes. Gateway dispatches to connector agent, agent calls the localhost app, then returns the response payload.",
						},
					},
					map[string]any{
						"@type": "Question",
						"name":  "Can I run Proxer locally?",
						"acceptedAnswer": map[string]any{
							"@type": "Answer",
							"text":  "Yes. The full stack runs in Docker Compose, with native desktop agents available for host routing.",
						},
					},
					map[string]any{
						"@type": "Question",
						"name":  "How are limits enforced?",
						"acceptedAnswer": map[string]any{
							"@type": "Answer",
							"text":  "Plan caps for entities and traffic are hard enforced with deterministic error responses.",
						},
					},
				},
			},
		},
	}
	return marshalStructuredData(graph)
}

func buildSignupStructuredData(baseURL string) string {
	payload := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "WebPage",
		"@id":         canonicalURL(baseURL, "/signup"),
		"url":         canonicalURL(baseURL, "/signup"),
		"name":        "Sign up for Proxer",
		"description": "Create a Proxer workspace, pair your connector, and start routing localhost traffic securely.",
		"potentialAction": map[string]any{
			"@type":  "RegisterAction",
			"target": canonicalURL(baseURL, "/signup"),
		},
	}
	return marshalStructuredData(payload)
}

func marshalStructuredData(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	escaped := strings.ReplaceAll(string(data), "</", "<\\/")
	return escaped
}

func injectSEOBlock(indexHTML, block string) string {
	start := strings.Index(indexHTML, seoMarkerStart)
	end := strings.Index(indexHTML, seoMarkerEnd)
	if start >= 0 && end > start {
		end += len(seoMarkerEnd)
		return indexHTML[:start] + block + indexHTML[end:]
	}
	if strings.Contains(indexHTML, "</head>") {
		return strings.Replace(indexHTML, "</head>", block+"\n  </head>", 1)
	}
	return block + "\n" + indexHTML
}
