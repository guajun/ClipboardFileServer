package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var version = "dev"

//go:embed public/*
var publicFiles embed.FS

type config struct {
	host        string
	port        int
	dataDir     string
	maxUpload   int64
	authToken   string
	showVersion bool
}

type entry struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Text       string `json:"text,omitempty"`
	FileName   string `json:"fileName,omitempty"`
	StoredName string `json:"storedName,omitempty"`
	Mime       string `json:"mime,omitempty"`
	Bytes      int64  `json:"bytes"`
	CreatedAt  string `json:"createdAt"`
}

type publicEntry struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	FileName  string `json:"fileName,omitempty"`
	Mime      string `json:"mime,omitempty"`
	Bytes     int64  `json:"bytes"`
	CreatedAt string `json:"createdAt"`
	URL       string `json:"url,omitempty"`
}

type entriesFile struct {
	Version int     `json:"version"`
	Entries []entry `json:"entries"`
}

type store struct {
	mu        sync.Mutex
	entries   []entry
	dataDir   string
	filesDir  string
	indexPath string
}

type eventBroker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
	version uint64
}

type app struct {
	cfg    config
	store  *store
	events *eventBroker
}

type httpError struct {
	status  int
	message string
}

func (err httpError) Error() string {
	return err.message
}

func main() {
	cfg := parseConfig(os.Args[1:])
	if cfg.showVersion {
		fmt.Println(version)
		return
	}

	dataDir, err := filepath.Abs(cfg.dataDir)
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}
	cfg.dataDir = dataDir

	st := &store{
		dataDir:   cfg.dataDir,
		filesDir:  filepath.Join(cfg.dataDir, "files"),
		indexPath: filepath.Join(cfg.dataDir, "entries.json"),
	}
	if err := st.load(); err != nil {
		log.Fatalf("load store: %v", err)
	}

	application := &app{cfg: cfg, store: st, events: newEventBroker()}
	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port)),
		Handler:           application.routes(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatalf("listen on %s: %v", server.Addr, err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	fmt.Println("ClipboardFileServer is running")
	for _, url := range serverURLs(cfg.host, actualPort) {
		fmt.Printf("- %s\n", url)
	}
	fmt.Printf("Data: %s\n", cfg.dataDir)
	fmt.Printf("Max upload: %d MB\n", cfg.maxUpload/1024/1024)
	if cfg.authToken != "" {
		fmt.Println("Auth token: enabled")
	}

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

func parseConfig(args []string) config {
	cfg := config{
		host:      envString("HOST", "0.0.0.0"),
		port:      envInt("PORT", 8787, 0, 65535),
		dataDir:   envString("CLIP_SERVER_DATA_DIR", "data"),
		maxUpload: int64(envInt("MAX_UPLOAD_MB", 50, 1, 2048)) * 1024 * 1024,
		authToken: os.Getenv("CLIP_TOKEN"),
	}

	flags := flag.NewFlagSet("clip-server", flag.ExitOnError)
	flags.StringVar(&cfg.host, "host", cfg.host, "host/IP to bind")
	flags.IntVar(&cfg.port, "port", cfg.port, "port to listen on")
	flags.StringVar(&cfg.dataDir, "data-dir", cfg.dataDir, "directory for clipboard history and files")
	flags.Int64Var(&cfg.maxUpload, "max-upload-mb", cfg.maxUpload/1024/1024, "maximum upload size in MB")
	flags.StringVar(&cfg.authToken, "token", cfg.authToken, "optional access token")
	flags.BoolVar(&cfg.showVersion, "version", false, "print version and exit")
	_ = flags.Parse(args)

	cfg.maxUpload *= 1024 * 1024
	if cfg.maxUpload < 1 {
		cfg.maxUpload = 50 * 1024 * 1024
	}
	return cfg
}

func (application *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", application.handleAPI)
	mux.HandleFunc("/files/", application.handleStoredFile)
	mux.HandleFunc("/", application.handleStatic)
	return mux
}

func (application *app) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeCORSOptions(w)
		return
	}

	if r.URL.Path == "/api/health" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": application.store.count()})
		return
	}

	if !application.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "需要有效令牌"})
		return
	}

	switch {
	case r.URL.Path == "/api/events" && r.Method == http.MethodGet:
		application.streamEvents(w, r)
	case r.URL.Path == "/api/items" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, application.store.publicEntries())
	case r.URL.Path == "/api/text" && r.Method == http.MethodPost:
		application.createText(w, r)
	case r.URL.Path == "/api/upload" && r.Method == http.MethodPost:
		application.uploadFile(w, r)
	case r.URL.Path == "/api/items" && r.Method == http.MethodPost:
		application.createJSONItem(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/items/") && r.Method == http.MethodDelete:
		application.deleteItem(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "接口不存在"})
	}
}

func (application *app) createText(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, application.cfg.maxUpload)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	text := string(body)
	if text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "文本为空"})
		return
	}

	item := application.store.addText(text)
	application.events.publish("items")
	writeJSON(w, http.StatusCreated, toPublicEntry(item))
}

func (application *app) uploadFile(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, application.cfg.maxUpload)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "文件为空"})
		return
	}

	fileName := r.URL.Query().Get("name")
	if fileName == "" {
		fileName = r.Header.Get("X-File-Name")
	}
	if fileName == "" {
		fileName = "upload.bin"
	}

	item, err := application.store.addFile(body, fileName, contentTypeOnly(r.Header.Get("Content-Type")))
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	application.events.publish("items")
	writeJSON(w, http.StatusCreated, toPublicEntry(item))
}

func (application *app) createJSONItem(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, application.cfg.maxUpload)
	if err != nil {
		writeHTTPError(w, err)
		return
	}

	var payload struct {
		Kind    string `json:"kind"`
		Text    string `json:"text"`
		Name    string `json:"name"`
		Mime    string `json:"mime"`
		DataURL string `json:"dataUrl"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 无效"})
		return
	}

	switch payload.Kind {
	case "text":
		if payload.Text == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "文本为空"})
			return
		}
		item := application.store.addText(payload.Text)
		application.events.publish("items")
		writeJSON(w, http.StatusCreated, toPublicEntry(item))
	case "file":
		data, mimeType, err := decodeDataURL(payload.DataURL)
		if err != nil {
			writeHTTPError(w, err)
			return
		}
		if int64(len(data)) > application.cfg.maxUpload {
			writeHTTPError(w, httpError{status: http.StatusRequestEntityTooLarge, message: "上传超过大小限制"})
			return
		}
		if payload.Mime == "" {
			payload.Mime = mimeType
		}
		item, err := application.store.addFile(data, payload.Name, payload.Mime)
		if err != nil {
			writeHTTPError(w, err)
			return
		}
		application.events.publish("items")
		writeJSON(w, http.StatusCreated, toPublicEntry(item))
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind 只支持 text 或 file"})
	}
}

func (application *app) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathUnescape(strings.TrimPrefix(r.URL.Path, "/api/items/"))
	if err != nil || id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "条目 ID 无效"})
		return
	}
	if err := application.store.delete(id); err != nil {
		writeHTTPError(w, err)
		return
	}
	application.events.publish("items")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (application *app) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "实时同步不可用"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	client := application.events.subscribe()
	defer application.events.unsubscribe(client)

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case message := <-client:
			_, _ = fmt.Fprintf(w, "event: items\ndata: %s\n\n", message)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (application *app) handleStoredFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "方法不支持"})
		return
	}
	if !application.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "需要有效令牌"})
		return
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/files/")
	parts := strings.SplitN(trimmed, "/", 2)
	id, err := pathUnescape(parts[0])
	if err != nil || id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "文件路径无效"})
		return
	}

	item, ok := application.store.findFile(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
		return
	}

	filePath := filepath.Join(application.store.filesDir, item.StoredName)
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("open stored file id=%s path=%q: %v", id, filePath, err)
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		log.Printf("stat stored file id=%s path=%q: %v", id, filePath, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
		return
	}

	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", defaultString(item.Mime, "application/octet-stream"))
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("Content-Disposition", contentDisposition(disposition, item.FileName))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, file); err != nil {
		log.Printf("send stored file id=%s path=%q: %v", id, filePath, err)
	}
}

func (application *app) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "方法不支持"})
		return
	}

	requestPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	if requestPath == "/" {
		requestPath = "/index.html"
	}

	embeddedPath := "public" + requestPath
	info, err := fs.Stat(publicFiles, embeddedPath)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "页面不存在"})
		return
	}
	data, err := publicFiles.ReadFile(embeddedPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
		return
	}

	contentType := mime.TypeByExtension(path.Ext(embeddedPath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func (application *app) authorized(r *http.Request) bool {
	if application.cfg.authToken == "" {
		return true
	}
	provided := r.URL.Query().Get("token")
	if provided == "" {
		provided = bearerToken(r.Header.Get("Authorization"))
	}
	if provided == "" {
		provided = r.Header.Get("X-Clip-Token")
	}
	if provided == "" || len(provided) != len(application.cfg.authToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(application.cfg.authToken)) == 1
}

func (st *store) load() error {
	if err := os.MkdirAll(st.filesDir, 0o755); err != nil {
		return err
	}

	data, err := os.ReadFile(st.indexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return st.persistLocked()
		}
		return err
	}

	var parsed entriesFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		var legacy []entry
		if legacyErr := json.Unmarshal(data, &legacy); legacyErr != nil {
			return err
		}
		parsed.Entries = legacy
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	st.entries = validEntries(parsed.Entries)
	sortEntries(st.entries)
	return nil
}

func (st *store) count() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.entries)
}

func (st *store) publicEntries() []publicEntry {
	st.mu.Lock()
	defer st.mu.Unlock()

	items := make([]publicEntry, 0, len(st.entries))
	for _, item := range st.entries {
		items = append(items, toPublicEntry(item))
	}
	return items
}

func (st *store) addText(text string) entry {
	st.mu.Lock()
	defer st.mu.Unlock()

	item := entry{
		ID:        createID(),
		Kind:      "text",
		Text:      text,
		Bytes:     int64(len([]byte(text))),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	st.entries = append([]entry{item}, st.entries...)
	sortEntries(st.entries)
	if err := st.persistLocked(); err != nil {
		log.Printf("persist entries: %v", err)
	}
	return item
}

func (st *store) addFile(data []byte, fileName string, mimeType string) (entry, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	id := createID()
	safeName := safeFileName(fileName)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	storedName := id + "-" + safeName
	if err := os.WriteFile(filepath.Join(st.filesDir, storedName), data, 0o644); err != nil {
		return entry{}, httpError{status: http.StatusInternalServerError, message: "保存文件失败"}
	}

	item := entry{
		ID:         id,
		Kind:       "file",
		FileName:   safeName,
		StoredName: storedName,
		Mime:       mimeType,
		Bytes:      int64(len(data)),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	st.entries = append([]entry{item}, st.entries...)
	sortEntries(st.entries)
	if err := st.persistLocked(); err != nil {
		return entry{}, httpError{status: http.StatusInternalServerError, message: "保存索引失败"}
	}
	return item, nil
}

func (st *store) delete(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	index := -1
	for i, item := range st.entries {
		if item.ID == id {
			index = i
			break
		}
	}
	if index == -1 {
		return httpError{status: http.StatusNotFound, message: "条目不存在"}
	}

	removed := st.entries[index]
	st.entries = append(st.entries[:index], st.entries[index+1:]...)
	if err := st.persistLocked(); err != nil {
		return httpError{status: http.StatusInternalServerError, message: "保存索引失败"}
	}
	if removed.Kind == "file" && removed.StoredName != "" {
		if err := os.Remove(filepath.Join(st.filesDir, removed.StoredName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("delete stored file %s: %v", removed.StoredName, err)
		}
	}
	return nil
}

func (st *store) findFile(id string) (entry, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, item := range st.entries {
		if item.ID == id && item.Kind == "file" {
			return item, true
		}
	}
	return entry{}, false
}

func newEventBroker() *eventBroker {
	return &eventBroker{clients: map[chan string]struct{}{}}
}

func (broker *eventBroker) subscribe() chan string {
	client := make(chan string, 8)
	broker.mu.Lock()
	broker.clients[client] = struct{}{}
	broker.mu.Unlock()
	return client
}

func (broker *eventBroker) unsubscribe(client chan string) {
	broker.mu.Lock()
	delete(broker.clients, client)
	broker.mu.Unlock()
	close(client)
}

func (broker *eventBroker) publish(kind string) {
	broker.mu.Lock()
	broker.version++
	message := fmt.Sprintf(`{"kind":%q,"version":%d}`, kind, broker.version)
	for client := range broker.clients {
		select {
		case client <- message:
		default:
		}
	}
	broker.mu.Unlock()
}

func (st *store) persistLocked() error {
	if err := os.MkdirAll(st.dataDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(st.filesDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(entriesFile{Version: 1, Entries: st.entries}, "", "  ")
	if err != nil {
		return err
	}
	tempPath := st.indexPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	_ = os.Remove(st.indexPath)
	return os.Rename(tempPath, st.indexPath)
}

func toPublicEntry(item entry) publicEntry {
	public := publicEntry{
		ID:        item.ID,
		Kind:      item.Kind,
		Bytes:     item.Bytes,
		CreatedAt: item.CreatedAt,
	}
	if item.Kind == "text" {
		public.Text = item.Text
	}
	if item.Kind == "file" {
		public.FileName = defaultString(item.FileName, "download.bin")
		public.Mime = defaultString(item.Mime, "application/octet-stream")
		public.URL = "/files/" + pathEscape(item.ID) + "/" + pathEscape(public.FileName)
	}
	return public
}

func readBody(r *http.Request, maxBytes int64) ([]byte, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, httpError{status: http.StatusBadRequest, message: "读取请求失败"}
	}
	if int64(len(body)) > maxBytes {
		return nil, httpError{status: http.StatusRequestEntityTooLarge, message: "上传超过大小限制"}
	}
	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		status = http.StatusInternalServerError
		data = []byte(`{"error":"服务器内部错误"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeHTTPError(w http.ResponseWriter, err error) {
	var httpErr httpError
	if errors.As(err, &httpErr) {
		writeJSON(w, httpErr.status, map[string]string{"error": httpErr.message})
		return
	}
	log.Printf("request error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
}

func writeCORSOptions(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET,HEAD,POST,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-Clip-Token,X-File-Name,Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "GET,HEAD,POST,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusNoContent)
}

func envString(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(name string, fallback int, minimum int, maximum int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < minimum || number > maximum {
		return fallback
	}
	return number
}

func bearerToken(header string) string {
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("bearer "):])
}

func contentTypeOnly(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.SplitN(value, ";", 2)
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func contentDisposition(disposition string, fileName string) string {
	name := defaultString(fileName, "download.bin")
	return fmt.Sprintf("%s; filename=%q; filename*=UTF-8''%s", disposition, asciiHeaderFileName(name), encodeRFC5987(name))
}

func asciiHeaderFileName(fileName string) string {
	var builder strings.Builder
	for _, character := range fileName {
		if character >= 32 && character <= 126 && character != '"' && character != '\\' && character != ';' {
			builder.WriteRune(character)
			continue
		}
		builder.WriteByte('_')
	}

	name := strings.TrimSpace(builder.String())
	if name == "" || name == "." {
		return "download.bin"
	}
	if len(name) > 120 {
		return name[:120]
	}
	return name
}

func encodeRFC5987(value string) string {
	var builder strings.Builder
	for _, valueByte := range []byte(value) {
		if isRFC5987AttrChar(valueByte) {
			builder.WriteByte(valueByte)
			continue
		}
		fmt.Fprintf(&builder, "%%%02X", valueByte)
	}
	return builder.String()
}

func isRFC5987AttrChar(valueByte byte) bool {
	if valueByte >= 'a' && valueByte <= 'z' {
		return true
	}
	if valueByte >= 'A' && valueByte <= 'Z' {
		return true
	}
	if valueByte >= '0' && valueByte <= '9' {
		return true
	}
	return strings.ContainsRune("!#$&+-.^_`|~", rune(valueByte))
}

func decodeDataURL(dataURL string) ([]byte, string, error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, "", httpError{status: http.StatusBadRequest, message: "dataUrl 无效"}
	}
	comma := strings.IndexByte(dataURL, ',')
	if comma == -1 {
		return nil, "", httpError{status: http.StatusBadRequest, message: "dataUrl 无效"}
	}
	metadata := strings.TrimPrefix(dataURL[:comma], "data:")
	data := dataURL[comma+1:]
	mimeType := "application/octet-stream"
	isBase64 := false
	for _, part := range strings.Split(metadata, ";") {
		if part == "base64" {
			isBase64 = true
			continue
		}
		if strings.Contains(part, "/") {
			mimeType = part
		}
	}
	if isBase64 {
		decoded, err := decodeBase64(data)
		if err != nil {
			return nil, "", httpError{status: http.StatusBadRequest, message: "dataUrl 无效"}
		}
		return decoded, mimeType, nil
	}
	decoded, err := pathUnescape(data)
	if err != nil {
		return nil, "", httpError{status: http.StatusBadRequest, message: "dataUrl 无效"}
	}
	return []byte(decoded), mimeType, nil
}

func decodeBase64(data string) ([]byte, error) {
	decoder := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	decodedLen, err := base64.StdEncoding.Decode(decoder, []byte(data))
	if err != nil {
		return nil, err
	}
	return decoder[:decodedLen], nil
}

func safeFileName(fileName string) string {
	name := strings.ReplaceAll(fileName, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		}
		if r < 32 {
			return '_'
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || name == "." {
		name = "upload.bin"
	}
	if len([]rune(name)) > 180 {
		name = string([]rune(name)[:180])
	}
	return name
}

func createID() string {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	return strconv.FormatInt(time.Now().UnixMilli(), 36) + "-" + hex.EncodeToString(random)
}

func validEntries(items []entry) []entry {
	valid := make([]entry, 0, len(items))
	for _, item := range items {
		if item.ID != "" && item.Kind != "" {
			valid = append(valid, item)
		}
	}
	return valid
}

func sortEntries(items []entry) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
}

func serverURLs(host string, port int) []string {
	seen := map[string]bool{}
	var urls []string
	add := func(value string) {
		if !seen[value] {
			seen[value] = true
			urls = append(urls, value)
		}
	}

	if host == "0.0.0.0" || host == "::" || host == "" {
		add(fmt.Sprintf("http://127.0.0.1:%d/", port))
		interfaces, err := net.Interfaces()
		if err == nil {
			for _, iface := range interfaces {
				addresses, err := iface.Addrs()
				if err != nil {
					continue
				}
				for _, address := range addresses {
					ipNet, ok := address.(*net.IPNet)
					if !ok {
						continue
					}
					ip := ipNet.IP.To4()
					if ip == nil || ip.IsLoopback() {
						continue
					}
					add(fmt.Sprintf("http://%s:%d/", ip.String(), port))
				}
			}
		}
		return urls
	}

	printableHost := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		printableHost = "[" + host + "]"
	}
	add(fmt.Sprintf("http://%s:%d/", printableHost, port))
	return urls
}

func pathEscape(value string) string {
	return url.PathEscape(value)
}

func pathUnescape(value string) (string, error) {
	return url.PathUnescape(value)
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
