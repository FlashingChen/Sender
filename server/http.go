package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	apiPrefix              = "/api/v1"
	registerPath           = apiPrefix + "/devices/register"
	bindDevicePath         = apiPrefix + "/devices/bind"
	messagesPath           = apiPrefix + "/messages"
	appsPath               = apiPrefix + "/apps"
	tokenPath              = apiPrefix + "/oauth/token"
	healthPath             = "/healthz"
	apiHealthPath          = apiPrefix + "/healthz"
	authorizePath          = "/authorize"
	registerPagePath       = "/register"
	loginPath              = "/login"
	logoutPath             = "/logout"
	bindPagePath           = "/bind"
	overviewPath           = "/overview"
	historyPath            = "/history"
	sessionCookieName      = "sender_session"
	maxRequestBytes        = 8 << 20
	defaultQuerySize       = 100
	authorizationCodeGrant = "authorization_code"
)

type HandlerOptions struct {
	AllowRegistration bool
	// AuthRateLimiter throttles the unauthenticated register, login, and
	// token endpoints per client IP. Nil disables throttling.
	AuthRateLimiter *RateLimiter
}

type registerRequest struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}

type messageBatchRequest struct {
	Messages []MessageInput `json:"messages"`
}

type messageBatchResponse struct {
	Inserted   int `json:"inserted"`
	Duplicates int `json:"duplicates"`
}

type messagesResponse struct {
	Messages   []MessageRecord `json:"messages"`
	NextCursor string          `json:"next_cursor"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type bindDeviceRequest struct {
	DeviceID string `json:"device_id"`
	Secret   string `json:"secret"`
}

// NewHandler returns the complete API and Web UI router for store.
func NewHandler(store *Store) http.Handler {
	return NewHandlerWithOptions(store, HandlerOptions{
		AllowRegistration: registrationAllowedFromEnv(),
		AuthRateLimiter:   NewRateLimiter(authRateLimit, authRateWindow),
	})
}

func NewHandlerWithOptions(store *Store, options HandlerOptions) http.Handler {
	mux := http.NewServeMux()
	handler := &apiHandler{
		store:             store,
		allowRegistration: options.AllowRegistration,
		authLimiter:       options.AuthRateLimiter,
	}
	mux.HandleFunc(healthPath, handler.health)
	mux.HandleFunc(apiHealthPath, handler.health)
	mux.HandleFunc(registerPath, handler.register)
	mux.HandleFunc(bindDevicePath, handler.bindDevice)
	mux.HandleFunc(tokenPath, handler.token)
	mux.HandleFunc(messagesPath, handler.messagesQuery)
	mux.HandleFunc(appsPath, handler.apps)
	mux.HandleFunc(apiPrefix+"/", handler.notFound)
	mux.HandleFunc(apiPrefix+"/devices/", handler.deviceMessages)
	mux.HandleFunc(authorizePath, handler.authorize)
	mux.HandleFunc(registerPagePath, handler.registerPage)
	mux.HandleFunc(loginPath, handler.login)
	mux.HandleFunc(logoutPath, handler.logout)
	mux.HandleFunc(bindPagePath, handler.bindPage)
	mux.HandleFunc(overviewPath, handler.overview)
	mux.HandleFunc(historyPath, handler.history)
	mux.HandleFunc("/", handler.home)
	return mux
}

type apiHandler struct {
	store             *Store
	allowRegistration bool
	authLimiter       *RateLimiter
}

func registrationAllowedFromEnv() bool {
	raw := strings.TrimSpace(os.Getenv("ALLOW_REGISTRATION"))
	if raw == "" {
		return true
	}
	allowed, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return allowed
}

func (h *apiHandler) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

func (h *apiHandler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *apiHandler) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authLimiter.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	if !h.allowRegistration {
		writeError(w, http.StatusForbidden, "registration disabled")
		return
	}
	secret := strings.TrimSpace(r.Header.Get("X-Device-Secret"))
	if !validDeviceSecret(secret) {
		writeError(w, http.StatusBadRequest, "X-Device-Secret must be 32 hexadecimal characters")
		return
	}
	var request registerRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(request.DeviceID) == "" {
		writeError(w, http.StatusBadRequest, "device_id is required")
		return
	}
	if err := h.store.RegisterDevice(strings.TrimSpace(request.DeviceID), request.Name, secret); err != nil {
		switch {
		case errors.Is(err, ErrDeviceExists):
			writeError(w, http.StatusConflict, "device already registered with a different secret")
		default:
			writeError(w, http.StatusInternalServerError, "could not register device")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *apiHandler) deviceMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	deviceID, ok := deviceIDFromMessagesPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	secret, ok := bearerSecret(r.Header.Get("Authorization"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authorized, err := h.store.AuthenticateDevice(deviceID, secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not authenticate device")
		return
	}
	if !authorized {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	bound, err := h.store.IsDeviceBound(deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check device binding")
		return
	}
	if !bound {
		writeError(w, http.StatusForbidden, "device not bound")
		return
	}

	var request messageBatchRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(request.Messages) > maxMessageBatch {
		writeError(w, http.StatusBadRequest, "message batch exceeds 500")
		return
	}
	inserted, duplicates, err := h.store.InsertMessages(deviceID, request.Messages)
	if err != nil {
		log.Printf("insert messages for device %s: %v", deviceID, err)
		writeError(w, http.StatusBadRequest, "could not insert messages")
		return
	}
	writeJSON(w, http.StatusOK, messageBatchResponse{Inserted: inserted, Duplicates: duplicates})
}

func (h *apiHandler) messagesQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, authorized, err := h.userIDFromAccessToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not authenticate user")
		return
	}
	if !authorized {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	query, err := parseMessageQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query.UserID = userID
	messages, nextCursor, err := h.store.QueryMessages(query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not query messages")
		return
	}
	writeJSON(w, http.StatusOK, messagesResponse{Messages: messages, NextCursor: nextCursor})
}

func (h *apiHandler) apps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, authorized, err := h.userIDFromAccessToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not authenticate user")
		return
	}
	if !authorized {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	day := r.URL.Query().Get("day")
	deviceID := r.URL.Query().Get("device_id")
	apps, err := h.store.QueryAppsForUser(userID, day, deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not query apps")
		return
	}
	writeJSON(w, http.StatusOK, apps)
}

func (h *apiHandler) bindDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var user User
	if sessionUser, ok, err := h.currentUser(r); err != nil {
		writeError(w, http.StatusInternalServerError, "could not authenticate session")
		return
	} else if ok {
		user = sessionUser
	} else {
		userID, ok, err := h.userIDFromAccessToken(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not authenticate token")
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err = h.store.FindUserByID(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not authenticate token")
			return
		}
	}
	var request bindDeviceRequest
	if err := decodeFormOrJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	request.Secret = strings.TrimSpace(request.Secret)
	if request.DeviceID == "" || request.Secret == "" {
		writeError(w, http.StatusBadRequest, "device_id and secret are required")
		return
	}
	if err := h.store.BindDevice(request.DeviceID, request.Secret, user.ID); err != nil {
		switch {
		case errors.Is(err, ErrDeviceSecretInvalid):
			writeError(w, http.StatusBadRequest, "invalid device secret")
		case errors.Is(err, ErrDeviceNotFound):
			writeError(w, http.StatusBadRequest, "device not found")
		case errors.Is(err, ErrDeviceAlreadyBound):
			writeError(w, http.StatusConflict, "device already bound to another account")
		default:
			writeError(w, http.StatusInternalServerError, "could not bind device")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": user.Username})
}

func (h *apiHandler) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request")
		return
	}
	if !h.authLimiter.Allow(clientIP(r)) {
		writeOAuthError(w, http.StatusTooManyRequests, "too_many_requests")
		return
	}
	var request tokenRequest
	if err := decodeFormOrJSON(w, r, &request); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if strings.TrimSpace(request.Code) == "" ||
		strings.TrimSpace(request.CodeVerifier) == "" ||
		strings.TrimSpace(request.ClientID) == "" ||
		strings.TrimSpace(request.RedirectURI) == "" ||
		request.GrantType != authorizationCodeGrant {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	userID, err := h.store.ConsumeAuthorizationCode(
		request.Code,
		request.CodeVerifier,
		request.ClientID,
		request.RedirectURI,
		time.Now().UTC(),
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrAuthorizationNotFound),
			errors.Is(err, ErrAuthorizationExpired),
			errors.Is(err, ErrAuthorizationUsed),
			errors.Is(err, ErrAuthorizationClient),
			errors.Is(err, ErrAuthorizationRedirect),
			errors.Is(err, ErrAuthorizationPKCE):
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		default:
			writeOAuthError(w, http.StatusInternalServerError, "server_error")
		}
		return
	}
	accessToken, err := h.store.IssueAccessToken(userID, accessTokenTTL)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(accessTokenTTL / time.Second),
	})
}

func (h *apiHandler) userIDFromAccessToken(r *http.Request) (int64, bool, error) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !validDeviceSecret(parts[1]) {
		return 0, false, nil
	}
	return h.store.ValidateAccessToken(parts[1])
}

func parseMessageQuery(r *http.Request) (messageQuery, error) {
	values := r.URL.Query()
	result := messageQuery{
		Day:      values.Get("day"),
		DeviceID: values.Get("device_id"),
		App:      values.Get("app"),
		Limit:    defaultQuerySize,
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return messageQuery{}, errors.New("limit must be a positive integer")
		}
		if limit > maxMessageBatch {
			limit = maxMessageBatch
		}
		result.Limit = limit
	}
	if raw := values.Get("cursor"); raw != "" {
		parts := strings.Split(raw, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return messageQuery{}, errors.New("cursor must use ts:id format")
		}
		ts, tsErr := strconv.ParseInt(parts[0], 10, 64)
		id, idErr := strconv.ParseInt(parts[1], 10, 64)
		if tsErr != nil || idErr != nil || id < 1 {
			return messageQuery{}, errors.New("cursor must use ts:id format")
		}
		result.Cursor = &messageCursor{TS: ts, ID: id}
	}
	if result.Cursor != nil && result.Day == "" {
		return messageQuery{}, errors.New("cursor requires a day filter")
	}
	return result, nil
}

func deviceIDFromMessagesPath(path string) (string, bool) {
	const prefix = apiPrefix + "/devices/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "messages" || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func bearerSecret(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !validDeviceSecret(parts[1]) {
		return "", false
	}
	return parts[1], true
}

func validDeviceSecret(secret string) bool {
	if len(secret) != 32 {
		return false
	}
	_, err := hex.DecodeString(secret)
	return err == nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	body := http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body must contain JSON")
		}
		if strings.Contains(err.Error(), "request body too large") {
			return errors.New("request body too large")
		}
		return errors.New("invalid JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid JSON")
	}
	return nil
}

func decodeFormOrJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return decodeJSON(w, r, destination)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := r.ParseForm(); err != nil {
		return errors.New("invalid form")
	}
	switch value := destination.(type) {
	case *tokenRequest:
		value.GrantType = r.FormValue("grant_type")
		value.Code = r.FormValue("code")
		value.CodeVerifier = r.FormValue("code_verifier")
		value.ClientID = r.FormValue("client_id")
		value.RedirectURI = r.FormValue("redirect_uri")
	case *bindDeviceRequest:
		value.DeviceID = r.FormValue("device_id")
		value.Secret = r.FormValue("secret")
	default:
		return errors.New("unsupported form request")
	}
	return nil
}

func writeOAuthError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
