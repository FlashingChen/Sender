package server

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testSecretA = "0123456789abcdef0123456789abcdef"
	testSecretB = "fedcba9876543210fedcba9876543210"
)

type testApp struct {
	httpServer *httptest.Server
	store      *Store
	location   *time.Location
	userID     int64
}

var testAccessTokens sync.Map

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	return newTestAppAt(t, time.FixedZone("Asia/Shanghai", 8*60*60))
}

func newTestAppAt(t *testing.T, location *time.Location) *testApp {
	t.Helper()
	return newTestAppWithOptions(t, location, HandlerOptions{
		AllowRegistration: registrationAllowedFromEnv(),
		AuthRateLimiter:   NewRateLimiter(authRateLimit, authRateWindow),
	})
}

func newTestAppWithOptions(t *testing.T, location *time.Location, options HandlerOptions) *testApp {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "messages.db"), location)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	user, err := store.RegisterUser("test-user", "test-password")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	accessToken, err := store.IssueAccessToken(user.ID, accessTokenTTL)
	if err != nil {
		t.Fatalf("create test access token: %v", err)
	}
	httpServer := httptest.NewServer(NewHandlerWithOptions(store, options))
	testAccessTokens.Store(serverHost(httpServer.URL), accessToken)
	app := &testApp{httpServer: httpServer, store: store, location: location, userID: user.ID}
	t.Cleanup(func() {
		testAccessTokens.Delete(serverHost(httpServer.URL))
		httpServer.Close()
		if err := store.Close(); err != nil {
			t.Errorf("close test store: %v", err)
		}
	})
	return app
}

func serverHost(rawURL string) string {
	parsed, _ := url.Parse(rawURL)
	return parsed.Host
}

func (a *testApp) url(path string) string {
	return a.httpServer.URL + path
}

func registerTestDevice(t *testing.T, app *testApp, deviceID, secret, name string) {
	t.Helper()
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": secret,
	}, registerRequest{DeviceID: deviceID, Name: name})
	if status != http.StatusOK {
		t.Fatalf("register status=%d body=%s", status, body)
	}
	if err := app.store.BindDevice(deviceID, secret, app.userID); err != nil {
		t.Fatalf("bind test device: %v", err)
	}
}

func uploadTestMessages(t *testing.T, app *testApp, deviceID, secret string, messages []MessageInput) (int, []byte) {
	t.Helper()
	return jsonRequest(t, http.MethodPost, app.url(apiPrefix+"/devices/"+deviceID+"/messages"), map[string]string{
		"Authorization": "Bearer " + secret,
	}, messageBatchRequest{Messages: messages})
}

func messageFor(id string, ts int64) MessageInput {
	return MessageInput{
		ClientMsgID: id,
		App:         "com.tencent.mm",
		AppName:     "微信",
		Chat:        "张三",
		Sender:      "张三",
		Content:     id,
		TS:          ts,
	}
}

func jsonRequest(t *testing.T, method, url string, headers map[string]string, value any) (int, []byte) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return rawRequest(t, method, url, headers, encoded)
}
func rawRequest(t *testing.T, method, url string, headers map[string]string, body []byte) (int, []byte) {
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if request.Header.Get("Authorization") == "" &&
		(method == http.MethodGet && (request.URL.Path == messagesPath || request.URL.Path == appsPath)) {
		if token, ok := testAccessTokens.Load(request.URL.Host); ok {
			request.Header.Set("Authorization", "Bearer "+token.(string))
		}
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return response.StatusCode, responseBody
}

func decodeResponse[T any](t *testing.T, body []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode response %q: %v", body, err)
	}
	return value
}

func dayFor(location *time.Location, ts int64) string {
	return time.Unix(ts, 0).In(location).Format("2006-01-02")
}

func TestHealthEndpoint(t *testing.T) {
	app := newTestApp(t)
	status, body := rawRequest(t, http.MethodGet, app.url(healthPath), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[map[string]bool](t, body)
	if !response["ok"] {
		t.Fatalf("health response=%v", response)
	}
}

func TestAPIHealthEndpoint(t *testing.T) {
	app := newTestApp(t)
	status, body := rawRequest(t, http.MethodGet, app.url(apiHealthPath), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if response := decodeResponse[map[string]bool](t, body); !response["ok"] {
		t.Fatalf("health response=%v", response)
	}
}

func TestRegisterDevice(t *testing.T) {
	app := newTestApp(t)
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretA,
	}, registerRequest{DeviceID: "device-1", Name: "Pixel 8"})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if response := decodeResponse[map[string]bool](t, body); !response["ok"] {
		t.Fatalf("register response=%v", response)
	}
}

func TestRegisterDeviceIsIdempotent(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Old name")

	// Same-secret re-registration refreshes the name without touching binding.
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretA,
	}, registerRequest{DeviceID: "device-1", Name: "New name"})
	if status != http.StatusOK {
		t.Fatalf("same-secret re-register status=%d body=%s", status, body)
	}
	var name string
	if err := app.store.db.QueryRow(`SELECT name FROM devices WHERE device_id = ?`, "device-1").Scan(&name); err != nil {
		t.Fatalf("read device: %v", err)
	}
	if name != "New name" {
		t.Fatalf("device name=%q, want New name", name)
	}

	// A different secret must be rejected and must not rotate the stored one.
	status, body = jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretB,
	}, registerRequest{DeviceID: "device-1", Name: "Squatter"})
	if status != http.StatusConflict {
		t.Fatalf("conflicting re-register status=%d body=%s", status, body)
	}
	status, _ = uploadTestMessages(t, app, "device-1", testSecretA, []MessageInput{messageFor("original-secret", 1780000000)})
	if status != http.StatusOK {
		t.Fatalf("original secret status=%d, want 200", status)
	}
}

func TestMessageIngestion(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, []MessageInput{
		messageFor("msg-1", 1780000000),
		messageFor("msg-2", 1780000001),
		messageFor("msg-3", 1780000002),
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[messageBatchResponse](t, body)
	if response.Inserted != 3 || response.Duplicates != 0 {
		t.Fatalf("response=%+v", response)
	}
}

func TestDuplicateMessages(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := []MessageInput{messageFor("msg-1", 1780000000), messageFor("msg-2", 1780000001), messageFor("msg-3", 1780000002)}
	status, _ := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("first upload status=%d", status)
	}
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("second upload status=%d body=%s", status, body)
	}
	response := decodeResponse[messageBatchResponse](t, body)
	if response.Inserted != 0 || response.Duplicates != 3 {
		t.Fatalf("response=%+v", response)
	}
}

func TestUnauthorizedMessageUpload(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	status, body := uploadTestMessages(t, app, "device-1", testSecretB, []MessageInput{messageFor("msg-1", 1780000000)})
	if status != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[errorResponse](t, body)
	if response.Error == "" {
		t.Fatal("unauthorized response has no error")
	}
}

func TestMissingAuthorization(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	status, body := jsonRequest(t, http.MethodPost, app.url(apiPrefix+"/devices/device-1/messages"), nil, messageBatchRequest{})
	if status != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", status, body)
	}
}

func TestBatchTooLarge(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := make([]MessageInput, 501)
	for index := range messages {
		messages[index] = messageFor("msg-"+strconv.Itoa(index), int64(1780000000+index))
	}
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if response := decodeResponse[errorResponse](t, body); response.Error == "" {
		t.Fatal("batch error response has no error")
	}
}

func TestInvalidJSON(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	status, body := rawRequest(t, http.MethodPost, app.url(apiPrefix+"/devices/device-1/messages"), map[string]string{
		"Authorization": "Bearer " + testSecretA,
	}, []byte(`{"messages":[`))
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if response := decodeResponse[errorResponse](t, body); response.Error == "" {
		t.Fatal("invalid JSON response has no error")
	}
}

func TestUnknownPath(t *testing.T) {
	app := newTestApp(t)
	status, body := rawRequest(t, http.MethodGet, app.url(apiPrefix+"/unknown"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if response := decodeResponse[errorResponse](t, body); response.Error == "" {
		t.Fatal("unknown path response has no error")
	}
}

func TestMessageDayFilterAndAscending(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := []MessageInput{
		messageFor("late", 1780000002),
		messageFor("early", 1780000000),
		messageFor("middle", 1780000001),
	}
	status, _ := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("upload status=%d", status)
	}
	status, body := rawRequest(t, http.MethodGet, app.url(messagesPath+"?day="+dayFor(app.location, 1780000000)), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[messagesResponse](t, body)
	if len(response.Messages) != 3 {
		t.Fatalf("message count=%d", len(response.Messages))
	}
	wantIDs := []string{"early", "middle", "late"}
	for index, wantID := range wantIDs {
		if response.Messages[index].ClientMsgID != wantID {
			t.Fatalf("message[%d]=%q, want %q", index, response.Messages[index].ClientMsgID, wantID)
		}
	}
	if response.NextCursor != "" {
		t.Fatalf("next_cursor=%q, want empty", response.NextCursor)
	}
}

func TestMessageCursorPagination(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := []MessageInput{messageFor("msg-1", 1780000000), messageFor("msg-2", 1780000001), messageFor("msg-3", 1780000002)}
	status, _ := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("upload status=%d", status)
	}
	day := dayFor(app.location, messages[0].TS)
	status, body := rawRequest(t, http.MethodGet, app.url(messagesPath+"?day="+day+"&limit=2"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("first status=%d body=%s", status, body)
	}
	first := decodeResponse[messagesResponse](t, body)
	if len(first.Messages) != 2 || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	status, body = rawRequest(t, http.MethodGet, app.url(messagesPath+"?day="+day+"&limit=2&cursor="+first.NextCursor), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("second status=%d body=%s", status, body)
	}
	second := decodeResponse[messagesResponse](t, body)
	if len(second.Messages) != 1 || second.NextCursor != "" {
		t.Fatalf("second page=%+v", second)
	}
	if first.Messages[0].ID == second.Messages[0].ID || first.Messages[1].ID == second.Messages[0].ID {
		t.Fatalf("page repeated message: first=%+v second=%+v", first.Messages, second.Messages)
	}
}

func TestLimitIsCappedAt500(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := make([]MessageInput, 500)
	for index := range messages {
		messages[index] = messageFor("msg-"+strconv.Itoa(index), int64(1780000000+index))
	}
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", status, body)
	}
	if response := decodeResponse[messageBatchResponse](t, body); response.Inserted != 500 {
		t.Fatalf("upload response=%+v", response)
	}
	day := dayFor(app.location, messages[0].TS)
	status, body = rawRequest(t, http.MethodGet, app.url(messagesPath+"?day="+day+"&limit=600"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[messagesResponse](t, body)
	if len(response.Messages) != 500 {
		t.Fatalf("message count=%d, want 500", len(response.Messages))
	}
}

func TestRecentMessagesWhenDayOmitted(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := []MessageInput{
		messageFor("old", 1780000000),
		messageFor("newer", 1780000001),
		messageFor("newest", 1780000002),
	}
	status, _ := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("upload status=%d", status)
	}
	status, body := rawRequest(t, http.MethodGet, app.url(messagesPath+"?limit=2"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[messagesResponse](t, body)
	if len(response.Messages) != 2 {
		t.Fatalf("message count=%d", len(response.Messages))
	}
	if response.Messages[0].ClientMsgID != "newer" || response.Messages[1].ClientMsgID != "newest" {
		t.Fatalf("recent messages=%+v", response.Messages)
	}
}

func TestMessageFiltersByDeviceAndApp(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	registerTestDevice(t, app, "device-2", testSecretB, "Other")
	first := messageFor("first", 1780000000)
	second := messageFor("second", 1780000001)
	second.App = "com.example.other"
	if status, _ := uploadTestMessages(t, app, "device-1", testSecretA, []MessageInput{first, second}); status != http.StatusOK {
		t.Fatalf("first upload status=%d", status)
	}
	third := messageFor("third", 1780000002)
	if status, _ := uploadTestMessages(t, app, "device-2", testSecretB, []MessageInput{third}); status != http.StatusOK {
		t.Fatalf("second upload status=%d", status)
	}
	day := dayFor(app.location, first.TS)
	path := messagesPath + "?day=" + day + "&device_id=device-1&app=com.tencent.mm"
	status, body := rawRequest(t, http.MethodGet, app.url(path), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[messagesResponse](t, body)
	if len(response.Messages) != 1 || response.Messages[0].ClientMsgID != "first" {
		t.Fatalf("filtered response=%+v", response)
	}
}

func TestTimezoneDerivation(t *testing.T) {
	ts := time.Date(2026, time.January, 1, 16, 0, 0, 0, time.UTC).Unix()
	utcApp := newTestAppAt(t, time.UTC)
	shanghaiApp := newTestAppAt(t, time.FixedZone("Asia/Shanghai", 8*60*60))
	if _, _, err := utcApp.store.InsertMessages("utc-device", []MessageInput{messageFor("utc", ts)}); err != nil {
		t.Fatalf("insert UTC message: %v", err)
	}
	if _, _, err := shanghaiApp.store.InsertMessages("sh-device", []MessageInput{messageFor("shanghai", ts)}); err != nil {
		t.Fatalf("insert Shanghai message: %v", err)
	}
	utcMessages, _, err := utcApp.store.QueryMessages(messageQuery{Day: "2026-01-01", Limit: 100})
	if err != nil {
		t.Fatalf("query UTC message: %v", err)
	}
	shanghaiMessages, _, err := shanghaiApp.store.QueryMessages(messageQuery{Day: "2026-01-02", Limit: 100})
	if err != nil {
		t.Fatalf("query Shanghai message: %v", err)
	}
	if len(utcMessages) != 1 || utcMessages[0].Day != "2026-01-01" {
		t.Fatalf("UTC messages=%+v", utcMessages)
	}
	if len(shanghaiMessages) != 1 || shanghaiMessages[0].Day != "2026-01-02" {
		t.Fatalf("Shanghai messages=%+v", shanghaiMessages)
	}
}

func TestAppsAggregation(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	first := messageFor("first", 1780000000)
	second := messageFor("second", 1780000002)
	other := messageFor("other", 1780000001)
	other.App = "com.example.other"
	other.AppName = "Other"
	status, _ := uploadTestMessages(t, app, "device-1", testSecretA, []MessageInput{first, second, other})
	if status != http.StatusOK {
		t.Fatalf("upload status=%d", status)
	}
	day := dayFor(app.location, first.TS)
	status, body := rawRequest(t, http.MethodGet, app.url(appsPath+"?day="+day), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[[]AppSummary](t, body)
	if len(response) != 2 {
		t.Fatalf("apps=%+v", response)
	}
	var wechat AppSummary
	for _, summary := range response {
		if summary.App == "com.tencent.mm" {
			wechat = summary
		}
	}
	if wechat.Count != 2 || wechat.LastTS != 1780000002 || wechat.AppName != "微信" {
		t.Fatalf("wechat summary=%+v", wechat)
	}
}

func TestAppsFiltersByDayAndDevice(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	registerTestDevice(t, app, "device-2", testSecretB, "Other")
	first := messageFor("first", 1780000000)
	if status, _ := uploadTestMessages(t, app, "device-1", testSecretA, []MessageInput{first}); status != http.StatusOK {
		t.Fatalf("first upload status=%d", status)
	}
	second := messageFor("second", 1780000001)
	if status, _ := uploadTestMessages(t, app, "device-2", testSecretB, []MessageInput{second}); status != http.StatusOK {
		t.Fatalf("second upload status=%d", status)
	}
	day := dayFor(app.location, first.TS)
	status, body := rawRequest(t, http.MethodGet, app.url(appsPath+"?day="+day+"&device_id=device-1"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[[]AppSummary](t, body)
	if len(response) != 1 || response[0].Count != 1 {
		t.Fatalf("filtered apps=%+v", response)
	}
}

func TestRegisterSecretValidation(t *testing.T) {
	app := newTestApp(t)
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": "not-a-secret",
	}, registerRequest{DeviceID: "device-1", Name: "Pixel 8"})
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if response := decodeResponse[errorResponse](t, body); response.Error == "" {
		t.Fatal("secret validation response has no error")
	}
}

func TestLimitValidation(t *testing.T) {
	app := newTestApp(t)
	status, body := rawRequest(t, http.MethodGet, app.url(messagesPath+"?limit=0"), nil, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if response := decodeResponse[errorResponse](t, body); response.Error == "" {
		t.Fatal("limit validation response has no error")
	}
}

func TestEmptyMessageBatch(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	response := decodeResponse[messageBatchResponse](t, body)
	if response.Inserted != 0 || response.Duplicates != 0 {
		t.Fatalf("response=%+v", response)
	}
}

func TestMessageValidationDoesNotPartiallyInsert(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	valid := messageFor("valid", 1780000000)
	invalid := messageFor("", 1780000001)
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, []MessageInput{valid, invalid})
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, body)
	}
	status, body = rawRequest(t, http.MethodGet, app.url(messagesPath), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("query status=%d body=%s", status, body)
	}
	response := decodeResponse[messagesResponse](t, body)
	if len(response.Messages) != 0 {
		t.Fatalf("messages after rejected batch=%+v", response.Messages)
	}
}

func TestMessageCursorHandlesOutOfOrderTimestamps(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := []MessageInput{
		messageFor("ts-30", 30),
		messageFor("ts-10", 10),
		messageFor("ts-20", 20),
	}
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", status, body)
	}

	seen := make(map[string]bool, len(messages))
	cursor := ""
	finished := false
	day := dayFor(app.location, messages[0].TS)
	for page := range len(messages) + 1 {
		path := messagesPath + "?day=" + day + "&limit=1"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		status, body = rawRequest(t, http.MethodGet, app.url(path), nil, nil)
		if status != http.StatusOK {
			t.Fatalf("page %d status=%d body=%s", page+1, status, body)
		}
		response := decodeResponse[messagesResponse](t, body)
		if len(response.Messages) != 1 {
			t.Fatalf("page %d messages=%+v", page+1, response.Messages)
		}
		for _, message := range response.Messages {
			if seen[message.ClientMsgID] {
				t.Fatalf("duplicate message %q on page %d", message.ClientMsgID, page+1)
			}
			seen[message.ClientMsgID] = true
		}
		if response.NextCursor == "" {
			finished = true
			break
		}
		cursor = response.NextCursor
	}
	if !finished {
		t.Fatal("pagination did not finish")
	}
	if len(seen) != len(messages) {
		t.Fatalf("seen %d messages, want %d: %v", len(seen), len(messages), seen)
	}
}

func TestMessageCursorHandlesEqualTimestamps(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := []MessageInput{
		messageFor("same-1", 1780000000),
		messageFor("same-2", 1780000000),
		messageFor("same-3", 1780000000),
	}
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", status, body)
	}

	seen := make(map[string]bool, len(messages))
	day := dayFor(app.location, messages[0].TS)
	status, body = rawRequest(t, http.MethodGet, app.url(messagesPath+"?day="+day+"&limit=2"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("first page status=%d body=%s", status, body)
	}
	first := decodeResponse[messagesResponse](t, body)
	if len(first.Messages) != 2 || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	for _, message := range first.Messages {
		seen[message.ClientMsgID] = true
	}

	status, body = rawRequest(t, http.MethodGet, app.url(messagesPath+"?day="+day+"&limit=2&cursor="+first.NextCursor), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("second page status=%d body=%s", status, body)
	}
	second := decodeResponse[messagesResponse](t, body)
	if len(second.Messages) != 1 || second.NextCursor != "" {
		t.Fatalf("second page=%+v", second)
	}
	for _, message := range second.Messages {
		if seen[message.ClientMsgID] {
			t.Fatalf("duplicate message %q", message.ClientMsgID)
		}
		seen[message.ClientMsgID] = true
	}
	if len(seen) != len(messages) {
		t.Fatalf("seen %d messages, want %d: %v", len(seen), len(messages), seen)
	}
}

type capturedResponse struct {
	status  int
	header  http.Header
	body    []byte
	cookies []*http.Cookie
}

func doCapturedRequest(t *testing.T, method, target string, headers map[string]string, body []byte, cookies []*http.Cookie) capturedResponse {
	t.Helper()
	request, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build captured request: %v", err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send captured request: %v", err)
	}
	defer response.Body.Close()
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read captured response: %v", err)
	}
	return capturedResponse{
		status:  response.StatusCode,
		header:  response.Header.Clone(),
		body:    bodyBytes,
		cookies: response.Cookies(),
	}
}

func formCapturedRequest(t *testing.T, method, target string, values url.Values, cookies []*http.Cookie) capturedResponse {
	return doCapturedRequest(t, method, target, map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, []byte(values.Encode()), cookies)
}

func jsonCapturedRequest(t *testing.T, method, target string, headers map[string]string, value any, cookies []*http.Cookie) capturedResponse {
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal captured request: %v", err)
	}
	if headers == nil {
		headers = make(map[string]string)
	}
	headers["Content-Type"] = "application/json"
	return doCapturedRequest(t, method, target, headers, encoded, cookies)
}

func registerWebAccount(t *testing.T, app *testApp, username string) capturedResponse {
	t.Helper()
	response := formCapturedRequest(t, http.MethodPost, app.url(registerPagePath), url.Values{
		"username": {username},
		"password": {"correct-password"},
	}, nil)
	if response.status != http.StatusSeeOther {
		t.Fatalf("web registration status=%d body=%s", response.status, response.body)
	}
	return response
}

func loginWebAccount(t *testing.T, app *testApp, username string) []*http.Cookie {
	t.Helper()
	registerWebAccount(t, app, username)
	response := formCapturedRequest(t, http.MethodPost, app.url(loginPath), url.Values{
		"username": {username},
		"password": {"correct-password"},
	}, nil)
	if response.status != http.StatusSeeOther {
		t.Fatalf("web login status=%d body=%s", response.status, response.body)
	}
	if len(response.cookies) != 1 || response.cookies[0].HttpOnly != true || response.cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie=%v", response.cookies)
	}
	return response.cookies
}

const testCodeVerifier = "aGVsbG8td29ybGQtMjAyNi1wa2NlLXZlcmlmaWVyLXNlY3JldC1sb25nLXZhbHVlLXhh"

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// loginAsTestUser logs in the account every newTestApp creates.
func loginAsTestUser(t *testing.T, app *testApp) []*http.Cookie {
	t.Helper()
	response := formCapturedRequest(t, http.MethodPost, app.url(loginPath), url.Values{
		"username": {"test-user"},
		"password": {"test-password"},
	}, nil)
	if response.status != http.StatusSeeOther || len(response.cookies) != 1 {
		t.Fatalf("test user login status=%d cookies=%v", response.status, response.cookies)
	}
	return response.cookies
}

func authorizeQuery(redirectURI, state string) string {
	values := url.Values{
		"response_type":         {"code"},
		"client_id":             {"test-agent"},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {pkceChallenge(testCodeVerifier)},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return "?" + values.Encode()
}

func authorizeConsent(t *testing.T, app *testApp, redirectURI, state string, cookies []*http.Cookie) capturedResponse {
	t.Helper()
	return doCapturedRequest(t, http.MethodGet, app.url(authorizePath+authorizeQuery(redirectURI, state)), nil, nil, cookies)
}

func approveAuthorization(t *testing.T, app *testApp, redirectURI, state, action string, cookies []*http.Cookie) capturedResponse {
	t.Helper()
	return formCapturedRequest(t, http.MethodPost, app.url(authorizePath), url.Values{
		"response_type":         {"code"},
		"client_id":             {"test-agent"},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {pkceChallenge(testCodeVerifier)},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"action":                {action},
	}, cookies)
}

func exchangeCode(t *testing.T, app *testApp, code, verifier, clientID, redirectURI string) capturedResponse {
	t.Helper()
	return formCapturedRequest(t, http.MethodPost, app.url(tokenPath), url.Values{
		"grant_type":    {authorizationCodeGrant},
		"code":          {code},
		"code_verifier": {verifier},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
	}, nil)
}

func codeFromLocation(t *testing.T, location string) string {
	t.Helper()
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location %q: %v", location, err)
	}
	return parsed.Query().Get("code")
}

func TestAccountRegistrationAndDuplicate(t *testing.T) {
	app := newTestApp(t)
	first := registerWebAccount(t, app, "alice")
	if first.status != http.StatusSeeOther {
		t.Fatalf("first registration status=%d", first.status)
	}
	duplicate := formCapturedRequest(t, http.MethodPost, app.url(registerPagePath), url.Values{
		"username": {"alice"},
		"password": {"another-password"},
	}, nil)
	if duplicate.status != http.StatusBadRequest || !strings.Contains(string(duplicate.body), "用户名已存在") {
		t.Fatalf("duplicate registration status=%d body=%s", duplicate.status, duplicate.body)
	}
}

func TestLoginSuccessAndWrongPassword(t *testing.T) {
	app := newTestApp(t)
	registerWebAccount(t, app, "bob")
	wrong := formCapturedRequest(t, http.MethodPost, app.url(loginPath), url.Values{
		"username": {"bob"},
		"password": {"wrong-password"},
	}, nil)
	if wrong.status != http.StatusUnauthorized {
		t.Fatalf("wrong login status=%d body=%s", wrong.status, wrong.body)
	}
	correct := formCapturedRequest(t, http.MethodPost, app.url(loginPath), url.Values{
		"username": {"bob"},
		"password": {"correct-password"},
	}, nil)
	if correct.status != http.StatusSeeOther || len(correct.cookies) != 1 {
		t.Fatalf("correct login status=%d cookies=%v", correct.status, correct.cookies)
	}
	if correct.cookies[0].MaxAge != int(sessionTTL/time.Second) {
		t.Fatalf("session max age=%d", correct.cookies[0].MaxAge)
	}
}

func TestProtectedAuthorizationRedirectsToLogin(t *testing.T) {
	app := newTestApp(t)
	response := doCapturedRequest(t, http.MethodGet, app.url(authorizePath+authorizeQuery("http://127.0.0.1:9999/cb", "s1")), nil, nil, nil)
	if response.status != http.StatusSeeOther {
		t.Fatalf("authorize redirect status=%d body=%s", response.status, response.body)
	}
	if !strings.HasPrefix(response.header.Get("Location"), loginPath+"?next=") {
		t.Fatalf("authorize redirect location=%q", response.header.Get("Location"))
	}
}

func TestOAuthCodeFullFlow(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	redirectURI := "http://127.0.0.1:9999/callback"
	consent := authorizeConsent(t, app, redirectURI, "state-1", cookies)
	if consent.status != http.StatusOK || !strings.Contains(string(consent.body), "test-agent") {
		t.Fatalf("consent status=%d body=%s", consent.status, consent.body)
	}
	approved := approveAuthorization(t, app, redirectURI, "state-1", "approve", cookies)
	if approved.status != http.StatusFound {
		t.Fatalf("approval status=%d body=%s", approved.status, approved.body)
	}
	location := approved.header.Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse approval location %q: %v", location, err)
	}
	code := parsed.Query().Get("code")
	if len(code) != 32 {
		t.Fatalf("code length=%d, want 32 hex chars", len(code))
	}
	if parsed.Query().Get("state") != "state-1" {
		t.Fatalf("state=%q, want state-1", parsed.Query().Get("state"))
	}
	exchanged := exchangeCode(t, app, code, testCodeVerifier, "test-agent", redirectURI)
	if exchanged.status != http.StatusOK {
		t.Fatalf("token status=%d body=%s", exchanged.status, exchanged.body)
	}
	token := decodeResponse[tokenResponse](t, exchanged.body)
	if token.AccessToken == "" || token.TokenType != "Bearer" || token.ExpiresIn != int(accessTokenTTL/time.Second) {
		t.Fatalf("token response=%+v", token)
	}
	read := doCapturedRequest(t, http.MethodGet, app.url(messagesPath), map[string]string{
		"Authorization": "Bearer " + token.AccessToken,
	}, nil, nil)
	if read.status != http.StatusOK {
		t.Fatalf("authenticated messages status=%d body=%s", read.status, read.body)
	}
}

func TestOAuthPKCEMismatch(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	redirectURI := "http://127.0.0.1:9999/callback"
	approved := approveAuthorization(t, app, redirectURI, "", "approve", cookies)
	code := codeFromLocation(t, approved.header.Get("Location"))
	response := exchangeCode(t, app, code, testCodeVerifier+"different", "test-agent", redirectURI)
	if response.status != http.StatusBadRequest || decodeResponse[errorResponse](t, response.body).Error != "invalid_grant" {
		t.Fatalf("PKCE mismatch status=%d body=%s", response.status, response.body)
	}
}

func TestOAuthCodeReuse(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	redirectURI := "http://127.0.0.1:9999/callback"
	approved := approveAuthorization(t, app, redirectURI, "", "approve", cookies)
	code := codeFromLocation(t, approved.header.Get("Location"))
	first := exchangeCode(t, app, code, testCodeVerifier, "test-agent", redirectURI)
	if first.status != http.StatusOK {
		t.Fatalf("first token status=%d body=%s", first.status, first.body)
	}
	second := exchangeCode(t, app, code, testCodeVerifier, "test-agent", redirectURI)
	if second.status != http.StatusBadRequest || decodeResponse[errorResponse](t, second.body).Error != "invalid_grant" {
		t.Fatalf("reused code status=%d body=%s", second.status, second.body)
	}
}

func TestOAuthCodeExpired(t *testing.T) {
	app := newTestApp(t)
	code, err := app.store.CreateAuthorizationCode(
		"test-agent",
		app.userID,
		"http://127.0.0.1:9999/callback",
		pkceChallenge(testCodeVerifier),
		-time.Second,
	)
	if err != nil {
		t.Fatalf("create expired code: %v", err)
	}
	response := exchangeCode(t, app, code, testCodeVerifier, "test-agent", "http://127.0.0.1:9999/callback")
	if response.status != http.StatusBadRequest || decodeResponse[errorResponse](t, response.body).Error != "invalid_grant" {
		t.Fatalf("expired code status=%d body=%s", response.status, response.body)
	}
}

func TestOAuthRedirectMismatch(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	approved := approveAuthorization(t, app, "http://127.0.0.1:9999/callback", "", "approve", cookies)
	code := codeFromLocation(t, approved.header.Get("Location"))
	response := exchangeCode(t, app, code, testCodeVerifier, "test-agent", "http://127.0.0.1:9998/other")
	if response.status != http.StatusBadRequest || decodeResponse[errorResponse](t, response.body).Error != "invalid_grant" {
		t.Fatalf("redirect mismatch status=%d body=%s", response.status, response.body)
	}
}

func TestOAuthClientMismatch(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	approved := approveAuthorization(t, app, "http://127.0.0.1:9999/callback", "", "approve", cookies)
	code := codeFromLocation(t, approved.header.Get("Location"))
	response := exchangeCode(t, app, code, testCodeVerifier, "other-agent", "http://127.0.0.1:9999/callback")
	if response.status != http.StatusBadRequest || decodeResponse[errorResponse](t, response.body).Error != "invalid_grant" {
		t.Fatalf("client mismatch status=%d body=%s", response.status, response.body)
	}
}

func TestOAuthOOBPage(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	consent := authorizeConsent(t, app, oobRedirectURI, "", cookies)
	if consent.status != http.StatusOK {
		t.Fatalf("OOB consent status=%d body=%s", consent.status, consent.body)
	}
	approved := approveAuthorization(t, app, oobRedirectURI, "", "approve", cookies)
	if approved.status != http.StatusOK {
		t.Fatalf("OOB approval status=%d body=%s", approved.status, approved.body)
	}
	codeMatch := regexp.MustCompile(`[0-9a-f]{32}`).FindString(string(approved.body))
	if len(codeMatch) != 32 {
		t.Fatalf("OOB page has no 32-hex code: body=%s", approved.body)
	}
	exchanged := exchangeCode(t, app, codeMatch, testCodeVerifier, "test-agent", oobRedirectURI)
	if exchanged.status != http.StatusOK {
		t.Fatalf("OOB token status=%d body=%s", exchanged.status, exchanged.body)
	}
	if decodeResponse[tokenResponse](t, exchanged.body).AccessToken == "" {
		t.Fatalf("OOB token response=%s", exchanged.body)
	}
}

func TestOAuthLoopbackAllowed(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	for _, uri := range []string{"http://localhost:8080/cb", "http://127.0.0.1/cb", "http://localhost/cb?x=1"} {
		consent := authorizeConsent(t, app, uri, "", cookies)
		if consent.status != http.StatusOK {
			t.Fatalf("loopback %q status=%d body=%s", uri, consent.status, consent.body)
		}
	}
}

func TestOAuthCustomSchemeAllowed(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	consent := authorizeConsent(t, app, "sender://agent.example.com/callback", "s2", cookies)
	if consent.status != http.StatusOK || !strings.Contains(string(consent.body), "sender://agent.example.com/callback") {
		t.Fatalf("custom scheme status=%d body=%s", consent.status, consent.body)
	}
}

func TestOAuthRemoteHTTPRejected(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	consent := authorizeConsent(t, app, "http://evil.example.com/cb", "", cookies)
	if consent.status != http.StatusBadRequest || !strings.Contains(string(consent.body), "redirect_uri") {
		t.Fatalf("remote http status=%d body=%s", consent.status, consent.body)
	}
}

func TestOAuthMissingCodeChallengeRejected(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	base := url.Values{
		"response_type":         {"code"},
		"client_id":             {"test-agent"},
		"redirect_uri":          {"http://127.0.0.1:9999/cb"},
		"code_challenge_method": {"S256"},
	}
	noChallenge := doCapturedRequest(t, http.MethodGet, app.url(authorizePath+"?"+base.Encode()), nil, nil, cookies)
	if noChallenge.status != http.StatusBadRequest {
		t.Fatalf("missing challenge status=%d body=%s", noChallenge.status, noChallenge.body)
	}
	noMethod := make(url.Values)
	for key, entries := range base {
		noMethod[key] = entries
	}
	noMethod.Del("code_challenge_method")
	noMethod.Set("code_challenge", pkceChallenge(testCodeVerifier))
	response := doCapturedRequest(t, http.MethodGet, app.url(authorizePath+"?"+noMethod.Encode()), nil, nil, cookies)
	if response.status != http.StatusBadRequest {
		t.Fatalf("missing method status=%d body=%s", response.status, response.body)
	}
}

func TestOAuthDenyAccessDenied(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	redirectURI := "http://127.0.0.1:9999/callback"
	denied := approveAuthorization(t, app, redirectURI, "state-deny", "deny", cookies)
	if denied.status != http.StatusFound {
		t.Fatalf("deny status=%d body=%s", denied.status, denied.body)
	}
	location, err := url.Parse(denied.header.Get("Location"))
	if err != nil {
		t.Fatalf("parse deny location: %v", err)
	}
	if location.Query().Get("error") != "access_denied" || location.Query().Get("state") != "state-deny" {
		t.Fatalf("deny location=%q", denied.header.Get("Location"))
	}
}

func TestReadWithoutTokenIsUnauthorized(t *testing.T) {
	app := newTestApp(t)
	response := doCapturedRequest(t, http.MethodGet, app.url(messagesPath), nil, nil, nil)
	if response.status != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d body=%s", response.status, response.body)
	}
}

func TestReadWithBadTokenIsUnauthorized(t *testing.T) {
	app := newTestApp(t)
	response := doCapturedRequest(t, http.MethodGet, app.url(appsPath), map[string]string{
		"Authorization": "Bearer 0123456789abcdef0123456789abcdef",
	}, nil, nil)
	if response.status != http.StatusUnauthorized {
		t.Fatalf("bad token status=%d body=%s", response.status, response.body)
	}
}

func TestExpiredAccessTokenIsUnauthorized(t *testing.T) {
	app := newTestApp(t)
	expired, err := app.store.IssueAccessToken(app.userID, -time.Second)
	if err != nil {
		t.Fatalf("create expired access token: %v", err)
	}
	response := doCapturedRequest(t, http.MethodGet, app.url(messagesPath), map[string]string{
		"Authorization": "Bearer " + expired,
	}, nil, nil)
	if response.status != http.StatusUnauthorized {
		t.Fatalf("expired token status=%d body=%s", response.status, response.body)
	}
}

func TestUnboundUploadForbidden(t *testing.T) {
	app := newTestApp(t)
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretA,
	}, registerRequest{DeviceID: "unbound-device", Name: "Pixel"})
	if status != http.StatusOK {
		t.Fatalf("register status=%d body=%s", status, body)
	}
	status, body = uploadTestMessages(t, app, "unbound-device", testSecretA, []MessageInput{messageFor("unbound", 1780000000)})
	if status != http.StatusForbidden || decodeResponse[errorResponse](t, body).Error != "device not bound" {
		t.Fatalf("unbound upload status=%d body=%s", status, body)
	}
}

func TestBindDeviceAndUpload(t *testing.T) {
	app := newTestApp(t)
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretA,
	}, registerRequest{DeviceID: "bound-device", Name: "Pixel"})
	if status != http.StatusOK {
		t.Fatalf("register status=%d body=%s", status, body)
	}
	cookies := loginWebAccount(t, app, "bind-user")
	bound := formCapturedRequest(t, http.MethodPost, app.url(bindDevicePath), url.Values{
		"device_id": {"bound-device"},
		"secret":    {testSecretA},
	}, cookies)
	if bound.status != http.StatusOK {
		t.Fatalf("bind status=%d body=%s", bound.status, bound.body)
	}
	status, body = uploadTestMessages(t, app, "bound-device", testSecretA, []MessageInput{messageFor("bound", 1780000000)})
	if status != http.StatusOK || decodeResponse[messageBatchResponse](t, body).Inserted != 1 {
		t.Fatalf("bound upload status=%d body=%s", status, body)
	}
}

func TestBindDeviceWrongSecret(t *testing.T) {
	app := newTestApp(t)
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretA,
	}, registerRequest{DeviceID: "bind-secret-device", Name: "Pixel"})
	if status != http.StatusOK {
		t.Fatalf("register status=%d body=%s", status, body)
	}
	cookies := loginWebAccount(t, app, "bad-bind-user")
	response := formCapturedRequest(t, http.MethodPost, app.url(bindDevicePath), url.Values{
		"device_id": {"bind-secret-device"},
		"secret":    {testSecretB},
	}, cookies)
	if response.status != http.StatusBadRequest {
		t.Fatalf("bad bind status=%d body=%s", response.status, response.body)
	}
}

func TestRegistrationDisabled(t *testing.T) {
	t.Setenv("ALLOW_REGISTRATION", "false")
	app := newTestApp(t)
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretA,
	}, registerRequest{DeviceID: "disabled-device", Name: "Pixel"})
	if status != http.StatusForbidden || decodeResponse[errorResponse](t, body).Error != "registration disabled" {
		t.Fatalf("disabled registration status=%d body=%s", status, body)
	}
}

func TestDeviceAuthorizationEndpointGone(t *testing.T) {
	app := newTestApp(t)
	response := jsonCapturedRequest(t, http.MethodPost, app.url(apiPrefix+"/oauth/device_authorization"), nil, map[string]string{
		"client_id": "agent",
	}, nil)
	if response.status != http.StatusNotFound {
		t.Fatalf("device authorization status=%d body=%s", response.status, response.body)
	}
}

func TestBindWithBearerTokenReturnsUsername(t *testing.T) {
	app := newTestApp(t)
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretA,
	}, registerRequest{DeviceID: "token-bind-device", Name: "Pixel"})
	if status != http.StatusOK {
		t.Fatalf("register status=%d body=%s", status, body)
	}
	accessToken, err := app.store.IssueAccessToken(app.userID, accessTokenTTL)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	response := jsonCapturedRequest(t, http.MethodPost, app.url(bindDevicePath), map[string]string{
		"Authorization": "Bearer " + accessToken,
	}, bindDeviceRequest{DeviceID: "token-bind-device", Secret: testSecretA}, nil)
	if response.status != http.StatusOK {
		t.Fatalf("token bind status=%d body=%s", response.status, response.body)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.body, &payload); err != nil {
		t.Fatalf("decode token bind body %q: %v", response.body, err)
	}
	if payload["ok"] != true || payload["username"] != "test-user" {
		t.Fatalf("token bind payload=%v", payload)
	}
	if _, bound, _, err := app.store.DeviceBinding("token-bind-device"); err != nil || !bound {
		t.Fatalf("device not bound after token bind: bound=%v err=%v", bound, err)
	}
}

func TestHistoryFTSSearchAndEscaping(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	needle := messageFor("needle-1", 1780000000)
	needle.Content = "flubberwump 阿尔法"
	quoted := messageFor("quoted-1", 1780000001)
	quoted.Content = `他说 "hello" 世界`
	plain := messageFor("plain-1", 1780000002)
	plain.Content = "无关内容"
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, []MessageInput{needle, quoted, plain})
	if status != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", status, body)
	}
	cookies := loginAsTestUser(t, app)
	hit := doCapturedRequest(t, http.MethodGet, app.url(historyPath+"?q=flubberwump"), nil, nil, cookies)
	if hit.status != http.StatusOK {
		t.Fatalf("FTS hit status=%d body=%s", hit.status, hit.body)
	}
	if !strings.Contains(string(hit.body), "flubberwump") || strings.Contains(string(hit.body), "无关内容") {
		t.Fatalf("FTS hit body=%s", hit.body)
	}
	escaped := doCapturedRequest(t, http.MethodGet, app.url(historyPath+"?q=%22hello%22"), nil, nil, cookies)
	if escaped.status != http.StatusOK {
		t.Fatalf("FTS escaped status=%d body=%s", escaped.status, escaped.body)
	}
	// html/template escapes the content quotes, so assert on the escaped form.
	if !strings.Contains(string(escaped.body), `他说 &#34;hello&#34; 世界`) {
		t.Fatalf("FTS escaped body=%s", escaped.body)
	}
	empty := doCapturedRequest(t, http.MethodGet, app.url(historyPath+"?q=%22%22"), nil, nil, cookies)
	if empty.status != http.StatusOK {
		t.Fatalf("FTS empty phrase status=%d body=%s", empty.status, empty.body)
	}
}

func TestHistoryPage(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	messages := []MessageInput{
		messageFor("hist-1", 1780000000),
		messageFor("hist-2", 1780000001),
	}
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, messages)
	if status != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", status, body)
	}
	cookies := loginAsTestUser(t, app)
	page := doCapturedRequest(t, http.MethodGet, app.url(historyPath), nil, nil, cookies)
	if page.status != http.StatusOK {
		t.Fatalf("history page status=%d body=%s", page.status, page.body)
	}
	html := string(page.body)
	for _, want := range []string{"hist-1", "hist-2", "条匹配", "第 1/1 页"} {
		if !strings.Contains(html, want) {
			t.Fatalf("history page missing %q: %s", want, html)
		}
	}
	unauthenticated := doCapturedRequest(t, http.MethodGet, app.url(historyPath), nil, nil, nil)
	if unauthenticated.status != http.StatusSeeOther || !strings.HasPrefix(unauthenticated.header.Get("Location"), loginPath+"?next=") {
		t.Fatalf("history login redirect status=%d location=%q", unauthenticated.status, unauthenticated.header.Get("Location"))
	}
}

func TestOverviewPage(t *testing.T) {
	app := newTestApp(t)
	registerTestDevice(t, app, "device-1", testSecretA, "Pixel 8")
	status, body := uploadTestMessages(t, app, "device-1", testSecretA, []MessageInput{messageFor("ov-1", 1780000000)})
	if status != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", status, body)
	}
	cookies := loginAsTestUser(t, app)
	page := doCapturedRequest(t, http.MethodGet, app.url(overviewPath), nil, nil, cookies)
	if page.status != http.StatusOK {
		t.Fatalf("overview page status=%d body=%s", page.status, page.body)
	}
	html := string(page.body)
	for _, want := range []string{"TODAY", "WEEK", "AGENTS", "UPLINK", "7 日趋势", "App 占比"} {
		if !strings.Contains(html, want) {
			t.Fatalf("overview page missing %q: %s", want, html)
		}
	}
}

func TestLegacyDatabaseMigration(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE devices (
    device_id TEXT PRIMARY KEY, name TEXT NOT NULL, secret TEXT NOT NULL,
    created_at INTEGER NOT NULL, user_id INTEGER, bound_at INTEGER
);
CREATE TABLE messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT, client_msg_id TEXT NOT NULL UNIQUE,
    device_id TEXT NOT NULL, app TEXT NOT NULL, app_name TEXT NOT NULL,
    chat TEXT NOT NULL, sender TEXT NOT NULL, content TEXT NOT NULL,
    ts INTEGER NOT NULL, day TEXT NOT NULL
);
CREATE INDEX idx_messages_day_device_app ON messages(day, device_id, app);
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE TABLE device_grants (
    device_code TEXT PRIMARY KEY, client_id TEXT NOT NULL, user_code TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL, user_id INTEGER, created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL, last_poll_at INTEGER
);
CREATE INDEX idx_device_grants_expires_at ON device_grants(expires_at);
CREATE TABLE tokens (
    token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
CREATE INDEX idx_tokens_user_id ON tokens(user_id);
INSERT INTO users(id, username, password_hash, created_at) VALUES (1, 'legacy-user', 'x', 1);
INSERT INTO devices(device_id, name, secret, created_at, user_id, bound_at)
VALUES ('legacy-device', 'Legacy', 'secret', 1, 1, 1);
INSERT INTO messages(id, client_msg_id, device_id, app, app_name, chat, sender, content, ts, day)
VALUES (1, 'm1', 'legacy-device', 'app', 'App', 'chat', 'sender', 'legacy hello world', 1, '2026-01-01'),
       (2, 'm2', 'legacy-device', 'app', 'App', 'chat', 'sender', 'other row', 2, '2026-01-01');
INSERT INTO device_grants(device_code, client_id, user_code, status, user_id, created_at, expires_at)
VALUES ('device-code', 'client', 'USERCODE', 'pending', NULL, 1, 2);
`)
	if err != nil {
		t.Fatalf("seed legacy database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	store, err := OpenStore(dbPath, location)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("messages after migration=%d err=%v, want 2", count, err)
	}
	for table, want := range map[string]int{
		"device_grants": 0,
		"oauth_codes":   1,
		"messages_fts":  1,
	} {
		if err := store.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&count); err != nil || count != want {
			t.Fatalf("table %s after migration=%d err=%v, want %d", table, count, err, want)
		}
	}
	messages, total, err := store.QueryHistory(historyQuery{UserID: 1, Search: "hello", Page: 1})
	if err != nil {
		t.Fatalf("query migrated history: %v", err)
	}
	if total != 1 || len(messages) != 1 || messages[0].Content != "legacy hello world" {
		t.Fatalf("migrated FTS search total=%d messages=%+v", total, messages)
	}
}

func TestOAuthDangerousSchemeRejected(t *testing.T) {
	app := newTestApp(t)
	cookies := loginAsTestUser(t, app)
	for _, uri := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
		"about:blank",
		"blob:https://sender.example/x",
	} {
		consent := authorizeConsent(t, app, uri, "s1", cookies)
		if consent.status != http.StatusBadRequest {
			t.Fatalf("dangerous redirect_uri %q status=%d body=%s, want 400", uri, consent.status, consent.body)
		}
	}
}

func TestBindDeviceRebindRejected(t *testing.T) {
	app := newTestApp(t)
	status, body := jsonRequest(t, http.MethodPost, app.url(registerPath), map[string]string{
		"X-Device-Secret": testSecretA,
	}, registerRequest{DeviceID: "shared-device", Name: "Pixel"})
	if status != http.StatusOK {
		t.Fatalf("register status=%d body=%s", status, body)
	}
	// Owner A (the test user) binds first.
	if err := app.store.BindDevice("shared-device", testSecretA, app.userID); err != nil {
		t.Fatalf("bind owner A: %v", err)
	}
	// User B must not be able to steal the device with the same secret.
	cookiesB := loginWebAccount(t, app, "user-b")
	bound := formCapturedRequest(t, http.MethodPost, app.url(bindDevicePath), url.Values{
		"device_id": {"shared-device"},
		"secret":    {testSecretA},
	}, cookiesB)
	if bound.status != http.StatusConflict {
		t.Fatalf("cross-user rebind status=%d body=%s, want 409", bound.status, bound.body)
	}
	ownerID, boundTo, _, err := app.store.DeviceBinding("shared-device")
	if err != nil || !boundTo || ownerID != app.userID {
		t.Fatalf("ownership after rejected rebind: owner=%d bound=%v err=%v", ownerID, boundTo, err)
	}
	// Same-owner re-bind stays idempotent.
	again := formCapturedRequest(t, http.MethodPost, app.url(bindDevicePath), url.Values{
		"device_id": {"shared-device"},
		"secret":    {testSecretA},
	}, loginAsTestUser(t, app))
	if again.status != http.StatusOK {
		t.Fatalf("same-owner rebind status=%d body=%s", again.status, again.body)
	}
}

func TestCursorRequiresDay(t *testing.T) {
	app := newTestApp(t)
	status, body := rawRequest(t, http.MethodGet, app.url(messagesPath+"?cursor=1780000000:1"), nil, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("cursor without day status=%d body=%s, want 400", status, body)
	}
}

func TestAuthRateLimit(t *testing.T) {
	app := newTestAppWithOptions(t, time.FixedZone("Asia/Shanghai", 8*60*60), HandlerOptions{
		AllowRegistration: true,
		AuthRateLimiter:   NewRateLimiter(2, time.Minute),
	})
	for attempt := 1; attempt <= 2; attempt++ {
		status := formCapturedRequest(t, http.MethodPost, app.url(loginPath), url.Values{
			"username": {"nobody"},
			"password": {"wrong-password"},
		}, nil).status
		if status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d, want 401", attempt, status)
		}
	}
	status := formCapturedRequest(t, http.MethodPost, app.url(loginPath), url.Values{
		"username": {"nobody"},
		"password": {"wrong-password"},
	}, nil).status
	if status != http.StatusTooManyRequests {
		t.Fatalf("third attempt status=%d, want 429", status)
	}
}
