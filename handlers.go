package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

)

// Server holds shared state for HTTP handlers.
type Server struct {
	DataDir string
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/board", s.handleBoard)
	mux.HandleFunc("/api/story", s.handleStory)
	mux.HandleFunc("/api/story/move", s.handleMoveStory)
	mux.HandleFunc("/api/story/delete", s.handleDeleteStory)
	mux.HandleFunc("/api/story/get", s.handleGetStory)
	mux.HandleFunc("/api/column/create", s.handleCreateColumn)
	mux.HandleFunc("/api/column/rename", s.handleRenameColumn)
	mux.HandleFunc("/api/column/list-numbers", s.handleListColumnNumbers)
}

// handleBoard returns the full board state.
func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, err := Scan(s.DataDir)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, b)
}

// handleStory creates or updates a story (PUT = update, POST = create).
func (s *Server) handleStory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var story Story
	if err := json.NewDecoder(r.Body).Decode(&story); err != nil {
		jsonError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if story.ID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}
	if story.ColumnDir == "" {
		jsonError(w, "columnDir is required", http.StatusBadRequest)
		return
	}

	// Sanitise defaults.
	if story.Assignee == "" {
		story.Assignee = "UNKNOWN"
	}
	if story.Status == "" {
		story.Status = "UNKNOWN"
	}
	if story.Priority == "" {
		story.Priority = "UNKNOWN"
	}

	// On POST (create), refuse to overwrite an existing story.
	if r.Method == http.MethodPost && StoryExists(s.DataDir, story.ColumnDir, story.ID) {
		jsonError(w, fmt.Sprintf("a story with ID %q already exists in that column", story.ID), http.StatusConflict)
		return
	}

	if err := WriteStory(s.DataDir, &story); err != nil {
		jsonError(w, "failed to write story: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// handleMoveStory moves a story file from one column to another.
func (s *Server) handleMoveStory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		StoryID       string `json:"storyId"`
		FromColumnDir string `json:"fromColumnDir"`
		ToColumnDir   string `json:"toColumnDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.StoryID == "" || req.FromColumnDir == "" || req.ToColumnDir == "" {
		jsonError(w, "storyId, fromColumnDir, toColumnDir are required", http.StatusBadRequest)
		return
	}
	if err := MoveStory(s.DataDir, req.StoryID, req.FromColumnDir, req.ToColumnDir); err != nil {
		jsonError(w, "failed to move story: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// handleDeleteStory deletes a story file.
func (s *Server) handleDeleteStory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		StoryID   string `json:"storyId"`
		ColumnDir string `json:"columnDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.StoryID == "" || req.ColumnDir == "" {
		jsonError(w, "storyId and columnDir are required", http.StatusBadRequest)
		return
	}
	if err := DeleteStory(s.DataDir, req.StoryID, req.ColumnDir); err != nil {
		jsonError(w, "failed to delete story: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// handleGetStory reads a single story from disk and returns it.
// Used by the frontend to check for out-of-band changes.
func (s *Server) handleGetStory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	storyID := r.URL.Query().Get("id")
	columnDir := r.URL.Query().Get("columnDir")
	if storyID == "" || columnDir == "" {
		jsonError(w, "id and columnDir are required", http.StatusBadRequest)
		return
	}

	// Check if it even exists.
	if !StoryExists(s.DataDir, columnDir, storyID) {
		// Return a 404-style JSON so the client can handle deletion.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}

	story, warn := ReadStoryFromDisk(s.DataDir, columnDir, storyID)
	if warn != nil {
		jsonError(w, warn.Message, http.StatusUnprocessableEntity)
		return
	}
	jsonOK(w, story)
}

var columnNameRe = regexp.MustCompile(`^[A-Za-z0-9 _-]+$`)

// handleCreateColumn creates a new ##.Name directory.
func (s *Server) handleCreateColumn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Number int    `json:"number"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Number < 0 || req.Number > 99 {
		jsonError(w, "number must be 0-99", http.StatusBadRequest)
		return
	}
	if req.Name == "" || !columnNameRe.MatchString(req.Name) {
		jsonError(w, "name must be non-empty and contain only letters, digits, spaces, hyphens, underscores", http.StatusBadRequest)
		return
	}

	// Check for duplicate number in existing columns.
	if dup, err := columnNumberExists(s.DataDir, req.Number); err != nil {
		jsonError(w, "could not check existing columns: "+err.Error(), http.StatusInternalServerError)
		return
	} else if dup != "" {
		jsonError(w, fmt.Sprintf("column number %02d already exists as %q", req.Number, dup), http.StatusConflict)
		return
	}

	if err := CreateColumn(s.DataDir, req.Number, req.Name); err != nil {
		jsonError(w, "failed to create column: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok", "dirName": fmt.Sprintf("%02d.%s", req.Number, req.Name)})
}

// handleRenameColumn renames an existing column directory.
func (s *Server) handleRenameColumn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OldDirName string `json:"oldDirName"`
		NewNumber  int    `json:"newNumber"`
		NewName    string `json:"newName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.OldDirName == "" {
		jsonError(w, "oldDirName is required", http.StatusBadRequest)
		return
	}
	if req.NewNumber < 0 || req.NewNumber > 99 {
		jsonError(w, "number must be 0-99", http.StatusBadRequest)
		return
	}
	if req.NewName == "" || !columnNameRe.MatchString(req.NewName) {
		jsonError(w, "name must be non-empty and contain only letters, digits, spaces, hyphens, underscores", http.StatusBadRequest)
		return
	}

	newDirName := fmt.Sprintf("%02d.%s", req.NewNumber, req.NewName)
	if newDirName != req.OldDirName {
		// Check the new number isn't taken by a different column.
		if dup, err := columnNumberExists(s.DataDir, req.NewNumber); err != nil {
			jsonError(w, "could not check existing columns: "+err.Error(), http.StatusInternalServerError)
			return
		} else if dup != "" && dup != req.OldDirName {
			jsonError(w, fmt.Sprintf("column number %02d already exists as %q", req.NewNumber, dup), http.StatusConflict)
			return
		}
	}

	// Verify old dir exists.
	if _, err := os.Stat(filepath.Join(s.DataDir, req.OldDirName)); os.IsNotExist(err) {
		jsonError(w, "column not found on disk", http.StatusNotFound)
		return
	}

	if err := RenameColumn(s.DataDir, req.OldDirName, req.NewNumber, req.NewName); err != nil {
		jsonError(w, "failed to rename column: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok", "dirName": newDirName})
}

// handleListColumnNumbers returns the list of currently used column numbers,
// so the frontend can prevent duplicates when creating/renaming.
func (s *Server) handleListColumnNumbers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, err := os.ReadDir(s.DataDir)
	if err != nil {
		jsonError(w, "cannot read data directory", http.StatusInternalServerError)
		return
	}
	colRe := regexp.MustCompile(`^(\d{2})\.(.+)$`)
	nums := []int{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := colRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		nums = append(nums, n)
	}
	jsonOK(w, map[string]interface{}{"numbers": nums})
}

// columnNumberExists returns the dirName of any existing column with the given
// number, or "" if none.
func columnNumberExists(dataDir string, number int) (string, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("%02d.", number)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			return e.Name(), nil
		}
	}
	return "", nil
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
