package parse_test

import (
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

func TestExtractWindowVar_NextData(t *testing.T) {
	html := `<html><head>
	<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"title":"Hello"}},"page":"/index"}</script>
	</head><body></body></html>`

	resp := newHTMLResponse(html)
	val, err := parse.ExtractWindowVar(resp, "__NEXT_DATA__")
	if err != nil {
		t.Fatalf("ExtractWindowVar error: %v", err)
	}
	if val == nil {
		t.Fatal("ExtractWindowVar returned nil")
	}

	m, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	if m["page"] != "/index" {
		t.Errorf("page: got %v, want /index", m["page"])
	}
}

func TestExtractWindowVar_WindowAssignment(t *testing.T) {
	html := `<html><body>
	<script>
		window.__INITIAL_STATE__ = {"user":"alice","loggedIn":true};
	</script>
	</body></html>`

	resp := newHTMLResponse(html)
	val, err := parse.ExtractWindowVar(resp, "__INITIAL_STATE__")
	if err != nil {
		t.Fatalf("ExtractWindowVar error: %v", err)
	}
	if val == nil {
		t.Fatal("ExtractWindowVar returned nil")
	}

	m, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	if m["user"] != "alice" {
		t.Errorf("user: got %v, want alice", m["user"])
	}
	if m["loggedIn"] != true {
		t.Errorf("loggedIn: got %v, want true", m["loggedIn"])
	}
}

func TestExtractWindowVar_NestedBraces(t *testing.T) {
	html := `<html><body>
	<script>
		window.__APP_STATE__ = {"data":{"items":[{"id":1,"nested":{"deep":"value"}}]},"meta":{"count":1}};
	</script>
	</body></html>`

	resp := newHTMLResponse(html)
	val, err := parse.ExtractWindowVar(resp, "__APP_STATE__")
	if err != nil {
		t.Fatalf("ExtractWindowVar error: %v", err)
	}
	if val == nil {
		t.Fatal("ExtractWindowVar returned nil")
	}

	m, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	data, ok := m["data"].(map[string]any)
	if !ok {
		t.Fatal("data field not a map")
	}
	items, ok := data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items: got %v, want array of 1", data["items"])
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatal("first item not a map")
	}
	nested, ok := first["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested field not a map")
	}
	if nested["deep"] != "value" {
		t.Errorf("nested.deep: got %v, want value", nested["deep"])
	}
}

func TestExtractWindowVar_NotFound(t *testing.T) {
	html := `<html><body><p>No scripts here</p></body></html>`

	resp := newHTMLResponse(html)
	val, err := parse.ExtractWindowVar(resp, "__NONEXISTENT__")
	if err != nil {
		t.Fatalf("ExtractWindowVar error: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil, got %v", val)
	}
}

func TestExtractInlineJSON_VarDeclaration(t *testing.T) {
	html := `<html><body>
	<script>
		var config = {"api":"https://api.example.com","debug":false};
	</script>
	</body></html>`

	resp := newHTMLResponse(html)
	val, err := parse.ExtractInlineJSON(resp, "config")
	if err != nil {
		t.Fatalf("ExtractInlineJSON error: %v", err)
	}
	if val == nil {
		t.Fatal("ExtractInlineJSON returned nil")
	}

	m, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	if m["api"] != "https://api.example.com" {
		t.Errorf("api: got %v, want https://api.example.com", m["api"])
	}
	if m["debug"] != false {
		t.Errorf("debug: got %v, want false", m["debug"])
	}
}

func TestExtractPreloadedData_NextJS(t *testing.T) {
	html := `<html><head>
	<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"title":"Home","items":[1,2,3]}},"page":"/"}</script>
	</head><body><div id="__next"></div></body></html>`

	resp := newHTMLResponse(html)
	pd, err := parse.ExtractPreloadedData(resp)
	if err != nil {
		t.Fatalf("ExtractPreloadedData error: %v", err)
	}
	if pd.Framework != "nextjs" {
		t.Errorf("Framework: got %q, want %q", pd.Framework, "nextjs")
	}
	if pd.Variables["__NEXT_DATA__"] == nil {
		t.Fatal("__NEXT_DATA__ not found in Variables")
	}
	if pd.NextData == nil {
		t.Fatal("NextData shortcut is nil")
	}
	if pd.NextData["title"] != "Home" {
		t.Errorf("NextData title: got %v, want Home", pd.NextData["title"])
	}
}

func TestExtractPreloadedData_MultipleVars(t *testing.T) {
	html := `<html><head>
	<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"x":1}},"page":"/"}</script>
	</head><body>
	<script>
		window.__INITIAL_STATE__ = {"auth":true};
	</script>
	</body></html>`

	resp := newHTMLResponse(html)
	pd, err := parse.ExtractPreloadedData(resp)
	if err != nil {
		t.Fatalf("ExtractPreloadedData error: %v", err)
	}
	if pd.Variables["__NEXT_DATA__"] == nil {
		t.Error("__NEXT_DATA__ not found")
	}
	if pd.Variables["__INITIAL_STATE__"] == nil {
		t.Error("__INITIAL_STATE__ not found")
	}
	state, ok := pd.Variables["__INITIAL_STATE__"].(map[string]any)
	if !ok {
		t.Fatal("__INITIAL_STATE__ not a map")
	}
	if state["auth"] != true {
		t.Errorf("auth: got %v, want true", state["auth"])
	}
}

func TestDetectFramework_NextJS(t *testing.T) {
	html := `<html><head>
	<script id="__NEXT_DATA__" type="application/json">{}</script>
	</head><body></body></html>`

	resp := newHTMLResponse(html)
	fw := parse.DetectFramework(resp)
	if fw != "nextjs" {
		t.Errorf("DetectFramework: got %q, want %q", fw, "nextjs")
	}
}

func TestDetectFramework_Nuxt(t *testing.T) {
	html := `<html><body>
	<script>window.__NUXT__ = {data: []};</script>
	</body></html>`

	resp := newHTMLResponse(html)
	fw := parse.DetectFramework(resp)
	if fw != "nuxt" {
		t.Errorf("DetectFramework: got %q, want %q", fw, "nuxt")
	}
}

func TestDetectFramework_React(t *testing.T) {
	html := `<html><body>
	<div id="root" data-reactroot="">Content</div>
	</body></html>`

	resp := newHTMLResponse(html)
	fw := parse.DetectFramework(resp)
	if fw != "react" {
		t.Errorf("DetectFramework: got %q, want %q", fw, "react")
	}
}

func TestDetectFramework_Unknown(t *testing.T) {
	html := `<html><body><p>Plain HTML page</p></body></html>`

	resp := newHTMLResponse(html)
	fw := parse.DetectFramework(resp)
	if fw != "unknown" {
		t.Errorf("DetectFramework: got %q, want %q", fw, "unknown")
	}
}
