package handlers

import (
	"sort"
	"testing"
)

func TestProjectContextEmbeddingEvalSemanticV2(t *testing.T) {
	docs := []struct {
		id   string
		text string
	}{
		{"auth", "API key authentication validates bearer tokens and session credentials"},
		{"retrieval", "Hybrid project retrieval fuses lexical search, embeddings, vectors, anchors and graph context"},
		{"deploy", "Release deployment pins a Docker image tag and deploys it to ppm from GHCR"},
		{"billing", "Billing report calculates hourly cost, currency totals, invoice values and EUR amounts"},
		{"attachments", "Issue attachments upload files and documents with path metadata"},
	}
	cases := []struct {
		query string
		want  string
	}{
		{"signin credential access", "auth"},
		{"paraphrase meaning search", "retrieval"},
		{"ship container image", "deploy"},
		{"money invoice price", "billing"},
		{"document upload path", "attachments"},
	}

	hashMRR := embeddingMRR(cases, docs, embedTextDeterministic)
	semanticMRR := embeddingMRR(cases, docs, embedTextLocalSemantic)
	t.Logf("PAI-222 eval: local-hash-v1 MRR=%.3f local-semantic-v2 MRR=%.3f", hashMRR, semanticMRR)
	if semanticMRR < 0.95 {
		t.Fatalf("local-semantic-v2 MRR %.3f, want >= 0.95", semanticMRR)
	}
	if semanticMRR < hashMRR {
		t.Fatalf("local-semantic-v2 MRR %.3f regressed below local-hash-v1 %.3f", semanticMRR, hashMRR)
	}
}

func embeddingMRR(cases []struct {
	query string
	want  string
}, docs []struct {
	id   string
	text string
}, embed func(string) []float32) float64 {
	var total float64
	for _, tc := range cases {
		q := embed(tc.query)
		ranked := make([]struct {
			id    string
			score float64
		}, 0, len(docs))
		for _, doc := range docs {
			ranked = append(ranked, struct {
				id    string
				score float64
			}{id: doc.id, score: cosineSimilarity(q, embed(doc.text))})
		}
		sort.SliceStable(ranked, func(i, j int) bool {
			if ranked[i].score == ranked[j].score {
				return ranked[i].id < ranked[j].id
			}
			return ranked[i].score > ranked[j].score
		})
		for i, hit := range ranked {
			if hit.id == tc.want {
				total += 1 / float64(i+1)
				break
			}
		}
	}
	return total / float64(len(cases))
}
