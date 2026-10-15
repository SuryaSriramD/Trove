// Package hosting is an authenticated, single-owner HTTP adapter to the workspace kernel.
package hosting

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

type Server struct {
	Kernel    *workspace.Kernel
	execution contracts.ServiceExecutionContext
	tokenHash [32]byte
	Assets    fs.FS
	Secure    bool
	sessions  sync.Map
	gate      chan struct{}
}

// New requires a high entropy owner credential supplied outside the workspace.
func New(k *workspace.Kernel, token string, assets fs.FS, secure bool) (*Server, error) {
	if len(token) < 32 || len(token) > 256 || strings.TrimSpace(token) != token {
		return nil, fmt.Errorf("owner token must contain 32–256 non-whitespace bytes")
	}
	return &Server{Kernel: k, execution: k.OwnerContext(), tokenHash: sha256.Sum256([]byte(token)), Assets: assets, Secure: secure, gate: make(chan struct{}, 4)}, nil
}
func (s *Server) Close() { s.Kernel.RevokeGrant(string(s.execution.ScopeGrantId)) }
func ValidateListen(address, cert, key string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if (cert == "") != (key == "") {
		return fmt.Errorf("TLS certificate and key are required together")
	}
	if (ip == nil || !ip.IsLoopback()) && (cert == "" || key == "") {
		return fmt.Errorf("LAN/remote listeners require TLS; use a literal loopback address for local HTTP")
	}
	return nil
}
func (s *Server) authenticated(r *http.Request) bool {
	if token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); token != r.Header.Get("Authorization") {
		hash := sha256.Sum256([]byte(token))
		return subtle.ConstantTimeCompare(hash[:], s.tokenHash[:]) == 1
	}
	if cookie, err := r.Cookie("trove_session"); err == nil {
		if until, ok := s.sessions.Load(cookie.Value); ok {
			if time.Now().Before(until.(time.Time)) {
				return true
			}
			s.sessions.Delete(cookie.Value)
		}
	}
	return false
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin == "" || origin == scheme+"://"+r.Host
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	if !sameOrigin(r) {
		http.Error(w, "Cross-origin access denied", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/login" && r.Method == http.MethodPost {
		s.login(w, r)
		return
	}
	if !s.authenticated(r) {
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, loginHTML)
			return
		}
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/logout" && r.Method == http.MethodPost:
		if c, e := r.Cookie("trove_session"); e == nil {
			s.sessions.Delete(c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "trove_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode})
		w.WriteHeader(204)
	case r.URL.Path == "/api" && r.Method == http.MethodPost:
		s.dispatch(w, r)
	case r.URL.Path == "/upload" && r.Method == http.MethodPost:
		s.upload(w, r)
	case strings.HasPrefix(r.URL.Path, "/files/") && (r.Method == "GET" || r.Method == "HEAD"):
		s.file(w, r)
	case r.URL.Path == "/host-bridge.js" && r.Method == "GET":
		w.Header().Set("Content-Type", "application/javascript")
		io.WriteString(w, bridgeJS)
	case r.URL.Path == "/host/status" && r.Method == "GET":
		writeJSON(w, map[string]any{"mode": "headless", "workspaceId": s.Kernel.ID(), "owner": "host", "offlineSynchronization": false})
	case r.Method == "GET":
		if s.Assets == nil {
			http.Error(w, "Build frontend assets before starting the host", 503)
			return
		}
		if r.URL.Path == "/" {
			b, e := fs.ReadFile(s.Assets, "index.html")
			if e != nil {
				http.Error(w, "Assets unavailable", 503)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, strings.Replace(string(b), "<head>", "<head><script src=\"/host-bridge.js\"></script>", 1))
			return
		}
		http.FileServer(http.FS(s.Assets)).ServeHTTP(w, r)
	default:
		http.Error(w, "Unsupported route", 405)
	}
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "Invalid login", 400)
		return
	}
	hash := sha256.Sum256([]byte(body.Token))
	if subtle.ConstantTimeCompare(hash[:], s.tokenHash[:]) != 1 {
		http.Error(w, "Invalid owner token", 401)
		return
	}
	// Bound session inventory even if a client repeatedly logs in.
	count := 0
	s.sessions.Range(func(key, value any) bool {
		if time.Now().After(value.(time.Time)) {
			s.sessions.Delete(key)
		} else {
			count++
		}
		return true
	})
	if count >= 32 {
		http.Error(w, "Session limit reached", 429)
		return
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		http.Error(w, "Session unavailable", 500)
		return
	}
	token := hex.EncodeToString(b)
	s.sessions.Store(token, time.Now().Add(12*time.Hour))
	http.SetCookie(w, &http.Cookie{Name: "trove_session", Value: token, Path: "/", HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	w.WriteHeader(204)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		http.Error(w, "Host busy", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var call struct {
		Method  string          `json:"method"`
		Request json.RawMessage `json:"request"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&call) != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	// Native-only filesystem, microphone and provider capabilities are deliberately
	// absent. Remote capture uses the browser's upload capability.
	if !allowedMethod(call.Method) {
		writeJSON(w, map[string]any{"ok": false, "error": map[string]any{"code": "SCOPE_DENIED", "message": "This operation requires the desktop adapter", "requestId": workspace.NewID(), "retryable": false}})
		return
	}
	var req map[string]any
	if json.Unmarshal(call.Request, &req) != nil || req == nil {
		req = map[string]any{}
	}
	if call.Method == "workspace.snapshot" {
		req["contractVersion"] = 1
		req["workspaceId"] = s.Kernel.ID()
		req["requestId"] = workspace.NewID()
	}
	raw, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result := s.Kernel.Dispatch(ctx, s.execution, call.Method, raw)
	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}
func allowedMethod(method string) bool {
	for _, m := range strings.Fields(`tasks.detachPreview page.freshness page.reviewQueue operations.list operations.previewUndo recordLayouts.get scheduling.preview skills.list skills.preview review.propose review.apply review.cancel review.retry meetings.prepareExtraction meetings.extractions workspace.snapshot batch allocateIds readPage readBlock readRecord readEvents queryRecords search search.query search.status search.rebuild resolveCitation prepareFile previewImport getImportReport listMemory memory.list enqueueJob getJob cancelJob cancel jobs.list jobs.retry databaseDerived databaseP2 databaseChart pageDisplay.get graph.query ocr.status ocr.run review.list review.run editor.lock editor.setLock editor.references editor.templates editor.saveTemplate editor.template editor.saveButton editor.button history.list history.previewRestore history.previewSubtree history.trash history.recoveryPoint recovery.settings recovery.setRetention links.backlinks imports.preview imports.plan imports.configure imports.commit imports.list imports.enqueue ai.runs ai.run ai.prepareAction ai.applyAction ai.suggestions automationPreview automation.execute automationHistory automationOptions meetings.source meetings.drafts meetings.transcribe meetings.applyDraft meetings.createFromTranscript documents.status capture.preview search.semanticSettings search.configureSemantic`) {
		if m == method {
			return true
		}
	}
	return false
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		http.Error(w, "Host busy", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 36<<20)
	var b struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		http.Error(w, "Invalid upload", 400)
		return
	}
	data, e := base64.StdEncoding.DecodeString(b.Data)
	if e != nil || len(data) > 25<<20 {
		http.Error(w, "Upload must be valid base64 and at most 25 MiB", 400)
		return
	}
	dir, e := os.MkdirTemp("", "trove-host-upload-")
	if e != nil {
		http.Error(w, "Upload staging failed", 500)
		return
	}
	defer os.RemoveAll(dir)
	name := filepath.Base(strings.ReplaceAll(b.Name, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		name = "attachment"
	}
	path := filepath.Join(dir, name)
	if e = os.WriteFile(path, data, 0600); e != nil {
		http.Error(w, "Upload staging failed", 500)
		return
	}
	handle, e := s.Kernel.RegisterSelection(path)
	if e != nil {
		http.Error(w, "Upload rejected", 400)
		return
	}
	writeJSON(w, map[string]string{"selectionHandle": handle})
}
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/files/")
	if strings.ContainsAny(id, "/\\") || len(id) != 36 {
		http.NotFound(w, r)
		return
	}
	f, meta, e := s.Kernel.ReadFile(r.Context(), s.execution, id)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	// Treat all user bytes as sandboxed data, including SVG/HTML attachments.
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Content-Type", meta.MediaType)
	if !strings.HasPrefix(meta.MediaType, "image/") && !strings.HasPrefix(meta.MediaType, "audio/") && !strings.HasPrefix(meta.MediaType, "video/") && meta.MediaType != "application/pdf" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": meta.OriginalName}))
	}
	http.ServeContent(w, r, meta.OriginalName, time.Time{}, f)
}

const loginHTML = `<!doctype html><html><meta charset="utf-8"><title>Trove host</title><style>body{font:18px system-ui;max-width:460px;margin:15vh auto;padding:24px;background:#f6f5f1}input,button{font:inherit;padding:12px;margin-top:16px;width:100%;box-sizing:border-box}p{line-height:1.5}</style><h1>Trove</h1><p>Connect to your workspace host. Enter the owner token from your private token file.</p><form><input type="password" aria-label="Owner token" autocomplete="current-password" required><button>Connect</button></form><p role="alert"></p><script>document.querySelector('form').onsubmit=async e=>{e.preventDefault();const r=await fetch('/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:document.querySelector('input').value})});if(r.ok)location.reload();else document.querySelector('[role=alert]').textContent=await r.text()}</script></html>`
const bridgeJS = `(()=>{
 const call=async(path,body)=>{const r=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(r.status===401){location.reload();throw Error('Session expired')}if(!r.ok)throw Error(await r.text());return r.status===204?null:r.json()};
 const stage=async(name,mediaType,data)=>(await call('/upload',{name,data})).selectionHandle;
 const pick=()=>new Promise(resolve=>{const input=document.createElement('input');input.type='file';input.oncancel=()=>resolve('');input.onchange=async()=>{const f=input.files?.[0];if(!f){resolve('');return}const reader=new FileReader();reader.onload=async()=>{try{resolve(await stage(f.name,f.type,String(reader.result).split(',')[1]))}catch(e){alert(e.message);resolve('')}};reader.readAsDataURL(f)};input.click()});
 window.go={main:{App:{Dispatch:async(method,raw)=>JSON.stringify(await call('/api',{method,request:JSON.parse(raw)})),PickFile:pick,StageUpload:stage,SetEditing:async()=>{},ShellReady:async()=>{}}}};
 window.troveHost=true;
 const listeners=new Set();window.runtime={EventsOn:(event,fn)=>{if(event==='workspace:changed')listeners.add(fn);return()=>listeners.delete(fn)}};
 let last='';setInterval(async()=>{if(document.hidden)return;try{const r=await call('/api',{method:'workspace.snapshot',request:{}});const c=r.result?.snapshotCursor;if(last&&c!==last)listeners.forEach(fn=>fn());last=c||''}catch{}},5000);
})();`
