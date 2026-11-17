package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestConfigWebAuthenticationConfigAndPasswordRotation(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	passwordPath := filepath.Join(directory, "web_password")
	initialConfig := []byte(`{"sourceDir":"/legacy/photos"}`)
	if err := os.WriteFile(configPath, initialConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwordPath, []byte("initial-web-password-123"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(newConfigWebHandler(configPath, passwordPath))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + serverURL.Host

	response := doRequest(t, server.Client(), http.MethodGet, server.URL+"/api/config", nil, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated config status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
	response.Body.Close()

	response = doRequest(t, server.Client(), http.MethodGet, server.URL+"/", nil, nil)
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Contains(page, []byte("挂载配置")) {
		t.Fatalf("config page response status=%d, missing page title", response.StatusCode)
	}
	if response.Header.Get("Content-Security-Policy") == "" || strings.Contains(string(page), "<script>") {
		t.Fatal("config page is missing its security policy or still contains inline script")
	}
	response = doRequest(t, server.Client(), http.MethodGet, server.URL+"/app.js", nil, nil)
	pageScript, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Contains(pageScript, []byte("/api/config")) {
		t.Fatalf("page script response status=%d or missing config API", response.StatusCode)
	}

	loginBody := []byte(`{"password":"initial-web-password-123"}`)
	response = doRequest(t, server.Client(), http.MethodPost, server.URL+"/api/login", []byte(`{"password":"incorrect-web-password"}`), http.Header{"Origin": []string{origin}})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-password login status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
	response.Body.Close()
	response = doRequest(t, server.Client(), http.MethodPost, server.URL+"/api/login", loginBody, http.Header{"Origin": []string{origin}})
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("login status = %d, response=%s", response.StatusCode, body)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
		}
	}
	response.Body.Close()
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("login cookie has unexpected security settings: %#v", sessionCookie)
	}

	requestHeader := http.Header{"Origin": []string{origin}}
	requestHeader.Add("Cookie", sessionCookie.Name+"="+sessionCookie.Value)
	response = doRequest(t, server.Client(), http.MethodGet, server.URL+"/api/config", nil, requestHeader)
	var existingConfig Config
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("authenticated config status=%d, response=%s", response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(&existingConfig); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if len(existingConfig.Mounts) != 1 || existingConfig.Mounts[0].Source != "/legacy/photos" {
		t.Fatalf("legacy config was not converted for the page: %#v", existingConfig)
	}

	response = doRequest(t, server.Client(), http.MethodPost, server.URL+"/api/config", []byte(`{"mounts":[{"source":"/vol2/photos","target":"/vol1/1000/Photos","checkFile":"remote"}],"intervalSeconds":45}`), requestHeader)
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("save status=%d, response=%s", response.StatusCode, body)
	}
	response.Body.Close()
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored Config
	if err := json.Unmarshal(saved, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.IntervalSecond != 45 || len(stored.Mounts) != 1 || stored.Mounts[0].Source != "/vol2/photos" {
		t.Fatalf("saved config did not match submitted values: %#v", stored)
	}

	response = doRequest(t, server.Client(), http.MethodPost, server.URL+"/api/config", []byte(`{"mounts":[{"source":"relative","target":"/vol1/1000/Photos","checkFile":"remote"}],"intervalSeconds":45}`), requestHeader)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid config status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
	response.Body.Close()
	unchanged, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, unchanged) {
		t.Fatal("invalid config changed the stored file")
	}
	if err := os.WriteFile(configPath, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	response = doRequest(t, server.Client(), http.MethodGet, server.URL+"/api/config", nil, requestHeader)
	var repairView struct {
		Mounts         []Mount `json:"mounts"`
		IntervalSecond int     `json:"intervalSeconds"`
		Warning        string  `json:"warning"`
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("broken config recovery view status=%d, response=%s", response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(&repairView); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if repairView.Warning == "" || len(repairView.Mounts) != 1 || repairView.Mounts[0].Source != "" {
		t.Fatalf("broken config did not produce an editable repair view: %#v", repairView)
	}
	response = doRequest(t, server.Client(), http.MethodPost, server.URL+"/api/config", []byte(`{"mounts":[{"source":"/vol2/photos","target":"/vol1/1000/Photos","checkFile":"remote"}],"intervalSeconds":45}`), requestHeader)
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("repair save status=%d, response=%s", response.StatusCode, body)
	}
	response.Body.Close()

	csrfHeader := http.Header{"Origin": []string{"http://attacker.example"}}
	csrfHeader.Add("Cookie", sessionCookie.Name+"="+sessionCookie.Value)
	response = doRequest(t, server.Client(), http.MethodPost, server.URL+"/api/config", saved, csrfHeader)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin save status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
	response.Body.Close()

	newPassword := "new-web-password-456"
	response = doRequest(t, server.Client(), http.MethodPost, server.URL+"/api/password", []byte(`{"newPassword":"`+newPassword+`"}`), requestHeader)
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("password rotation status=%d, response=%s", response.StatusCode, body)
	}
	response.Body.Close()
	updatedPassword, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(updatedPassword) != newPassword {
		t.Fatalf("stored password = %q, want updated password", updatedPassword)
	}
	requestHeader.Set("Cookie", sessionCookie.Name+"="+sessionCookie.Value)
	response = doRequest(t, server.Client(), http.MethodGet, server.URL+"/api/config", nil, requestHeader)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old session remained valid after password rotation: status=%d", response.StatusCode)
	}
	response.Body.Close()
}

func TestEnsureWebPasswordCreatesRestrictedRandomSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "web_password")
	first, err := ensureWebPassword(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 32 {
		t.Fatalf("generated password has only %d bytes", len(first))
	}
	second, err := ensureWebPassword(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("existing password unexpectedly changed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("password file permissions are too broad: %o", info.Mode().Perm())
	}
}

func TestSignatureDetectsContentChangesWithSameSizeAndTimestamp(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte("{\"a\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(configPath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	before := signature(configPath)
	if err := os.WriteFile(configPath, []byte("{\"b\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(configPath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	after := signature(configPath)
	if before == after {
		t.Fatal("content hash did not detect a same-size update with unchanged timestamp")
	}
}

func TestValidateWebConfigRejectsUnsafeOrAmbiguousMounts(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "relative source", config: Config{IntervalSecond: 30, Mounts: []Mount{{Source: "source", Target: "/vol1/target"}}}},
		{name: "root target", config: Config{IntervalSecond: 30, Mounts: []Mount{{Source: "/vol2/source", Target: "/"}}}},
		{name: "duplicate target", config: Config{IntervalSecond: 30, Mounts: []Mount{{Source: "/vol2/a", Target: "/vol1/target"}, {Source: "/vol2/b", Target: "/vol1/target"}}}},
		{name: "check file path traversal", config: Config{IntervalSecond: 30, Mounts: []Mount{{Source: "/vol2/source", Target: "/vol1/target", CheckFile: "../secret"}}}},
		{name: "invalid interval", config: Config{IntervalSecond: 4, Mounts: []Mount{{Source: "/vol2/source", Target: "/vol1/target"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateWebConfig(&test.config); err == nil {
				t.Fatal("expected config validation error")
			}
		})
	}
}

func doRequest(t *testing.T, client *http.Client, method, url string, body []byte, header http.Header) *http.Response {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		requestBody = bytes.NewReader(body)
	}
	request, err := http.NewRequest(method, url, requestBody)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, values := range header {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
