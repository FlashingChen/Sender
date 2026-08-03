package server

import (
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

//go:embed templates/*.html
var webTemplateFS embed.FS

var webTemplates = template.Must(template.New("pages").Funcs(template.FuncMap{
	"formatTime": func(ts int64) string {
		return time.Unix(ts, 0).Format("15:04:05")
	},
}).ParseFS(webTemplateFS, "templates/*.html"))

const oobRedirectURI = "urn:ietf:wg:oauth:2.0:oob"

type pageData struct {
	Title         string
	HasUser       bool
	User          User
	Error         string
	Message       string
	Username      string
	Next          string
	DeviceID      string
	Authorization authorizationPageData
	OOBCode       string
	Overview      overviewPageData
	History       historyPageData
	Devices       []boundDevicePageData
}

type authorizationRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	State               string
}

type authorizationPageData struct {
	Ready               bool
	ResponseType        string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	State               string
}

type overviewPageData struct {
	Day      string
	Today    int64
	Week     int64
	Agents   int64
	Devices  int64
	Uplink   bool
	Trend    []TrendPoint
	AppShare []AppShareRow
	Tail     []messagePageData
}

type historyPageData struct {
	Messages      []messagePageData
	Total         int64
	Page          int
	Pages         int
	HasPrevious   bool
	HasNext       bool
	PreviousURL   string
	NextURL       string
	Day           string
	App           string
	DeviceID      string
	Search        string
	AppOptions    []historyOption
	DeviceOptions []historyOption
}

type historyOption struct {
	Value    string
	Label    string
	Selected bool
}

type messagePageData struct {
	Time    string
	App     string
	AppName string
	Sender  string
	Chat    string
	Content string
}

type boundDevicePageData struct {
	DeviceID string
	Name     string
	BoundAt  string
}

func (h *apiHandler) currentUser(r *http.Request) (User, bool, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if errors.Is(err, http.ErrNoCookie) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	return h.store.ValidateSession(cookie.Value)
}

func (h *apiHandler) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		h.notFound(w, r)
		return
	}
	h.overview(w, r)
}

func (h *apiHandler) overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePageError(w, http.StatusMethodNotAllowed, "请求方法不允许")
		return
	}
	user, ok := h.requirePageUser(w, r)
	if !ok {
		return
	}
	stats, err := h.store.Overview(user.ID)
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "无法读取概览统计")
		return
	}
	tail, _, err := h.store.QueryMessages(messageQuery{UserID: user.ID, Limit: 5})
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "无法读取最新消息")
		return
	}
	reverseMessages(tail)
	renderPage(w, http.StatusOK, "home.html", pageData{
		Title:   "概览 · Sender",
		HasUser: true,
		User:    user,
		Overview: overviewPageData{
			Day:      time.Now().In(h.store.loc).Format("2006-01-02"),
			Today:    stats.Today,
			Week:     stats.Week,
			Agents:   stats.Agents,
			Devices:  stats.Devices,
			Uplink:   stats.Uplink,
			Trend:    stats.Trend,
			AppShare: stats.AppShare,
			Tail:     pageMessages(h.store.loc, tail),
		},
	})
}

func (h *apiHandler) history(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePageError(w, http.StatusMethodNotAllowed, "请求方法不允许")
		return
	}
	user, ok := h.requirePageUser(w, r)
	if !ok {
		return
	}
	query, err := parseHistoryQuery(r)
	if err != nil {
		writePageError(w, http.StatusBadRequest, err.Error())
		return
	}
	query.UserID = user.ID
	messages, total, err := h.store.QueryHistory(query)
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "无法读取历史消息")
		return
	}
	apps, err := h.store.QueryAppsForUser(user.ID, "", "")
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "无法读取应用列表")
		return
	}
	devices, err := h.store.BoundDevicesForUser(user.ID)
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "无法读取设备列表")
		return
	}
	values := r.URL.Query()
	pages := int((total + historyPageSize - 1) / historyPageSize)
	if pages < 1 {
		pages = 1
	}
	history := historyPageData{
		Messages: pageMessages(h.store.loc, messages),
		Total:    total,
		Page:     query.Page,
		Pages:    pages,
		Day:      query.Day,
		App:      query.App,
		DeviceID: query.DeviceID,
		Search:   query.Search,
	}
	if query.Page > 1 {
		history.HasPrevious = true
		history.PreviousURL = historyPageURL(values, query.Page-1)
	}
	if query.Page < pages {
		history.HasNext = true
		history.NextURL = historyPageURL(values, query.Page+1)
	}
	for _, app := range apps {
		history.AppOptions = append(history.AppOptions, historyOption{
			Value:    app.App,
			Label:    app.AppName,
			Selected: app.App == query.App,
		})
	}
	for _, device := range devices {
		history.DeviceOptions = append(history.DeviceOptions, historyOption{
			Value:    device.DeviceID,
			Label:    device.DeviceID,
			Selected: device.DeviceID == query.DeviceID,
		})
	}
	renderPage(w, http.StatusOK, "history.html", pageData{
		Title:   "历史 · Sender",
		HasUser: true,
		User:    user,
		History: history,
	})
}

func (h *apiHandler) registerPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		renderStandaloneError(w, http.StatusMethodNotAllowed, "请求方法不允许", "register.html", pageData{Title: "注册"})
		return
	}
	data := pageData{Title: "注册 · Sender"}
	if !h.allowRegistration {
		data.Error = "注册已关闭"
		renderPage(w, http.StatusForbidden, "register.html", data)
		return
	}
	if r.Method == http.MethodGet {
		renderPage(w, http.StatusOK, "register.html", data)
		return
	}
	if err := parsePageForm(w, r); err != nil {
		data.Error = err.Error()
		renderPage(w, http.StatusBadRequest, "register.html", data)
		return
	}
	data.Username = strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if data.Username == "" {
		data.Error = "用户名不能为空"
		renderPage(w, http.StatusBadRequest, "register.html", data)
		return
	}
	if len(password) < 8 {
		data.Error = "密码至少需要 8 位"
		renderPage(w, http.StatusBadRequest, "register.html", data)
		return
	}
	if _, err := h.store.RegisterUser(data.Username, password); errors.Is(err, ErrUserExists) {
		data.Error = "用户名已存在"
		renderPage(w, http.StatusBadRequest, "register.html", data)
		return
	} else if err != nil {
		writePageError(w, http.StatusInternalServerError, "注册失败")
		return
	}
	http.Redirect(w, r, loginPath+"?registered=1", http.StatusSeeOther)
}

func (h *apiHandler) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		renderStandaloneError(w, http.StatusMethodNotAllowed, "请求方法不允许", "login.html", pageData{Title: "登录"})
		return
	}
	data := pageData{Title: "登录 · Sender", Next: safeNext(r.FormValue("next"))}
	if r.URL.Query().Get("registered") == "1" {
		data.Message = "注册成功，请登录"
	}
	if r.Method == http.MethodGet {
		if _, ok, err := h.currentUser(r); err != nil {
			writePageError(w, http.StatusInternalServerError, "无法读取登录状态")
			return
		} else if ok {
			http.Redirect(w, r, data.Next, http.StatusSeeOther)
			return
		}
		renderPage(w, http.StatusOK, "login.html", data)
		return
	}
	if err := parsePageForm(w, r); err != nil {
		data.Error = err.Error()
		renderPage(w, http.StatusBadRequest, "login.html", data)
		return
	}
	data.Next = safeNext(r.FormValue("next"))
	data.Username = strings.TrimSpace(r.FormValue("username"))
	user, err := h.store.FindUserByUsername(data.Username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(r.FormValue("password"))) != nil {
		data.Error = "用户名或密码错误"
		renderPage(w, http.StatusUnauthorized, "login.html", data)
		return
	}
	rawToken, err := h.store.CreateSession(user.ID)
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "登录失败")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL / time.Second),
		Expires:  time.Now().UTC().Add(sessionTTL),
	})
	http.Redirect(w, r, data.Next, http.StatusSeeOther)
}

func (h *apiHandler) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writePageError(w, http.StatusMethodNotAllowed, "请求方法不允许")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *apiHandler) authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		renderPage(w, http.StatusMethodNotAllowed, "authorize.html", pageData{Title: "OAuth 授权", Error: "请求方法不允许"})
		return
	}
	var (
		request authorizationRequest
		err     error
	)
	if r.Method == http.MethodGet {
		request, err = authorizationRequestFromValues(r.URL.Query())
	} else if formErr := parsePageForm(w, r); formErr != nil {
		err = formErr
	} else {
		if r.PostForm.Get("response_type") == "" {
			r.PostForm.Set("response_type", "code")
		}
		request, err = authorizationRequestFromValues(r.PostForm)
	}
	if err != nil {
		renderPage(w, http.StatusBadRequest, "authorize.html", pageData{
			Title: "OAuth 授权",
			Error: "授权请求无效：" + err.Error(),
			Authorization: authorizationPageData{
				ClientID:    request.ClientID,
				RedirectURI: request.RedirectURI,
			},
		})
		return
	}
	user, ok := h.requirePageUser(w, r)
	if !ok {
		return
	}
	data := pageData{
		Title:   "授权 · Sender",
		HasUser: true,
		User:    user,
		Authorization: authorizationPageData{
			Ready:               true,
			ResponseType:        request.ResponseType,
			ClientID:            request.ClientID,
			RedirectURI:         request.RedirectURI,
			CodeChallenge:       request.CodeChallenge,
			CodeChallengeMethod: request.CodeChallengeMethod,
			State:               request.State,
		},
	}
	if r.Method == http.MethodGet {
		renderPage(w, http.StatusOK, "authorize.html", data)
		return
	}
	switch r.FormValue("action") {
	case "deny":
		h.finishAuthorization(w, r, request, "", "access_denied")
	case "approve":
		code, createErr := h.store.CreateAuthorizationCode(
			request.ClientID,
			user.ID,
			request.RedirectURI,
			request.CodeChallenge,
			authorizationCodeTTL,
		)
		if createErr != nil {
			writePageError(w, http.StatusInternalServerError, "无法创建授权码")
			return
		}
		h.finishAuthorization(w, r, request, code, "")
	default:
		data.Error = "请选择批准或拒绝"
		renderPage(w, http.StatusBadRequest, "authorize.html", data)
	}
}

func (h *apiHandler) finishAuthorization(w http.ResponseWriter, r *http.Request, request authorizationRequest, code, oauthError string) {
	if request.RedirectURI == oobRedirectURI {
		data := pageData{Title: "授权结果 · Sender"}
		if code != "" {
			data.OOBCode = code
		} else {
			data.Error = "用户拒绝了本次授权。"
		}
		renderPage(w, http.StatusOK, "oob.html", data)
		return
	}
	values := url.Values{}
	if code != "" {
		values.Set("code", code)
	}
	if oauthError != "" {
		values.Set("error", oauthError)
	}
	if request.State != "" {
		values.Set("state", request.State)
	}
	callback, err := url.Parse(request.RedirectURI)
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "无法生成回调地址")
		return
	}
	query := callback.Query()
	for key, items := range values {
		query.Del(key)
		for _, item := range items {
			query.Add(key, item)
		}
	}
	callback.RawQuery = query.Encode()
	http.Redirect(w, r, callback.String(), http.StatusFound)
}

func (h *apiHandler) bindPage(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requirePageUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writePageError(w, http.StatusMethodNotAllowed, "请求方法不允许")
		return
	}
	data := pageData{Title: "绑定设备 · Sender", HasUser: true, User: user}
	if r.Method == http.MethodPost {
		if err := parsePageForm(w, r); err != nil {
			data.Error = err.Error()
		} else {
			data.DeviceID = strings.TrimSpace(r.FormValue("device_id"))
			secret := strings.TrimSpace(r.FormValue("secret"))
			switch {
			case data.DeviceID == "" || secret == "":
				data.Error = "设备 ID 和密钥不能为空"
			default:
				bindErr := h.store.BindDevice(data.DeviceID, secret, user.ID)
				switch {
				case bindErr == nil:
					data.Message = "设备绑定成功。"
				case errors.Is(bindErr, ErrDeviceSecretInvalid):
					data.Error = "设备密钥错误"
				case errors.Is(bindErr, ErrDeviceNotFound):
					data.Error = "设备不存在"
				default:
					writePageError(w, http.StatusInternalServerError, "绑定失败")
					return
				}
			}
		}
	}
	devices, err := h.store.BoundDevicesForUser(user.ID)
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "无法读取设备列表")
		return
	}
	for _, device := range devices {
		data.Devices = append(data.Devices, boundDevicePageData{
			DeviceID: device.DeviceID,
			Name:     device.Name,
			BoundAt:  device.BoundAt.In(h.store.loc).Format("2006-01-02 15:04"),
		})
	}
	renderPage(w, http.StatusOK, "bind.html", data)
}

func (h *apiHandler) requirePageUser(w http.ResponseWriter, r *http.Request) (User, bool) {
	user, ok, err := h.currentUser(r)
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "无法读取登录状态")
		return User{}, false
	}
	if ok {
		return user, true
	}
	next := r.URL.RequestURI()
	location := loginPath + "?next=" + url.QueryEscape(safeNext(next))
	http.Redirect(w, r, location, http.StatusSeeOther)
	return User{}, false
}

func parsePageForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := r.ParseForm(); err != nil {
		return errors.New("表单无效")
	}
	return nil
}

func parseHistoryQuery(r *http.Request) (historyQuery, error) {
	values := r.URL.Query()
	page := 1
	if raw := values.Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return historyQuery{}, errors.New("page must be a positive integer")
		}
		page = parsed
	}
	search := strings.TrimSpace(values.Get("q"))
	if search == "" {
		search = strings.TrimSpace(values.Get("search"))
	}
	return historyQuery{
		Day:      strings.TrimSpace(values.Get("day")),
		App:      strings.TrimSpace(values.Get("app")),
		DeviceID: strings.TrimSpace(values.Get("device_id")),
		Search:   search,
		Page:     page,
		PageSize: historyPageSize,
	}, nil
}

func authorizationRequestFromValues(values url.Values) (authorizationRequest, error) {
	request := authorizationRequest{
		ResponseType:        strings.TrimSpace(values.Get("response_type")),
		ClientID:            strings.TrimSpace(values.Get("client_id")),
		RedirectURI:         values.Get("redirect_uri"),
		CodeChallenge:       strings.TrimSpace(values.Get("code_challenge")),
		CodeChallengeMethod: strings.TrimSpace(values.Get("code_challenge_method")),
		State:               values.Get("state"),
	}
	switch {
	case request.ResponseType != "code":
		return request, errors.New("response_type must be code")
	case request.ClientID == "":
		return request, errors.New("client_id is required")
	case request.RedirectURI == "" || !validRedirectURI(request.RedirectURI):
		return request, errors.New("redirect_uri is not allowed")
	case request.CodeChallenge == "":
		return request, errors.New("code_challenge is required")
	case request.CodeChallengeMethod != "S256":
		return request, errors.New("code_challenge_method must be S256")
	default:
		return request, nil
	}
}

func validRedirectURI(raw string) bool {
	if raw == oobRedirectURI {
		return true
	}
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Fragment != "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "http" || scheme == "https" {
		if parsed.Host == "" || parsed.User != nil || parsed.Hostname() == "" {
			return false
		}
		if scheme == "https" {
			return true
		}
		host := strings.ToLower(parsed.Hostname())
		return host == "localhost" || host == "127.0.0.1"
	}
	return true
}

func historyPageURL(values url.Values, page int) string {
	next := make(url.Values, len(values)+1)
	for key, items := range values {
		next[key] = append([]string(nil), items...)
	}
	next.Set("page", strconv.Itoa(page))
	return "/history?" + next.Encode()
}

func pageMessages(loc *time.Location, messages []MessageRecord) []messagePageData {
	result := make([]messagePageData, 0, len(messages))
	for _, message := range messages {
		result = append(result, messagePageData{
			Time:    time.Unix(message.TS, 0).In(loc).Format("01-02 15:04:05"),
			App:     message.App,
			AppName: message.AppName,
			Sender:  message.Sender,
			Chat:    message.Chat,
			Content: message.Content,
		})
	}
	return result
}

func safeNext(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	return raw
}

func renderPage(w http.ResponseWriter, status int, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = webTemplates.ExecuteTemplate(w, name, data)
}

func renderStandaloneError(w http.ResponseWriter, status int, message, name string, data pageData) {
	data.Error = message
	renderPage(w, status, name, data)
}

func writePageError(w http.ResponseWriter, status int, message string) {
	renderPage(w, status, "home.html", pageData{Title: "Sender", Error: message})
}
