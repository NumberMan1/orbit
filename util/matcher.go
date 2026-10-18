package util

import "github.com/NumberMan1/numbox/pkg/matcher"

// Match is a rune-indexed half-open range returned by Matcher.
type Match = matcher.Match

// Matcher is numbox's immutable, concurrency-safe Unicode matcher.
type Matcher = matcher.Matcher

func NewMatcher(words []string) *Matcher { return matcher.NewMatcher(words) }
