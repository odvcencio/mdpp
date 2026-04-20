package main

import (
	"bytes"
	"encoding/json"
	"flag"
	stdfmt "fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/odvcencio/mdpp"
	mdppfmt "github.com/odvcencio/mdpp/fmt"
	mdpplint "github.com/odvcencio/mdpp/lint"
)

const (
	exitOK       = 0
	exitFindings = 1
	exitError    = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitError
	}

	switch args[0] {
	case "render":
		return runRender(args[1:], stdin, stdout, stderr)
	case "parse":
		return runParse(args[1:], stdin, stdout, stderr)
	case "fmt", "format":
		return runFormat(args[1:], stdin, stdout, stderr)
	case "lint":
		return runLint(args[1:], stdin, stdout, stderr)
	case "version", "--version", "-v":
		stdfmt.Fprintf(stdout, "mdpp %s (spec %s)\n", mdpp.Version, mdpp.SpecVersion)
		return exitOK
	case "help", "-h", "--help":
		printUsage(stdout)
		return exitOK
	default:
		stdfmt.Fprintf(stderr, "mdpp: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return exitError
	}
}

func runRender(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("render", stderr)
	output := fs.String("o", "", "write output to file")
	fs.StringVar(output, "out", "", "write output to file")
	fs.StringVar(output, "output", "", "write output to file")
	pdf := fs.Bool("pdf", false, "render PDF instead of HTML")
	format := fs.String("format", "html", `output format: "html", "pdf", or "slides"`)
	unsafeHTML := fs.Bool("unsafe-html", false, "allow raw HTML passthrough")
	headingIDs := fs.Bool("heading-ids", true, "render heading id attributes")
	noHeadingIDs := fs.Bool("no-heading-ids", false, `skip auto id="..." on headings`)
	highlight := fs.Bool("highlight", false, "render highlighted code")
	hardWraps := fs.Bool("hard-wraps", false, "render soft line breaks as <br>")
	wrapEmoji := fs.Bool("emoji", false, "wrap emoji in accessible spans")
	mathMode := fs.String("math", "server", "math rendering mode: server, raw, omit")
	paper := fs.String("paper", "letter", "PDF paper size: letter, a4, legal")
	margin := fs.Float64("margin", 0.5, "PDF margin in inches, all sides")
	cssPath := fs.String("css", "", "CSS file to include when rendering PDF")
	browserURL := fs.String("browser-url", "", "remote Chrome DevTools URL for PDF rendering")
	timeout := fs.Duration("timeout", 60*time.Second, "PDF render timeout")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if fs.NArg() > 1 {
		stdfmt.Fprintln(stderr, "mdpp render: expected at most one input file")
		return exitError
	}
	switch strings.ToLower(*format) {
	case "", "html":
	case "pdf":
		*pdf = true
	case "slides":
		stdfmt.Fprintln(stderr, "mdpp render: --format slides is reserved and not implemented")
		return exitError
	default:
		stdfmt.Fprintf(stderr, "mdpp render: unknown output format %q\n", *format)
		return exitError
	}
	if *noHeadingIDs {
		*headingIDs = false
	}

	src, _, err := readInput(optionalArg(fs.Args()), stdin)
	if err != nil {
		stdfmt.Fprintf(stderr, "mdpp render: %v\n", err)
		return exitError
	}
	doc, err := mdpp.Parse(src)
	if err != nil {
		stdfmt.Fprintf(stderr, "mdpp render: parse: %v\n", err)
		return exitError
	}
	renderOpts, err := renderOptions(*headingIDs, *highlight, *unsafeHTML, *hardWraps, *wrapEmoji, *mathMode)
	if err != nil {
		stdfmt.Fprintf(stderr, "mdpp render: %v\n", err)
		return exitError
	}

	var out []byte
	if *pdf {
		var css string
		if *cssPath != "" {
			cssBytes, err := os.ReadFile(*cssPath)
			if err != nil {
				stdfmt.Fprintf(stderr, "mdpp render: read css: %v\n", err)
				return exitError
			}
			css = string(cssBytes)
		}
		browser := *browserURL
		if browser == "" {
			browser = os.Getenv("CHROME_WS_URL")
		}
		paperSize, err := parsePaperSize(*paper)
		if err != nil {
			stdfmt.Fprintf(stderr, "mdpp render: %v\n", err)
			return exitError
		}
		out, err = mdpp.RenderPDF(doc, mdpp.PDFOptions{
			PaperSize:     paperSize,
			MarginInches:  mdpp.Margins{Top: *margin, Right: *margin, Bottom: *margin, Left: *margin},
			UserCSS:       css,
			BrowserURL:    browser,
			Timeout:       *timeout,
			RenderOptions: renderOpts,
			Background:    true,
		})
	} else {
		out, err = mdpp.Render(doc, renderOpts)
	}
	if err != nil {
		stdfmt.Fprintf(stderr, "mdpp render: %v\n", err)
		return exitError
	}
	if err := writeOutput(*output, stdout, out); err != nil {
		stdfmt.Fprintf(stderr, "mdpp render: %v\n", err)
		return exitError
	}
	return exitOK
}

func runParse(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("parse", stderr)
	jsonOut := fs.Bool("json", false, "write the parsed AST as JSON")
	compact := fs.Bool("compact", false, "write compact JSON")
	pretty := fs.Bool("pretty", true, "pretty-print JSON")
	diagnosticsOnly := fs.Bool("diagnostics-only", false, "emit only diagnostics, suppressing the AST")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if fs.NArg() > 1 {
		stdfmt.Fprintln(stderr, "mdpp parse: expected at most one input file")
		return exitError
	}
	if !*jsonOut {
		// JSON is currently the only stable parse output. Keep the flag accepted
		// so scripts can spell the contract explicitly.
		*jsonOut = true
	}

	src, name, err := readInput(optionalArg(fs.Args()), stdin)
	if err != nil {
		stdfmt.Fprintf(stderr, "mdpp parse: %v\n", err)
		return exitError
	}
	doc, err := mdpp.Parse(src)
	if err != nil {
		stdfmt.Fprintf(stderr, "mdpp parse: %v\n", err)
		return exitError
	}
	diagnostics := convertParseDiagnostics(doc.Diagnostics())
	if *diagnosticsOnly {
		return writeJSON(stdout, stderr, "mdpp parse", diagnostics, *compact || !*pretty)
	}
	payload := jsonDocument{
		File:        name,
		Root:        convertJSONNode(doc.AST()),
		Frontmatter: doc.Frontmatter(),
		Diagnostics: diagnostics,
		Headings:    doc.Headings(),
		TOC:         doc.TableOfContents(),
	}
	return writeJSON(stdout, stderr, "mdpp parse", payload, *compact || !*pretty)
}

func runFormat(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("fmt", stderr)
	write := fs.Bool("w", false, "write result back to input file")
	fs.BoolVar(write, "write", false, "write result back to input file")
	check := fs.Bool("check", false, "report files that are not formatted")
	diff := fs.Bool("diff", false, "print a simple before/after diff for unformatted files")
	stdinFilepath := fs.String("stdin-filepath", "", "path to use for stdin diagnostics")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	paths := fs.Args()
	if *write {
		if len(paths) == 0 {
			stdfmt.Fprintln(stderr, "mdpp fmt: -w requires at least one file")
			return exitError
		}
		for _, path := range paths {
			if path == "-" {
				stdfmt.Fprintln(stderr, "mdpp fmt: -w cannot be used with stdin")
				return exitError
			}
			changed, err := formatFile(path)
			if err != nil {
				stdfmt.Fprintf(stderr, "mdpp fmt: %v\n", err)
				return exitError
			}
			if changed {
				stdfmt.Fprintln(stdout, path)
			}
		}
		return exitOK
	}

	if len(paths) == 0 {
		paths = []string{"-"}
	}
	changed := false
	for _, path := range paths {
		src, name, err := readInput(path, stdin)
		if err != nil {
			stdfmt.Fprintf(stderr, "mdpp fmt: %v\n", err)
			return exitError
		}
		if path == "-" && *stdinFilepath != "" {
			name = *stdinFilepath
		}
		formatted, err := mdppfmt.Format(src)
		if err != nil {
			stdfmt.Fprintf(stderr, "mdpp fmt: %v\n", err)
			return exitError
		}
		if bytes.Equal(src, formatted) {
			continue
		}
		changed = true
		if *check {
			stdfmt.Fprintln(stdout, name)
			continue
		}
		if *diff {
			writeSimpleDiff(stdout, name, src, formatted)
			continue
		}
		if _, err := stdout.Write(formatted); err != nil {
			stdfmt.Fprintf(stderr, "mdpp fmt: write stdout: %v\n", err)
			return exitError
		}
	}
	if (*check || *diff) && changed {
		return exitFindings
	}
	return exitOK
}

func runLint(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("lint", stderr)
	jsonOut := fs.Bool("json", false, "write diagnostics as JSON")
	format := fs.String("format", "text", `output format: "text", "json", or "github"`)
	compact := fs.Bool("compact", false, "write compact JSON")
	minSeverity := fs.String("severity", "", "minimum severity: error, warning, info, hint")
	rulesOnly := fs.String("rules", "", "comma-separated rule allow-list")
	noRules := fs.String("no-rules", "", "comma-separated rule deny-list")
	quiet := fs.Bool("quiet", false, "suppress info and hint diagnostics")
	fs.Bool("no-color", false, "accepted for compatibility; output is currently plain")
	fix := fs.Bool("fix", false, "apply available single-edit fixes to files")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	outputFormat := strings.ToLower(*format)
	switch strings.ToLower(*format) {
	case "", "text", "plain", "human":
		outputFormat = "text"
	case "json":
		*jsonOut = true
		outputFormat = "json"
	case "github":
		outputFormat = "github"
	default:
		stdfmt.Fprintf(stderr, "mdpp lint: unknown output format %q\n", *format)
		return exitError
	}
	if *quiet && *minSeverity == "" {
		*minSeverity = "warning"
	}
	minRank, err := parseSeverityRank(*minSeverity)
	if err != nil {
		stdfmt.Fprintf(stderr, "mdpp lint: %v\n", err)
		return exitError
	}
	includeRules := parseRuleSet(*rulesOnly)
	excludeRules := parseRuleSet(*noRules)
	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"-"}
	}

	var all []jsonLintDiagnostic
	for _, path := range paths {
		src, name, err := readInput(path, stdin)
		if err != nil {
			stdfmt.Fprintf(stderr, "mdpp lint: %v\n", err)
			return exitError
		}
		doc, err := mdpp.Parse(src)
		if err != nil {
			stdfmt.Fprintf(stderr, "mdpp lint: parse %s: %v\n", name, err)
			return exitError
		}
		all = append(all, filterDiagnostics(parseDiagnosticsForLint(name, doc.Diagnostics()), minRank, includeRules, excludeRules)...)
		var fixes []mdpplint.TextEdit
		for _, d := range mdpplint.Lint(doc) {
			diag := jsonLintDiagnostic{
				File:     name,
				Range:    convertRange(d.Range),
				Severity: lintSeverityString(d.Severity),
				Code:     d.Code,
				Message:  d.Message,
				Fix:      convertLintFix(d.Fix),
				Related:  convertLintRelated(d.Related),
			}
			if diagnosticAllowed(diag, minRank, includeRules, excludeRules) {
				all = append(all, diag)
				if d.Fix != nil {
					fixes = append(fixes, *d.Fix)
				}
			}
		}
		if *fix && path != "-" && len(fixes) > 0 {
			fixed, err := applyTextEdits(src, fixes)
			if err != nil {
				stdfmt.Fprintf(stderr, "mdpp lint: fix %s: %v\n", name, err)
				return exitError
			}
			if err := os.WriteFile(path, fixed, 0o644); err != nil {
				stdfmt.Fprintf(stderr, "mdpp lint: write %s: %v\n", name, err)
				return exitError
			}
		}
	}

	if outputFormat == "json" || *jsonOut {
		if code := writeJSON(stdout, stderr, "mdpp lint", all, *compact); code != exitOK {
			return code
		}
	} else if outputFormat == "github" {
		for _, d := range all {
			writeGitHubDiagnostic(stdout, d)
		}
	} else {
		for _, d := range all {
			stdfmt.Fprintf(stdout, "%s:%d:%d: %s %s: %s\n", d.File, d.Range.StartLine, d.Range.StartCol, d.Severity, d.Code, d.Message)
		}
	}
	if len(all) > 0 {
		return exitFindings
	}
	return exitOK
}

func printUsage(w io.Writer) {
	stdfmt.Fprint(w, `Usage:
  mdpp render [flags] [file]
  mdpp parse --json [file]
  mdpp fmt [-w] [file...]
  mdpp lint [--json] [file...]
  mdpp version

Use "-" or omit the file argument to read from stdin.
`)
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("mdpp "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func optionalArg(args []string) string {
	if len(args) == 0 {
		return "-"
	}
	return args[0]
}

func readInput(path string, stdin io.Reader) ([]byte, string, error) {
	if path == "" || path == "-" {
		src, err := io.ReadAll(stdin)
		if err != nil {
			return nil, "<stdin>", stdfmt.Errorf("read stdin: %w", err)
		}
		return src, "<stdin>", nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, path, stdfmt.Errorf("read %s: %w", path, err)
	}
	return src, path, nil
}

func writeOutput(path string, stdout io.Writer, data []byte) error {
	if path == "" || path == "-" {
		_, err := stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func renderOptions(headingIDs bool, highlight bool, unsafeHTML bool, hardWraps bool, wrapEmoji bool, mathMode string) (mdpp.RenderOptions, error) {
	math, err := parseMathOption(mathMode)
	if err != nil {
		return mdpp.RenderOptions{}, err
	}
	return mdpp.RenderOptions{
		HeadingIDs:    headingIDs,
		HighlightCode: highlight,
		UnsafeHTML:    unsafeHTML,
		HardWraps:     hardWraps,
		WrapEmoji:     wrapEmoji,
		Math:          math,
	}, nil
}

func parseMathOption(mode string) (mdpp.MathOption, error) {
	switch strings.ToLower(mode) {
	case "", "server":
		return mdpp.MathServer, nil
	case "raw":
		return mdpp.MathRaw, nil
	case "omit":
		return mdpp.MathOmit, nil
	default:
		return mdpp.MathServer, stdfmt.Errorf("unknown math mode %q", mode)
	}
}

func parsePaperSize(raw string) (mdpp.PaperSize, error) {
	switch strings.ToLower(raw) {
	case "", "letter":
		return mdpp.PaperLetter, nil
	case "a4":
		return mdpp.PaperA4, nil
	case "legal":
		return mdpp.PaperLegal, nil
	default:
		return mdpp.PaperLetter, stdfmt.Errorf("unknown paper size %q", raw)
	}
}

func formatFile(path string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, stdfmt.Errorf("read %s: %w", path, err)
	}
	formatted, err := mdppfmt.Format(src)
	if err != nil {
		return false, stdfmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(src, formatted) {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, stdfmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, formatted, info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func writeSimpleDiff(w io.Writer, name string, before []byte, after []byte) {
	stdfmt.Fprintf(w, "--- %s\n+++ %s (formatted)\n@@\n", name, name)
	for _, line := range strings.Split(strings.TrimSuffix(string(before), "\n"), "\n") {
		stdfmt.Fprintf(w, "-%s\n", line)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(after), "\n"), "\n") {
		stdfmt.Fprintf(w, "+%s\n", line)
	}
}

func parseRuleSet(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if part != "" {
			out[strings.ToUpper(part)] = true
		}
	}
	return out
}

func parseSeverityRank(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	switch strings.ToLower(raw) {
	case "error", "err":
		return 4, nil
	case "warning", "warn":
		return 3, nil
	case "info":
		return 2, nil
	case "hint":
		return 1, nil
	default:
		return 0, stdfmt.Errorf("unknown severity %q", raw)
	}
}

func severityRank(sev string) int {
	switch strings.ToLower(sev) {
	case "error":
		return 4
	case "warning":
		return 3
	case "info":
		return 2
	case "hint":
		return 1
	default:
		return 0
	}
}

func filterDiagnostics(in []jsonLintDiagnostic, minRank int, includeRules map[string]bool, excludeRules map[string]bool) []jsonLintDiagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make([]jsonLintDiagnostic, 0, len(in))
	for _, diag := range in {
		if diagnosticAllowed(diag, minRank, includeRules, excludeRules) {
			out = append(out, diag)
		}
	}
	return out
}

func diagnosticAllowed(diag jsonLintDiagnostic, minRank int, includeRules map[string]bool, excludeRules map[string]bool) bool {
	code := strings.ToUpper(diag.Code)
	if minRank > 0 && severityRank(diag.Severity) < minRank {
		return false
	}
	if len(includeRules) > 0 && !includeRules[code] {
		return false
	}
	if len(excludeRules) > 0 && excludeRules[code] {
		return false
	}
	return true
}

func writeGitHubDiagnostic(w io.Writer, d jsonLintDiagnostic) {
	level := "notice"
	if d.Severity == "error" {
		level = "error"
	} else if d.Severity == "warning" {
		level = "warning"
	}
	message := strings.ReplaceAll(d.Message, "\n", " ")
	stdfmt.Fprintf(w, "::%s file=%s,line=%d,col=%d,title=%s::%s\n", level, d.File, d.Range.StartLine, d.Range.StartCol, d.Code, message)
}

func applyTextEdits(src []byte, edits []mdpplint.TextEdit) ([]byte, error) {
	out := append([]byte(nil), src...)
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		if edit.Range.StartByte < 0 || edit.Range.EndByte < edit.Range.StartByte || edit.Range.EndByte > len(out) {
			return nil, stdfmt.Errorf("invalid edit range for %s", edit.NewText)
		}
		next := make([]byte, 0, len(out)-(edit.Range.EndByte-edit.Range.StartByte)+len(edit.NewText))
		next = append(next, out[:edit.Range.StartByte]...)
		next = append(next, edit.NewText...)
		next = append(next, out[edit.Range.EndByte:]...)
		out = next
	}
	return out, nil
}

func writeJSON(stdout io.Writer, stderr io.Writer, context string, value any, compact bool) int {
	var (
		data []byte
		err  error
	)
	if compact {
		data, err = json.Marshal(value)
	} else {
		data, err = json.MarshalIndent(value, "", "  ")
	}
	if err != nil {
		stdfmt.Fprintf(stderr, "%s: json: %v\n", context, err)
		return exitError
	}
	data = append(data, '\n')
	if _, err := stdout.Write(data); err != nil {
		stdfmt.Fprintf(stderr, "%s: write stdout: %v\n", context, err)
		return exitError
	}
	return exitOK
}

type jsonDocument struct {
	File        string                `json:"file"`
	Root        *jsonNode             `json:"root"`
	Frontmatter map[string]any        `json:"frontmatter,omitempty"`
	Diagnostics []jsonParseDiagnostic `json:"diagnostics,omitempty"`
	Headings    []mdpp.Heading        `json:"headings,omitempty"`
	TOC         []mdpp.TOCEntry       `json:"toc,omitempty"`
}

type jsonNode struct {
	Type     string            `json:"type"`
	Literal  string            `json:"literal,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Range    jsonRange         `json:"range"`
	Children []*jsonNode       `json:"children,omitempty"`
}

type jsonParseDiagnostic struct {
	Range    jsonRange `json:"range"`
	Severity string    `json:"severity"`
	Code     string    `json:"code"`
	Message  string    `json:"message"`
}

type jsonRange struct {
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
	StartLine int `json:"start_line"`
	StartCol  int `json:"start_col"`
	EndLine   int `json:"end_line"`
	EndCol    int `json:"end_col"`
}

func convertRange(r mdpp.Range) jsonRange {
	return jsonRange{
		StartByte: r.StartByte,
		EndByte:   r.EndByte,
		StartLine: r.StartLine,
		StartCol:  r.StartCol,
		EndLine:   r.EndLine,
		EndCol:    r.EndCol,
	}
}

func convertJSONNode(n *mdpp.Node) *jsonNode {
	if n == nil {
		return nil
	}
	out := &jsonNode{
		Type:    n.Type.String(),
		Literal: n.Literal,
		Attrs:   n.Attrs,
		Range:   convertRange(n.Range),
	}
	if len(n.Children) > 0 {
		out.Children = make([]*jsonNode, 0, len(n.Children))
		for _, child := range n.Children {
			out.Children = append(out.Children, convertJSONNode(child))
		}
	}
	return out
}

func convertParseDiagnostics(in []mdpp.Diagnostic) []jsonParseDiagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make([]jsonParseDiagnostic, 0, len(in))
	for _, d := range in {
		out = append(out, jsonParseDiagnostic{
			Range:    convertRange(d.Range),
			Severity: parseSeverityString(d.Severity),
			Code:     d.Code,
			Message:  d.Message,
		})
	}
	return out
}

type jsonLintDiagnostic struct {
	File     string            `json:"file"`
	Range    jsonRange         `json:"range"`
	Severity string            `json:"severity"`
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Fix      *jsonTextEdit     `json:"fix,omitempty"`
	Related  []jsonRelatedInfo `json:"related,omitempty"`
}

type jsonTextEdit struct {
	Range   jsonRange `json:"range"`
	NewText string    `json:"newText"`
}

type jsonRelatedInfo struct {
	Range   jsonRange `json:"range"`
	Message string    `json:"message"`
}

func parseDiagnosticsForLint(file string, in []mdpp.Diagnostic) []jsonLintDiagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make([]jsonLintDiagnostic, 0, len(in))
	for _, d := range in {
		out = append(out, jsonLintDiagnostic{
			File:     file,
			Range:    convertRange(d.Range),
			Severity: parseSeverityString(d.Severity),
			Code:     d.Code,
			Message:  d.Message,
		})
	}
	return out
}

func convertLintFix(in *mdpplint.TextEdit) *jsonTextEdit {
	if in == nil {
		return nil
	}
	return &jsonTextEdit{Range: convertRange(in.Range), NewText: in.NewText}
}

func convertLintRelated(in []mdpplint.RelatedInfo) []jsonRelatedInfo {
	if len(in) == 0 {
		return nil
	}
	out := make([]jsonRelatedInfo, 0, len(in))
	for _, r := range in {
		out = append(out, jsonRelatedInfo{Range: convertRange(r.Range), Message: r.Message})
	}
	return out
}

func parseSeverityString(sev mdpp.Severity) string {
	switch sev {
	case mdpp.SeverityInfo:
		return "info"
	case mdpp.SeverityWarning:
		return "warning"
	case mdpp.SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

func lintSeverityString(sev mdpplint.Severity) string {
	switch sev {
	case mdpplint.SeverityError:
		return "error"
	case mdpplint.SeverityWarning:
		return "warning"
	case mdpplint.SeverityInfo:
		return "info"
	case mdpplint.SeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}
