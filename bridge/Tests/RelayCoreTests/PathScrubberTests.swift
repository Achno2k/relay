import Testing
@testable import RelayCore

@Suite struct PathScrubberTests {
    let s = PathScrubber(cwd: "/Users/dev/shop-api")

    @Test(arguments: [
        ("/Users/dev/shop-api/src/auth.py", "src/auth.py"),
        ("cd /Users/dev/shop-api && ls", "cd . && ls"),
        ("/Users/dev/shop-api-old/x.py", "x.py"),
        ("/Users/dev/other/thing.txt", "thing.txt"),
        ("see ~/.claude/settings.json", "see settings.json"),
        ("/opt/homebrew/bin/", "bin"),
        ("https://example.com/a/b/c", "https://example.com/a/b/c"),
        ("src/auth.py and and/or", "src/auth.py and and/or"),
        ("run /clear now", "run /clear now"),
        (#"{"file_path":"/Users/dev/shop-api/a.swift"}"#, #"{"file_path":"a.swift"}"#),
        ("`/tmp/x/y.log`", "`y.log`"),
    ])
    func scrubs(input: String, expected: String) {
        #expect(s.scrub(input) == expected)
    }

    @Test func noCwd() {
        #expect(PathScrubber(cwd: nil).scrub("/a/b/c.txt") == "c.txt")
        #expect(PathScrubber(cwd: "/").scrub("/a/b") == "b")
    }

    @Test func cwdName() {
        #expect(CwdName.of("/Users/dev/shop-api") == "shop-api")
        #expect(CwdName.of("/Users/dev/shop-api/") == "shop-api")
        #expect(CwdName.of(nil) == "")
    }
}
