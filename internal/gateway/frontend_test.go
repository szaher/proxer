package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildSEODocumentAppPathNoIndex(t *testing.T) {
	doc := buildSEODocument("/app/routes", "https://proxer.dev")
	if doc.Robots != "noindex, nofollow" {
		t.Fatalf("expected app routes to be noindex, got %q", doc.Robots)
	}
	if doc.CanonicalURL != "https://proxer.dev/app" {
		t.Fatalf("expected app canonical URL, got %q", doc.CanonicalURL)
	}
}

func TestBuildSEODocumentSignupIsIndexable(t *testing.T) {
	doc := buildSEODocument("/signup", "https://proxer.dev")
	if doc.Robots != "index, follow" {
		t.Fatalf("expected signup to be indexable, got %q", doc.Robots)
	}
	if !strings.Contains(doc.Title, "Sign up") {
		t.Fatalf("expected signup title variant, got %q", doc.Title)
	}
	if doc.OpenGraphImage != "https://proxer.dev/images/og-signup.svg" {
		t.Fatalf("expected signup og image URL, got %q", doc.OpenGraphImage)
	}
	if len(doc.StructuredDataJSON) == 0 {
		t.Fatalf("expected signup structured data")
	}
}

func TestInjectSEOBlockReplacesMarkedSection(t *testing.T) {
	template := "<html><head>" + seoMarkerStart + "<title>old</title>" + seoMarkerEnd + "</head><body></body></html>"
	out := injectSEOBlock(template, "<title>new</title>")
	if !strings.Contains(out, "<title>new</title>") {
		t.Fatalf("expected new SEO block to be present: %s", out)
	}
	if strings.Contains(out, "<title>old</title>") {
		t.Fatalf("expected old SEO block to be replaced: %s", out)
	}
}

func TestResolvePublicBaseURLFallsBackToForwardedRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "http://internal.local/signup", nil)
	req.Host = "app.proxer.dev"
	req.Header.Set("X-Forwarded-Proto", "https")

	resolved := resolvePublicBaseURL("", req)
	if resolved != "https://app.proxer.dev" {
		t.Fatalf("expected forwarded request URL, got %q", resolved)
	}
}

func TestServeSitemapXML(t *testing.T) {
	srv := &Server{cfg: Config{PublicBaseURL: "https://proxer.dev"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://localhost/sitemap.xml", nil)

	srv.serveSitemapXML(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "https://proxer.dev/") {
		t.Fatalf("expected root URL in sitemap, got %s", body)
	}
	if !strings.Contains(body, "https://proxer.dev/signup") {
		t.Fatalf("expected signup URL in sitemap, got %s", body)
	}
}

func TestBuildSEODocumentLandingIncludesFAQStructuredData(t *testing.T) {
	doc := buildSEODocument("/", "https://proxer.dev")
	if len(doc.StructuredDataJSON) == 0 {
		t.Fatalf("expected structured data for landing page")
	}
	if !strings.Contains(doc.StructuredDataJSON[0], "\"FAQPage\"") {
		t.Fatalf("expected FAQPage in structured data, got %s", doc.StructuredDataJSON[0])
	}
}

func TestBuildSEOBlockIncludesSocialImageAndJSONLD(t *testing.T) {
	doc := buildSEODocument("/", "https://proxer.dev")
	rendered := buildSEOBlock(doc)
	if !strings.Contains(rendered, "og:image") {
		t.Fatalf("expected og:image tag in SEO block: %s", rendered)
	}
	if !strings.Contains(rendered, "application/ld+json") {
		t.Fatalf("expected json-ld script in SEO block: %s", rendered)
	}
}

func TestBuildHomeStructuredDataUsesFragmentIDs(t *testing.T) {
	payload := buildHomeStructuredData("https://proxer.dev")
	if strings.Contains(payload, "%23website") {
		t.Fatalf("expected plain fragment IDs in structured data: %s", payload)
	}
	if !strings.Contains(payload, "#website") {
		t.Fatalf("expected #website anchor in structured data: %s", payload)
	}
}
