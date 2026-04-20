package mdpp

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// RenderPDF renders a document to PDF using headless Chrome.
func RenderPDF(doc *Document, opts PDFOptions) ([]byte, error) {
	rendered, err := Render(doc, opts.RenderOptions)
	if err != nil {
		return nil, fmt.Errorf("pdf: render: %w", err)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	settle := opts.SettleDelay
	if settle == 0 {
		settle = 250 * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var browserCancel context.CancelFunc
	if opts.BrowserURL != "" {
		ctx, browserCancel = chromedp.NewRemoteAllocator(ctx, opts.BrowserURL)
	} else {
		allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", "new"),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("use-gl", "swiftshader"),
		)
		ctx, browserCancel = chromedp.NewExecAllocator(ctx, allocOpts...)
	}
	defer browserCancel()

	ctx, tabCancel := chromedp.NewContext(ctx)
	defer tabCancel()

	htmlDoc := pdfHTMLShell(string(rendered), opts.UserCSS)
	url := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(htmlDoc))
	var out []byte
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(settle),
		chromedp.ActionFunc(func(ctx context.Context) error {
			printOpts := page.PrintToPDF().
				WithPrintBackground(opts.Background || !explicitFalseBackground(opts)).
				WithDisplayHeaderFooter(opts.HeaderFooter.HeaderHTML != "" || opts.HeaderFooter.FooterHTML != "")
			width, height := paperDimensions(opts)
			printOpts = printOpts.WithPaperWidth(width).WithPaperHeight(height)
			margins := pdfMargins(opts.MarginInches)
			printOpts = printOpts.WithMarginTop(margins.Top).
				WithMarginRight(margins.Right).
				WithMarginBottom(margins.Bottom).
				WithMarginLeft(margins.Left)
			if opts.HeaderFooter.HeaderHTML != "" {
				printOpts = printOpts.WithHeaderTemplate(opts.HeaderFooter.HeaderHTML)
			}
			if opts.HeaderFooter.FooterHTML != "" {
				printOpts = printOpts.WithFooterTemplate(opts.HeaderFooter.FooterHTML)
			}
			buf, _, err := printOpts.Do(ctx)
			if err != nil {
				return err
			}
			out = buf
			return nil
		}),
	); err != nil {
		return nil, fmt.Errorf("pdf: print: %w", err)
	}
	return out, nil
}

func pdfHTMLShell(body string, userCSS string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><style>")
	b.WriteString(`body{font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;line-height:1.5;color:#111;margin:0;}pre,code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;}pre{white-space:pre-wrap;background:#f6f8fa;padding:12px;border-radius:6px;}table{border-collapse:collapse;width:100%;}th,td{border:1px solid #d0d7de;padding:4px 8px;}blockquote{border-left:4px solid #d0d7de;margin-left:0;padding-left:12px;color:#57606a;}.admonition,.mdpp-container{border:1px solid #d0d7de;border-left-width:4px;padding:8px 12px;margin:1em 0;border-radius:6px;}.admonition-title,.mdpp-container-title{font-weight:700;margin-top:0;}.mdpp-toc{border:1px solid #d0d7de;padding:8px 12px;margin:1em 0;border-radius:6px;}.mdpp-embed{border:1px solid #d0d7de;padding:12px;margin:1em 0;border-radius:6px;}@page{margin:0.5in;}`)
	b.WriteString(userCSS)
	b.WriteString("</style></head><body>")
	b.WriteString(body)
	b.WriteString("</body></html>")
	return b.String()
}

func paperDimensions(opts PDFOptions) (float64, float64) {
	switch opts.PaperSize {
	case PaperA4:
		return 8.27, 11.69
	case PaperLegal:
		return 8.5, 14
	case PaperCustom:
		if opts.PaperWidthInches > 0 && opts.PaperHeightInches > 0 {
			return opts.PaperWidthInches, opts.PaperHeightInches
		}
	}
	return 8.5, 11
}

func pdfMargins(m Margins) Margins {
	if m.Top == 0 && m.Right == 0 && m.Bottom == 0 && m.Left == 0 {
		return Margins{Top: 0.5, Right: 0.5, Bottom: 0.5, Left: 0.5}
	}
	return m
}

func explicitFalseBackground(opts PDFOptions) bool {
	return !opts.Background
}

func escapePDFTemplate(s string) string {
	return html.EscapeString(s)
}
