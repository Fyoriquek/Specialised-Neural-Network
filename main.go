package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"model"
	"server"
)

func main() {
	csvFile := "dataset.csv"
	modelFile := "header_validator.json"
	rulesFile := "learned_rules.json"
	port := ":8080"

	if _, err := os.Stat(csvFile); err == nil {
		rawBytes, err := os.ReadFile(csvFile)
		if err != nil {
			log.Fatalf("Failed to read %s: %v", csvFile, err)
		}

		content := strings.TrimPrefix(string(rawBytes), "\ufeff")
		firstLine, _, _ := strings.Cut(content, "\n")

		reader := csv.NewReader(strings.NewReader(content))
		if strings.Contains(firstLine, ";") && !strings.Contains(firstLine, ",") {
			reader.Comma = ';'
		}

		records, err := reader.ReadAll()
		if err != nil || len(records) == 0 {
			log.Fatalf("Failed to parse %s: %v", csvFile, err)
		}

		var headers []string
		for _, col := range records[0] {
			headers = append(headers, strings.TrimSpace(col))
		}

		columnRules := model.InferColumnRules(records)

		fmt.Printf("Training Header Neural Network on columns: %v\n", headers)
		fmt.Printf("column rules infer: %v\n", columnRules)

		// like eh...
		cfg := model.HeaderValidatorConfig{
			MaxNameLen:     16,
			HiddenNeurons:  24,
			LearningRate:   0.8,
			Epochs:         2000,
			Threshold:      0.80,
			ColumnRules:    columnRules,
		}

		hv := model.NewHeaderValidator(cfg)
		hv.Train(headers)

		if err := hv.SaveToFile(modelFile); err != nil {
			log.Fatalf("Failed to save model weights: %v", err)
		}
		fmt.Printf("Saved header validator weights to %s\n", modelFile)
	}

	validator, err := model.LoadHeaderValidatorFromFile(modelFile)
	if err != nil {
		log.Fatalf("Failed to load model: %v", err)
	}

	typeValidator := model.NewTypeValidator(validator.Config.ColumnRules)

	apiServer := server.NewServer(validator, typeValidator, rulesFile)
	defer apiServer.Shutdown()

	// a download enpoint might be useful for further automation 
	// and integration with data pipelines 
	http.HandleFunc("/validate/csv", apiServer.HandleCSVUpload)
	http.HandleFunc("/validate/confirm", apiServer.HandleConfirmation)
	http.HandleFunc("/download", apiServer.HandleDownload)

	log.Printf("CSV Validator running on http://localhost%s/validate/csv", port)
	log.Fatal(http.ListenAndServe(port, nil))
}

