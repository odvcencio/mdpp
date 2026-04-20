package lsp

import "encoding/json"

type DocumentURI string

type Position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type TextDocumentIdentifier struct {
	URI DocumentURI `json:"uri"`
}

type VersionedTextDocumentIdentifier struct {
	URI     DocumentURI `json:"uri"`
	Version int32       `json:"version,omitempty"`
}

type TextDocumentItem struct {
	URI        DocumentURI `json:"uri"`
	LanguageID string      `json:"languageId,omitempty"`
	Version    int32       `json:"version"`
	Text       string      `json:"text"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type TextDocumentContentChangeEvent struct {
	Range       *Range  `json:"range,omitempty"`
	RangeLength *uint32 `json:"rangeLength,omitempty"`
	Text        string  `json:"text"`
}

type InitializeParams struct {
	RootURI               DocumentURI     `json:"rootUri,omitempty"`
	InitializationOptions json.RawMessage `json:"initializationOptions,omitempty"`
	ClientInfo            *ClientInfo     `json:"clientInfo,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ServerCapabilities struct {
	TextDocumentSync           TextDocumentSyncOptions `json:"textDocumentSync"`
	HoverProvider              bool                    `json:"hoverProvider"`
	DefinitionProvider         bool                    `json:"definitionProvider"`
	ReferencesProvider         bool                    `json:"referencesProvider"`
	RenameProvider             bool                    `json:"renameProvider"`
	FoldingRangeProvider       bool                    `json:"foldingRangeProvider"`
	DocumentSymbolProvider     bool                    `json:"documentSymbolProvider"`
	DocumentFormattingProvider bool                    `json:"documentFormattingProvider"`
	CompletionProvider         CompletionOptions       `json:"completionProvider"`
	SemanticTokensProvider     SemanticTokensOptions   `json:"semanticTokensProvider"`
}

type TextDocumentSyncOptions struct {
	OpenClose bool        `json:"openClose"`
	Change    int         `json:"change"`
	Save      SaveOptions `json:"save"`
}

type SaveOptions struct {
	IncludeText bool `json:"includeText"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters"`
	ResolveProvider   bool     `json:"resolveProvider"`
}

type SemanticTokensOptions struct {
	Legend SemanticTokensLegend `json:"legend"`
	Range  bool                 `json:"range"`
	Full   bool                 `json:"full"`
}

type SemanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

type PublishDiagnosticsParams struct {
	URI         DocumentURI  `json:"uri"`
	Version     *int32       `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           int                            `json:"severity,omitempty"`
	Code               string                         `json:"code,omitempty"`
	Source             string                         `json:"source,omitempty"`
	Message            string                         `json:"message"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

type Location struct {
	URI   DocumentURI `json:"uri"`
	Range Range       `json:"range"`
}

type HoverParams = TextDocumentPositionParams

type DefinitionParams = TextDocumentPositionParams

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context,omitempty"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

type WorkspaceEdit struct {
	Changes map[DocumentURI][]TextEdit `json:"changes,omitempty"`
}

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      *CompletionContext     `json:"context,omitempty"`
}

type CompletionContext struct {
	TriggerKind      int    `json:"triggerKind,omitempty"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CompletionItem struct {
	Label            string         `json:"label"`
	Kind             int            `json:"kind,omitempty"`
	Detail           string         `json:"detail,omitempty"`
	Documentation    *MarkupContent `json:"documentation,omitempty"`
	InsertText       string         `json:"insertText,omitempty"`
	InsertTextFormat int            `json:"insertTextFormat,omitempty"`
}

type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type SemanticTokensRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

type SemanticTokens struct {
	Data []uint32 `json:"data"`
}

type FoldingRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type FoldingRange struct {
	StartLine      uint32  `json:"startLine"`
	StartCharacter *uint32 `json:"startCharacter,omitempty"`
	EndLine        uint32  `json:"endLine"`
	EndCharacter   *uint32 `json:"endCharacter,omitempty"`
	Kind           string  `json:"kind,omitempty"`
}

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options,omitempty"`
}

type FormattingOptions struct {
	TabSize      int  `json:"tabSize,omitempty"`
	InsertSpaces bool `json:"insertSpaces,omitempty"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type RenderPreviewParams struct {
	URI          DocumentURI            `json:"uri,omitempty"`
	TextDocument TextDocumentIdentifier `json:"textDocument,omitempty"`
}

type RenderPreviewResult struct {
	URI         DocumentURI    `json:"uri"`
	HTML        string         `json:"html"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	TOCEntries  []mdppTOCEntry `json:"tocEntries,omitempty"`
	Version     int32          `json:"version"`
}

type mdppTOCEntry struct {
	Level int    `json:"level"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

const (
	textDocumentSyncKindIncremental = 2

	diagnosticSeverityError       = 1
	diagnosticSeverityWarning     = 2
	diagnosticSeverityInformation = 3
	diagnosticSeverityHint        = 4

	completionItemKindValue   = 12
	completionItemKindKeyword = 14
	completionItemKindSnippet = 15

	insertTextFormatSnippet = 2

	symbolKindFile      = 1
	symbolKindNamespace = 3
	symbolKindClass     = 5
	symbolKindField     = 8
	symbolKindString    = 15
)

var semanticTokenTypes = []string{
	"comment",
	"string",
	"keyword",
	"operator",
	"number",
	"heading",
	"link",
	"footnote",
	"math",
	"containerType",
	"admonitionType",
	"emojiShortcode",
	"frontmatterKey",
	"frontmatterValue",
	"strikethrough",
	"emphasis",
	"taskMarker",
	"definitionTerm",
	"tableHeader",
	"tableSeparator",
	"imageAlt",
	"imageUrl",
	"directive",
	"directiveArgument",
}

var semanticTokenModifiers = []string{
	"level1",
	"level2",
	"level3",
	"level4",
	"level5",
	"level6",
	"inline",
	"reference",
	"autolink",
	"resolved",
	"broken",
	"definition",
	"display",
	"italic",
	"bold",
	"bolditalic",
	"checked",
	"unchecked",
	"toc",
	"embed",
}

const (
	tokenComment = iota
	tokenString
	tokenKeyword
	tokenOperator
	tokenNumber
	tokenHeading
	tokenLink
	tokenFootnote
	tokenMath
	tokenContainerType
	tokenAdmonitionType
	tokenEmojiShortcode
	tokenFrontmatterKey
	tokenFrontmatterValue
	tokenStrikethrough
	tokenEmphasis
	tokenTaskMarker
	tokenDefinitionTerm
	tokenTableHeader
	tokenTableSeparator
	tokenImageAlt
	tokenImageURL
	tokenDirective
	tokenDirectiveArgument
)

const (
	tokenModLevel1 = iota
	tokenModLevel2
	tokenModLevel3
	tokenModLevel4
	tokenModLevel5
	tokenModLevel6
	tokenModInline
	tokenModReference
	tokenModAutolink
	tokenModResolved
	tokenModBroken
	tokenModDefinition
	tokenModDisplay
	tokenModItalic
	tokenModBold
	tokenModBoldItalic
	tokenModChecked
	tokenModUnchecked
	tokenModTOC
	tokenModEmbed
)
