package model

import (
	"fmt"
	"math"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

type ColumnDataType string

const (
	TypeString  ColumnDataType = "string"
	TypeNumeric ColumnDataType = "numeric"
	TypeEmail   ColumnDataType = "email"
	TypeDate    ColumnDataType = "date"
)

type ColumnRule struct {
	DataType   ColumnDataType `json:"data_type"`
	MinVal     *float64       `json:"min_val,omitempty"`
	MaxVal     *float64       `json:"max_val,omitempty"`
	DateLayout string         `json:"date_layout,omitempty"`
	Unique     bool           `json: "unique,omitempty"`
}

type RowTypeError struct {
	Row    int    `json:"row"`
	Column string `json:"column"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

type TypeValidator struct {
	Rules map[string]ColumnRule
}

var supportedDateLayouts = []string{
	"2006-01-02",
	"02/01/2006",
	"01/02/2006",
	"2006/01/02",
	time.RFC3339,
}

func NewTypeValidator(rules map[string]ColumnRule) *TypeValidator {
	normalized := make(map[string]ColumnRule, len(rules))
	for col, rule := range rules {
		normalized[strings.ToLower(strings.TrimSpace(col))] = rule
	}
	return &TypeValidator{Rules: normalized}
}

func isValidEmail(val string) bool {
	trimmed := strings.TrimSpace(val)
	if !strings.Contains(trimmed, "@") {
		return false
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr.Address != trimmed {
		return false
	}
	parts := strings.Split(trimmed, "@")
	return len(parts) == 2 && strings.Contains(parts[1], ".")
}

func detectDateLayout(val string) (string, bool) {
	trimmed := strings.TrimSpace(val)
	for _, layout := range supportedDateLayouts {
		if _, err := time.Parse(layout, trimmed); err == nil {
			return layout, true
		}
	}
	return "", false
}

func isPureString(val string) bool {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return false
	}
	if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return false
	}
	lower := strings.ToLower(trimmed)
	if lower == "true" || lower == "false" {
		return false
	}
	return true
}

//training CSV records to infer data types, numeric bounds, date layouts, and email formats.
func InferColumnRules(records [][]string) map[string]ColumnRule {
	rules := make(map[string]ColumnRule)
	if len(records) < 2 {
		return rules
	}

	headers := records[0]
	colCount := len(headers)

	for colIdx := 0; colIdx < colCount; colIdx++ {
		colName := strings.TrimSpace(headers[colIdx])
		if colName == "" {
			continue
		}

		allNumeric := true
		allEmail := true
		allDate := true
		allString := true
		hasData := false

		minVal := math.Inf(1)
		maxVal := math.Inf(-1)
		detectedLayout := ""

		for rowIdx := 1; rowIdx < len(records); rowIdx++ {
			row := records[rowIdx]
			if colIdx >= len(row) {
				continue
			}
			val := strings.TrimSpace(row[colIdx])
			if val == "" {
				continue
			}
			hasData = true

			if num, err := strconv.ParseFloat(val, 64); err == nil {
				if num < minVal {
					minVal = num
				}
				if num > maxVal {
					maxVal = num
				}
			} else {
				allNumeric = false
			}

			if !isValidEmail(val) {
				allEmail = false
			}

			if layout, ok := detectDateLayout(val); ok {
				if detectedLayout == "" {
					detectedLayout = layout
				}
			} else {
				allDate = false
			}

			if !isPureString(val) {
				allString = false
			}
		}

		if !hasData {
			continue
		}

		switch {
		case allNumeric:

			lowerCol := strings.ToLower(colName)
			isIdentifier := lowerCol == "id" || strings.HasSuffix(lowerCol, "_id")


			if isIdentifier {
				seen := make(map[string]bool)
				allDistinct := true
				for rowIdx := 1; rowIdx < len(records); rowIdx++ {
					if colIdx >= len(records[rowIdx]) {
						continue
					}
					v := strings.TrimSpace(records[rowIdx][colIdx])
					if v == "" {
						continue
					}
					if seen[v] {
						allDistinct = false
						break
					}
					seen[v] = true
				}
				zero := 0.0
				rules[colName] = ColumnRule{
					DataType: TypeNumeric,
					MinVal:   &zero,
					Unique:   allDistinct,
				}
				continue
			}
			span := maxVal - minVal
			if span == 0 {
				span = math.Abs(maxVal) * 0.2
			}
			paddedMin := minVal - (span * 0.2)
			paddedMax := maxVal + (span * 0.2)
			if minVal >= 0 && paddedMin < 0 {
				paddedMin = 0
			}
			rules[colName] = ColumnRule{
				DataType: TypeNumeric,
				MinVal:   &paddedMin,
				MaxVal:   &paddedMax,
			}
		case allEmail:
			rules[colName] = ColumnRule{
				DataType: TypeEmail,
			}
		case allDate:
			rules[colName] = ColumnRule{
				DataType:   TypeDate,
				DateLayout: detectedLayout,
			}
		case allString:
			rules[colName] = ColumnRule{
				DataType: TypeString,
			}
		}
	}

	return rules
}

func (tv *TypeValidator) ValidateRows(records [][]string) []RowTypeError {
	if len(records) < 2 {
		return nil
	}

	var errors []RowTypeError
	headers := records[0]

	seenUnique := make(map[string]map[string]int)
	for rowIdx := 1; rowIdx < len(records); rowIdx++ {
		row := records[rowIdx]
		for colIdx, cellValue := range row {
			if colIdx >= len(headers) {
				continue
			}

			colName := strings.ToLower(strings.TrimSpace(headers[colIdx]))
			rule, hasRule := tv.Rules[colName]
			if !hasRule {
				continue
			}

			trimmed := strings.TrimSpace(cellValue)
			if trimmed == "" {
				errors = append(errors, RowTypeError{
					Row:    rowIdx,
					Column: headers[colIdx],
					Value:  cellValue,
					Reason: fmt.Sprintf("empty value is not allowed in %s column", rule.DataType),
				})
				continue
			}

			if rule.Unique {
				if seenUnique[colName] == nil {
					seenUnique[colName] = make(map[string]int)
				}

				// treating numerical ids as duplicates
				normKey := trimmed
				if num, err := strconv.ParseFloat(trimmed, 64); err == nil {
					normKey = strconv.FormatFloat(num, 'f', -1, 64)
				}

				if firstRow, exists := seenUnique[colName][normKey]; exists {
					errors = append(errors, RowTypeError{
						Row:    rowIdx,
						Column: headers[colIdx],
						Value:  cellValue,
						Reason: fmt.Sprintf("duplicate value '%s' in unique column '%s' (first seen at row %d)", trimmed, headers[colIdx], firstRow),
					})
				} else {
					seenUnique[colName][normKey] = rowIdx
				}
			}

			switch rule.DataType {
			case TypeNumeric:
				num, err := strconv.ParseFloat(trimmed, 64)
				if err != nil {
					errors = append(errors, RowTypeError{
						Row:    rowIdx,
						Column: headers[colIdx],
						Value:  cellValue,
						Reason: fmt.Sprintf("expected numeric value, got '%s'", trimmed),
					})
					continue
				}
				if rule.MinVal != nil && num < *rule.MinVal {
					errors = append(errors, RowTypeError{
						Row:    rowIdx,
						Column: headers[colIdx],
						Value:  cellValue,
						Reason: fmt.Sprintf("value %.2f is below allowed minimum %.2f", num, *rule.MinVal),
					})
				} else if rule.MaxVal != nil && num > *rule.MaxVal {
					errors = append(errors, RowTypeError{
						Row:    rowIdx,
						Column: headers[colIdx],
						Value:  cellValue,
						Reason: fmt.Sprintf("value %.2f exceeds allowed maximum %.2f", num, *rule.MaxVal),
					})
				}

			case TypeEmail:
				if !isValidEmail(trimmed) {
					errors = append(errors, RowTypeError{
						Row:    rowIdx,
						Column: headers[colIdx],
						Value:  cellValue,
						Reason: fmt.Sprintf("invalid email format '%s'", trimmed),
					})
				}

			case TypeDate:
				layout := rule.DateLayout
				if layout == "" {
					layout = "2006-01-02"
				}
				if _, err := time.Parse(layout, trimmed); err != nil {
					errors = append(errors, RowTypeError{
						Row:    rowIdx,
						Column: headers[colIdx],
						Value:  cellValue,
						Reason: fmt.Sprintf("invalid date '%s', expected format %s", trimmed, layout),
					})
				}

			case TypeString:
				if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
					errors = append(errors, RowTypeError{
						Row:    rowIdx,
						Column: headers[colIdx],
						Value:  cellValue,
						Reason: fmt.Sprintf("numeric value '%s' is not allowed in a string-only column", trimmed),
					})
					continue
				}
				lower := strings.ToLower(trimmed)
				if lower == "true" || lower == "false" {
					errors = append(errors, RowTypeError{
						Row:    rowIdx,
						Column: headers[colIdx],
						Value:  cellValue,
						Reason: fmt.Sprintf("boolean value '%s' is not allowed in a string-only column", trimmed),
					})
				}
			}
		}
	}

	return errors
}

