package core

// Self-hosted licensing for WhatsappGo.
//
// Set LICENSE_MODE=self and the app becomes its own license server:
//   - license keys + verified emails live in YOUR Postgres (self_* tables)
//   - registration pages are served locally, in English, under your brand
//   - heartbeats/activations loop back to your own public URL
//
// Nothing is sent to any third party. Unset LICENSE_MODE to keep the
// original remote-licensing behavior.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// mode + base URL
// ---------------------------------------------------------------------------

// IsSelfHosted reports whether self-hosted licensing is enabled.
func IsSelfHosted() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LICENSE_MODE"))) {
	case "self", "selfhosted", "self-hosted", "local":
		return true
	}
	return false
}

// SelfBaseURL returns this deployment's public base URL, used for license
// loopback calls. Env first, then localhost (Docker/local zero-config).
func SelfBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("PUBLIC_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	// Render injects this automatically on web services.
	if u := strings.TrimSpace(os.Getenv("RENDER_EXTERNAL_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	port := strings.TrimSpace(os.Getenv("SERVER_PORT"))
	if port == "" {
		port = "8080"
	}
	return "http://localhost:" + port
}

// selfPublicBase returns the browser-facing base URL: explicit env first,
// otherwise derived from the incoming request (proxy-aware). This makes
// licensing work on any URL (localhost, LAN IP, VPS, custom domain) with
// zero configuration.
func selfPublicBase(c *gin.Context) string {
	if u := strings.TrimSpace(os.Getenv("PUBLIC_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := strings.TrimSpace(os.Getenv("RENDER_EXTERNAL_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		proto = "http"
		if c.Request.TLS != nil {
			proto = "https"
		}
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(c.Request.Host)
	}
	if host == "" {
		return SelfBaseURL()
	}
	return proto + "://" + strings.TrimRight(host, "/")
}

// ---------------------------------------------------------------------------
// storage models (live in YOUR database)
// ---------------------------------------------------------------------------

// SelfLicense is a license issued by your own server.
type SelfLicense struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"customer_id"`
	Email        string     `gorm:"uniqueIndex;size:255;not null" json:"email"`
	APIKey       string     `gorm:"uniqueIndex;size:128;not null" json:"-"`
	Tier         string     `gorm:"size:64;not null" json:"tier"`
	InstanceID   string     `gorm:"size:64" json:"instance_id"`
	Status       string     `gorm:"size:32;not null;default:active" json:"status"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	MessagesSent int64      `json:"messages_sent"`
	MessagesRecv int64      `json:"messages_recv"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// TableName keeps the table clearly namespaced.
func (SelfLicense) TableName() string { return "self_licenses" }

// SelfRegToken is a pending browser registration started via /v1/register/init.
type SelfRegToken struct {
	Token       string    `gorm:"primaryKey;size:64" json:"-"`
	Tier        string    `gorm:"size:64" json:"-"`
	Version     string    `gorm:"size:64" json:"-"`
	InstanceID  string    `gorm:"size:64" json:"-"`
	RedirectURI string    `gorm:"size:512" json:"-"`
	CreatedAt   time.Time `json:"-"`
	ExpiresAt   time.Time `json:"-"`
}

// TableName keeps the table clearly namespaced.
func (SelfRegToken) TableName() string { return "self_reg_tokens" }

// SelfAuthCode is a one-time code handed to the app after email registration.
type SelfAuthCode struct {
	Code       string    `gorm:"primaryKey;size:64" json:"-"`
	LicenseID  uint      `json:"-"`
	InstanceID string    `gorm:"size:64" json:"-"`
	Used       bool      `json:"-"`
	CreatedAt  time.Time `json:"-"`
	ExpiresAt  time.Time `json:"-"`
}

// TableName keeps the table clearly namespaced.
func (SelfAuthCode) TableName() string { return "self_auth_codes" }

// MigrateSelfDB creates the self-licensing tables. Safe to call always.
func MigrateSelfDB() error {
	if _k4 == nil {
		return fmt.Errorf("core: database not set, call SetDB first")
	}
	return _k4.AutoMigrate(&SelfLicense{}, &SelfRegToken{}, &SelfAuthCode{})
}

func selfRandHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// HMAC auth (same scheme the app client uses)
// ---------------------------------------------------------------------------

func selfAuthLicense(c *gin.Context, body []byte) (*SelfLicense, bool) {
	key := c.GetHeader("X-Api-Key")
	sig := c.GetHeader("X-Signature")
	if key == "" || sig == "" || _k4 == nil {
		return nil, false
	}
	var lic SelfLicense
	if err := _k4.Where("api_key = ? AND status = ?", key, "active").First(&lic).Error; err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	expect := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expect), []byte(strings.ToLower(sig))) != 1 {
		return nil, false
	}
	return &lic, true
}

// ---------------------------------------------------------------------------
// tiny in-memory fixed-window rate limiter (per client IP) for the public
// license endpoints — prevents registration spam / email stuffing.
// ---------------------------------------------------------------------------

var selfRL = struct {
	sync.Mutex
	hits map[string][]int64
}{hits: map[string][]int64{}}

func selfRateLimitMW(max int, windowSec int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now().Unix()
		selfRL.Lock()
		if len(selfRL.hits) > 20000 {
			selfRL.hits = map[string][]int64{}
		}
		kept := selfRL.hits[ip][:0]
		for _, t := range selfRL.hits[ip] {
			if t > now-windowSec {
				kept = append(kept, t)
			}
		}
		if len(kept) >= max {
			selfRL.hits[ip] = kept
			selfRL.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests, please slow down",
			})
			return
		}
		selfRL.hits[ip] = append(kept, now)
		selfRL.Unlock()
		c.Next()
	}
}

// selfAdminGuard ensures only someone holding the deployment's GLOBAL_API_KEY
// (the owner, signed into the Manager) can start a license registration.
// The Manager SPA always sends the key it was given at login.
func selfAdminGuard(c *gin.Context) bool {
	want := strings.TrimSpace(os.Getenv("GLOBAL_API_KEY"))
	got := strings.TrimSpace(c.GetHeader("apikey"))
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// ---------------------------------------------------------------------------
// HTTP API
// ---------------------------------------------------------------------------

// SelfLicenseRoutes registers the self-hosted license server endpoints.
// No-op unless LICENSE_MODE=self.
func SelfLicenseRoutes(eng *gin.Engine) {
	if !IsSelfHosted() {
		return
	}
	v1 := eng.Group("/v1")
	v1.Use(selfRateLimitMW(120, 60))
	{
		v1.POST("/register/init", selfHandleRegisterInit)
		v1.POST("/register/exchange", selfHandleRegisterExchange)
		v1.POST("/register/auto", selfHandleRegisterAuto)
		v1.POST("/activate", selfHandleActivate)
		v1.POST("/heartbeat", selfHandleHeartbeat)
		v1.POST("/deactivate", selfHandleDeactivate)
	}
	lim := selfRateLimitMW(30, 60)
	eng.GET("/license-server/register", lim, selfHandleRegisterPage)
	eng.POST("/license-server/complete", lim, selfHandleRegisterComplete)
	eng.GET("/license-server/google/start", lim, selfHandleGoogleStart)
	eng.GET("/license-server/google/callback", lim, selfHandleGoogleCallback)
}

func selfHandleRegisterInit(c *gin.Context) {
	base := selfPublicBase(c)
	if base == "" {
		base = SelfBaseURL()
	}
	var req struct {
		Tier        string `json:"tier"`
		Version     string `json:"version"`
		InstanceID  string `json:"instance_id"`
		RedirectURI string `json:"redirect_uri"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Tier == "" {
		req.Tier = "whatsapp-go"
	}
	if req.RedirectURI == "" {
		req.RedirectURI = base + "/manager/license/callback"
	}
	token, err := selfRandHex(24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create session"})
		return
	}
	rt := SelfRegToken{
		Token:       token,
		Tier:        req.Tier,
		Version:     req.Version,
		InstanceID:  req.InstanceID,
		RedirectURI: req.RedirectURI,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(30 * time.Minute),
	}
	if err := _k4.Create(&rt).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"register_url": base + "/license-server/register?token=" + token,
		"token":        token,
	})
}

func selfHandleRegisterPage(c *gin.Context) {
	token := c.Query("token")
	var rt SelfRegToken
	if token == "" || _k4.Where("token = ?", token).First(&rt).Error != nil || time.Now().After(rt.ExpiresAt) {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(selfErrorPage("Invalid or expired registration link. Please start again from the Manager login page.")))
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(selfRegisterPage(token, rt.InstanceID)))
}

func selfHandleRegisterComplete(c *gin.Context) {
	token := strings.TrimSpace(c.PostForm("token"))
	email := strings.ToLower(strings.TrimSpace(c.PostForm("email")))
	if _, err := mail.ParseAddress(email); err != nil || !strings.Contains(email, "@") {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(selfErrorPage("Please enter a valid email address.")))
		return
	}
	var rt SelfRegToken
	if token == "" || _k4.Where("token = ?", token).First(&rt).Error != nil || time.Now().After(rt.ExpiresAt) {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(selfErrorPage("Invalid or expired registration link. Please start again from the Manager login page.")))
		return
	}
	var lic SelfLicense
	if err := _k4.Where("email = ?", email).First(&lic).Error; err != nil {
		key, err := selfRandHex(32)
		if err != nil {
			c.Data(http.StatusInternalServerError, "text/html; charset=utf-8", []byte(selfErrorPage("Could not issue a license right now. Please try again.")))
			return
		}
		lic = SelfLicense{Email: email, APIKey: key, Tier: rt.Tier, Status: "active"}
		if err := _k4.Create(&lic).Error; err != nil {
			c.Data(http.StatusInternalServerError, "text/html; charset=utf-8", []byte(selfErrorPage("Could not issue a license right now. Please try again.")))
			return
		}
	}
	if lic.Status != "active" {
		lic.Status = "active"
		_k4.Save(&lic)
	}
	code, err := selfRandHex(24)
	if err != nil {
		c.Data(http.StatusInternalServerError, "text/html; charset=utf-8", []byte(selfErrorPage("Could not issue a license right now. Please try again.")))
		return
	}
	_k4.Create(&SelfAuthCode{
		Code:       code,
		LicenseID:  lic.ID,
		InstanceID: rt.InstanceID,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(15 * time.Minute),
	})
	_k4.Delete(&rt)
	sep := "?"
	if strings.Contains(rt.RedirectURI, "?") {
		sep = "&"
	}
	continueURL := rt.RedirectURI + sep + "code=" + code
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(selfSuccessPage(email, continueURL)))
}

func selfGoogleConfigured() (id, secret string, ok bool) {
	id = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	secret = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	return id, secret, id != "" && secret != ""
}

// selfHandleGoogleStart begins "Continue with Google": verifies the
// registration token, then hands the browser to Google. Google returns to
// selfHandleGoogleCallback with an authorization code.
func selfHandleGoogleStart(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	var rt SelfRegToken
	if token == "" || _k4.Where("token = ?", token).First(&rt).Error != nil || time.Now().After(rt.ExpiresAt) {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(selfErrorPage("Invalid or expired registration link. Please start again from the Manager login page.")))
		return
	}
	id, _, ok := selfGoogleConfigured()
	if !ok {
		c.Data(http.StatusBadGateway, "text/html; charset=utf-8", []byte(selfErrorPage("Google sign-in is not configured on this server. Please use email registration instead.")))
		return
	}
	redirectURI := selfPublicBase(c) + "/license-server/google/callback"
	authURL := "https://accounts.google.com/o/oauth2/v2/auth" +
		"?client_id=" + url.QueryEscape(id) +
		"&redirect_uri=" + url.QueryEscape(redirectURI) +
		"&response_type=code&scope=" + url.QueryEscape("openid email profile") +
		"&access_type=online&prompt=select_account" +
		"&state=" + url.QueryEscape(token)
	c.Redirect(http.StatusFound, authURL)
}

// selfHandleGoogleCallback finishes Google sign-in: exchanges the code,
// reads the verified Gmail address, issues the license, and continues to
// the app exactly like email registration does.
func selfHandleGoogleCallback(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	token := strings.TrimSpace(c.Query("state"))
	if code == "" || token == "" {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(selfErrorPage("Google sign-in was cancelled or failed. Please try again.")))
		return
	}
	var rt SelfRegToken
	if _k4.Where("token = ?", token).First(&rt).Error != nil || time.Now().After(rt.ExpiresAt) {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(selfErrorPage("Invalid or expired registration link. Please start again from the Manager login page.")))
		return
	}
	id, secret, ok := selfGoogleConfigured()
	if !ok {
		c.Data(http.StatusBadGateway, "text/html; charset=utf-8", []byte(selfErrorPage("Google sign-in is not configured on this server. Please use email registration instead.")))
		return
	}
	redirectURI := selfPublicBase(c) + "/license-server/google/callback"

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", id)
	form.Set("client_secret", secret)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")
	req, err := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		c.Data(http.StatusBadGateway, "text/html; charset=utf-8", []byte(selfErrorPage("Could not reach Google. Please try again.")))
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := _3t.Do(req)
	if err != nil {
		c.Data(http.StatusBadGateway, "text/html; charset=utf-8", []byte(selfErrorPage("Could not reach Google. Please try again.")))
		return
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || (tok.AccessToken == "" && tok.IDToken == "") {
		c.Data(http.StatusBadGateway, "text/html; charset=utf-8", []byte(selfErrorPage("Google rejected the sign-in. Please try again or use email registration.")))
		return
	}
	email, verified := selfGoogleEmail(tok.AccessToken)
	if email == "" || !verified {
		c.Data(http.StatusForbidden, "text/html; charset=utf-8", []byte(selfErrorPage("Google did not return a verified email address. Please use email registration instead.")))
		return
	}
	selfFinishRegistration(c, rt, email)
}

// selfGoogleEmail returns the Google account email + verified flag.
func selfGoogleEmail(accessToken string) (string, bool) {
	req, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil || accessToken == "" {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := _3t.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var info struct {
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(info.Email)), info.VerifiedEmail
}

// selfFinishRegistration issues (or reuses) a license for email, creates the
// one-time app code, and renders the branded success page.
func selfFinishRegistration(c *gin.Context, rt SelfRegToken, email string) {
	selfFinishRegistration(c, rt, email)
}
	var req struct {
		AuthorizationCode string `json:"authorization_code"`
		InstanceID        string `json:"instance_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AuthorizationCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "authorization code is required"})
		return
	}
	var ac SelfAuthCode
	if err := _k4.Where("code = ?", req.AuthorizationCode).First(&ac).Error; err != nil ||
		ac.Used || time.Now().After(ac.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired code"})
		return
	}
	var lic SelfLicense
	if err := _k4.First(&lic, ac.LicenseID).Error; err != nil || lic.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "license is not active"})
		return
	}
	if req.InstanceID != "" && ac.InstanceID != "" && req.InstanceID != ac.InstanceID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code was issued for a different instance"})
		return
	}
	ac.Used = true
	_k4.Save(&ac)
	c.JSON(http.StatusOK, gin.H{
		"api_key":     lic.APIKey,
		"tier":        lic.Tier,
		"customer_id": lic.ID,
	})
}

func selfHandleRegisterAuto(c *gin.Context) {
	var req struct {
		Email      string `json:"email"`
		Tier       string `json:"tier"`
		Version    string `json:"version"`
		InstanceID string `json:"instance_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	var lic SelfLicense
	if err := _k4.Where("email = ? AND status = ?", strings.ToLower(strings.TrimSpace(req.Email)), "active").First(&lic).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not registered yet"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"api_key":     lic.APIKey,
		"customer_id": lic.ID,
		"tier":        lic.Tier,
		"status":      "active",
	})
}

func selfHandleActivate(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	lic, ok := selfAuthLicense(c, body)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid license key or signature"})
		return
	}
	var req struct {
		InstanceID string `json:"instance_id"`
		Version    string `json:"version"`
	}
	_ = json.Unmarshal(body, &req)
	now := time.Now()
	lic.LastSeenAt = &now
	if req.InstanceID != "" {
		lic.InstanceID = req.InstanceID
	}
	_k4.Save(lic)
	c.JSON(http.StatusOK, gin.H{"status": "active"})
}

func selfHandleHeartbeat(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	lic, ok := selfAuthLicense(c, body)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid license key or signature"})
		return
	}
	var req struct {
		InstanceID      string `json:"instance_id"`
		TelemetryBundle struct {
			MessagesSent int64 `json:"messages_sent"`
			MessagesRecv int64 `json:"messages_recv"`
		} `json:"telemetry_bundle"`
	}
	_ = json.Unmarshal(body, &req)
	now := time.Now()
	lic.LastSeenAt = &now
	lic.MessagesSent += req.TelemetryBundle.MessagesSent
	lic.MessagesRecv += req.TelemetryBundle.MessagesRecv
	_k4.Save(lic)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func selfHandleDeactivate(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "deactivated"})
}

// ---------------------------------------------------------------------------
// registration pages (English)
// ---------------------------------------------------------------------------

func selfRegisterPage(token, instanceID string) string {
	googleBtn := `<p class="note">Google sign-in is not enabled on this server — continue with email below.</p>`
	if _, _, ok := selfGoogleConfigured(); ok {
		googleBtn = `<div class="or"><span>or</span></div>` +
			`<a class="gbtn" href="/license-server/google/start?token=` + html.EscapeString(token) + `">` +
			`<svg width="17" height="17" viewBox="0 0 24 24"><path fill="#4285F4" d="M23.5 12.3c0-.9-.1-1.5-.3-2.3H12v4.3h6.5c-.1 1.1-.8 2.7-2.4 3.8l-.1.1 3.5 2.7.2.1c2.2-2 3.8-5 3.8-8.7z"/><path fill="#34A853" d="M12 24c3.2 0 5.9-1.1 7.9-2.9l-3.8-2.9c-1 .7-2.4 1.2-4.1 1.2-3.1 0-5.8-2.1-6.8-5l-.1.1-3.6 2.8v.1C3.5 21.3 7.5 24 12 24z"/><path fill="#FBBC05" d="M5.2 14.4c-.2-.7-.4-1.5-.4-2.4s.1-1.7.4-2.4l-.1-.1-3.6-2.8v.1C.5 8.9 0 10.4 0 12s.5 3.1 1.5 4.4l3.7-2z"/><path fill="#EA4335" d="M12 4.7c1.8 0 3 .8 3.7 1.4l3.3-3.2C17.9 1.1 15.2 0 12 0 7.5 0 3.5 2.7 1.5 6.8l3.7 2.9c1-2.9 3.7-5 6.8-5z"/></svg>` +
			`Continue with Google</a>`
	}
func selfSuccessPage(email, continueURL string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"/>` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"/>` +
		`<meta http-equiv="refresh" content="3;url=` + html.EscapeString(continueURL) + `"/>` +
		`<title>WhatsappGo — License issued</title>` +
		`<link rel="icon" href="https://raw.githubusercontent.com/nayem-48ai/whatsapp-go/main/public/whatsappgo/favicon.svg"/>` +
		`<style>*{box-sizing:border-box}body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:#09090b;color:#fafafa;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;padding:20px}` +
		`.card{background:#131316;border:1px solid #27272a;border-radius:16px;padding:36px;max-width:440px;width:100%;box-shadow:0 20px 60px rgba(0,0,0,.5);text-align:center}` +
		`.brand{display:flex;align-items:center;gap:12px;margin-bottom:6px;text-align:left}` +
		`.brand img{width:44px;height:44px;border-radius:11px}` +
		`.brand b{font-size:20px}` +
		`.check{width:64px;height:64px;border-radius:50%;background:#25d366;color:#062d1a;font-size:32px;font-weight:800;line-height:64px;margin:18px auto 6px}` +
		`h1{font-size:22px;margin:12px 0 8px}` +
		`.steps{display:flex;gap:6px;margin:16px 0 4px}` +
		`.steps span{flex:1;text-align:center;font-size:11px;color:#4ade80;padding-top:8px;border-top:2px solid #25d366}` +
		`p{color:#a1a1aa;font-size:14px;line-height:1.55}` +
		`.mail{color:#fafafa;font-weight:600;word-break:break-all}` +
		`.btn{display:inline-block;margin-top:18px;background:#25d366;border-radius:9px;color:#062d1a;padding:11px 34px;font-size:15px;font-weight:700;text-decoration:none}` +
		`.btn:hover{background:#1eb856}` +
		`.hint{margin-top:14px;font-size:12px}` +
		`.foot{margin-top:20px;padding-top:14px;border-top:1px solid #27272a;font-size:12px;color:#71717a}</style></head><body>` +
		`<div class="card"><div class="brand"><img src="https://raw.githubusercontent.com/nayem-48ai/whatsapp-go/main/public/whatsappgo/logo-400.png" alt="WhatsappGo"/><b>WhatsappGo</b></div>` +
		`<div class="steps"><span>1 · Email</span><span>2 · Activate</span><span>3 · Done</span></div>` +
		`<div class="check">✓</div><h1>License issued</h1>` +
		`<p>Your license for <span class="mail">` + html.EscapeString(email) + `</span> is active and stored on your own server. Continuing to your app…</p>` +
		`<a class="btn" href="` + html.EscapeString(continueURL) + `">Continue to Manager</a>` +
		`<p class="hint">Not redirected automatically? Click the button above.</p>` +
		`<div class="foot">WhatsappGo · Self-hosted license server</div></div></body></html>`
}

func selfErrorPage(msg string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"/>` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"/>` +
		`<title>WhatsappGo — Error</title>` +
		`<link rel="icon" href="https://raw.githubusercontent.com/nayem-48ai/whatsapp-go/main/public/whatsappgo/favicon.svg"/>` +
		`<style>*{box-sizing:border-box}body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:#09090b;color:#fafafa;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;padding:20px}` +
		`.card{background:#131316;border:1px solid #27272a;border-radius:16px;padding:36px;max-width:440px;width:100%;box-shadow:0 20px 60px rgba(0,0,0,.5)}` +
		`.brand{display:flex;align-items:center;gap:12px;margin-bottom:6px}` +
		`.brand img{width:44px;height:44px;border-radius:11px}` +
		`.brand b{font-size:20px}h1{font-size:20px;margin:14px 0 8px}` +
		`p{color:#a1a1aa;font-size:14px;line-height:1.55}a{color:#4ade80}` +
		`.btn{display:inline-block;margin-top:16px;background:#25d366;border-radius:9px;color:#062d1a;padding:10px 22px;font-size:14px;font-weight:700;text-decoration:none}</style></head><body>` +
		`<div class="card"><div class="brand"><img src="https://raw.githubusercontent.com/nayem-48ai/whatsapp-go/main/public/whatsappgo/logo-400.png" alt="WhatsappGo"/><b>WhatsappGo</b></div>` +
		`<h1>Something went wrong</h1><p>` + html.EscapeString(msg) + `</p>` +
		`<a class="btn" href="/manager/login">Back to login</a></div></body></html>`
}
