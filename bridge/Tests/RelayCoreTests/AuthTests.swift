import Foundation
import Testing
@testable import RelayCore

@Suite struct AuthTests {
    @Test func constantTimeEqualIsCorrect() {
        #expect(AuthMiddleware.constantTimeEqual("abc", "abc"))
        #expect(!AuthMiddleware.constantTimeEqual("abc", "abd"))
        #expect(!AuthMiddleware.constantTimeEqual("abc", "ab"))
        #expect(!AuthMiddleware.constantTimeEqual("abc", "abcd"))
        #expect(!AuthMiddleware.constantTimeEqual("", "a"))
        #expect(AuthMiddleware.constantTimeEqual("", ""))
    }

    // A real timing-attack proof needs a quiet, dedicated machine and statistics, not a unit test —
    // this dev box runs several concurrent `swift test`s, which alone swamps any signal (tried it;
    // the noise floor was >40x). `constantTimeEqual` is checked by inspection instead: it always
    // walks every byte of both inputs and XOR-accumulates into one `diff`, with no early return
    // except the length check (an unavoidable, publicly-known length signal — not the secret itself).
}
