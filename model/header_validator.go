package model

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"gonum.org/v1/gonum/mat"
)

type HeaderValidatorConfig struct {
	MaxNameLen     int      `json:"max_name_len"`
	HiddenNeurons  int      `json:"hidden_neurons"`
	LearningRate   float64  `json:"learning_rate"`
	Epochs         int      `json:"epochs"`
	Threshold      float64  `json:"threshold"`
	ValidHeaders   []string `json:"valid_headers"`
	StringOnlyCols []string `json:"string_only_cols"`
	ColumnRules    map[string] ColumnRule `json:"column_rules"`
}

type HeaderValidator struct {
	Config        HeaderValidatorConfig
	WeightsHidden *mat.Dense
	BiasHidden    *mat.Dense
	WeightsOutput *mat.Dense
	BiasOutput    *mat.Dense
}

type ExportedHeaderValidator struct {
	Config        HeaderValidatorConfig `json:"config"`
	WeightsHidden []float64             `json:"weights_hidden"`
	BiasHidden    []float64             `json:"bias_hidden"`
	WeightsOutput []float64             `json:"weights_output"`
	BiasOutput    []float64             `json:"bias_output"`
}

func NewHeaderValidator(cfg HeaderValidatorConfig) *HeaderValidator {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	hv := &HeaderValidator{
		Config:        cfg,
		WeightsHidden: mat.NewDense(cfg.MaxNameLen, cfg.HiddenNeurons, nil),
		BiasHidden:    mat.NewDense(1, cfg.HiddenNeurons, nil),
		WeightsOutput: mat.NewDense(cfg.HiddenNeurons, 1, nil),
		BiasOutput:    mat.NewDense(1, 1, nil),
	}
	initRand(hv.WeightsHidden, rng)
	initRand(hv.BiasHidden, rng)
	initRand(hv.WeightsOutput, rng)
	initRand(hv.BiasOutput, rng)
	return hv
}

// reserves 0.0 strictly for padding.
func (hv *HeaderValidator) EncodeHeader(name string) []float64 {
	clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\ufeff")))
	vec := make([]float64, hv.Config.MaxNameLen)
	for i := 0; i < len(clean) && i < hv.Config.MaxNameLen; i++ {
		ch := clean[i]
		if ch >= 32 && ch <= 126 {
			vec[i] = 0.1 + 0.9*(float64(ch-32)/94.0)
		} else {
			vec[i] = 0.05
		}
	}
	return vec
}


func (hv *HeaderValidator) Train(validHeaders []string) {
	hv.Config.ValidHeaders = validHeaders
	validSet := make(map[string]bool, len(validHeaders))
	for _, h := range validHeaders {
		validSet[strings.ToLower(strings.TrimSpace(h))] = true
	}

	var samples [][]float64

	var targets []float64

	// Positive samples
	for _, h := range validHeaders {
		for rep := 0; rep < 30; rep++ {
			samples = append(samples, hv.EncodeHeader(h))
			targets = append(targets, 1.0)
		}
	}

	// Negative candidates + synthetic typos
	candidates := []string{
		"id", "target", "label", "col", "name", "value", "extra", "unknown",
		"index", "unnamed", "field", "data", "temp", "dummy", "test", "null",
	}
	for ch := 'a'; ch <= 'z'; ch++ {
		candidates = append(candidates, string(ch))
	}
	for _, h := range validHeaders {
		candidates = append(candidates, generateSyntheticTypos(h)...)
	}

	seenNeg := make(map[string]bool)
	for _, cand := range candidates {
		norm := strings.ToLower(strings.TrimSpace(cand))
		if norm == "" || validSet[norm] || seenNeg[norm] {
			continue
		}
		seenNeg[norm] = true
		samples = append(samples, hv.EncodeHeader(norm))
		targets = append(targets, 0.0)
	}


	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	lr := 0.08

	epochs := 1500

	for epoch := 0; epoch < epochs; epoch++ {

		// SGD convergence
		rng.Shuffle(len(samples), func(i, j int) {
			samples[i], samples[j] = samples[j], samples[i]
			targets[i], targets[j] = targets[j], targets[i]
		})

		for idx, sample := range samples {
			X := mat.NewDense(1, hv.Config.MaxNameLen, sample)
			target := targets[idx]

			var hiddenIn, hiddenAct, outIn, outAct mat.Dense
			hiddenIn.Mul(X, hv.WeightsHidden)
			addBias(&hiddenIn, hv.BiasHidden)
			hiddenAct.Apply(func(i, j int, v float64) float64 { return 1.0 / (1.0 + math.Exp(-v)) }, &hiddenIn)

			outIn.Mul(&hiddenAct, hv.WeightsOutput)
			addBias(&outIn, hv.BiasOutput)
			outAct.Apply(func(i, j int, v float64) float64 { return 1.0 / (1.0 + math.Exp(-v)) }, &outIn)

			pred := outAct.At(0, 0)
			//hmm...
			outDeltaVal := target-pred
			outDelta := mat.NewDense(1, 1, []float64{outDeltaVal})

			var hidErr, hidSlope, hidDelta mat.Dense
			hidErr.Mul(outDelta, hv.WeightsOutput.T())
			hidSlope.Apply(func(i, j int, v float64) float64 { return v * (1.0 - v) }, &hiddenAct)
			hidDelta.MulElem(&hidErr, &hidSlope)

			var outAdj, hidAdj mat.Dense
			outAdj.Mul(hiddenAct.T(), outDelta)
			outAdj.Scale(lr, &outAdj)
			hv.WeightsOutput.Add(hv.WeightsOutput, &outAdj)

			hidAdj.Mul(X.T(), &hidDelta)
			hidAdj.Scale(lr, &hidAdj)
			hv.WeightsHidden.Add(hv.WeightsHidden, &hidAdj)

			updateBias(hv.BiasOutput, outDelta, lr)
			updateBias(hv.BiasHidden, &hidDelta, lr)
		}
	}
}


func (hv *HeaderValidator) PredictColumn(colName string) (bool, float64) {
	vec := hv.EncodeHeader(colName)
	X := mat.NewDense(1, hv.Config.MaxNameLen, vec)

	var hiddenIn, hiddenAct, outIn, outAct mat.Dense
	hiddenIn.Mul(X, hv.WeightsHidden)
	addBias(&hiddenIn, hv.BiasHidden)
	hiddenAct.Apply(func(i, j int, v float64) float64 { return 1.0 / (1.0 + math.Exp(-v)) }, &hiddenIn)

	outIn.Mul(&hiddenAct, hv.WeightsOutput)
	addBias(&outIn, hv.BiasOutput)
	outAct.Apply(func(i, j int, v float64) float64 { return 1.0 / (1.0 + math.Exp(-v)) }, &outIn)

	score := outAct.At(0, 0)
	return score >= hv.Config.Threshold, score
}

func (hv *HeaderValidator) SaveToFile(filename string) error {
	export := ExportedHeaderValidator{
		Config:        hv.Config,
		WeightsHidden: hv.WeightsHidden.RawMatrix().Data,
		BiasHidden:    hv.BiasHidden.RawMatrix().Data,
		WeightsOutput: hv.WeightsOutput.RawMatrix().Data,
		BiasOutput:    hv.BiasOutput.RawMatrix().Data,
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

func LoadHeaderValidatorFromFile(filename string) (*HeaderValidator, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var export ExportedHeaderValidator
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, err
	}
	return &HeaderValidator{
		Config:        export.Config,
		WeightsHidden: mat.NewDense(export.Config.MaxNameLen, export.Config.HiddenNeurons, export.WeightsHidden),
		BiasHidden:    mat.NewDense(1, export.Config.HiddenNeurons, export.BiasHidden),
		WeightsOutput: mat.NewDense(export.Config.HiddenNeurons, 1, export.WeightsOutput),
		BiasOutput:    mat.NewDense(1, 1, export.BiasOutput),
	}, nil
}

func generateSyntheticTypos(header string) []string {
	clean := strings.ToLower(strings.TrimSpace(header))
	if len(clean) == 0 {
		return nil
	}

	// some dropping, some suffixing... 
	var typos []string
	typos = append(typos, clean+"_1", clean+"_x", "old_"+clean, clean+"s")

	for i := 0; i < len(clean); i++ {
		if len(clean) > 1 {
			typos = append(typos, clean[:i]+clean[i+1:])
		}
	}

	//sWap adjacent characters
	bytes := []byte(clean)
	for i := 0; i < len(bytes)-1; i++ {
		if bytes[i] != bytes[i+1] {
			swapped := make([]byte, len(bytes))
			copy(swapped, bytes)
			swapped[i], swapped[i+1] = swapped[i+1], swapped[i]
			typos = append(typos, string(swapped))
		}
	}

	return typos
}

func initRand(m *mat.Dense, rng *rand.Rand) {
	r, c := m.Dims()
	for i := 0; i < r; i++ {
		for j := 0; j < c; j++ {
			m.Set(i, j, rng.NormFloat64()*0.5)
		}
	}
}

func addBias(target, bias *mat.Dense) {
	r, c := target.Dims()
	_, bc := bias.Dims()
	for i := 0; i < r; i++ {
		for j := 0; j < c; j++ {
			target.Set(i, j, target.At(i, j)+bias.At(0, j%bc))
		}
	}
}

func updateBias(bias, delta *mat.Dense, lr float64) {
	r, c := delta.Dims()
	for j := 0; j < c; j++ {
		var sum float64
		for i := 0; i < r; i++ {
			sum += delta.At(i, j)
		}
		bias.Set(0, j, bias.At(0, j)+lr*sum)
	}
}

