package tui

import (
	"fmt"
	"strings"

	"git.sr.ht/~rockorager/vaxis"
	"github.com/alecthomas/chroma/v2"

	"github.com/rockorager/comview/diff"
)

type SyntaxHighlighter struct {
	style  *chroma.Style
	lexers map[string]chroma.Lexer
}

func NewSyntaxHighlighter() *SyntaxHighlighter {
	return NewSyntaxHighlighterWithScheme(DefaultColorScheme())
}

func NewSyntaxHighlighterWithScheme(scheme ColorScheme) *SyntaxHighlighter {
	return &SyntaxHighlighter{
		style:  syntaxStyle(scheme),
		lexers: make(map[string]chroma.Lexer),
	}
}

func (h *SyntaxHighlighter) SetColorScheme(scheme ColorScheme) {
	h.style = syntaxStyle(scheme)
}

func (h *SyntaxHighlighter) Highlight(fileName string, code string, base vaxis.Style) []vaxis.Segment {
	if h == nil || code == "" {
		return []vaxis.Segment{{Text: code, Style: base}}
	}

	lexer := h.lexerFor(fileName)
	if lexer == nil {
		return []vaxis.Segment{{Text: code, Style: base}}
	}

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return []vaxis.Segment{{Text: code, Style: base}}
	}

	var segments []vaxis.Segment
	for token := iterator(); token != chroma.EOF; token = iterator() {
		text := strings.TrimSuffix(token.Value, "\n")
		if text == "" {
			continue
		}

		segments = append(segments, vaxis.Segment{
			Text:  text,
			Style: h.styleFor(token.Type, base),
		})
	}

	if len(segments) == 0 {
		return []vaxis.Segment{{Text: code, Style: base}}
	}
	return segments
}

func (h *SyntaxHighlighter) HighlightRows(rows []diff.Row, baseFor func(diff.RowKind) vaxis.Style) map[int][]vaxis.Segment {
	segments := make(map[int][]vaxis.Segment, len(rows))
	var oldSide []syntaxLine
	var newSide []syntaxLine

	flush := func() {
		h.highlightSide(oldSide, baseFor, segments)
		h.highlightSide(newSide, baseFor, segments)
		oldSide = nil
		newSide = nil
	}

	for index, row := range rows {
		switch row.Kind {
		case diff.RowHunk, diff.RowFile, diff.RowMeta, diff.RowPreamble,
			diff.RowCommitHeader, diff.RowCommitMeta, diff.RowCommitMessage, diff.RowCommitTrailer,
			diff.RowBlank:
			flush()
		}

		if row.Code == "" {
			continue
		}

		line := syntaxLine{
			rowIndex: index,
			fileName: row.FileName,
			code:     row.Code,
			kind:     row.Kind,
		}
		switch row.Kind {
		case diff.RowContext:
			oldSide = append(oldSide, line)
			newSide = append(newSide, line)
		case diff.RowDelete:
			oldSide = append(oldSide, line)
		case diff.RowAdd:
			newSide = append(newSide, line)
		}
	}
	flush()

	return segments
}

type syntaxLine struct {
	rowIndex int
	fileName string
	code     string
	kind     diff.RowKind
}

func (h *SyntaxHighlighter) highlightSide(lines []syntaxLine, baseFor func(diff.RowKind) vaxis.Style, out map[int][]vaxis.Segment) {
	if len(lines) == 0 {
		return
	}
	if h == nil {
		for _, line := range lines {
			out[line.rowIndex] = []vaxis.Segment{{Text: line.code, Style: baseFor(line.kind)}}
		}
		return
	}

	lexer := h.lexerFor(lines[0].fileName)
	if lexer == nil {
		for _, line := range lines {
			out[line.rowIndex] = []vaxis.Segment{{Text: line.code, Style: baseFor(line.kind)}}
		}
		return
	}

	var source strings.Builder
	for _, line := range lines {
		source.WriteString(line.code)
		source.WriteByte('\n')
	}

	iterator, err := lexer.Tokenise(nil, source.String())
	if err != nil {
		for _, line := range lines {
			out[line.rowIndex] = []vaxis.Segment{{Text: line.code, Style: baseFor(line.kind)}}
		}
		return
	}

	tokenLines := chroma.SplitTokensIntoLines(iterator.Tokens())
	for index, line := range lines {
		if index >= len(tokenLines) {
			out[line.rowIndex] = []vaxis.Segment{{Text: line.code, Style: baseFor(line.kind)}}
			continue
		}
		out[line.rowIndex] = h.tokensToSegments(tokenLines[index], baseFor(line.kind), line.code)
	}
}

func (h *SyntaxHighlighter) tokensToSegments(tokens []chroma.Token, base vaxis.Style, fallback string) []vaxis.Segment {
	segments := make([]vaxis.Segment, 0, len(tokens))
	for _, token := range tokens {
		text := strings.TrimSuffix(token.Value, "\n")
		if text == "" {
			continue
		}
		segments = append(segments, vaxis.Segment{
			Text:  text,
			Style: h.styleFor(token.Type, base),
		})
	}
	if len(segments) == 0 {
		return []vaxis.Segment{{Text: fallback, Style: base}}
	}
	return segments
}

func (h *SyntaxHighlighter) lexerFor(fileName string) chroma.Lexer {
	if lexer, ok := h.lexers[fileName]; ok {
		return lexer
	}

	lexer := matchFastLexer(fileName)
	h.lexers[fileName] = lexer
	return lexer
}

func matchFastLexer(fileName string) chroma.Lexer {
	if strings.HasSuffix(fileName, ".go") || fileName == "go.mod" || fileName == "go.sum" {
		return goLexer
	}
	return nil
}

var goLexer = chroma.Coalesce(chroma.MustNewLexer(
	&chroma.Config{
		Name:      "Go",
		Aliases:   []string{"go", "golang"},
		Filenames: []string{"*.go", "go.mod", "go.sum"},
		MimeTypes: []string{"text/x-gosrc"},
	},
	func() chroma.Rules {
		return chroma.Rules{
			"root": {
				{Pattern: `\s+`, Type: chroma.Text, Mutator: nil},
				{Pattern: `//.*?$`, Type: chroma.CommentSingle, Mutator: nil},
				{Pattern: `/\*`, Type: chroma.CommentMultiline, Mutator: chroma.Push("comment")},
				{Pattern: "`", Type: chroma.LiteralStringBacktick, Mutator: chroma.Push("rawstring")},
				{Pattern: `\"(\\.|[^\"\\])*\"`, Type: chroma.LiteralStringDouble, Mutator: nil},
				{Pattern: `'(?:\\.|[^'\\])+'`, Type: chroma.LiteralStringChar, Mutator: nil},
				{Pattern: `\b(break|case|chan|const|continue|default|defer|else|fallthrough|for|func|go|goto|if|import|interface|map|package|range|return|select|struct|switch|type|var)\b`, Type: chroma.Keyword, Mutator: nil},
				{Pattern: `\b(bool|byte|complex64|complex128|error|float32|float64|int|int8|int16|int32|int64|rune|string|uint|uint8|uint16|uint32|uint64|uintptr)\b`, Type: chroma.KeywordType, Mutator: nil},
				{Pattern: `\b(true|false|iota|nil)\b`, Type: chroma.KeywordConstant, Mutator: nil},
				{Pattern: `\b[0-9](?:[0-9a-fA-F_xXoObB\.]*[0-9a-fA-F])?\b`, Type: chroma.LiteralNumber, Mutator: nil},
				{Pattern: `[{}()[\].,;:]`, Type: chroma.Punctuation, Mutator: nil},
				{Pattern: `[-+*/%=!<>&|^~?]+`, Type: chroma.Operator, Mutator: nil},
				{Pattern: `[A-Za-z_][A-Za-z0-9_]*`, Type: chroma.Name, Mutator: nil},
			},
			"comment": {
				{Pattern: `[^*/]+`, Type: chroma.CommentMultiline, Mutator: nil},
				{Pattern: `/\*`, Type: chroma.CommentMultiline, Mutator: chroma.Push("comment")},
				{Pattern: `\*/`, Type: chroma.CommentMultiline, Mutator: chroma.Pop(1)},
				{Pattern: `[*/]`, Type: chroma.CommentMultiline, Mutator: nil},
			},
			"rawstring": {
				{Pattern: "[^`]+", Type: chroma.LiteralStringBacktick, Mutator: nil},
				{Pattern: "`", Type: chroma.LiteralStringBacktick, Mutator: chroma.Pop(1)},
			},
		}
	},
))

func (h *SyntaxHighlighter) styleFor(tokenType chroma.TokenType, base vaxis.Style) vaxis.Style {
	style := base
	entry := h.style.Get(tokenType)
	if entry.Colour.IsSet() {
		style.Foreground = vaxis.RGBColor(entry.Colour.Red(), entry.Colour.Green(), entry.Colour.Blue())
	}
	if entry.Bold == chroma.Yes {
		style.Attribute |= vaxis.AttrBold
	}
	if entry.Italic == chroma.Yes {
		style.Attribute |= vaxis.AttrItalic
	}
	if entry.Underline == chroma.Yes {
		style.UnderlineStyle = vaxis.UnderlineSingle
	}
	return style
}

func syntaxStyle(scheme ColorScheme) *chroma.Style {
	entries := chroma.StyleEntries{
		chroma.Text:              chromaColor(scheme.Foreground),
		chroma.Keyword:           chromaColor(scheme.Magenta()),
		chroma.KeywordType:       chromaColor(scheme.Cyan()),
		chroma.KeywordConstant:   chromaColor(scheme.Yellow),
		chroma.NameConstant:      chromaColor(scheme.Yellow),
		chroma.NameDecorator:     chromaColor(scheme.Magenta()),
		chroma.NameEntity:        chromaColor(scheme.Cyan()),
		chroma.NameException:     chromaColor(scheme.Yellow),
		chroma.NameKeyword:       chromaColor(scheme.Magenta()),
		chroma.NameLabel:         chromaColor(scheme.Cyan()),
		chroma.NameNamespace:     chromaColor(scheme.Blue),
		chroma.NameOperator:      chromaColor(scheme.Magenta()),
		chroma.NamePseudo:        chromaColor(scheme.Cyan()),
		chroma.NameProperty:      chromaColor(scheme.Cyan()),
		chroma.NameTag:           chromaColor(scheme.Blue),
		chroma.NameBuiltin:       chromaColor(scheme.Cyan()),
		chroma.NameClass:         chromaColor(scheme.Yellow),
		chroma.NameFunction:      chromaColor(scheme.Blue),
		chroma.NameAttribute:     chromaColor(scheme.Cyan()),
		chroma.NameVariable:      chromaColor(scheme.Foreground),
		chroma.NameVariableClass: chromaColor(scheme.Cyan()),
		chroma.NameVariableMagic: chromaColor(scheme.Magenta()),
		chroma.LiteralDate:       chromaColor(scheme.Yellow),
		chroma.LiteralOther:      chromaColor(scheme.Cyan()),
		chroma.LiteralString:     chromaColor(scheme.Green()),
		chroma.LiteralNumber:     chromaColor(scheme.Yellow),
		chroma.Operator:          chromaColor(scheme.Magenta()),
		chroma.Punctuation:       chromaColor(scheme.Muted),
		chroma.Comment:           chromaColor(scheme.Muted) + " italic",
		chroma.CommentPreproc:    chromaColor(scheme.Cyan()) + " italic",
		chroma.GenericDeleted:    chromaColor(scheme.Delete),
		chroma.GenericInserted:   chromaColor(scheme.Add),
		chroma.GenericEmph:       chromaColor(scheme.Foreground) + " italic",
		chroma.GenericError:      chromaColor(scheme.Delete) + " bold",
		chroma.GenericHeading:    chromaColor(scheme.Header) + " bold",
		chroma.GenericOutput:     chromaColor(scheme.Muted),
		chroma.GenericPrompt:     chromaColor(scheme.Cyan()),
		chroma.GenericStrong:     chromaColor(scheme.Foreground) + " bold",
		chroma.GenericSubheading: chromaColor(scheme.Hunk),
		chroma.GenericTraceback:  chromaColor(scheme.Delete),
		chroma.GenericUnderline:  chromaColor(scheme.Foreground) + " underline",
		chroma.TextPunctuation:   chromaColor(scheme.Muted),
		chroma.TextSymbol:        chromaColor(scheme.Yellow),
	}
	return chroma.MustNewStyle("comview", entries)
}

func chromaColor(color vaxis.Color) string {
	r, g, b := rgb(color)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
