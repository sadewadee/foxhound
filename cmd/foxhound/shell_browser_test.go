package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Tests for the browser shell command parser and helper functions.
// These tests do NOT require playwright — they validate the parsing logic,
// help text, and command dispatch table that live in the non-playwright path.
// ---------------------------------------------------------------------------

func TestBrowserShellHelpText_ContainsNavigationCommands(t *testing.T) {
	text := browserShellHelpText()
	navCmds := []string{"goto", "back", "forward", "reload", "url", "title"}
	for _, cmd := range navCmds {
		if !strings.Contains(text, cmd) {
			t.Errorf("browser shell help text missing navigation command %q\nFull help:\n%s", cmd, text)
		}
	}
}

func TestBrowserShellHelpText_ContainsInteractionCommands(t *testing.T) {
	text := browserShellHelpText()
	interactionCmds := []string{"click", "type", "select", "scroll", "wait"}
	for _, cmd := range interactionCmds {
		if !strings.Contains(text, cmd) {
			t.Errorf("browser shell help text missing interaction command %q\nFull help:\n%s", cmd, text)
		}
	}
}

func TestBrowserShellHelpText_ContainsExtractionCommands(t *testing.T) {
	text := browserShellHelpText()
	extractCmds := []string{"text", "attr", "html", "links", "count"}
	for _, cmd := range extractCmds {
		if !strings.Contains(text, cmd) {
			t.Errorf("browser shell help text missing extraction command %q\nFull help:\n%s", cmd, text)
		}
	}
}

func TestBrowserShellHelpText_ContainsUtilityCommands(t *testing.T) {
	text := browserShellHelpText()
	utilityCmds := []string{"screenshot", "eval", "cookies", "status"}
	for _, cmd := range utilityCmds {
		if !strings.Contains(text, cmd) {
			t.Errorf("browser shell help text missing utility command %q\nFull help:\n%s", cmd, text)
		}
	}
}

func TestBrowserShellHelpText_ContainsShellCommands(t *testing.T) {
	text := browserShellHelpText()
	shellCmds := []string{"help", "clear", "exit"}
	for _, cmd := range shellCmds {
		if !strings.Contains(text, cmd) {
			t.Errorf("browser shell help text missing shell command %q\nFull help:\n%s", cmd, text)
		}
	}
}

func TestParseBrowserCommand_Goto(t *testing.T) {
	cmd, args := parseBrowserCommand("goto https://example.com")
	if cmd != "goto" {
		t.Errorf("expected cmd=goto, got %q", cmd)
	}
	if len(args) != 1 || args[0] != "https://example.com" {
		t.Errorf("expected args=[https://example.com], got %v", args)
	}
}

func TestParseBrowserCommand_Empty(t *testing.T) {
	cmd, args := parseBrowserCommand("")
	if cmd != "" {
		t.Errorf("expected empty cmd, got %q", cmd)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}

func TestParseBrowserCommand_WhitespaceOnly(t *testing.T) {
	cmd, args := parseBrowserCommand("   ")
	if cmd != "" {
		t.Errorf("expected empty cmd for whitespace-only input, got %q", cmd)
	}
	if len(args) != 0 {
		t.Errorf("expected no args for whitespace-only input, got %v", args)
	}
}

func TestParseBrowserCommand_Click(t *testing.T) {
	cmd, args := parseBrowserCommand("click .submit-btn")
	if cmd != "click" {
		t.Errorf("expected cmd=click, got %q", cmd)
	}
	if len(args) != 1 || args[0] != ".submit-btn" {
		t.Errorf("expected args=[.submit-btn], got %v", args)
	}
}

func TestParseBrowserCommand_TypeWithText(t *testing.T) {
	// type takes selector then text — text may contain spaces; we split on first
	// two fields and treat the rest as text.
	cmd, args := parseBrowserCommand("type #search hello world")
	if cmd != "type" {
		t.Errorf("expected cmd=type, got %q", cmd)
	}
	// args[0] = selector, args[1...] = remaining words
	if len(args) < 2 {
		t.Fatalf("expected at least 2 args for type command, got %d: %v", len(args), args)
	}
	if args[0] != "#search" {
		t.Errorf("expected selector=#search, got %q", args[0])
	}
}

func TestParseBrowserCommand_Scroll_Down(t *testing.T) {
	cmd, args := parseBrowserCommand("scroll down 300")
	if cmd != "scroll" {
		t.Errorf("expected cmd=scroll, got %q", cmd)
	}
	if len(args) < 2 || args[0] != "down" || args[1] != "300" {
		t.Errorf("expected args=[down 300], got %v", args)
	}
}

func TestParseBrowserCommand_Scroll_Bottom(t *testing.T) {
	cmd, args := parseBrowserCommand("scroll bottom")
	if cmd != "scroll" {
		t.Errorf("expected cmd=scroll, got %q", cmd)
	}
	if len(args) != 1 || args[0] != "bottom" {
		t.Errorf("expected args=[bottom], got %v", args)
	}
}

func TestParseBrowserCommand_Screenshot_Default(t *testing.T) {
	cmd, args := parseBrowserCommand("screenshot")
	if cmd != "screenshot" {
		t.Errorf("expected cmd=screenshot, got %q", cmd)
	}
	if len(args) != 0 {
		t.Errorf("expected no args for bare screenshot, got %v", args)
	}
}

func TestParseBrowserCommand_Screenshot_WithFilename(t *testing.T) {
	cmd, args := parseBrowserCommand("screenshot my-page.png")
	if cmd != "screenshot" {
		t.Errorf("expected cmd=screenshot, got %q", cmd)
	}
	if len(args) != 1 || args[0] != "my-page.png" {
		t.Errorf("expected args=[my-page.png], got %v", args)
	}
}

func TestParseBrowserCommand_Eval(t *testing.T) {
	cmd, args := parseBrowserCommand("eval document.title")
	if cmd != "eval" {
		t.Errorf("expected cmd=eval, got %q", cmd)
	}
	if len(args) < 1 {
		t.Fatalf("expected at least 1 arg for eval, got 0")
	}
	if args[0] != "document.title" {
		t.Errorf("expected args[0]=document.title, got %q", args[0])
	}
}

func TestParseBrowserCommand_Wait_WithSelector(t *testing.T) {
	cmd, args := parseBrowserCommand("wait .content 10")
	if cmd != "wait" {
		t.Errorf("expected cmd=wait, got %q", cmd)
	}
	if len(args) < 2 || args[0] != ".content" || args[1] != "10" {
		t.Errorf("expected args=[.content 10], got %v", args)
	}
}

func TestDefaultScreenshotFilename_HasPNGExtension(t *testing.T) {
	name := defaultScreenshotFilename()
	if !strings.HasSuffix(name, ".png") {
		t.Errorf("default screenshot filename should end with .png, got %q", name)
	}
}

func TestDefaultScreenshotFilename_ContainsFoxhound(t *testing.T) {
	name := defaultScreenshotFilename()
	if !strings.HasPrefix(name, "foxhound-") {
		t.Errorf("default screenshot filename should start with foxhound-, got %q", name)
	}
}

func TestFormatScrollPixels_ValidInt(t *testing.T) {
	px, ok := parseScrollPixels("500")
	if !ok {
		t.Error("expected ok=true for valid integer")
	}
	if px != 500 {
		t.Errorf("expected 500, got %d", px)
	}
}

func TestFormatScrollPixels_InvalidString(t *testing.T) {
	_, ok := parseScrollPixels("notanumber")
	if ok {
		t.Error("expected ok=false for non-numeric string")
	}
}

func TestFormatScrollPixels_Empty(t *testing.T) {
	_, ok := parseScrollPixels("")
	if ok {
		t.Error("expected ok=false for empty string")
	}
}

func TestFormatScrollPixels_Zero(t *testing.T) {
	px, ok := parseScrollPixels("0")
	if !ok {
		t.Error("expected ok=true for 0")
	}
	if px != 0 {
		t.Errorf("expected 0, got %d", px)
	}
}

func TestJoinArgs_EmptySlice(t *testing.T) {
	result := joinArgs([]string{})
	if result != "" {
		t.Errorf("expected empty string for empty slice, got %q", result)
	}
}

func TestJoinArgs_SingleElement(t *testing.T) {
	result := joinArgs([]string{"hello"})
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestJoinArgs_MultipleElements(t *testing.T) {
	result := joinArgs([]string{"hello", "world", "foo"})
	if result != "hello world foo" {
		t.Errorf("expected 'hello world foo', got %q", result)
	}
}

func TestBrowserShellHelpText_IsNotEmpty(t *testing.T) {
	text := browserShellHelpText()
	if strings.TrimSpace(text) == "" {
		t.Error("browser shell help text should not be empty")
	}
}
