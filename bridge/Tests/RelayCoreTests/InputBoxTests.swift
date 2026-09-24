import Testing
@testable import RelayCore

@Suite struct InputBoxTests {
    @Test func readsRestoredPrompt() throws {
        #expect(InputBox.content(try Fixture.text("input-restored.txt")) == "Write a 400 word essay about rivers.\nSecond line of the prompt.")
        #expect(InputBox.content(try Fixture.text("input-empty.txt")) == "")
    }

    @Test func noBoxOnDialogs() throws {
        #expect(InputBox.content(try Fixture.text("approval-trust.txt")) == nil)
        #expect(InputBox.content("plain output") == nil)
    }

    @Test func clearKeysCoverEveryLineAndBreak() {
        #expect(InputBox.clearKeys(for: "one line") == ["ctrl+u", "ctrl+u"])
        #expect(InputBox.clearKeys(for: "a\nb\nc").count == 6)
    }
}
