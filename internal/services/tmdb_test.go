package services

import "testing"

func TestExtractPlatformCanonicalizesTVAndMovie(t *testing.T) {
	tvDetails := map[string]interface{}{
		"networks": []interface{}{
			map[string]interface{}{"name": "HBO Max"},
		},
	}
	movieDetails := map[string]interface{}{
		"production_companies": []interface{}{
			map[string]interface{}{"name": "Amazon Prime Video"},
		},
	}

	tv := ExtractPlatform(tvDetails, "Series")
	if tv == nil || *tv != "HBO" {
		t.Fatalf("expected HBO for tv network, got %#v", tv)
	}

	movie := ExtractPlatform(movieDetails, "Movie")
	if movie == nil || *movie != "Amazon" {
		t.Fatalf("expected Amazon for movie company, got %#v", movie)
	}
}

func TestExtractPlatformFallsBackToCanonicalPlatformMap(t *testing.T) {
	details := map[string]interface{}{
		"networks": []interface{}{
			map[string]interface{}{"name": "Tencent Video"},
		},
	}

	got := ExtractPlatform(details, "Series")
	if got == nil || *got != "Tencent Video" {
		t.Fatalf("expected Tencent Video, got %#v", got)
	}
}
