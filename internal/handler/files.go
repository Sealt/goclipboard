package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"goclipboard/internal/model"
	"goclipboard/internal/store"
)

const (
	// Multipart parts spill to temp disk after this many bytes in memory.
	multipartMemory      = 32 << 20 // 32 MiB
	maxUploadPasswordLen = 256
)

// handleFilesAPI routes:
//
//	GET|POST  /api/clipboard/{key}/files
//	GET|DELETE /api/clipboard/{key}/files/{id}
func (h *Handler) handleFilesAPI(w http.ResponseWriter, r *http.Request, roomKey, rest string) {
	rest = strings.Trim(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			h.handleListFiles(w, r, roomKey)
		case http.MethodPost:
			h.handleUploadFile(w, r, roomKey)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return
	}

	// Single path segment = file id
	if strings.Contains(rest, "/") {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	fileID, err := model.ValidateFileID(rest)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleDownloadFile(w, r, roomKey, fileID)
	case http.MethodDelete:
		h.handleDeleteFile(w, r, roomKey, fileID)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (h *Handler) handleListFiles(w http.ResponseWriter, r *http.Request, roomKey string) {
	if !h.requireViewPassword(w, r, roomKey) {
		return
	}
	if h.files == nil {
		writeJSON(w, http.StatusOK, model.FileListResponse{Key: roomKey, Files: []model.FileInfo{}})
		return
	}
	files, _, uploadOn := h.files.ListWithSettings(roomKey)
	writeJSON(w, http.StatusOK, model.FileListResponse{
		Key:               roomKey,
		Files:             files,
		FileUploadEnabled: uploadOn,
	})
}

func (h *Handler) handleRoomSettings(w http.ResponseWriter, r *http.Request, roomKey string) {
	if h.files == nil {
		writeError(w, http.StatusServiceUnavailable, "file features are disabled")
		return
	}
	switch r.Method {
	case http.MethodGet:
		// View-protected rooms: settings metadata is part of the sealed surface.
		if !h.requireViewPassword(w, r, roomKey) {
			return
		}
		writeJSON(w, http.StatusOK, h.files.RoomSettings(roomKey))
	case http.MethodPut:
		h.handleUpdateRoomSettings(w, r, roomKey)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

func (h *Handler) handleUpdateRoomSettings(w http.ResponseWriter, r *http.Request, roomKey string) {
	if h.uploadPassword == "" {
		writeError(w, http.StatusForbidden, "file access is disabled (UPLOAD_PASSWORD not set)")
		return
	}
	// View-protected rooms: settings are part of the sealed surface.
	if !h.requireViewPassword(w, r, roomKey) {
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	var req model.RoomSettingsUpdate
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.FileUploadEnabled == nil {
		writeError(w, http.StatusBadRequest, "fileUploadEnabled is required")
		return
	}

	// Prefer body password, then headers / query (same as other admin ops).
	adminPW := strings.TrimSpace(req.AdminPassword)
	if adminPW == "" {
		adminPW = strings.TrimSpace(req.Password)
	}
	if adminPW == "" {
		adminPW = extractAdminPassword(r)
	}
	if !passwordMatch(h.uploadPassword, adminPW) {
		writeError(w, http.StatusUnauthorized, "invalid admin password")
		return
	}

	if err := h.files.SetFileUploadEnabled(roomKey, *req.FileUploadEnabled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.broker.ping(roomKey)
	writeJSON(w, http.StatusOK, h.files.RoomSettings(roomKey))
}

func (h *Handler) handleUploadFile(w http.ResponseWriter, r *http.Request, roomKey string) {
	if h.files == nil {
		writeError(w, http.StatusServiceUnavailable, "file upload is disabled")
		return
	}
	if h.uploadPassword == "" {
		writeError(w, http.StatusForbidden, "file upload is disabled (UPLOAD_PASSWORD not set)")
		return
	}
	// View-protected rooms: uploads mutate the room surface (file list).
	if !h.requireViewPassword(w, r, roomKey) {
		return
	}
	// No size cap: password-gated single-user store streams large files to disk.
	// ParseMultipartForm keeps only multipartMemory in RAM; the rest is temp files.
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	// Room open → anyone may upload with only a file password.
	// Room closed → admin password required for this upload (does not auto-enable the room).
	if !h.files.IsFileUploadEnabled(roomKey) {
		if h.uploadPassword == "" {
			writeError(w, http.StatusForbidden, "file upload is disabled (UPLOAD_PASSWORD not set)")
			return
		}
		if !passwordMatch(h.uploadPassword, extractAdminPassword(r)) {
			writeError(w, http.StatusUnauthorized, "invalid admin password")
			return
		}
	}

	filePassword := extractNamedPassword(r, "filePassword", "X-File-Password")
	if strings.TrimSpace(filePassword) == "" {
		writeError(w, http.StatusBadRequest, "file password is required")
		return
	}
	if len(filePassword) > store.MaxFilePasswordLen {
		writeError(w, http.StatusBadRequest, "file password is too long")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	name := header.Filename
	if alt := strings.TrimSpace(r.FormValue("name")); alt != "" {
		name = alt
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		if ext := filepath.Ext(name); ext != "" {
			if guessed := mime.TypeByExtension(ext); guessed != "" {
				contentType = guessed
			}
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	ttlSeconds := int64(defaultTTL.Seconds())
	if raw := strings.TrimSpace(r.FormValue("ttlSeconds")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid ttlSeconds")
			return
		}
		ttlSeconds = n
	}
	ttl, err := model.TTLFromSeconds(ttlSeconds)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sizeHint := header.Size
	stored, err := h.files.PutReader(roomKey, name, contentType, file, sizeHint, ttl, filePassword)
	if err != nil {
		writeFileStoreError(w, err)
		return
	}

	h.broker.ping(roomKey)
	writeJSON(w, http.StatusCreated, model.FileInfoFrom(stored))
}

func (h *Handler) handleDownloadFile(w http.ResponseWriter, r *http.Request, roomKey, fileID string) {
	if h.files == nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if !h.requireViewPassword(w, r, roomKey) {
		return
	}
	filePassword := extractFileDownloadPassword(r)
	if strings.TrimSpace(filePassword) == "" {
		writeError(w, http.StatusUnauthorized, "invalid file password")
		return
	}
	if err := h.files.CheckFilePassword(roomKey, fileID, filePassword); err != nil {
		if errors.Is(err, store.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid file password")
		return
	}
	meta, rc, err := h.files.Open(roomKey, fileID)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	defer rc.Close()

	disposition := contentDisposition(meta.Name)
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Disposition", disposition)
	if meta.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (h *Handler) handleDeleteFile(w http.ResponseWriter, r *http.Request, roomKey, fileID string) {
	if h.files == nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	// View-protected rooms: deletes mutate the sealed file list.
	if !h.requireViewPassword(w, r, roomKey) {
		return
	}
	if !h.requireAdminPassword(w, r) {
		return
	}

	if !h.files.Delete(roomKey, fileID) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	h.broker.ping(roomKey)
	w.WriteHeader(http.StatusNoContent)
}

// requireViewPassword enforces the room password when the room is
// view-protected (scope "view"): the file list, downloads, uploads, deletes
// and room file-settings are part of the room's sealed surface. Unprotected
// and edit-protected rooms pass through.
func (h *Handler) requireViewPassword(w http.ResponseWriter, r *http.Request, roomKey string) bool {
	if !h.viewProtected(roomKey) {
		return true
	}
	if !h.allowPasswordAttempt(r, roomKey) {
		writeError(w, http.StatusTooManyRequests, "too many password attempts")
		return false
	}
	pw := roomPasswordFromRequest(r)
	if h.store.PasswordOK(roomKey, pw) {
		h.recordPasswordSuccess(r, roomKey)
		return true
	}
	h.recordPasswordFailure(r, roomKey)
	msg := "view password required"
	if pw != "" {
		msg = "invalid view password"
	}
	writeJSON(w, http.StatusUnauthorized, struct {
		Error         string `json:"error"`
		PasswordScope string `json:"passwordScope"`
	}{msg, model.PasswordScopeView})
	return false
}

func writeFileStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrFileTooLarge), errors.Is(err, model.ErrEmptyFile), errors.Is(err, model.ErrInvalidFile):
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrFileTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, err.Error())
	case errors.Is(err, store.ErrFileIO):
		writeError(w, http.StatusInternalServerError, "failed to store file")
	case errors.Is(err, store.ErrUploadDisabled):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// requireAdminPassword enforces UPLOAD_PASSWORD for upload and delete.
// Accepts (in order): X-Admin-Password, X-Upload-Password, query
// ?adminPassword=, form adminPassword / password, or JSON body
// {"adminPassword":"..."}. Bare ?password= is reserved for the room password
// (see roomPasswordFromRequest) and is intentionally not accepted here.
func (h *Handler) requireAdminPassword(w http.ResponseWriter, r *http.Request) bool {
	if h.uploadPassword == "" {
		writeError(w, http.StatusForbidden, "file access is disabled (UPLOAD_PASSWORD not set)")
		return false
	}
	password := extractAdminPassword(r)
	if !passwordMatch(h.uploadPassword, password) {
		writeError(w, http.StatusUnauthorized, "invalid admin password")
		return false
	}
	return true
}

func extractAdminPassword(r *http.Request) string {
	if p := r.Header.Get("X-Admin-Password"); p != "" {
		return p
	}
	// Backward-compatible alias used by older clients.
	if p := r.Header.Get("X-Upload-Password"); p != "" {
		return p
	}
	if p := r.URL.Query().Get("adminPassword"); p != "" {
		return p
	}
	// Note: query ?password= is the room password (X-Goclip-Password / room
	// gate) and must not be treated as the admin secret.
	if r.MultipartForm != nil {
		if vals := r.MultipartForm.Value["adminPassword"]; len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
		if vals := r.MultipartForm.Value["password"]; len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}
	if r.Form != nil {
		if p := r.Form.Get("adminPassword"); p != "" {
			return p
		}
		if p := r.Form.Get("password"); p != "" {
			return p
		}
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && r.Body != nil {
		var body struct {
			AdminPassword string `json:"adminPassword"`
			Password      string `json:"password"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1024))
		if err := dec.Decode(&body); err == nil {
			if body.AdminPassword != "" {
				return body.AdminPassword
			}
			// JSON "password" remains accepted for older upload-settings clients.
			if body.Password != "" {
				return body.Password
			}
		}
	}
	return ""
}

func extractFileDownloadPassword(r *http.Request) string {
	if p := extractNamedPassword(r, "filePassword", "X-File-Password"); p != "" {
		return p
	}
	// Prefer distinct names so ?password= can stay the room password on
	// view-scoped rooms. X-Upload-Password is a legacy alias only.
	if p := r.Header.Get("X-Upload-Password"); p != "" {
		return p
	}
	return ""
}

// extractNamedPassword reads a password from a preferred header, query, multipart, form, or JSON body field.
func extractNamedPassword(r *http.Request, formKey, headerKey string) string {
	if headerKey != "" {
		if p := r.Header.Get(headerKey); p != "" {
			return p
		}
	}
	if formKey != "" {
		if p := r.URL.Query().Get(formKey); p != "" {
			return p
		}
	}
	if r.MultipartForm != nil && formKey != "" {
		if vals := r.MultipartForm.Value[formKey]; len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}
	if r.Form != nil && formKey != "" {
		if p := r.Form.Get(formKey); p != "" {
			return p
		}
	}
	if formKey != "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && r.Body != nil {
		// Best-effort JSON peek; callers that need the body again must re-buffer.
		var body map[string]string
		dec := json.NewDecoder(io.LimitReader(r.Body, 1024))
		if err := dec.Decode(&body); err == nil {
			if p := body[formKey]; p != "" {
				return p
			}
		}
	}
	return ""
}

func passwordMatch(expected, provided string) bool {
	if expected == "" {
		return false
	}
	if len(provided) > maxUploadPasswordLen {
		return false
	}
	// subtle.ConstantTimeCompare requires equal length; pad comparison via sha-less length check.
	a := []byte(expected)
	b := []byte(provided)
	if len(a) != len(b) {
		// Still do a dummy compare to reduce timing leak of length-only early exit.
		_ = subtle.ConstantTimeCompare(a, a)
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

// contentDisposition builds a safe Content-Disposition header value.
func contentDisposition(name string) string {
	// ASCII fallback for legacy clients.
	ascii := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' {
			ascii = append(ascii, c)
		} else {
			ascii = append(ascii, '_')
		}
	}
	if len(ascii) == 0 {
		ascii = []byte("download")
	}
	// RFC 5987 filename* for UTF-8 names.
	if utf8.ValidString(name) && name != string(ascii) {
		return `attachment; filename="` + string(ascii) + `"; filename*=UTF-8''` + percentEncode(name)
	}
	return `attachment; filename="` + string(ascii) + `"`
}

func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	return b.String()
}

// PingFilesExpired is called when a room's files fully expire (optional hook).
func (h *Handler) PingFilesExpired(key string) {
	h.broker.ping(key)
}
