package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"model"
)

func setupTestServer(t *testing.T) *Server {
	t.Helper()

	baseline := [][]string{
		{"customer_id", "customer_name", "city", "age", "account_balance", "email", "date"},
		{"101", "Margaret", "New York", "29", "1450.50", "margaret@example.com", "2022-01-10"},
		{"102", "Karen", "Chicago", "34", "3200.00", "karry@example.com", "2021-02-15"},
	}

	rules := model.InferColumnRules(baseline)
	cfg := model.HeaderValidatorConfig{
		MaxNameLen:    16,
		HiddenNeurons: 24,
		LearningRate:  0.08,
		Epochs:        1200,
		Threshold:     0.80,
		ColumnRules:   rules,
	}

	hv := model.NewHeaderValidator(cfg)
	hv.Train(baseline[0])
	tv := model.NewTypeValidator(rules)

	tmpDir := t.TempDir()
	rulesPath := filepath.Join(tmpDir, "learned_rules.json")

	srv := NewServer(hv, tv, rulesPath)
	srv.OutputDir = filepath.Join(tmpDir, "persisted_csvs")
	t.Cleanup(srv.Shutdown)

	return srv
}

func uploadMultipartCSV(t *testing.T, handler http.HandlerFunc, filename, csvContent string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	_, _ = part.Write([]byte(csvContent))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/validate/csv", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestServer_SelfHealingAndDownloadFlow(t *testing.T) {
	srv := setupTestServer(t)

	typoCSV := strings.Join([]string{
		"customer_id,custmer_name,city,age,account_balnce,email,date",
		"109,Dinu,Berlin,31,2100.00,dinu@kek.com,2022-12-12",
		"111,Karl,Sankt Petersburg,28,765.40,karl@kek.com,2021-11-11",
	}, "\n")

	//treat these comments as documentation

	// 1. upload CSV with two typos -> expects 202 REQUIRES_CONFIRMATION
	rec1 := uploadMultipartCSV(t, srv.HandleCSVUpload, "example1.csv", typoCSV)
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec1.Code, rec1.Body.String())
	}

	var resp1 CSVValidationResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp1.SessionID == "" {
		t.Fatal("expected non-empty session_id")
	}

	// 2. confirm the suggested renames -> expects 200 VALID and saved file
	confirmPayload, _ := json.Marshal(ConfirmRequest{
		SessionID: resp1.SessionID,
		Approved:  true,
	})
	reqConfirm := httptest.NewRequest(http.MethodPost, "/validate/confirm", bytes.NewReader(confirmPayload))
	reqConfirm.Header.Set("Content-Type", "application/json")
	recConfirm := httptest.NewRecorder()
	srv.HandleConfirmation(recConfirm, reqConfirm)

	if recConfirm.Code != http.StatusOK {
		t.Fatalf("expected 200 OK after confirmation, got %d: %s", recConfirm.Code, recConfirm.Body.String())
	}

	var resp2 CSVValidationResponse
	if err := json.Unmarshal(recConfirm.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to decode confirm response: %v", err)
	}
	if !resp2.IsValid || resp2.SavedFilePath == "" {
		t.Fatalf("expected IsValid=true and non-empty SavedFilePath, got %+v", resp2)
	}

	// 3. download the saved file via /download
	savedFilename := filepath.Base(resp2.SavedFilePath)
	reqDown := httptest.NewRequest(http.MethodGet, "/download?file="+savedFilename, nil)
	recDown := httptest.NewRecorder()
	srv.HandleDownload(recDown, reqDown)

	if recDown.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /download, got %d: %s", recDown.Code, recDown.Body.String())
	}
	if !strings.Contains(recDown.Body.String(), "customer_name") || !strings.Contains(recDown.Body.String(), "account_balance") {
		t.Errorf("downloaded CSV did not contain healed headers: %s", recDown.Body.String())
	}

	// 4. re-upload the same typo CSV & auto-heal immediately (200 OK) via LearnedRules
	rec3 := uploadMultipartCSV(t, srv.HandleCSVUpload, "example1.csv", typoCSV)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on auto-healed repeat upload, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// 5. check directory traversal protection on /download
	reqTraversal := httptest.NewRequest(http.MethodGet, "/download?file=../../etc/passwd", nil)
	recTraversal := httptest.NewRecorder()
	srv.HandleDownload(recTraversal, reqTraversal)
	if recTraversal.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for traversal attempt, got %d", recTraversal.Code)
	}
}

