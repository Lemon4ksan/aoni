// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"sort"
	"sync"
)

// PortPrediction represents a predicted port recommendation with its confidence score.
type PortPrediction struct {
	Port       int
	Confidence float64
}

// Predictor holds conditional probabilities P(TargetPort | KnownOpenPort) derived from global scan models.
type Predictor struct {
	mu           sync.RWMutex
	correlations map[int]map[int]float64
}

// NewPredictor instantiates a new [Predictor] seeded with default port correlation probability data.
func NewPredictor() *Predictor {
	p := &Predictor{correlations: make(map[int]map[int]float64)}
	p.seedDefaultCorrelations()
	return p
}

// Predict ranks candidate ports by likelihood of being open, based on currently known open ports.
func (p *Predictor) Predict(openPorts []int, threshold float64) []PortPrediction {
	p.mu.RLock()
	defer p.mu.RUnlock()

	openSet := make(map[int]struct{}, len(openPorts))
	for _, port := range openPorts {
		openSet[port] = struct{}{}
	}

	scores := make(map[int]float64)
	for _, open := range openPorts {
		if targets, ok := p.correlations[open]; ok {
			for targetPort, prob := range targets {
				if _, exists := openSet[targetPort]; !exists {
					if prob > scores[targetPort] {
						scores[targetPort] = prob
					}
				}
			}
		}
	}

	predictions := make([]PortPrediction, 0, len(scores))
	for targetPort, conf := range scores {
		if conf >= threshold {
			predictions = append(predictions, PortPrediction{Port: targetPort, Confidence: conf})
		}
	}

	sort.Slice(predictions, func(i, j int) bool {
		if predictions[i].Confidence != predictions[j].Confidence {
			return predictions[i].Confidence > predictions[j].Confidence
		}

		return predictions[i].Port < predictions[j].Port
	})

	return predictions
}

// set registers a conditional probability P(target|given) into the correlation matrix.
func (p *Predictor) set(given, target int, prob float64) {
	if _, ok := p.correlations[given]; !ok {
		p.correlations[given] = make(map[int]float64)
	}

	p.correlations[given][target] = prob
}

// seedDefaultCorrelations populates standard conditional port probabilities from Internet scan data.
func (p *Predictor) seedDefaultCorrelations() {
	// Web & SSH Correlations
	p.set(80, 443, 0.6343)
	p.set(80, 22, 0.1832)
	p.set(80, 21, 0.1031)

	p.set(22, 80, 0.6468)
	p.set(22, 443, 0.4404)
	p.set(22, 21, 0.1271)
	p.set(22, 3306, 0.0851)

	// Database Correlations
	p.set(3306, 80, 0.8270)
	p.set(3306, 22, 0.3841)
	p.set(3306, 443, 0.3825)

	p.set(5432, 3389, 0.8367)
	p.set(5432, 2534, 0.7823)

	p.set(6379, 80, 0.5385)
	p.set(6379, 22, 0.4472)
	p.set(6379, 3306, 0.3417)

	// AMQP & RabbitMQ
	p.set(5672, 80, 0.5847)
	p.set(5672, 22, 0.5350)
	p.set(5672, 15672, 0.5282) // RabbitMQ Web Management
	p.set(15672, 5672, 0.6944)

	// Mail Services
	p.set(25, 80, 0.6938)
	p.set(25, 443, 0.4083)
	p.set(25, 110, 0.2902)
	p.set(25, 587, 0.2676)

	p.set(993, 80, 0.8109)
	p.set(993, 995, 0.7516)
	p.set(993, 143, 0.6974)
}
