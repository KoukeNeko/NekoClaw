package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/security"
)

type securitySessionContextKey struct{}

type securitySetupRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type securityLoginRequest struct {
	Password string `json:"password"`
}

type securityPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type securitySessionContext struct {
	ID string
}

func (s *Server) wrapSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		if s.security == nil {
			next.ServeHTTP(w, r)
			return
		}
		if s.isPublicSecurityRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		if !s.security.Config().AuthEnabled {
			next.ServeHTTP(w, r)
			return
		}

		sessionID, _ := s.sessionIDFromCookie(r)
		if sessionID == "" {
			status := s.security.Status("", clientKeyFromRequest(r))
			if status.SetupRequired {
				respondErrorDetail(w, http.StatusUnauthorized, "setup_required", security.ErrSetupRequired.Error())
				return
			}
			respondErrorDetail(w, http.StatusUnauthorized, "auth_required", "browser auth required")
			return
		}

		session, err := s.security.Authenticate(sessionID)
		if err != nil {
			clearSecuritySessionCookie(w, r)
			switch {
			case errors.Is(err, security.ErrSessionExpired):
				respondErrorDetail(w, http.StatusUnauthorized, "session_expired", err.Error())
			default:
				respondErrorDetail(w, http.StatusUnauthorized, "auth_required", "browser auth required")
			}
			return
		}
		if isMutatingMethod(r.Method) && !sameOriginMatches(r) {
			respondErrorDetail(w, http.StatusForbidden, "origin_mismatch", "request origin does not match current host")
			return
		}

		ctx := context.WithValue(r.Context(), securitySessionContextKey{}, securitySessionContext{ID: session.ID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleSecurityStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.security == nil {
		respondJSON(w, http.StatusOK, security.StatusSnapshot{})
		return
	}
	sessionID, _ := s.sessionIDFromCookie(r)
	respondJSON(w, http.StatusOK, s.security.Status(sessionID, clientKeyFromRequest(r)))
}

func (s *Server) handleSecuritySetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.security == nil {
		respondError(w, http.StatusServiceUnavailable, "browser security runtime not configured")
		return
	}

	var req securitySetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	session, err := s.security.Setup(req.Token, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, security.ErrAlreadyInitialized):
			respondErrorDetail(w, http.StatusConflict, "already_initialized", err.Error())
		case errors.Is(err, security.ErrSetupTokenInvalid):
			respondErrorDetail(w, http.StatusBadRequest, "invalid_setup_token", err.Error())
		case errors.Is(err, security.ErrInvalidPassword):
			respondErrorDetail(w, http.StatusBadRequest, "invalid_password", err.Error())
		default:
			respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	setSecuritySessionCookie(w, r, session.ID)
	respondJSON(w, http.StatusOK, s.security.Status(session.ID, clientKeyFromRequest(r)))
}

func (s *Server) handleSecurityLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.security == nil {
		respondError(w, http.StatusServiceUnavailable, "browser security runtime not configured")
		return
	}

	var req securityLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	session, err := s.security.Login(clientKeyFromRequest(r), req.Password)
	if err != nil {
		switch {
		case errors.Is(err, security.ErrSetupRequired):
			respondErrorDetail(w, http.StatusUnauthorized, "setup_required", err.Error())
		case errors.Is(err, security.ErrInvalidCredentials):
			respondErrorDetail(w, http.StatusUnauthorized, "auth_required", err.Error())
		case errors.Is(err, security.ErrLoginRateLimited):
			respondErrorDetail(w, http.StatusTooManyRequests, "login_rate_limited", err.Error())
		case errors.Is(err, security.ErrAuthDisabled):
			respondErrorDetail(w, http.StatusConflict, "auth_disabled", err.Error())
		default:
			respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	setSecuritySessionCookie(w, r, session.ID)
	respondJSON(w, http.StatusOK, s.security.Status(session.ID, clientKeyFromRequest(r)))
}

func (s *Server) handleSecurityLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.security == nil {
		respondError(w, http.StatusServiceUnavailable, "browser security runtime not configured")
		return
	}
	sessionID := currentSecuritySessionID(r)
	if sessionID == "" {
		sessionID, _ = s.sessionIDFromCookie(r)
	}
	if sessionID != "" {
		s.security.Logout(sessionID)
	}
	clearSecuritySessionCookie(w, r)
	respondJSON(w, http.StatusOK, s.security.Status("", clientKeyFromRequest(r)))
}

func (s *Server) handleSecurityLogoutAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.security == nil {
		respondError(w, http.StatusServiceUnavailable, "browser security runtime not configured")
		return
	}
	s.security.LogoutAll()
	clearSecuritySessionCookie(w, r)
	respondJSON(w, http.StatusOK, s.security.Status("", clientKeyFromRequest(r)))
}

func (s *Server) handleSecurityConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respondJSON(w, http.StatusOK, s.svc.GetSecurityConfig())
	case http.MethodPut:
		var cfg core.SecurityConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			respondError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if err := s.svc.SaveSecurityConfig(cfg); err != nil {
			if errors.Is(err, app.ErrInvalidSecurityConfig) {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if s.security != nil {
			s.security.SetConfig(s.svc.GetSecurityConfig())
		}
		respondJSON(w, http.StatusOK, s.svc.GetSecurityConfig())
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSecurityPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.security == nil {
		respondError(w, http.StatusServiceUnavailable, "browser security runtime not configured")
		return
	}

	var req securityPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	session, err := s.security.ChangePassword(req.CurrentPassword, req.NewPassword, currentSecuritySessionID(r))
	if err != nil {
		switch {
		case errors.Is(err, security.ErrSetupRequired):
			respondErrorDetail(w, http.StatusUnauthorized, "setup_required", err.Error())
		case errors.Is(err, security.ErrInvalidCredentials):
			respondErrorDetail(w, http.StatusUnauthorized, "auth_required", err.Error())
		case errors.Is(err, security.ErrInvalidPassword):
			respondErrorDetail(w, http.StatusBadRequest, "invalid_password", err.Error())
		default:
			respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if strings.TrimSpace(session.ID) != "" {
		setSecuritySessionCookie(w, r, session.ID)
	} else {
		clearSecuritySessionCookie(w, r)
	}
	respondJSON(w, http.StatusOK, s.security.Status(session.ID, clientKeyFromRequest(r)))
}

func currentSecuritySessionID(r *http.Request) string {
	value, _ := r.Context().Value(securitySessionContextKey{}).(securitySessionContext)
	return strings.TrimSpace(value.ID)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}

func (s *Server) isPublicSecurityRequest(r *http.Request) bool {
	switch r.URL.Path {
	case "/healthz", "/oauth2callback", "/v1/integrations/discord/events", "/v1/security/status", "/v1/security/setup", "/v1/security/login":
		return true
	default:
		return false
	}
}

func (s *Server) sessionIDFromCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie(security.CookieName)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(cookie.Value), nil
}

func setSecuritySessionCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     security.CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieShouldBeSecure(r),
	})
}

func clearSecuritySessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     security.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieShouldBeSecure(r),
		MaxAge:   -1,
	})
}

func cookieShouldBeSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func sameOriginMatches(r *http.Request) bool {
	expectedScheme, expectedHost := requestOrigin(r)
	if expectedScheme == "" || expectedHost == "" {
		return false
	}

	originHeader := strings.TrimSpace(r.Header.Get("Origin"))
	if originHeader != "" {
		parsed, err := url.Parse(originHeader)
		if err != nil {
			return false
		}
		return strings.EqualFold(parsed.Scheme, expectedScheme) && equalHosts(parsed.Host, expectedHost)
	}

	refererHeader := strings.TrimSpace(r.Header.Get("Referer"))
	if refererHeader != "" {
		parsed, err := url.Parse(refererHeader)
		if err != nil {
			return false
		}
		return strings.EqualFold(parsed.Scheme, expectedScheme) && equalHosts(parsed.Host, expectedHost)
	}

	return false
}

func requestOrigin(r *http.Request) (string, string) {
	scheme := "http"
	if cookieShouldBeSecure(r) {
		scheme = "https"
	}

	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	return scheme, host
}

func equalHosts(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if strings.EqualFold(left, right) {
		return true
	}

	leftHost, leftPort, leftErr := net.SplitHostPort(left)
	rightHost, rightPort, rightErr := net.SplitHostPort(right)
	if leftErr == nil && rightErr == nil {
		return strings.EqualFold(leftHost, rightHost) && leftPort == rightPort
	}
	return false
}

func clientKeyFromRequest(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if remote := strings.TrimSpace(r.RemoteAddr); remote != "" {
		return remote
	}
	return "unknown"
}
