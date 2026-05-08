package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
)

func TestHandleSetCookies_InvalidJSON(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)
	req := httptest.NewRequest("POST", "/cookies", bytes.NewReader([]byte(`not json`)))
	w := httptest.NewRecorder()

	h.HandleSetCookies(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleGetCookies_DisabledByDefault(t *testing.T) {
	h := New(&failMockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/cookies", nil)
	w := httptest.NewRecorder()

	h.HandleGetCookies(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("security.allowCookies")) {
		t.Fatalf("expected allowCookies hint, got %s", w.Body.String())
	}
}

func TestHandleSetCookies_DisabledByDefault(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("POST", "/cookies", bytes.NewReader([]byte(`{"url":"https://pinchtab.com","cookies":[{"name":"a","value":"b"}]}`)))
	w := httptest.NewRecorder()

	h.HandleSetCookies(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("security.allowCookies")) {
		t.Fatalf("expected allowCookies hint, got %s", w.Body.String())
	}
}

func TestHandleClearCookies_DisabledByDefault(t *testing.T) {
	b := &cookieClearMockBridge{}
	h := New(b, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("DELETE", "/cookies", nil)
	w := httptest.NewRecorder()

	h.HandleClearCookies(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if b.clearCookiesCalled {
		t.Fatal("expected ClearCookies not to be called")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("security.allowCookies")) {
		t.Fatalf("expected allowCookies hint, got %s", w.Body.String())
	}
}

func TestHandleSetCookies_NoTab(t *testing.T) {
	h := New(&failMockBridge{}, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)
	body := `{"url":"https://pinchtab.com","cookies":[{"name":"a","value":"b"}],"tabId":"nonexistent"}`
	req := httptest.NewRequest("POST", "/cookies", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	h.HandleSetCookies(w, req)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleGetCookies_NameFilter(t *testing.T) {
	h := New(&failMockBridge{}, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/cookies?name=session_id&tabId=nonexistent", nil)
	w := httptest.NewRecorder()

	h.HandleGetCookies(w, req)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == nil {
		t.Error("expected error in response")
	}
}

func TestHandleTabGetCookies_MissingTabID(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/tabs//cookies", nil)
	w := httptest.NewRecorder()
	h.HandleTabGetCookies(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTabGetCookies_NoTab(t *testing.T) {
	h := New(&failMockBridge{}, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/tabs/tab_abc/cookies", nil)
	req.SetPathValue("id", "tab_abc")
	w := httptest.NewRecorder()
	h.HandleTabGetCookies(w, req)
	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleTabSetCookies_TabIDMismatch(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)
	body := `{"tabId":"tab_other","url":"https://pinchtab.com","cookies":[{"name":"a","value":"b"}]}`
	req := httptest.NewRequest("POST", "/tabs/tab_abc/cookies", bytes.NewReader([]byte(body)))
	req.SetPathValue("id", "tab_abc")
	w := httptest.NewRecorder()
	h.HandleTabSetCookies(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTabSetCookies_NoTab(t *testing.T) {
	h := New(&failMockBridge{}, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)
	body := `{"url":"https://pinchtab.com","cookies":[{"name":"a","value":"b"}]}`
	req := httptest.NewRequest("POST", "/tabs/tab_abc/cookies", bytes.NewReader([]byte(body)))
	req.SetPathValue("id", "tab_abc")
	w := httptest.NewRecorder()
	h.HandleTabSetCookies(w, req)
	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// cookieClearMockBridge provides BrowserContext and ClearCookies for handler tests.
type cookieClearMockBridge struct {
	mockBridge
	clearCookiesCalled bool
	clearCookiesErr    error
}

func (m *cookieClearMockBridge) BrowserContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func (m *cookieClearMockBridge) ClearCookies(ctx context.Context) error {
	m.clearCookiesCalled = true
	return m.clearCookiesErr
}

func TestHandleClearCookies_Success(t *testing.T) {
	b := &cookieClearMockBridge{}
	h := New(b, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/cookies", nil)
	w := httptest.NewRecorder()
	h.HandleClearCookies(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !b.clearCookiesCalled {
		t.Fatal("expected ClearCookies to be called")
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "cleared" {
		t.Errorf("expected status=cleared, got %v", resp["status"])
	}
}

func TestHandleTabClearCookies_Success(t *testing.T) {
	b := &cookieClearMockBridge{}
	h := New(b, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/tabs/tab1/cookies", nil)
	req.SetPathValue("id", "tab1")
	w := httptest.NewRecorder()
	h.HandleTabClearCookies(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !b.clearCookiesCalled {
		t.Fatal("expected ClearCookies to be called")
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "cleared" {
		t.Errorf("expected status=cleared, got %v", resp["status"])
	}
}

func TestHandleTabClearCookies_MissingTabID(t *testing.T) {
	h := New(&cookieClearMockBridge{}, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/tabs//cookies", nil)
	w := httptest.NewRecorder()
	h.HandleTabClearCookies(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTabClearCookies_DisabledByDefault(t *testing.T) {
	b := &cookieClearMockBridge{}
	h := New(b, &config.RuntimeConfig{}, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/tabs/tab1/cookies", nil)
	req.SetPathValue("id", "tab1")
	w := httptest.NewRecorder()
	h.HandleTabClearCookies(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if b.clearCookiesCalled {
		t.Fatal("expected ClearCookies not to be called")
	}
}

func TestHandleTabClearCookies_NoTab(t *testing.T) {
	b := &cookieClearMockBridge{}
	b.failTab = true
	h := New(b, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/tabs/nonexistent/cookies", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	h.HandleTabClearCookies(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleClearCookies_RouteRegistration(t *testing.T) {
	b := &cookieClearMockBridge{}
	h := New(b, &config.RuntimeConfig{AllowCookies: true}, nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, nil)

	req := httptest.NewRequest("DELETE", "/cookies", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected DELETE /cookies to be registered, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("DELETE", "/tabs/tab1/cookies", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected DELETE /tabs/{id}/cookies to be registered, got %d: %s", w.Code, w.Body.String())
	}
}
