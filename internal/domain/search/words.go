package search

import (
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var ErrInvalidQuery = errors.New("search must contain 1 to 8 words and at most 200 characters")

// Words deliberately has no stemming, stop words, prefix matching or query syntax.
func Words(value string) []string {
	value = norm.NFD.String(strings.ToLower(value))
	var normalized strings.Builder
	for _, r := range value {
		if unicode.IsMark(r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			normalized.WriteRune(r)
		} else {
			normalized.WriteByte(' ')
		}
	}
	unique := map[string]bool{}
	words := make([]string, 0)
	for _, word := range strings.Fields(normalized.String()) {
		if !unique[word] {
			unique[word] = true
			words = append(words, word)
		}
	}
	// Use the longest word as the index equality; remaining words are residual filters.
	sort.Slice(words, func(i, j int) bool {
		if len(words[i]) == len(words[j]) {
			return words[i] < words[j]
		}
		return len(words[i]) > len(words[j])
	})
	return words
}

func Parse(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	words := Words(value)
	if utf8.RuneCountInString(value) > 200 || len(words) == 0 || len(words) > 8 {
		return nil, ErrInvalidQuery
	}
	return words, nil
}
