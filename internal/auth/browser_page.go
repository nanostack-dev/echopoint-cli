package auth

import (
	"embed"
	"encoding/base64"
	"html/template"
	"strings"
)

//go:embed pages/callback.html pages/fonts/*.woff2 pages/fonts/*-OFL.txt
var callbackAssets embed.FS

var callbackTemplate = template.Must(template.ParseFS(callbackAssets, "pages/callback.html"))

// All assets are in the initial HTML response: BrowserLogin shuts down its
// local server as soon as it receives the token, before later asset requests.
var callbackFonts = struct {
	Body, Heading, Mono template.URL
}{
	Body:    embeddedFont("plus-jakarta-sans-latin.woff2"),
	Heading: embeddedFont("outfit-latin.woff2"),
	Mono:    embeddedFont("geist-mono-latin.woff2"),
}

var callbackFontLicenses = template.HTML("<!-- Bundled font licenses\n" +
	string(callbackAsset("pages/fonts/Plus-Jakarta-Sans-OFL.txt")) + "\n" +
	string(callbackAsset("pages/fonts/Outfit-OFL.txt")) + "\n" +
	string(callbackAsset("pages/fonts/Geist-Mono-OFL.txt")) + "\n-->")

type callbackPageData struct {
	Success                         bool
	Message                         string
	BodyFont, HeadingFont, MonoFont template.URL
	FontLicenses                    template.HTML
}

func successPage() string { return renderCallbackPage(true, "") }

func errorPage(message string) string { return renderCallbackPage(false, message) }

func renderCallbackPage(success bool, message string) string {
	var page strings.Builder
	data := callbackPageData{
		Success:      success,
		Message:      message,
		BodyFont:     callbackFonts.Body,
		HeadingFont:  callbackFonts.Heading,
		MonoFont:     callbackFonts.Mono,
		FontLicenses: callbackFontLicenses,
	}
	// This fixed template writes only strings to an in-memory builder. An
	// execution error indicates a programming error in the bundled document.
	if err := callbackTemplate.Execute(&page, data); err != nil {
		panic(err)
	}
	return page.String()
}

func embeddedFont(name string) template.URL {
	// The URL contains only our bundled font bytes, never callback/user input.
	return template.URL(
		"data:font/woff2;base64," + base64.StdEncoding.EncodeToString(callbackAsset("pages/fonts/"+name)),
	)
}

func callbackAsset(name string) []byte {
	content, err := callbackAssets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return content
}
