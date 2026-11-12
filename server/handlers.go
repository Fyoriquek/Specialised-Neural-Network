package server

import (
	"bytes"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agent"
	"model"
)

type ColumnScore struct {
	Column     string  `json:"column"`
	Recognized bool    `json:"recognized"`
	Confidence float64 `json:"confidence"`
}

type CSVValidationResponse struct {
	Status          string               `json:"status"`
	IsValid         bool                 `json:"is_valid"`
	DetectedHeaders []string             `json:"detected_headers"`
	ColumnScores    []ColumnScore        `json:"column_scores"`
	TypeErrors      []model.RowTypeError `json:"type_errors,omitempty"`
	SessionID       string               `json:"session_id,omitempty"`
	SavedFilePath   string               `json:"saved_file_path,omitempty"`
	UpdatedCSV      string               `json:"updated_csv,omitempty"`
	Message         string               `json:"message"`
	Duration        string               `json:"duration"`
}

type ConfirmRequest struct {
	SessionID string `json:"session_id"`
	Approved  bool   `json:"approved"`
}

type CachedCSVSession struct {
	OriginalFilename string
	AllRecords       [][]string
	SuggestedMap     map[string]string
	CreatedAt        time.Time
}

type Server struct {
	Validator     *model.HeaderValidator
	TypeValidator *model.TypeValidator
	Agent         *agent.SchemaAgent
	Cache         map[string]CachedCSVSession
	LearnedRules  map[string]string
	RulesFile     string
	OutputDir     string
	CacheMutex    sync.RWMutex
	SessionTTL    time.Duration
	stopJanitor   chan struct{}
}

func NewServer(hv *model.HeaderValidator, tv *model.TypeValidator, rulesFile string) *Server {
	outDir := "persisted_csvs"
	_ = os.MkdirAll(outDir, 0755)

	s := &Server{
		Validator:     hv,
		TypeValidator: tv,
		Agent:         agent.NewSchemaAgent(hv.Config.ValidHeaders),
		Cache:         make(map[string]CachedCSVSession),
		LearnedRules:  make(map[string]string),
		RulesFile:     rulesFile,
		OutputDir:     outDir,
		SessionTTL:    5 * time.Minute,
		stopJanitor:   make(chan struct{}),
	}

	s.loadLearnedRules()
	go s.startCacheJanitor(1 * time.Minute)
	return s
}

func (s *Server) Shutdown() {
	close(s.stopJanitor)
}

func (s *Server) loadLearnedRules() {
	if s.RulesFile == "" {
		return
	}
	data, err := os.ReadFile(s.RulesFile)
	if err != nil {
		return
	}
	var loaded map[string]string
	if err := json.Unmarshal(data, &loaded); err == nil && loaded != nil {
		s.LearnedRules = loaded
		log.Printf("Loaded %d learned header rules from %s", len(loaded), s.RulesFile)
	}
}

func (s *Server) saveLearnedRulesLocked() {
	if s.RulesFile == "" {
		return
	}
	data, err := json.MarshalIndent(s.LearnedRules, "", "  ")
	if err != nil {
		log.Printf("Failed to serialize learned rules: %v", err)
		return
	}
	if err := os.WriteFile(s.RulesFile, data, 0644); err != nil {
		log.Printf("Failed to persist learned rules to %s: %v", s.RulesFile, err)
	}
}

func (s *Server) startCacheJanitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.CacheMutex.Lock()
			now := time.Now()
			for id, session := range s.Cache {
				if now.Sub(session.CreatedAt) > s.SessionTTL {
					delete(s.Cache, id)
				}
			}
			s.CacheMutex.Unlock()
		case <-s.stopJanitor:
			return
		}
	}
}

func (s *Server) HandleCSVUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Missing 'file' field in multipart/form-data upload"}`))
		return
	}
	defer file.Close()

	rawBytes, err := io.ReadAll(file)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Failed to read uploaded CSV"}`))
		return
	}

	content := strings.TrimPrefix(string(rawBytes), "\ufeff")
	firstLine, _, _ := strings.Cut(content, "\n")
	reader := csv.NewReader(strings.NewReader(content))
	if strings.Contains(firstLine, ";") && !strings.Contains(firstLine, ",") {
		reader.Comma = ';'
	}

	allRecords, err := reader.ReadAll()
	if err != nil || len(allRecords) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "CSV file is empty or unreadable"}`))
		return
	}

	s.CacheMutex.RLock()
	autoApplied := false
	for colIdx, h := range allRecords[0] {
		clean := strings.TrimSpace(h)
		if mapped, exists := s.LearnedRules[clean]; exists {
			clean = mapped
			autoApplied = true
		}
		allRecords[0][colIdx] = clean
	}
	s.CacheMutex.RUnlock()

	s.evaluateRecords(w, header.Filename, allRecords, start, autoApplied)
}

func (s *Server) HandleConfirmation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()
	var req ConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Malformed JSON payload"}`))
		return
	}

	s.CacheMutex.Lock()
	session, exists := s.Cache[req.SessionID]
	if !exists || time.Since(session.CreatedAt) > s.SessionTTL {
		if exists {
			delete(s.Cache, req.SessionID)
		}
		s.CacheMutex.Unlock()
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "Session expired or not found"}`))
		return
	}
	delete(s.Cache, req.SessionID)

	currentHeaders := session.AllRecords[0]

	if !req.Approved {
		s.CacheMutex.Unlock()
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(CSVValidationResponse{
			Status:          "REJECTED",
			IsValid:         false,
			DetectedHeaders: currentHeaders,
			Message:         "Column rename rejected by user.",
			Duration:        time.Since(start).String(),
		})
		return
	}

	for oldCol, newCol := range session.SuggestedMap {
		s.LearnedRules[oldCol] = newCol
	}
	s.saveLearnedRulesLocked()

	for colIdx, h := range session.AllRecords[0] {
		if target, ok := session.SuggestedMap[h]; ok {
			session.AllRecords[0][colIdx] = target
		}
	}
	s.CacheMutex.Unlock()

	s.evaluateRecords(w, session.OriginalFilename, session.AllRecords, start, true)

}


//for downloading

func (s *Server) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"error": "Method not allowed, use GET"}`))
		return
	}

	rawFilename := strings.TrimSpace(r.URL.Query().Get("file"))
	if rawFilename == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Missing required 'file' query parameter"}`))
		return
	}

	// prevent directory traversal
	safeName := filepath.Base(rawFilename)
	if safeName == "." || safeName == "/" || strings.HasPrefix(safeName, ".") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid filename"}`))
		return
	}

	fullPath := filepath.Join(s.OutputDir, safeName)

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "Requested persisted and validated CSV file not found"}`))
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeName))
	http.ServeFile(w, r, fullPath)
}


func (s *Server) evaluateRecords(w http.ResponseWriter, origFilename string, records [][]string, start time.Time, wasRemapped bool) {
	if len(records) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "CSV contains no records"}`))
		return
	}

	headers := records[0]

	var scores []ColumnScore
	headerMap := make(map[string]float64, len(headers))

	for _, col := range headers {
		ok, conf := s.Validator.PredictColumn(col)
		scores = append(scores, ColumnScore{
			Column:     col,
			Recognized: ok,
			Confidence: conf,
		})
		headerMap[col] = conf
	}

	// schema alignment using SchemaAgent to be checked
	resolution := s.Agent.Analyze(headerMap)
	if resolution.Status == agent.StatusRequiresConfirm {
		sessionID := generateSessionID()
		s.CacheMutex.Lock()
		s.Cache[sessionID] = CachedCSVSession{
			OriginalFilename: origFilename,
			AllRecords:       records,
			SuggestedMap:     resolution.SuggestedMap,
			CreatedAt:        time.Now(),
		}
		s.CacheMutex.Unlock()

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(CSVValidationResponse{
			Status:          string(resolution.Status),
			IsValid:         false,
			DetectedHeaders: headers,
			ColumnScores:    scores,
			SessionID:       sessionID,
			Message:         resolution.Message,
			Duration:        time.Since(start).String(),
		})
		return
	}

	if resolution.Status == agent.StatusFailed  {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(CSVValidationResponse{
			Status:          "FAILED",
			IsValid:         false,
			DetectedHeaders: headers,
			ColumnScores:    scores,
			Message:         resolution.Message,
			Duration:        time.Since(start).String(),
		})
		return
	}


	// row data types for string-only columns validation
	typeErrors := s.TypeValidator.ValidateRows(records)
	if len(typeErrors) > 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(CSVValidationResponse{
			Status:          "TYPE_VALIDATION_FAILED",
			IsValid:         false,
			DetectedHeaders: headers,
			ColumnScores:    scores,
			TypeErrors:      typeErrors,
			Message:         fmt.Sprintf("Found %d row type validation error(s).", len(typeErrors)),
			Duration:        time.Since(start).String(),
		})
		return
	}

	// serializing & saving the validated  &/or fixing/cleaning CSV
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.WriteAll(records)
	writer.Flush()
	csvString := buf.String()

	var savedPath, updatedCSV string
	if wasRemapped {
		baseName := filepath.Base(origFilename)
		outName := fmt.Sprintf("cleaned_%d_%s", time.Now().Unix(), baseName)
		savedPath = filepath.Join(s.OutputDir, outName)
		if err := os.WriteFile(savedPath, []byte(csvString), 0644); err != nil {
			log.Printf("Failed to save cleaned CSV to %s: %v", savedPath, err)
			savedPath = ""
		}
		updatedCSV = csvString
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CSVValidationResponse{
		Status:          "VALID",
		IsValid:         true,
		DetectedHeaders: headers,
		ColumnScores:    scores,
		SavedFilePath:   savedPath,
		UpdatedCSV:      updatedCSV,
		Message:         "CSV schema and row data types validated successfully.",
		Duration:        time.Since(start).String(),
	})
}

func generateSessionID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

