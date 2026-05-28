package diff

import (
	"unicode/utf8"

	"github.com/rockorager/go-uucode"
)

type token struct {
	text       string
	start, end int
	kind       int
}

type inlineDiffs struct {
	deleteSpans [][]InlineSpan
	addSpans    [][]InlineSpan
}

func pairInlineDiffs(deletes []Line, adds []Line) inlineDiffs {
	diffs := inlineDiffs{
		deleteSpans: make([][]InlineSpan, len(deletes)),
		addSpans:    make([][]InlineSpan, len(adds)),
	}

	for _, pair := range inlineLinePairs(deletes, adds) {
		_, oldCode := splitLine(deletes[pair.deleteIndex])
		_, newCode := splitLine(adds[pair.addIndex])
		deleteSpans, addSpans := inlineSpans(oldCode, newCode)
		diffs.deleteSpans[pair.deleteIndex] = deleteSpans
		diffs.addSpans[pair.addIndex] = addSpans
	}

	return diffs
}

type inlineLinePair struct {
	deleteIndex int
	addIndex    int
}

type RowPair struct {
	DeleteIndex int
	AddIndex    int
}

func PairChangedRows(deletes []Row, adds []Row) []RowPair {
	if len(deletes) == 0 || len(adds) == 0 {
		return nil
	}

	scores := make([][]float64, len(deletes))
	for i := range deletes {
		scores[i] = make([]float64, len(adds))
		for j := range adds {
			scores[i][j] = lineSimilarity(deletes[i].Code, adds[j].Code)
		}
	}

	pairs := bestLinePairs(scores)
	rowPairs := make([]RowPair, 0, len(pairs))
	for _, pair := range pairs {
		rowPairs = append(rowPairs, RowPair{
			DeleteIndex: pair.deleteIndex,
			AddIndex:    pair.addIndex,
		})
	}
	return rowPairs
}

const (
	minInlineLineSimilarity = 0.45
	leadingTokenMatchBonus  = 0.5
	maxInlineLinePairs      = 10000
)

func inlineLinePairs(deletes []Line, adds []Line) []inlineLinePair {
	if len(deletes) == 0 || len(adds) == 0 {
		return nil
	}
	if len(deletes)*len(adds) > maxInlineLinePairs {
		return nil
	}

	scores := make([][]float64, len(deletes))
	for i := range deletes {
		scores[i] = make([]float64, len(adds))
		_, oldCode := splitLine(deletes[i])
		for j := range adds {
			_, newCode := splitLine(adds[j])
			scores[i][j] = lineSimilarity(oldCode, newCode)
		}
	}

	return bestLinePairs(scores)
}

func bestLinePairs(scores [][]float64) []inlineLinePair {
	if len(scores) == 0 || len(scores[0]) == 0 {
		return nil
	}

	deletes := len(scores)
	adds := len(scores[0])
	dp := make([][]float64, deletes+1)
	for i := range dp {
		dp[i] = make([]float64, adds+1)
	}

	for i := deletes - 1; i >= 0; i-- {
		for j := adds - 1; j >= 0; j-- {
			best := dp[i+1][j]
			if dp[i][j+1] > best {
				best = dp[i][j+1]
			}
			if scores[i][j] >= minInlineLineSimilarity && scores[i][j]+dp[i+1][j+1] > best {
				best = scores[i][j] + dp[i+1][j+1]
			}
			dp[i][j] = best
		}
	}

	pairs := make([]inlineLinePair, 0)
	for i, j := 0, 0; i < deletes && j < adds; {
		pairScore := scores[i][j] + dp[i+1][j+1]
		if scores[i][j] >= minInlineLineSimilarity && pairScore >= dp[i+1][j] && pairScore >= dp[i][j+1] {
			pairs = append(pairs, inlineLinePair{
				deleteIndex: i,
				addIndex:    j,
			})
			i++
			j++
			continue
		}
		if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}

	return pairs
}

func inlineSpans(oldCode string, newCode string) ([]InlineSpan, []InlineSpan) {
	oldTokens := tokenizeInline(oldCode)
	newTokens := tokenizeInline(newCode)
	oldMatched, newMatched, pairs := tokenMatches(oldTokens, newTokens)

	oldSpans := changedSpans(oldTokens, oldMatched)
	newSpans := changedSpans(newTokens, newMatched)
	if len(oldSpans) > 0 && len(newSpans) == 0 {
		newSpans = counterpartSpans(oldTokens, newTokens, pairs, oldSpans, false)
	}
	if len(newSpans) > 0 && len(oldSpans) == 0 {
		oldSpans = counterpartSpans(oldTokens, newTokens, pairs, newSpans, true)
	}
	return oldSpans, newSpans
}

func lineSimilarity(oldCode string, newCode string) float64 {
	oldTokens := tokenizeInline(oldCode)
	newTokens := tokenizeInline(newCode)
	oldSimilarityTokens := similarityTokens(oldTokens)
	newSimilarityTokens := similarityTokens(newTokens)
	if len(oldSimilarityTokens) > 0 && len(newSimilarityTokens) > 0 {
		oldTokens = oldSimilarityTokens
		newTokens = newSimilarityTokens
	}
	if len(oldTokens) == 0 || len(newTokens) == 0 {
		if oldCode == newCode {
			return 1
		}
		return 0
	}

	common := matchedTokenCount(oldTokens, newTokens)
	similarity := float64(2*common) / float64(len(oldTokens)+len(newTokens))
	if oldTokens[0].text == newTokens[0].text {
		similarity += leadingTokenMatchBonus
		if similarity > 1 {
			similarity = 1
		}
	}
	return similarity
}

func similarityTokens(tokens []token) []token {
	filtered := make([]token, 0, len(tokens))
	for _, tok := range tokens {
		if tok.kind == wordToken {
			filtered = append(filtered, tok)
		}
	}
	return filtered
}

func matchedTokenCount(oldTokens []token, newTokens []token) int {
	return tokenLCSMatrix(oldTokens, newTokens)[0][0]
}

func tokenLCSMatrix(oldTokens []token, newTokens []token) [][]int {
	dp := make([][]int, len(oldTokens)+1)
	for i := range dp {
		dp[i] = make([]int, len(newTokens)+1)
	}

	for i := len(oldTokens) - 1; i >= 0; i-- {
		for j := len(newTokens) - 1; j >= 0; j-- {
			if oldTokens[i].text == newTokens[j].text {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	return dp
}

type tokenPair struct {
	oldIndex int
	newIndex int
}

func tokenMatches(oldTokens []token, newTokens []token) ([]bool, []bool, []tokenPair) {
	oldMatched := make([]bool, len(oldTokens))
	newMatched := make([]bool, len(newTokens))
	pairs := make([]tokenPair, 0)
	prefix := 0
	for prefix < len(oldTokens) && prefix < len(newTokens) && oldTokens[prefix].text == newTokens[prefix].text {
		oldMatched[prefix] = true
		newMatched[prefix] = true
		pairs = append(pairs, tokenPair{
			oldIndex: prefix,
			newIndex: prefix,
		})
		prefix++
	}

	suffixPairs := make([]tokenPair, 0)
	oldSuffix := len(oldTokens) - 1
	newSuffix := len(newTokens) - 1
	for oldSuffix >= prefix && newSuffix >= prefix && oldTokens[oldSuffix].text == newTokens[newSuffix].text {
		oldMatched[oldSuffix] = true
		newMatched[newSuffix] = true
		suffixPairs = append(suffixPairs, tokenPair{
			oldIndex: oldSuffix,
			newIndex: newSuffix,
		})
		oldSuffix--
		newSuffix--
	}

	middleOldTokens := oldTokens[prefix : oldSuffix+1]
	middleNewTokens := newTokens[prefix : newSuffix+1]
	dp := tokenLCSMatrix(middleOldTokens, middleNewTokens)
	for i, j := 0, 0; i < len(oldTokens) && j < len(newTokens); {
		if i < prefix || i > oldSuffix {
			i++
			continue
		}
		if j < prefix || j > newSuffix {
			j++
			continue
		}
		middleI := i - prefix
		middleJ := j - prefix
		if middleOldTokens[middleI].text == middleNewTokens[middleJ].text {
			oldMatched[i] = true
			newMatched[j] = true
			pairs = append(pairs, tokenPair{
				oldIndex: i,
				newIndex: j,
			})
			i++
			j++
			continue
		}
		if dp[middleI+1][middleJ] >= dp[middleI][middleJ+1] {
			i++
		} else {
			j++
		}
	}

	for i := len(suffixPairs) - 1; i >= 0; i-- {
		pairs = append(pairs, suffixPairs[i])
	}
	return oldMatched, newMatched, pairs
}

func changedSpans(tokens []token, matched []bool) []InlineSpan {
	spans := make([]InlineSpan, 0)
	for i, tok := range tokens {
		if matched[i] {
			continue
		}
		spans = append(spans, InlineSpan{
			Start: tok.start,
			End:   tok.end,
			Kind:  InlineChange,
		})
	}
	return mergeInlineSpans(tokens, spans)
}

func counterpartSpans(oldTokens []token, newTokens []token, pairs []tokenPair, sourceSpans []InlineSpan, oldTarget bool) []InlineSpan {
	spans := make([]InlineSpan, 0)
	for _, pair := range pairs {
		source := oldTokens[pair.oldIndex]
		target := newTokens[pair.newIndex]
		if oldTarget {
			source = newTokens[pair.newIndex]
			target = oldTokens[pair.oldIndex]
		}
		if target.kind != wordToken || !tokenOverlapsSpans(source, sourceSpans) {
			continue
		}
		spans = append(spans, InlineSpan{
			Start: target.start,
			End:   target.end,
			Kind:  InlineChange,
		})
	}
	return mergeInlineSpans(targetTokens(oldTokens, newTokens, oldTarget), spans)
}

func targetTokens(oldTokens []token, newTokens []token, oldTarget bool) []token {
	if oldTarget {
		return oldTokens
	}
	return newTokens
}

func tokenOverlapsSpans(tok token, spans []InlineSpan) bool {
	for _, span := range spans {
		if tok.start < span.End && tok.end > span.Start {
			return true
		}
	}
	return false
}

func mergeInlineSpans(tokens []token, spans []InlineSpan) []InlineSpan {
	if len(spans) == 0 {
		return nil
	}

	merged := []InlineSpan{spans[0]}
	for _, span := range spans[1:] {
		last := &merged[len(merged)-1]
		if span.Start <= last.End || shouldCoalesceInlineGap(tokens, last.End, span.Start) {
			if span.End > last.End {
				last.End = span.End
			}
			continue
		}
		merged = append(merged, span)
	}
	return merged
}

func shouldCoalesceInlineGap(tokens []token, start int, end int) bool {
	const maxGapBytes = 12

	if end-start > maxGapBytes {
		return false
	}

	gapTokens := 0
	for _, tok := range tokens {
		if tok.start >= end {
			return true
		}
		if tok.end <= start {
			continue
		}
		gapTokens++
		if gapTokens > 2 {
			return false
		}
	}
	return true
}

func tokenizeInline(text string) []token {
	tokens := make([]token, 0)
	for start := 0; start < len(text); {
		r, size := utf8.DecodeRuneInString(text[start:])
		if isSpace(r) {
			start += size
			continue
		}
		end := start + size
		kind := tokenKind(r)
		if kind == wordToken {
			for end < len(text) {
				next, size := runeAt(text, end)
				if isSpace(next) || tokenKind(next) != kind {
					break
				}
				end += size
			}
		}
		tokens = append(tokens, token{
			text:  text[start:end],
			start: start,
			end:   end,
			kind:  kind,
		})
		start = end
	}
	return tokens
}

const (
	wordToken = iota + 1
	punctuationToken
	symbolToken
)

func tokenKind(r rune) int {
	switch {
	case uucode.IsLetter(r) || uucode.IsDigit(r) || r == '_':
		return wordToken
	case uucode.IsPunct(r):
		return punctuationToken
	default:
		return symbolToken
	}
}

func isSpace(r rune) bool {
	return uucode.IsSpace(r)
}

func runeAt(text string, index int) (rune, int) {
	return utf8.DecodeRuneInString(text[index:])
}
