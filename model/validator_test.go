package model

import (
	"path/filepath"
	"testing"
)

func TestHeaderValidator_TrainPredictAndSaveLoad(t *testing.T) {
	headers := []string{"customer_id", "customer_name", "city", "age", "account_balance", "email", "date"}

	cfg := HeaderValidatorConfig{
		MaxNameLen:    16,
		HiddenNeurons: 24,
		LearningRate:  0.08,
		Epochs:        1500,
		Threshold:     0.80,
	}

	hv := NewHeaderValidator(cfg)
	hv.Train(headers)

	for _, h := range headers {
		ok, score := hv.PredictColumn(h)
		if !ok || score < 0.80 {
			t.Errorf("expected valid header %q to be recognized (>=0.80), got ok=%v score=%.4f", h, ok, score)
		}
	}

	typos := []string{"custmer_name", "account_balnce", "emial", "unknown_col"}
	for _, typo := range typos {
		ok, score := hv.PredictColumn(typo)
		if ok {
			t.Errorf("expected typo header %q to be rejected, got ok=true score=%.4f", typo, score)
		}
	}

	// test persistence roundtrip
	tmpModel := filepath.Join(t.TempDir(), "test_model.json")
	if err := hv.SaveToFile(tmpModel); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	loaded, err := LoadHeaderValidatorFromFile(tmpModel)
	if err != nil {
		t.Fatalf("LoadHeaderValidatorFromFile failed: %v", err)
	}

	ok, score := loaded.PredictColumn("email")
	if !ok || score < 0.80 {
		t.Errorf("loaded model failed prediction for 'email': ok=%v score=%.4f", ok, score)
	}
}

func TestTypeValidator_RulesAndDuplicates(t *testing.T) {
	baseline := [][]string{
		{"customer_id", "customer_name", "city", "age", "account_balance", "email", "date"},
		{"101", "Karen", "New York", "29", "1450.50", "karen@example.com", "2024-01-10"},
		{"102", "Mishu", "Bucharest", "34", "3200.00", "mimi@example.com", "2024-02-15"},
		{"103", "Simon", "Berlin", "41", "890.75", "simo@kek.com", "2024-03-20"},
	}

	rules := InferColumnRules(baseline)
	tv := NewTypeValidator(rules)

	t.Run("Valid Rows Pass Including Higher Customer IDs", func(t *testing.T) {
		upload := [][]string{
			{"customer_id", "customer_name", "city", "age", "account_balance", "email", "date"},
			{"109", "Dinu", "Berlin", "31", "2100.00", "dinu@kek.com", "2022-12-12"},
			{"111", "Karl", "Sankt Petersburg", "28", "765.40", "karl@kek.com", "2021-11-11"},
		}

		errs := tv.ValidateRows(upload)
		if len(errs) != 0 {
			t.Fatalf("expected 0 type errors, got %d: %+v", len(errs), errs)
		}
	})

	t.Run("Catches Duplicate IDs, Invalid Emails, Bad Dates, Out-of-Range Numbers, and Numbers in String Cols", func(t *testing.T) {
		badUpload := [][]string{
			{"customer_id", "customer_name", "city", "age", "account_balance", "email", "date"},
			{"109", "Dinu", "Berlin", "31", "2100.00", "dinu@kek.com", "2022-12-12"},
			{"109", "99999", "Berlin", "350", "2100.00", "not-an-email", "99-99-2022"},
		}

		errs := tv.ValidateRows(badUpload)
		if len(errs) != 5 {
			t.Fatalf("expected 5 validation errors on row 2, got %d: %+v", len(errs), errs)
		}
	})
}

