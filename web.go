package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultWebAddress = ":17891"
	sessionCookieName = "check_mount_session"
	sessionDuration   = 8 * time.Hour
	maxRequestBody    = 1 << 20
	maxWebMounts      = 20
)

//go:embed web/index.html web/app.js
var configWebAssets embed.FS

type configWeb struct {
	configPath   string
	passwordPath string

	mu            sync.Mutex
	sessions      map[string]time.Time
	loginFailures map[string]loginFailure
}

type loginFailure struct {
	count   int
	blocked time.Time
}

func newConfigWebHandler(configPath, passwordPath string) http.Handler {
	web := &configWeb{
		configPath:    configPath,
		passwordPath:  passwordPath,
		sessions:      make(map[string]time.Time),
		loginFailures: make(map[string]loginFailure),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", web.handlePage)
	mux.HandleFunc("/app.js", web.handleAppJS)
	mux.HandleFunc("/api/login", web.handleLogin)
	mux.HandleFunc("/api/logout", web.handleLogout)
	mux.HandleFunc("/api/config", web.handleConfig)
	mux.HandleFunc("/api/password", web.handlePassword)
	return securityHeaders(mux)
}

func serveConfigWeb(configPath string) error {
	if strings.TrimSpace(configPath) == "" {
		return errors.New("配置文件路径为空，配置网页未启动")
	}
	passwordPath := strings.TrimSpace(os.Getenv("WEB_PASSWORD_FILE"))
	if passwordPath == "" {
		passwordPath = filepath.Join(filepath.Dir(configPath), "web_password")
	}
	_, statErr := os.Stat(passwordPath)
	password, err := ensureWebPassword(passwordPath)
	if err != nil {
		return fmt.Errorf("初始化配置网页密码失败: %w", err)
	}
	if os.IsNotExist(statErr) {
		logf("配置网页首次管理密码（请登录后立即修改）: %s", password)
	}
	address := strings.TrimSpace(os.Getenv("WEB_ADDR"))
	if address == "" {
		address = defaultWebAddress
	}
	server := &http.Server{
		Addr:              address,
		Handler:           newConfigWebHandler(configPath, passwordPath),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logf("配置网页监听地址: %s", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func ensureWebPassword(path string) ([]byte, error) {
	password, err := os.ReadFile(path)
	if err == nil {
		password = bytes.TrimRight(password, "\r\n")
		if passwordLength(password) < 16 || passwordLength(password) > 128 {
			return nil, errors.New("网页密码长度必须为 16 到 128 个字符")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, err
		}
		return password, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}
	password = []byte(base64.RawURLEncoding.EncodeToString(randomBytes))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return ensureWebPassword(path)
		}
		return nil, err
	}
	if _, err := file.Write(password); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return password, nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'self'")
		next.ServeHTTP(w, r)
	})
}

func (web *configWeb) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	page, err := configWebAssets.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "页面资源不可用", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(page)
	}
}

func (web *configWeb) handleAppJS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.js" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	script, err := configWebAssets.ReadFile("web/app.js")
	if err != nil {
		http.Error(w, "页面资源不可用", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(script)
	}
}

func (web *configWeb) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "请求来源无效", http.StatusForbidden)
		return
	}
	client := clientAddress(r)
	if web.isLoginBlocked(client) {
		http.Error(w, "尝试次数过多，请稍后再试", http.StatusTooManyRequests)
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &request, 4<<10); err != nil {
		http.Error(w, "登录请求格式无效", http.StatusBadRequest)
		return
	}
	password, err := os.ReadFile(web.passwordPath)
	if err != nil {
		http.Error(w, "无法读取网页登录配置", http.StatusInternalServerError)
		return
	}
	password = bytes.TrimRight(password, "\r\n")
	if len(request.Password) != len(password) || subtle.ConstantTimeCompare([]byte(request.Password), password) != 1 {
		web.recordLoginFailure(client)
		http.Error(w, "密码不正确", http.StatusUnauthorized)
		return
	}
	web.clearLoginFailure(client)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		http.Error(w, "无法创建登录会话", http.StatusInternalServerError)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	web.mu.Lock()
	now := time.Now()
	for existingToken, expires := range web.sessions {
		if now.After(expires) {
			delete(web.sessions, existingToken)
		}
	}
	web.sessions[token] = now.Add(sessionDuration)
	web.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(sessionDuration),
		MaxAge:   int(sessionDuration.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "登录成功"})
}

func (web *configWeb) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "请求来源无效", http.StatusForbidden)
		return
	}
	if token := web.sessionToken(r); token != "" {
		web.mu.Lock()
		delete(web.sessions, token)
		web.mu.Unlock()
	}
	web.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"message": "已退出"})
}

func (web *configWeb) handleConfig(w http.ResponseWriter, r *http.Request) {
	if !web.authenticated(r) {
		http.Error(w, "请先登录", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(web.configPath)
		warning := ""
		var config Config
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "读取配置失败", http.StatusInternalServerError)
			return
		}
		if os.IsNotExist(err) {
			warning = "未找到配置文件；填写挂载项并保存即可创建配置。"
		} else if err := json.Unmarshal(data, &config); err != nil {
			warning = "当前配置文件格式无效；填写挂载项并保存即可修复配置。"
		}
		config.Mounts = config.effectiveMounts()
		if len(config.Mounts) == 0 {
			config.Mounts = []Mount{{Target: defaultTargetDir, CheckFile: defaultCheckFile}}
		}
		config.SourceDir = ""
		if config.IntervalSecond <= 0 {
			config.IntervalSecond = int(defaultInterval.Seconds())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"mounts":          config.Mounts,
			"intervalSeconds": config.IntervalSecond,
			"warning":         warning,
		})
	case http.MethodPost:
		if !sameOrigin(r) {
			http.Error(w, "请求来源无效", http.StatusForbidden)
			return
		}
		var config Config
		if err := decodeJSON(w, r, &config, maxRequestBody); err != nil {
			http.Error(w, "配置格式无效: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateWebConfig(&config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := saveConfigAtomically(web.configPath, config); err != nil {
			http.Error(w, "保存配置失败", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "配置已保存，守护进程将在下一个检查周期加载"})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (web *configWeb) handlePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !web.authenticated(r) {
		http.Error(w, "请先登录", http.StatusUnauthorized)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "请求来源无效", http.StatusForbidden)
		return
	}
	var request struct {
		NewPassword string `json:"newPassword"`
	}
	if err := decodeJSON(w, r, &request, 4<<10); err != nil {
		http.Error(w, "密码请求格式无效", http.StatusBadRequest)
		return
	}
	if passwordLength([]byte(request.NewPassword)) < 16 || passwordLength([]byte(request.NewPassword)) > 128 || strings.ContainsAny(request.NewPassword, "\r\n") {
		http.Error(w, "密码长度必须为 16 到 128 个字符，且不能包含换行", http.StatusBadRequest)
		return
	}
	if err := writeSecretAtomically(web.passwordPath, []byte(request.NewPassword)); err != nil {
		http.Error(w, "更新密码失败", http.StatusInternalServerError)
		return
	}
	web.mu.Lock()
	web.sessions = make(map[string]time.Time)
	web.mu.Unlock()
	web.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"message": "密码已修改，请重新登录"})
}

func (web *configWeb) authenticated(r *http.Request) bool {
	token := web.sessionToken(r)
	if token == "" {
		return false
	}
	web.mu.Lock()
	defer web.mu.Unlock()
	expires, ok := web.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(expires) {
		delete(web.sessions, token)
		return false
	}
	return true
}

func (web *configWeb) sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (web *configWeb) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (web *configWeb) isLoginBlocked(client string) bool {
	web.mu.Lock()
	defer web.mu.Unlock()
	failure := web.loginFailures[client]
	if failure.blocked.IsZero() {
		return false
	}
	if time.Now().After(failure.blocked) {
		delete(web.loginFailures, client)
		return false
	}
	return true
}

func (web *configWeb) recordLoginFailure(client string) {
	web.mu.Lock()
	defer web.mu.Unlock()
	failure := web.loginFailures[client]
	if !failure.blocked.IsZero() && time.Now().After(failure.blocked) {
		failure = loginFailure{}
	}
	failure.count++
	if failure.count >= 5 {
		failure.blocked = time.Now().Add(5 * time.Minute)
	}
	web.loginFailures[client] = failure
}

func (web *configWeb) clearLoginFailure(client string) {
	web.mu.Lock()
	delete(web.loginFailures, client)
	web.mu.Unlock()
}

func clientAddress(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Host != r.Host {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validateWebConfig(config *Config) error {
	if config.IntervalSecond < 5 || config.IntervalSecond > 86400 {
		return errors.New("检查间隔必须在 5 到 86400 秒之间")
	}
	if len(config.Mounts) == 0 {
		return errors.New("至少需要一个挂载项")
	}
	if len(config.Mounts) > maxWebMounts {
		return fmt.Errorf("挂载项最多为 %d 个", maxWebMounts)
	}
	seenTargets := make(map[string]struct{}, len(config.Mounts))
	for i := range config.Mounts {
		mount := &config.Mounts[i]
		mount.Source = path.Clean(strings.TrimSpace(mount.Source))
		mount.Target = path.Clean(strings.TrimSpace(mount.Target))
		mount.CheckFile = strings.TrimSpace(mount.CheckFile)
		if !strings.HasPrefix(mount.Source, "/") || mount.Source == "." || containsPathControl(mount.Source) {
			return fmt.Errorf("第 %d 项：源目录必须是绝对路径", i+1)
		}
		if !strings.HasPrefix(mount.Target, "/") || mount.Target == "." || mount.Target == "/" || containsPathControl(mount.Target) {
			return fmt.Errorf("第 %d 项：目标目录必须是绝对路径", i+1)
		}
		if mount.Source == mount.Target {
			return fmt.Errorf("第 %d 项：源目录和目标目录不能相同", i+1)
		}
		if mount.CheckFile == "" {
			mount.CheckFile = defaultCheckFile
		}
		if mount.CheckFile == "." || mount.CheckFile == ".." || strings.ContainsAny(mount.CheckFile, "/\\\x00\r\n") {
			return fmt.Errorf("第 %d 项：检查文件必须是单个文件名", i+1)
		}
		for _, character := range mount.CheckFile {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("._-", character)) {
				return fmt.Errorf("第 %d 项：检查文件只能包含字母、数字、点、下划线和短横线", i+1)
			}
		}
		if _, ok := seenTargets[mount.Target]; ok {
			return fmt.Errorf("第 %d 项：目标目录重复", i+1)
		}
		seenTargets[mount.Target] = struct{}{}
	}
	config.SourceDir = ""
	return nil
}

func passwordLength(password []byte) int {
	return utf8.RuneCount(password)
}

func containsPathControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func saveConfigAtomically(path string, config Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFileAtomically(temporaryPath, path)
}

func writeSecretAtomically(path string, secret []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".secret-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(secret); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFileAtomically(temporaryPath, path)
}

func replaceFileAtomically(temporaryPath, path string) error {
	if err := os.Rename(temporaryPath, path); err != nil {
		if runtime.GOOS != "windows" {
			return err
		}
		// Windows cannot replace an existing destination using os.Rename.
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		return os.Rename(temporaryPath, path)
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, maximum int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maximum)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("请求中包含多段 JSON")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
}
