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
	"os"
	"strings"
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
// loopback calls and for building registration links.
func SelfBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("PUBLIC_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	// Render injects this automatically on web services.
	if u := strings.TrimSpace(os.Getenv("RENDER_EXTERNAL_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return ""
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
// HTTP API
// ---------------------------------------------------------------------------

// SelfLicenseRoutes registers the self-hosted license server endpoints.
// No-op unless LICENSE_MODE=self.
func SelfLicenseRoutes(eng *gin.Engine) {
	if !IsSelfHosted() {
		return
	}
	v1 := eng.Group("/v1")
	{
		v1.POST("/register/init", selfHandleRegisterInit)
		v1.POST("/register/exchange", selfHandleRegisterExchange)
		v1.POST("/register/auto", selfHandleRegisterAuto)
		v1.POST("/activate", selfHandleActivate)
		v1.POST("/heartbeat", selfHandleHeartbeat)
		v1.POST("/deactivate", selfHandleDeactivate)
	}
	eng.GET("/license-server/register", selfHandleRegisterPage)
	eng.POST("/license-server/complete", selfHandleRegisterComplete)
}

func selfHandleRegisterInit(c *gin.Context) {
	base := SelfBaseURL()
	if base == "" {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "self-hosted license server has no public URL",
			"message": "Set PUBLIC_URL (Render provides RENDER_EXTERNAL_URL automatically).",
		})
		return
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
	c.Redirect(http.StatusFound, rt.RedirectURI+sep+"code="+code)
}

func selfHandleRegisterExchange(c *gin.Context) {
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
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"/>` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"/>` +
		`<title>Activate WhatsappGo</title>` +
		`<style>body{font-family:system-ui,sans-serif;background:#09090b;color:#fafafa;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}` +
		`.card{background:#18181b;border:1px solid #27272a;border-radius:12px;padding:32px;max-width:420px;width:90%}h1{font-size:22px;margin:0 0 8px}` +
		`p{color:#a1a1aa;font-size:14px}label{display:block;font-size:13px;margin:16px 0 6px}` +
		`input{width:100%;box-sizing:border-box;background:#09090b;border:1px solid #3f3f46;border-radius:8px;color:#fafafa;padding:10px 12px;font-size:14px}` +
		`button{margin-top:20px;width:100%;background:#2563eb;border:0;border-radius:8px;color:#fff;padding:11px;font-size:15px;font-weight:600;cursor:pointer}` +
		`button:hover{background:#1d4ed8}.mono{font-family:monospace;font-size:12px;color:#71717a;word-break:break-all}</style></head><body>` +
		`<div class="card"><h1>Activate WhatsappGo</h1>` +
		`<p>Enter the email address for this license. A license key will be issued and stored on your own server.</p>` +
		`<form method="POST" action="/license-server/complete">` +
		`<input type="hidden" name="token" value="` + html.EscapeString(token) + `"/>` +
		`<label for="email">Email address</label>` +
		`<input id="email" type="email" name="email" required placeholder="you@example.com" autocomplete="email"/>` +
		`<button type="submit">Activate license</button></form>` +
		`<p class="mono">Instance: ` + html.EscapeString(instanceID) + `</p></div></body></html>`
}

func selfErrorPage(msg string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"/>` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"/>` +
		`<title>WhatsappGo — Error</title>` +
		`<style>body{font-family:system-ui,sans-serif;background:#09090b;color:#fafafa;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}` +
		`.card{background:#18181b;border:1px solid #27272a;border-radius:12px;padding:32px;max-width:420px;width:90%}h1{font-size:20px;margin:0 0 8px}` +
		`p{color:#a1a1aa;font-size:14px}a{color:#60a5fa}</style></head><body>` +
		`<div class="card"><h1>Something went wrong</h1><p>` + html.EscapeString(msg) + `</p></div></body></html>`
}
