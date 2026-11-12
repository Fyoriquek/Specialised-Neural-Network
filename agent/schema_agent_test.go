package agent

import "testing"

func TestSchemaAgent_Analyze(t *testing.T) {
	expected := []string{"customer_id", "customer_name", "city", "age", "account_balance", "email", "date"}
	sa := NewSchemaAgent(expected)

	t.Run("Exact Match Returns Success", func(t *testing.T) {
		input := map[string]float64{
			"customer_id":     0.95,
			"customer_name":   0.94,
			"city":            0.96,
			"age":             0.99,
			"account_balance": 0.95,
			"email":           0.98,
			"date":            0.93,
		}

		res := sa.Analyze(input)
		if res.Status != StatusSuccess {
			t.Fatalf("expected StatusSuccess, got %s (%s)", res.Status, res.Message)
		}
	})

	t.Run("Multi-Column Typos Suggests Best Pairings", func(t *testing.T) {
		input := map[string]float64{
			"customer_id":    0.95,
			"custmer_name":   0.12, // Typo 1
			"city":           0.96,
			"age":            0.99,
			"account_balnce": 0.15, // Typo 2
			"email":          0.98,
			"date":           0.93,
		}

		res := sa.Analyze(input)
		if res.Status != StatusRequiresConfirm {
			t.Fatalf("expected StatusRequiresConfirm, got %s (%s)", res.Status, res.Message)
		}
		if res.SuggestedMap["custmer_name"] != "customer_name" {
			t.Errorf("expected custmer_name -> customer_name, got %s", res.SuggestedMap["custmer_name"])
		}
		if res.SuggestedMap["account_balnce"] != "account_balance" {
			t.Errorf("expected account_balnce -> account_balance, got %s", res.SuggestedMap["account_balnce"])
		}
	})

	t.Run("Column Count Mismatch Fails", func(t *testing.T) {
		input := map[string]float64{
			"customer_id": 0.95,
			"city":        0.96,
		}

		res := sa.Analyze(input)
		if res.Status != StatusFailed {
			t.Fatalf("expected StatusFailed, got %s", res.Status)
		}
	})

	t.Run("Completely Unrelated Multi-Column Names Fail Threshold", func(t *testing.T) {
		input := map[string]float64{
			"customer_id":     0.95,
			"zzzzzzzzzzzz":    0.05,
			"city":            0.96,
			"age":             0.99,
			"account_balance": 0.95,
			"xxxxxxxxxxxx":    0.04,
			"date":            0.93,
		}

		res := sa.Analyze(input)
		if res.Status != StatusFailed {
			t.Fatalf("expected StatusFailed for unrelated columns, got %s", res.Status)
		}
	})
}

