package scraper

import (
	"strings"
)

type AdultAssessment struct {
	Blocked        bool
	Reasons        []string
	Certifications []string
}

type AdultBlockedCandidate struct {
	Provider       string
	ProviderID     string
	ExternalIDs    map[string]string
	Title          string
	OriginalTitle  string
	Year           *int32
	Score          float64
	Popularity     float64
	PosterURL      string
	Source         string
	AdultReasons   []string
	Certifications []string
}

type ErrAdultContentFiltered struct {
	Blocked []AdultBlockedCandidate
}

func (e *ErrAdultContentFiltered) Error() string {
	return "scraper: blocked by adult-content filter"
}

var adultTitleTokens = []string{
	"fc2", "heyzo", "1pondo", "caribbeancom", "caribbean com",
	"tokyo hot", "pacopacomama", "10musume", "muramura",
	"无码", "有码", "番号", "成人视频", "成人影片",
	"成人影视", "AV片", "里番", "無修正", "アダルト",
}

var adultGenreTokens = []string{
	"adult", "porn", "pornography", "pornographic",
	"hentai", "jav", "成人视频", "成人", "色情", "情色",
	"里番", "エロ",
}

func AssessCandidateAdult(c Candidate) AdultAssessment {
	a := AdultAssessment{
		Blocked:        c.AdultContent,
		Reasons:        append([]string(nil), c.AdultReasons...),
		Certifications: dedupeStrings(c.Certifications),
	}
	for _, reason := range adultReasonsFromText("title", c.Title, c.OriginalTitle) {
		a.Blocked = true
		a.Reasons = append(a.Reasons, reason)
	}
	a.Reasons = dedupeStrings(a.Reasons)
	return a
}

func AssessDetailsAdult(d *Details) AdultAssessment {
	if d == nil {
		return AdultAssessment{}
	}
	a := AdultAssessment{
		Blocked:        d.AdultContent,
		Reasons:        append([]string(nil), d.AdultReasons...),
		Certifications: dedupeStrings(d.Certifications),
	}
	for _, reason := range adultReasonsFromText("title", d.Title, d.OriginalTitle) {
		a.Blocked = true
		a.Reasons = append(a.Reasons, reason)
	}
	for _, reason := range adultReasonsFromText("overview", d.Overview) {
		a.Blocked = true
		a.Reasons = append(a.Reasons, reason)
	}
	for _, reason := range adultReasonsFromGenres(d.Genres) {
		a.Blocked = true
		a.Reasons = append(a.Reasons, reason)
	}
	a.Reasons = dedupeStrings(a.Reasons)
	return a
}

func BlockedCandidateFromCandidate(provider string, c Candidate, source string, score float64) AdultBlockedCandidate {
	assessment := AssessCandidateAdult(c)
	return AdultBlockedCandidate{
		Provider:       strings.TrimSpace(provider),
		ProviderID:     strings.TrimSpace(c.ProviderID),
		ExternalIDs:    cloneStringMap(c.ExternalIDs),
		Title:          strings.TrimSpace(c.Title),
		OriginalTitle:  strings.TrimSpace(c.OriginalTitle),
		Year:           c.Year,
		Score:          score,
		Popularity:     c.Popularity,
		PosterURL:      strings.TrimSpace(c.PosterURL),
		Source:         strings.TrimSpace(source),
		AdultReasons:   assessment.Reasons,
		Certifications: assessment.Certifications,
	}
}

func BlockedCandidateFromDetails(d *Details, source string) AdultBlockedCandidate {
	if d == nil {
		return AdultBlockedCandidate{Source: strings.TrimSpace(source)}
	}
	assessment := AssessDetailsAdult(d)
	return AdultBlockedCandidate{
		Provider:       strings.TrimSpace(d.Provider),
		ProviderID:     strings.TrimSpace(d.ProviderID),
		ExternalIDs:    cloneStringMap(d.ExternalIDs),
		Title:          strings.TrimSpace(d.Title),
		OriginalTitle:  strings.TrimSpace(d.OriginalTitle),
		Year:           d.Year,
		PosterURL:      firstNonEmpty(d.PosterURLs...),
		Source:         strings.TrimSpace(source),
		AdultReasons:   assessment.Reasons,
		Certifications: assessment.Certifications,
	}
}

func MergeAdultBlockedCandidates(items ...[]AdultBlockedCandidate) []AdultBlockedCandidate {
	seen := map[string]int{}
	var out []AdultBlockedCandidate
	for _, group := range items {
		for _, item := range group {
			key := strings.ToLower(strings.TrimSpace(item.Provider) + ":" + strings.TrimSpace(item.ProviderID))
			if key == ":" {
				key = strings.ToLower(strings.TrimSpace(item.Title) + "|" + strings.TrimSpace(item.Source))
			}
			if idx, ok := seen[key]; ok {
				out[idx].AdultReasons = dedupeStrings(append(out[idx].AdultReasons, item.AdultReasons...))
				out[idx].Certifications = dedupeStrings(append(out[idx].Certifications, item.Certifications...))
				if out[idx].Score < item.Score {
					out[idx].Score = item.Score
				}
				continue
			}
			item.AdultReasons = dedupeStrings(item.AdultReasons)
			item.Certifications = dedupeStrings(item.Certifications)
			out = append(out, item)
			seen[key] = len(out) - 1
		}
	}
	return out
}

func adultReasonsFromText(field string, values ...string) []string {
	var out []string
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		for _, token := range adultTitleTokens {
			if strings.Contains(value, token) {
				out = append(out, field+"_token:"+token)
			}
		}
	}
	return dedupeStrings(out)
}

func adultReasonsFromGenres(genres []string) []string {
	var out []string
	for _, raw := range genres {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		for _, token := range adultGenreTokens {
			if strings.Contains(value, token) {
				out = append(out, "genre_token:"+token)
			}
		}
	}
	return dedupeStrings(out)
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
