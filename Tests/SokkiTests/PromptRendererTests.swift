import Testing
@testable import Sokki

struct PromptRendererTests {
    // MARK: - parse

    @Test func parse_zeroPlaceholders() throws {
        let t = try PromptTemplate.parse("Tell me a joke.")
        #expect(t.placeholders.count == 0)
        #expect(!t.needsSelection)
        #expect(!t.needsClipboard)
    }

    @Test func parse_emptyString() throws {
        let t = try PromptTemplate.parse("")
        #expect(t.placeholders.count == 0)
    }

    @Test func parse_simple_selection() throws {
        let t = try PromptTemplate.parse("Translate: {{selection}}")
        #expect(t.placeholders.count == 1)
        #expect(t.placeholders[0].chain == [.selection])
        #expect(t.needsSelection)
        #expect(!t.needsClipboard)
    }

    @Test func parse_aliases() throws {
        let t = try PromptTemplate.parse("{{s}} and {{c}}")
        #expect(t.placeholders[0].chain == [.selection])
        #expect(t.placeholders[1].chain == [.clipboard])
    }

    @Test func parse_fallback_chain() throws {
        let t = try PromptTemplate.parse("{{s|c}}")
        #expect(t.placeholders[0].chain == [.selection, .clipboard])
    }

    @Test func parse_whitespace_inside_braces() throws {
        let a = try PromptTemplate.parse("{{ selection }}")
        let b = try PromptTemplate.parse("{{ s | c }}")
        let c = try PromptTemplate.parse("{{selection|clipboard}}")
        #expect(a.placeholders[0].chain == [.selection])
        #expect(b.placeholders[0].chain == [.selection, .clipboard])
        #expect(c.placeholders[0].chain == [.selection, .clipboard])
    }

    @Test func parse_multiple_placeholders() throws {
        let t = try PromptTemplate.parse("Before {{s}} middle {{c}} after")
        #expect(t.placeholders.count == 2)
        #expect(t.needsSelection)
        #expect(t.needsClipboard)
    }

    // MARK: - parse errors

    @Test func parse_error_unknownSource() {
        #expect(throws: PromptParseError.self) {
            try PromptTemplate.parse("{{foo}}")
        }
    }

    @Test func parse_error_empty() {
        #expect(throws: PromptParseError.self) {
            try PromptTemplate.parse("{{}}")
        }
    }

    @Test func parse_error_emptySourceLeading() {
        #expect(throws: PromptParseError.self) {
            try PromptTemplate.parse("{{|s}}")
        }
    }

    @Test func parse_error_emptySourceTrailing() {
        #expect(throws: PromptParseError.self) {
            try PromptTemplate.parse("{{s|}}")
        }
    }

    @Test func parse_error_emptySourceMiddle() {
        #expect(throws: PromptParseError.self) {
            try PromptTemplate.parse("{{s||c}}")
        }
    }

    @Test func parse_error_unclosed() {
        #expect(throws: PromptParseError.self) {
            try PromptTemplate.parse("{{selection")
        }
    }

    // MARK: - render

    @Test func render_zeroPlaceholders_returnsRaw() throws {
        let t = try PromptTemplate.parse("No placeholders here.")
        #expect(t.render(selection: nil, clipboard: nil) == "No placeholders here.")
    }

    @Test func render_selection_fills() throws {
        let t = try PromptTemplate.parse("Translate: {{selection}}")
        #expect(t.render(selection: "hello", clipboard: nil) == "Translate: hello")
    }

    @Test func render_fallback_usesSecond() throws {
        let t = try PromptTemplate.parse("{{s|c}}")
        #expect(t.render(selection: "", clipboard: "fallback") == "fallback")
        #expect(t.render(selection: nil, clipboard: "fallback") == "fallback")
    }

    @Test func render_fallback_firstWins() throws {
        let t = try PromptTemplate.parse("{{s|c}}")
        #expect(t.render(selection: "first", clipboard: "second") == "first")
    }

    @Test func render_abortsWhenChainEmpty() throws {
        let t = try PromptTemplate.parse("{{s|c}}")
        #expect(t.render(selection: "", clipboard: "") == nil)
        #expect(t.render(selection: nil, clipboard: nil) == nil)
    }

    @Test func render_multiplePlaceholders() throws {
        let t = try PromptTemplate.parse("A={{s}} B={{c}}")
        #expect(t.render(selection: "one", clipboard: "two") == "A=one B=two")
    }

    @Test func render_isNonRecursive() throws {
        // If the rendered value itself contains {{clipboard}}, it must NOT
        // be re-expanded on a second pass.
        let t = try PromptTemplate.parse("{{s}}")
        #expect(t.render(selection: "{{clipboard}}", clipboard: "recursive") == "{{clipboard}}")
    }

    @Test func render_preservesSurroundingText() throws {
        let t = try PromptTemplate.parse("Before {{s}} After")
        #expect(t.render(selection: "MID", clipboard: nil) == "Before MID After")
    }

    // MARK: - firstUnresolvedChain

    @Test func firstUnresolvedChain_echoesRawInner() throws {
        let t = try PromptTemplate.parse("A {{s|c}} B")
        #expect(t.firstUnresolvedChain(selection: "", clipboard: "") == "{{s|c}}")
    }

    @Test func firstUnresolvedChain_nilWhenAllResolved() throws {
        let t = try PromptTemplate.parse("{{s}}")
        #expect(t.firstUnresolvedChain(selection: "x", clipboard: nil) == nil)
    }
}
