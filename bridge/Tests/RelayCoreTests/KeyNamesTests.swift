import Testing
@testable import RelayCore

@Suite struct KeyNamesTests {
    @Test func acceptsKnownShapes() {
        for k in ["esc", "enter", "up", "down", "tab", "space", "f1", "f24", "1", "9", "12", "a", "Z", "?", "ctrl+u", "shift+tab", "alt+a"] {
            #expect(KeyNames.isValid(k), "expected \(k) to be valid")
        }
    }

    @Test func rejectsGarbage() {
        for k in ["", "esc; rm -rf /", "ctrl+", "+u", "ctrl+u+u", String(repeating: "a", count: 40), "\n", "up\ndown", "ctrl+esc+u", "123"] {
            #expect(!KeyNames.isValid(k), "expected \(k) to be invalid")
        }
    }

    @Test func fuzzNeverCrashes() {
        var rng = SystemRandomNumberGenerator()
        let pool: [Character] = Array("abcdefgxyz012+-esc\n\t🎉\u{0}\u{7F}")
        for _ in 0..<2000 {
            let len = Int.random(in: 0...12, using: &rng)
            let s = String((0..<len).map { _ in pool.randomElement(using: &rng)! })
            _ = KeyNames.isValid(s)  // must not crash or hang
        }
    }

    @Test func firstInvalidFindsTheBadOne() {
        #expect(KeyNames.firstInvalid(["esc", "enter"]) == nil)
        #expect(KeyNames.firstInvalid(["esc", "bogus!!"]) == "bogus!!")
    }
}
